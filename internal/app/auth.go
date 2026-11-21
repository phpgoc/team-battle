package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// 服务端会话不设到期时间；持久Cookie在使用时续期，浏览器清理Cookie后需重新登录。
const sessionCookieMaxAge = 10 * 365 * 24 * 60 * 60

func setSessionCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("tb_session", token, maxAge, "/", "", secureSessionRequest(c.Request), true)
}

func (a *App) login(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !a.logins.allowAttempt(loginClientIP(c.Request), time.Now()) {
		c.Header("Retry-After", "60")
		fail(c, http.StatusTooManyRequests, "登录请求过于频繁")
		return
	}
	const maxLoginBody = 8 * 1024
	if c.Request.ContentLength > maxLoginBody {
		fail(c, http.StatusRequestEntityTooLarge, "登录请求过大")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLoginBody)
	raw, err := io.ReadAll(c.Request.Body)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(c, http.StatusRequestEntityTooLarge, "登录请求过大")
		return
	}
	var req loginRequest
	if err != nil || json.Unmarshal(raw, &req) != nil {
		fail(c, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if !a.logins.start() {
		c.Header("Retry-After", "1")
		fail(c, http.StatusTooManyRequests, "同时登录的请求过多")
		return
	}
	defer a.logins.finish()
	ctx := c.Request.Context()
	username := strings.TrimSpace(req.Username)
	var userID int64
	var hash string
	err = a.db.QueryRowContext(ctx, `SELECT id,password_hash FROM users WHERE username=? AND status='active' AND is_deleted=0`, username).Scan(&userID, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusInternalServerError, "读取账号失败")
		return
	}
	if errors.Is(err, sql.ErrNoRows) || hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		if a.operationLogger != nil {
			a.operationLogger.logLoginFailure(username)
		}
		fail(c, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	// 登录返回和账号设置相同的缓存资料，包含本人的修改时间。
	user, err := a.userByID(userID)
	if err != nil {
		fail(c, 500, "读取账号失败")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建会话失败")
		return
	}
	// 校验期间账号可能变更，仅凭原始凭据仍有效时才替换会话。
	result, err := a.db.ExecContext(withoutSQLLog(ctx), `UPDATE users SET session_token=? WHERE id=? AND username=? AND password_hash=? AND status='active' AND is_deleted=0`, token, userID, username, hash)
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存登录会话失败")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存登录会话失败")
		return
	}
	if changed != 1 {
		if a.operationLogger != nil {
			a.operationLogger.logLoginFailure(username)
		}
		fail(c, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	setSessionCookie(c, token, sessionCookieMaxAge)
	if a.operationLogger != nil {
		a.operationLogger.logLoginSuccess(user)
	}
	ok(c, gin.H{"user": user})
}

func (a *App) logout(c *gin.Context) {
	actor := a.operationLogActor(c)
	if token, err := c.Cookie("tb_session"); err == nil {
		// 只删除提交的会话，旧设备退出不能影响新设备的有效登录。
		if _, err := a.db.Exec(`UPDATE users SET session_token=NULL WHERE session_token=?`, token); err != nil {
			fail(c, http.StatusInternalServerError, "退出登录失败")
			return
		}
	}
	setSessionCookie(c, "", -1)
	if a.operationLogger != nil {
		a.operationLogger.logLogout(actor)
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) me(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	blocked := false
	if user.Role != roleSuperAdmin {
		blocked, err = hasOpenParticipation(a.db, user.ID)
		if err != nil {
			fail(c, 500, "检查参赛状态失败")
			return
		}
	}
	ok(c, gin.H{"user": user, "profile_edit_blocked": blocked})
}

func (a *App) updateProfile(c *gin.Context) {
	a.editAccount(c, false)
}

func (a *App) uploadAvatar(c *gin.Context) {
	a.updateAvatar(c, false)
}

func (a *App) avatar(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "账号编号无效")
		return
	}
	var data []byte
	var mimeType string
	if err := a.db.QueryRow(`SELECT avatar_data,avatar_mime FROM users WHERE id=?`, userID).Scan(&data, &mimeType); err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, mimeType, data)
}

func (a *App) listUsers(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil || user.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以管理账号")
		return
	}
	directory, err := a.userDirectory()
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	type accountListUser struct {
		User
		HasPassword bool `json:"has_password"`
		CanLogin    bool `json:"can_login"`
	}
	users := []accountListUser{}
	passwordRows, err := a.db.Query(`SELECT id,length(password_hash)>0 FROM users WHERE is_deleted=0`)
	if err != nil {
		fail(c, 500, "读取账号登录状态失败")
		return
	}
	hasPassword := map[int64]bool{}
	for passwordRows.Next() {
		var id int64
		var enabled bool
		if err := passwordRows.Scan(&id, &enabled); err != nil {
			passwordRows.Close()
			fail(c, 500, "读取账号登录状态失败")
			return
		}
		hasPassword[id] = enabled
	}
	if err := passwordRows.Err(); err != nil {
		passwordRows.Close()
		fail(c, 500, "读取账号登录状态失败")
		return
	}
	passwordRows.Close()
	rows, err := a.db.Query(`SELECT player_a_id FROM matches WHERE status='finished' UNION SELECT player_b_id FROM matches WHERE status='finished'`)
	if err != nil {
		fail(c, 500, "读取账号战绩失败")
		return
	}
	played := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			fail(c, 500, "读取账号战绩失败")
			return
		}
		played[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(c, 500, "读取账号战绩失败")
		return
	}
	for _, u := range directory {
		if u.IsDeleted {
			continue
		}
		u.CanDelete = u.ID != user.ID && !played[u.ID]
		canLogin := hasPassword[u.ID] && u.Status == "active"
		users = append(users, accountListUser{User: u, HasPassword: hasPassword[u.ID], CanLogin: canLogin})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	ok(c, gin.H{"users": users})
}

func (a *App) resetUserPassword(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil || actor.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以重置密码")
		return
	}
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "账号编号无效")
		return
	}
	var role, targetName string
	if err := a.db.QueryRow(`SELECT role,display_name FROM users WHERE id=? AND is_deleted=0`, userID).Scan(&role, &targetName); err != nil {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	var req passwordResetRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Password) < 6 {
		fail(c, http.StatusBadRequest, "密码至少需要6位")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if _, err := a.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(hash), userID); err != nil {
		fail(c, http.StatusInternalServerError, "保存密码失败")
		return
	}
	a.logAccountOperation(actor, userID, targetName, "重置账号密码", nil)
	ok(c, gin.H{"ok": true})
}

func (a *App) createUser(c *gin.Context) {
	a.createAccount(c)
}

func (a *App) currentUser(c *gin.Context) (User, error) {
	token, err := c.Cookie("tb_session")
	if err != nil || token == "" {
		return User{}, errors.New("unauthorized")
	}
	// 每次鉴权都核对数据库中的有效会话，避免进程内缓存让已被顶掉的设备继续操作。
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE session_token=? AND is_deleted=0 AND status='active'`, token).Scan(&userID); err != nil {
		c.Set("session_invalid", true)
		return User{}, errors.New("unauthorized")
	}
	user, err := a.userByID(userID)
	if err != nil || user.Status != "active" || user.IsDeleted {
		c.Set("session_invalid", true)
		return User{}, errors.New("unauthorized")
	}
	setSessionCookie(c, token, sessionCookieMaxAge)
	return user, nil
}

func (a *App) userByID(id int64) (User, error) { return a.cachedUser(id) }

func (a *App) managerTournament(c *gin.Context) (User, int64, bool) {
	actor, err := a.currentUser(c)
	if err != nil || (!isNonSuperAdmin(actor.Role) && actor.Role != roleSuperAdmin) {
		fail(c, http.StatusForbidden, "需要赛事管理员权限")
		return User{}, 0, false
	}
	tournamentID, err := parseID(c.Param("id"))
	if err != nil || !a.canManageTournament(actor.ID, actor.Role, tournamentID) {
		fail(c, http.StatusForbidden, "无权操作该赛事")
		return User{}, 0, false
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM tournaments WHERE id=?`, tournamentID).Scan(&status); err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return User{}, 0, false
	}
	if status == statusCompleted || status == statusCancelled {
		fail(c, http.StatusBadRequest, "已结束赛事不能编辑")
		return User{}, 0, false
	}
	return actor, tournamentID, true
}

func (a *App) canManageTournament(userID int64, role string, tournamentID int64) bool {
	if role == roleSuperAdmin {
		return true
	}
	var exists int
	return isNonSuperAdmin(role) && a.db.QueryRow(`SELECT COUNT(*) FROM tournaments WHERE id=? AND manager_id=?`, tournamentID, userID).Scan(&exists) == nil && exists == 1
}

func (a *App) canViewTournament(userID int64, role string, tournamentID int64) bool {
	if role == roleSuperAdmin {
		return a.tournamentExists(tournamentID)
	}
	if isNonSuperAdmin(role) {
		if a.canManageTournament(userID, role, tournamentID) {
			return true
		}
		var exists int
		return a.db.QueryRow(`SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=?`, tournamentID, userID).Scan(&exists) == nil && exists == 1
	}
	var exists int
	return a.db.QueryRow(`SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=?`, tournamentID, userID).Scan(&exists) == nil && exists == 1
}

func (a *App) tournamentExists(tournamentID int64) bool {
	var count int
	return a.db.QueryRow(`SELECT COUNT(*) FROM tournaments WHERE id=?`, tournamentID).Scan(&count) == nil && count == 1
}

func (a *App) isTournamentPlayer(tournamentID, teamID, userID int64) bool {
	var count int
	return a.db.QueryRow(`SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=? AND user_id=?`, tournamentID, teamID, userID).Scan(&count) == nil && count == 1
}

func (a *App) logAction(tournamentID, actorID int64, action string, payload any) {
	data := "{}"
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err == nil {
			data = string(encoded)
		}
	}
	_, _ = a.db.Exec(`INSERT INTO tournament_actions(tournament_id,actor_id,action,payload) VALUES(?,?,?,?)`, tournamentID, actorID, action, data)
	a.logTournamentOperation(tournamentOperation{TournamentID: tournamentID, ActorID: actorID, Action: action, Payload: data})
}
