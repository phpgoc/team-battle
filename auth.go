package main

import (
	"database/sql"
	"encoding/json"
	"errors"

	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// 服务端会话不设到期时间；持久Cookie在使用时续期，浏览器清理Cookie后需重新登录。
const sessionCookieMaxAge = 10 * 365 * 24 * 60 * 60

func setSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("tb_session", token, sessionCookieMaxAge, "/", "", false, true)
}

func (a *App) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式不正确")
		return
	}
	var user User
	var hash string
	var categoryID sql.NullInt64
	err := a.db.QueryRow(`SELECT id, username, display_name, role, status, created_at, password_hash, category_id FROM users WHERE username = ? AND is_deleted=0`, strings.TrimSpace(req.Username)).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Status, &user.CreatedAt, &hash, &categoryID)
	if categoryID.Valid {
		user.CategoryID = categoryID.Int64
	}
	if err != nil || user.Status != "active" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(c, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	// 登录返回和账号设置相同的缓存资料，包含本人的修改时间。
	user, err = a.userByID(user.ID)
	if err != nil {
		fail(c, 500, "读取账号失败")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建会话失败")
		return
	}
	// 唯一账号索引保证一次原子写入即替换旧会话，保存失败时不会签发新Cookie。
	if _, err := a.db.Exec(`INSERT INTO sessions(token,user_id) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET token=excluded.token,created_at=CURRENT_TIMESTAMP`, token, user.ID); err != nil {
		fail(c, http.StatusInternalServerError, "保存登录会话失败")
		return
	}
	setSessionCookie(c, token)
	a.populateAvatarURL(&user)
	ok(c, gin.H{"user": user})
}

func (a *App) logout(c *gin.Context) {
	if token, err := c.Cookie("tb_session"); err == nil {
		// 只删除提交的会话，旧设备退出不能影响新设备的有效登录。
		if _, err := a.db.Exec(`DELETE FROM sessions WHERE token = ?`, token); err != nil {
			fail(c, http.StatusInternalServerError, "退出登录失败")
			return
		}
	}
	c.SetCookie("tb_session", "", -1, "/", "", false, true)
	ok(c, gin.H{"ok": true})
}

func (a *App) me(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	ok(c, gin.H{"user": user})
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
	users := []User{}
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
		users = append(users, u)
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
	var role string
	if err := a.db.QueryRow(`SELECT role FROM users WHERE id=? AND is_deleted=0`, userID).Scan(&role); err != nil {
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
	if err := a.db.QueryRow(`SELECT user_id FROM sessions WHERE token=?`, token).Scan(&userID); err != nil {
		return User{}, errors.New("unauthorized")
	}
	user, err := a.userByID(userID)
	if err != nil || user.Status != "active" || user.IsDeleted {
		return User{}, errors.New("unauthorized")
	}
	setSessionCookie(c, token)
	return user, nil
}

func (a *App) userByID(id int64) (User, error) { return a.cachedUser(id) }
func (a *App) populateAvatarURL(u *User) {
	if cached, err := a.cachedUser(u.ID); err == nil {
		u.AvatarURL = cached.AvatarURL
	}
}

func (a *App) managerEvent(c *gin.Context) (User, int64, bool) {
	actor, err := a.currentUser(c)
	if err != nil || (!isNonSuperAdmin(actor.Role) && actor.Role != roleSuperAdmin) {
		fail(c, http.StatusForbidden, "需要赛事管理员权限")
		return User{}, 0, false
	}
	eventID, err := parseID(c.Param("id"))
	if err != nil || !a.canManageEvent(actor.ID, actor.Role, eventID) {
		fail(c, http.StatusForbidden, "无权操作该赛事")
		return User{}, 0, false
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM events WHERE id=?`, eventID).Scan(&status); err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return User{}, 0, false
	}
	if status == statusCompleted || status == statusCancelled {
		fail(c, http.StatusBadRequest, "已结束赛事不能编辑")
		return User{}, 0, false
	}
	return actor, eventID, true
}

func (a *App) canManageEvent(userID int64, role string, eventID int64) bool {
	if role == roleSuperAdmin {
		return true
	}
	var exists int
	return isNonSuperAdmin(role) && a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id=? AND manager_id=?`, eventID, userID).Scan(&exists) == nil && exists == 1
}

func (a *App) canViewEvent(userID int64, role string, eventID int64) bool {
	if role == roleSuperAdmin {
		return a.eventExists(eventID)
	}
	if isNonSuperAdmin(role) {
		if a.canManageEvent(userID, role, eventID) {
			return true
		}
		var exists int
		return a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=?`, eventID, userID).Scan(&exists) == nil && exists == 1
	}
	var exists int
	return a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=?`, eventID, userID).Scan(&exists) == nil && exists == 1
}

func (a *App) eventExists(eventID int64) bool {
	var count int
	return a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id=?`, eventID).Scan(&count) == nil && count == 1
}

func (a *App) isEventPlayer(eventID, teamID, userID int64) bool {
	var count int
	return a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND user_id=?`, eventID, teamID, userID).Scan(&count) == nil && count == 1
}

func (a *App) logAction(eventID, actorID int64, action string, payload any) {
	data := "{}"
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err == nil {
			data = string(encoded)
		}
	}
	_, _ = a.db.Exec(`INSERT INTO event_actions(event_id,actor_id,action,payload) VALUES(?,?,?,?)`, eventID, actorID, action, data)
}
