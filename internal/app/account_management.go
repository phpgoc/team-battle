package app

// 普通账号本人资料共享七天冷却，超管修改资料不受限制。
import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const profileChangeInterval = 7 * 24 * time.Hour
const maxAvatarSize = 2 * 1024 * 1024

type avatarUpload struct {
	data []byte
	mime string
}

func profileChangeAllowed(changedAt string, now time.Time) bool {
	if changedAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339Nano, changedAt)
	return err == nil && !now.Before(last.Add(profileChangeInterval))
}

// 待分组名单也属于参赛意向，赛事确认结束或取消后才解除资料锁定。
func hasOpenParticipation(db sqlRunner, userID int64) (bool, error) {
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM tournaments t WHERE t.status NOT IN ('completed','cancelled') AND (EXISTS(SELECT 1 FROM tournament_players p WHERE p.tournament_id=t.id AND p.user_id=? AND p.active=1) OR EXISTS(SELECT 1 FROM tournament_setup_roster s WHERE s.tournament_id=t.id AND s.user_id=?)))`, userID, userID).Scan(&exists)
	return exists, err
}

func readAvatarUpload(c *gin.Context, required bool) (*avatarUpload, error) {
	file, header, err := c.Request.FormFile("avatar")
	if errors.Is(err, http.ErrMissingFile) && !required {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("请选择头像文件")
	}
	defer file.Close()
	if header.Size > maxAvatarSize {
		return nil, errors.New("头像文件不能超过2MB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil || len(data) == 0 || len(data) > maxAvatarSize {
		return nil, errors.New("头像文件无效或超过2MB")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return &avatarUpload{data, mime}, nil
	default:
		return nil, errors.New("头像只支持 JPG、PNG、GIF 或 WebP")
	}
}

func parseAccountChange(c *gin.Context) (profileRequest, *avatarUpload, error) {
	var req profileRequest
	if c.ContentType() != "multipart/form-data" {
		err := c.ShouldBindJSON(&req)
		return req, nil, err
	}
	// 限制整个请求体，头像和资料在同一事务提交，校验失败时不保存任何字段。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarSize+64*1024)
	if err := c.Request.ParseMultipartForm(maxAvatarSize + 64*1024); err != nil {
		return req, nil, errors.New("账号资料或头像文件过大")
	}
	values := c.Request.PostForm
	for name, target := range map[string]**string{"username": &req.Username, "display_name": &req.DisplayName, "role": &req.Role} {
		if values.Has(name) {
			value := values.Get(name)
			*target = &value
		}
	}
	if values.Has("category_id") {
		id, err := strconv.ParseInt(values.Get("category_id"), 10, 64)
		if err != nil {
			return req, nil, errors.New("分类编号无效")
		}
		req.CategoryID = &id
	}
	if values.Has("no_password") {
		noPassword, err := strconv.ParseBool(values.Get("no_password"))
		if err != nil {
			return req, nil, errors.New("无密码选项无效")
		}
		req.NoPassword = &noPassword
	}
	req.Password = values.Get("password")
	req.NewPassword = values.Get("new_password")
	req.NewPasswordConfirm = values.Get("new_password_confirm")
	avatar, err := readAvatarUpload(c, false)
	return req, avatar, err
}

func (a *App) accountTarget(c *gin.Context, managed bool) (User, User, bool) {
	actor, err := a.currentUser(c)
	if err != nil {
		fail(c, 401, "未登录")
		return User{}, User{}, false
	}
	if !managed {
		return actor, actor, true
	}
	if actor.Role != roleSuperAdmin {
		fail(c, 403, "只有超级管理员可以管理账号")
		return User{}, User{}, false
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, 400, "账号编号无效")
		return User{}, User{}, false
	}
	target, err := a.userByID(id)
	if err != nil || target.IsDeleted {
		fail(c, 404, "账号不存在")
		return User{}, User{}, false
	}
	return actor, target, true
}

func (a *App) updateUser(c *gin.Context)       { a.editAccount(c, true) }
func (a *App) uploadUserAvatar(c *gin.Context) { a.updateAvatar(c, true) }

func (a *App) editAccount(c *gin.Context, managed bool) {
	actor, target, access := a.accountTarget(c, managed)
	if !access {
		return
	}
	req, avatar, err := parseAccountChange(c)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	a.saveAccountChange(c, actor, target, req, avatar)
}

func (a *App) updateAvatar(c *gin.Context, managed bool) {
	actor, target, access := a.accountTarget(c, managed)
	if !access {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarSize+64*1024)
	avatar, err := readAvatarUpload(c, true)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	a.saveAccountChange(c, actor, target, profileRequest{}, avatar)
}

func (a *App) validCategory(id int64) bool {
	if id == 0 {
		return true
	}
	var count int
	return id > 0 && a.db.QueryRow(`SELECT COUNT(*) FROM categories WHERE id=?`, id).Scan(&count) == nil && count == 1
}

func (a *App) saveAccountChange(c *gin.Context, actor, target User, req profileRequest, avatar *avatarUpload) {
	username, displayName, role, category := target.Username, target.DisplayName, target.Role, target.CategoryID
	self := actor.ID == target.ID
	if req.Username != nil {
		username = strings.TrimSpace(*req.Username)
		if username == "" {
			fail(c, 400, "登录名不能为空")
			return
		}
		if username != target.Username && actor.ID != target.ID {
			fail(c, 403, "登录名只能由账号本人修改")
			return
		}
	}
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if displayName == "" {
			fail(c, 400, "显示名不能为空")
			return
		}
	}
	if req.Role != nil {
		role = *req.Role
		if actor.Role != roleSuperAdmin && role != target.Role {
			fail(c, 403, "无权修改账号角色")
			return
		}
		if role != roleUser && role != roleSuperAdmin {
			fail(c, 400, "账号角色只能是超级管理员或选手")
			return
		}
	}
	if req.CategoryID != nil {
		category = *req.CategoryID
		if !a.validCategory(category) {
			fail(c, 400, "分类不存在")
			return
		}
	}
	noPassword := req.NoPassword != nil && *req.NoPassword
	if noPassword {
		if role == roleSuperAdmin {
			fail(c, 400, "超级管理员必须设置密码")
			return
		}
		if self {
			fail(c, 400, "不能将自己的账号设置为无密码")
			return
		}
		if actor.Role != roleSuperAdmin {
			fail(c, 403, "只有超级管理员可以设置无密码账号")
			return
		}
		if req.NewPassword != "" || req.NewPasswordConfirm != "" || req.Password != "" {
			fail(c, 400, "无密码账号不能同时设置密码")
			return
		}
	}
	now := time.Now().UTC()
	profileChanged := username != target.Username || displayName != target.DisplayName || role != target.Role || category != target.CategoryID || avatar != nil
	profileChangedAt := target.ProfileChangedAt
	nameChangedAt, avatarChangedAt := target.DisplayNameChangedAt, target.AvatarChangedAt
	if self && profileChanged && actor.Role != roleSuperAdmin {
		if !profileChangeAllowed(profileChangedAt, now) {
			fail(c, 400, "个人资料共享7天修改冷却，密码不受限制")
			return
		}
		profileChangedAt = now.Format(time.RFC3339Nano)
		if displayName != target.DisplayName {
			nameChangedAt = profileChangedAt
		}
		if avatar != nil {
			avatarChangedAt = profileChangedAt
		}
	}
	passwordChanged := false
	passwordHash := ""
	if req.NewPassword != "" || req.NewPasswordConfirm != "" {
		if len(req.NewPassword) < 6 {
			fail(c, 400, "新密码至少需要6位")
			return
		}
		if req.NewPassword != req.NewPasswordConfirm {
			fail(c, 400, "两次输入的新密码不一致")
			return
		}
		var err error
		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			fail(c, 400, "密码长度无效")
			return
		}
		passwordHash = string(hash)
		passwordChanged = true
	}
	if username != target.Username {
		var count int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username=? AND id<>?`, username, target.ID).Scan(&count); err != nil {
			fail(c, 500, "检查登录名失败")
			return
		} else if count > 0 {
			fail(c, 400, "登录名已存在")
			return
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, 500, "保存账号失败")
		return
	}
	defer tx.Rollback()
	if req.NoPassword != nil && !noPassword && req.NewPassword == "" && req.NewPasswordConfirm == "" {
		var hasPassword bool
		if err := tx.QueryRow(`SELECT length(password_hash)>0 FROM users WHERE id=? AND is_deleted=0`, target.ID).Scan(&hasPassword); err != nil {
			fail(c, 500, "读取账号登录状态失败")
			return
		}
		if !hasPassword {
			fail(c, 400, "启用登录必须设置新密码")
			return
		}
	}
	if self && profileChanged && actor.Role != roleSuperAdmin {
		blocked, err := hasOpenParticipation(tx, target.ID)
		if err != nil {
			fail(c, 500, "检查参赛状态失败")
			return
		}
		if blocked {
			fail(c, 403, "参加的赛事尚未关闭，暂不能修改个人资料，密码不受限制")
			return
		}
	}
	_, err = tx.Exec(`UPDATE users SET username=?,display_name=?,role=?,category_id=?,display_name_changed_at=?,avatar_changed_at=?,profile_changed_at=? WHERE id=? AND is_deleted=0`, username, displayName, role, nullableInt64(category), nameChangedAt, avatarChangedAt, profileChangedAt, target.ID)
	if err == nil && passwordChanged {
		_, err = tx.Exec(`UPDATE users SET password_hash=? WHERE id=?`, passwordHash, target.ID)
	}
	if err == nil && noPassword {
		result, updateErr := tx.Exec(`UPDATE users SET password_hash='',session_token=NULL WHERE id=? AND (length(password_hash)>0 OR session_token IS NOT NULL)`, target.ID)
		err = updateErr
		if err == nil {
			var changed int64
			changed, err = result.RowsAffected()
			passwordChanged = changed > 0
		}
	}
	if err == nil && avatar != nil {
		_, err = tx.Exec(`UPDATE users SET avatar_data=?,avatar_mime=?,avatar_version=avatar_version+1 WHERE id=?`, avatar.data, avatar.mime, target.ID)
	}
	if err != nil {
		fail(c, 400, "登录名已存在或账号资料无效")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, "保存账号失败")
		return
	}
	if profileChanged || passwordChanged {
		fields := []string{}
		if username != target.Username {
			fields = append(fields, "username")
		}
		if displayName != target.DisplayName {
			fields = append(fields, "display_name")
		}
		if role != target.Role {
			fields = append(fields, "role")
		}
		if category != target.CategoryID {
			fields = append(fields, "category")
		}
		if avatar != nil {
			fields = append(fields, "avatar")
		}
		if passwordChanged {
			fields = append(fields, "password")
		}
		action := "修改账号资料"
		if len(fields) == 1 && fields[0] == "password" {
			action = "修改账号密码"
		}
		a.logAccountOperation(actor, target.ID, displayName, action, fields)
	}
	a.invalidateCategoryDirectory()
	if displayName != target.DisplayName {
		a.invalidateHistoryCache()
	}
	updated, err := a.userByID(target.ID)
	if err != nil {
		fail(c, 500, "读取账号失败")
		return
	}
	ok(c, gin.H{"user": updated})
}

func (a *App) createAccount(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil || actor.Role != roleSuperAdmin {
		fail(c, 403, "只有超级管理员可以创建账号")
		return
	}
	req, avatar, err := parseAccountChange(c)
	if err != nil {
		fail(c, 400, "账号资料格式不正确")
		return
	}
	if req.Username == nil || strings.TrimSpace(*req.Username) == "" || req.Role == nil || (*req.Role != roleUser && *req.Role != roleSuperAdmin && *req.Role != rolePlayer) {
		fail(c, 400, "请填写登录名并选择超级管理员或选手")
		return
	}
	noPassword := req.NoPassword != nil && *req.NoPassword
	passwordHash := ""
	if noPassword {
		if *req.Role == roleSuperAdmin {
			fail(c, 400, "超级管理员必须设置密码")
			return
		}
		if req.NewPassword != "" || req.NewPasswordConfirm != "" || req.Password != "" {
			fail(c, 400, "无密码账号不能同时设置密码")
			return
		}
	} else {
		password := req.NewPassword
		if password == "" && req.NewPasswordConfirm == "" {
			password = req.Password // 兼容原创建账号接口。
		} else if password != req.NewPasswordConfirm {
			fail(c, 400, "两次输入的新密码不一致")
			return
		}
		if len(password) < 6 {
			fail(c, 400, "密码至少需要6位")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			fail(c, 400, "密码长度无效")
			return
		}
		passwordHash = string(hash)
	}
	username := strings.TrimSpace(*req.Username)
	displayName := username
	if req.DisplayName != nil && strings.TrimSpace(*req.DisplayName) != "" {
		displayName = strings.TrimSpace(*req.DisplayName)
	}
	category := int64(0)
	if req.CategoryID != nil {
		category = *req.CategoryID
	}
	if !a.validCategory(category) {
		fail(c, 400, "分类不存在")
		return
	}
	role := roleUser
	if *req.Role == roleSuperAdmin {
		role = roleSuperAdmin
	}
	var data []byte
	mime, version := "", 0
	if avatar != nil {
		data, mime, version = avatar.data, avatar.mime, 1
	}
	result, err := a.db.Exec(`INSERT INTO users(username,display_name,password_hash,role,category_id,avatar_data,avatar_mime,avatar_version) VALUES(?,?,?,?,?,?,?,?)`, username, displayName, passwordHash, role, nullableInt64(category), data, mime, version)
	if err != nil {
		fail(c, 400, "登录名已存在或账号资料无效")
		return
	}
	id, _ := result.LastInsertId()
	fields := []string{"role", "category"}
	if !noPassword {
		fields = append(fields, "password")
	}
	if avatar != nil {
		fields = append(fields, "avatar")
	}
	a.logAccountOperation(actor, id, displayName, "创建账号", fields)
	a.invalidateCategoryDirectory()
	ok(c, gin.H{"id": id})
}

func (a *App) deleteUser(c *gin.Context) {
	actor, target, access := a.accountTarget(c, true)
	if !access {
		return
	}
	if actor.ID == target.ID {
		fail(c, 400, "不能删除自己的账号")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, 500, "删除账号失败")
		return
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM matches WHERE status='finished' AND (player_a_id=? OR player_b_id=?)`, target.ID, target.ID).Scan(&count); err != nil {
		fail(c, 500, "读取账号战绩失败")
		return
	}
	if count > 0 {
		fail(c, 400, "已有战绩的账号不能删除")
		return
	}
	// 仅标记删除，保留 ID 与赛事关联；已删除账号不可登录或重新入选。
	if _, err := tx.Exec(`UPDATE users SET is_deleted=1,session_token=NULL WHERE id=?`, target.ID); err != nil {
		fail(c, 500, "删除账号失败")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, "删除账号失败")
		return
	}
	a.logAccountOperation(actor, target.ID, target.DisplayName, "删除账号", nil)
	a.invalidateCategoryDirectory()
	ok(c, gin.H{"ok": true})
}
