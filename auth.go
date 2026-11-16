package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func (a *App) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式不正确")
		return
	}
	var user User
	var hash string
	var categoryID sql.NullInt64
	err := a.db.QueryRow(`SELECT id, username, display_name, role, status, created_at, password_hash, category_id FROM users WHERE username = ?`, strings.TrimSpace(req.Username)).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Status, &user.CreatedAt, &hash, &categoryID)
	if categoryID.Valid {
		user.CategoryID = categoryID.Int64
	}
	if err != nil || user.Status != "active" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(c, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建会话失败")
		return
	}
	a.mu.Lock()
	a.sessions[token] = user.ID
	a.mu.Unlock()
	_, _ = a.db.Exec(`INSERT INTO sessions(token, user_id) VALUES(?,?)`, token, user.ID)
	c.SetCookie("tb_session", token, 60*60*24*7, "/", "", false, true)
	a.populateAvatarURL(&user)
	ok(c, gin.H{"user": user})
}

func (a *App) logout(c *gin.Context) {
	if token, err := c.Cookie("tb_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	}
	c.SetCookie("tb_session", "", -1, "/", "", false, true)
	ok(c, gin.H{"ok": true})
}

func (a *App) me(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	ok(c, gin.H{"user": user})
}

func (a *App) updateProfile(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if strings.TrimSpace(req.DisplayName) != "" {
		if _, err := a.db.Exec(`UPDATE users SET display_name=? WHERE id=?`, strings.TrimSpace(req.DisplayName), user.ID); err != nil {
			fail(c, http.StatusInternalServerError, "保存显示名失败")
			return
		}
	}
	if req.CategoryID != nil {
		if *req.CategoryID != 0 {
			var exists int
			if err := a.db.QueryRow(`SELECT COUNT(*) FROM categories WHERE id=?`, *req.CategoryID).Scan(&exists); err != nil || exists == 0 {
				fail(c, http.StatusBadRequest, "分类不存在")
				return
			}
		}
		if _, err := a.db.Exec(`UPDATE users SET category_id=? WHERE id=?`, nullableInt64(*req.CategoryID), user.ID); err != nil {
			fail(c, http.StatusInternalServerError, "保存分类失败")
			return
		}
	}
	if req.NewPassword != "" {
		if len(req.NewPassword) < 6 {
			fail(c, http.StatusBadRequest, "新密码至少需要6位")
			return
		}
		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			fail(c, http.StatusInternalServerError, "密码处理失败")
			return
		}
		if _, err := a.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(newHash), user.ID); err != nil {
			fail(c, http.StatusInternalServerError, "保存密码失败")
			return
		}
	}
	updated, err := a.userByID(user.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取账号失败")
		return
	}
	ok(c, gin.H{"user": updated})
}

func (a *App) uploadAvatar(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		fail(c, http.StatusBadRequest, "请选择头像文件")
		return
	}
	defer file.Close()
	if header.Size > 2*1024*1024 {
		fail(c, http.StatusBadRequest, "头像文件不能超过2MB")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil || len(data) == 0 || len(data) > 2*1024*1024 {
		fail(c, http.StatusBadRequest, "头像文件无效")
		return
	}
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		fail(c, http.StatusBadRequest, "头像只支持 JPG、PNG、GIF 或 WebP")
		return
	}
	if _, err := a.db.Exec(`UPDATE users SET avatar_data=?, avatar_mime=?, avatar_version=avatar_version+1 WHERE id=?`, data, mimeType, user.ID); err != nil {
		fail(c, http.StatusInternalServerError, "保存头像失败")
		return
	}
	updated, err := a.userByID(user.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取账号失败")
		return
	}
	ok(c, gin.H{"user": updated})
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
	rows, err := a.db.Query(`SELECT id, username, display_name, role, status, created_at, category_id FROM users ORDER BY role, id`)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var item User
		var categoryID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Role, &item.Status, &item.CreatedAt, &categoryID); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if categoryID.Valid {
			item.CategoryID = categoryID.Int64
		}
		a.populateAvatarURL(&item)
		users = append(users, item)
	}
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
	if err := a.db.QueryRow(`SELECT role FROM users WHERE id=?`, userID).Scan(&role); err != nil {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	if role == roleSuperAdmin {
		fail(c, http.StatusForbidden, "不能通过此功能修改超级管理员密码")
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
	actor, err := a.currentUser(c)
	if err != nil || actor.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以创建账号")
		return
	}
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写完整的账号信息")
		return
	}
	if req.Role != roleManager && req.Role != rolePlayer {
		fail(c, http.StatusBadRequest, "角色只能是赛事管理员或普通选手")
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		req.DisplayName = req.Username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "密码处理失败")
		return
	}
	result, err := a.db.Exec(`INSERT INTO users(username, display_name, password_hash, role) VALUES(?,?,?,?)`, strings.TrimSpace(req.Username), strings.TrimSpace(req.DisplayName), string(hash), req.Role)
	if err != nil {
		fail(c, http.StatusBadRequest, "账号名已存在或信息无效")
		return
	}
	id, _ := result.LastInsertId()
	ok(c, gin.H{"id": id})
}

func (a *App) currentUser(c *gin.Context) (User, error) {
	token, err := c.Cookie("tb_session")
	if err != nil || token == "" {
		return User{}, errors.New("unauthorized")
	}
	a.mu.RLock()
	userID, ok := a.sessions[token]
	a.mu.RUnlock()
	if !ok {
		if err := a.db.QueryRow(`SELECT user_id FROM sessions WHERE token=?`, token).Scan(&userID); err != nil {
			return User{}, errors.New("unauthorized")
		}
	}
	user, err := a.userByID(userID)
	if err != nil || user.Status != "active" {
		return User{}, errors.New("unauthorized")
	}
	return user, nil
}

func (a *App) userByID(userID int64) (User, error) {
	var user User
	var categoryID sql.NullInt64
	if err := a.db.QueryRow(`SELECT id,username,display_name,role,status,created_at,category_id FROM users WHERE id=?`, userID).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Status, &user.CreatedAt, &categoryID); err != nil {
		return User{}, err
	}
	if categoryID.Valid {
		user.CategoryID = categoryID.Int64
	}
	a.populateAvatarURL(&user)
	return user, nil
}

func (a *App) populateAvatarURL(user *User) {
	var exists int
	if err := a.db.QueryRow(`SELECT CASE WHEN avatar_data IS NULL OR length(avatar_data)=0 THEN 0 ELSE 1 END FROM users WHERE id=?`, user.ID).Scan(&exists); err == nil && exists == 1 {
		user.AvatarURL = "/api/users/" + strconv.FormatInt(user.ID, 10) + "/avatar"
	}
}

func (a *App) managerEvent(c *gin.Context) (User, int64, bool) {
	actor, err := a.currentUser(c)
	if err != nil || (actor.Role != roleSuperAdmin && actor.Role != roleManager) {
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
	return a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id=? AND manager_id=?`, eventID, userID).Scan(&exists) == nil && exists == 1
}

func (a *App) canViewEvent(userID int64, role string, eventID int64) bool {
	if role == roleSuperAdmin || role == roleManager {
		if role == roleSuperAdmin {
			return a.eventExists(eventID)
		}
		return a.canManageEvent(userID, role, eventID)
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
