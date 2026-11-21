package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func (f *workflowFixture) accountForm(t *testing.T, user int, method, path string, fields map[string]string, avatar bool, want int) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if avatar {
		part, err := writer.CreateFormFile("avatar", "test.png")
		if err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(testAvatarPNG)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "tb_session", Value: f.tokens[user]})
	res := httptest.NewRecorder()
	f.handler.ServeHTTP(res, req)
	if res.Code != want {
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, res.Code, res.Body.String())
	}
	return res.Body.Bytes()
}

// 包括超级管理员在内，所有人只能修改自己的登录名；失败时不应保存其他字段。
func TestAccountUsernameOwnership(t *testing.T) {
	f := newWorkflowFixture(t)
	f.request(t, 1, "PATCH", "/api/users/2", map[string]any{"username": "admin-edited", "display_name": "错误改名"}, 403)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM users WHERE id=2 AND username='user2' AND display_name='选手2'")
	f.request(t, 2, "PATCH", "/api/users/3", map[string]any{"display_name": "越权"}, 403)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"username": "user1"}, 400)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"username": "  "}, 400)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"username": "renamed", "new_password": "new-pass", "new_password_confirm": "new-pass"}, 200)
	f.request(t, 0, "POST", "/api/auth/login", map[string]any{"username": "user2", "password": "new-pass"}, 401)
	f.request(t, 0, "POST", "/api/auth/login", map[string]any{"username": "renamed", "password": "new-pass"}, 200)
	f.request(t, 1, "PATCH", "/api/auth/profile", map[string]any{"username": "renamed-admin"}, 200)
	f.request(t, 1, "PATCH", "/api/users/1", map[string]any{"username": "admin-again"}, 200)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"role": "superadmin"}, 403)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM users WHERE id=2 AND role='user'")
	var listed struct {
		Users []User `json:"users"`
	}
	if err := json.Unmarshal(f.request(t, 1, "GET", "/api/users", nil, 200), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Users[0].Username != "admin-again" || listed.Users[1].Username != "renamed" {
		t.Fatal("账号目录未更新登录名")
	}
}

func TestAccountWeeklyChangesAndAtomicSave(t *testing.T) {
	f := newWorkflowFixture(t)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"display_name": "新显示名", "new_password": "secret1", "new_password_confirm": "wrong"}, 400)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM users WHERE id=2 AND display_name='选手2' AND display_name_changed_at IS NULL")
	f.accountForm(t, 2, "PATCH", "/api/auth/profile", map[string]string{"display_name": "新显示名", "username": "new-user"}, true, 200)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"display_name": "第二次"}, 400)
	f.accountForm(t, 2, "POST", "/api/auth/avatar", nil, true, 400)
	// 本人所有资料共享冷却；只改密码或重复保存原值仍然允许。
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"display_name": "新显示名", "username": "new-user-2", "category_id": 0}, 400)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"display_name": "新显示名", "username": "new-user", "category_id": workflowCategoryID, "new_password": "secret2", "new_password_confirm": "secret2"}, 200)
	f.accountForm(t, 2, "PATCH", "/api/auth/profile", map[string]string{"username": "must-not-save"}, true, 400)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM users WHERE id=2 AND username='new-user'")
	// 管理员可以连续代改资料、头像和密码，不能更改他人的登录名。
	for i := 0; i < 2; i++ {
		f.accountForm(t, 1, "PATCH", "/api/users/2", map[string]string{"display_name": fmt.Sprint("管理员修改", i), "new_password": "admin-pass", "new_password_confirm": "admin-pass", "category_id": "0"}, true, 200)
		f.accountForm(t, 1, "PATCH", "/api/auth/profile", map[string]string{"display_name": fmt.Sprint("管理自己", i)}, true, 200)
	}
	f.request(t, 0, "POST", "/api/auth/login", map[string]any{"username": "new-user", "password": "admin-pass"}, 200)
	old := time.Now().UTC().Add(-profileChangeInterval - time.Second).Format(time.RFC3339Nano)
	if _, err := f.app.db.Exec("UPDATE users SET profile_changed_at=? WHERE id=2", old); err != nil {
		t.Fatal(err)
	}
	f.app.invalidateCategoryDirectory()
	f.accountForm(t, 2, "PATCH", "/api/auth/profile", map[string]string{"display_name": "一周后"}, true, 200)
	// 改名也会占用头像、登录名及分类的修改次数。
	f.request(t, 3, "PATCH", "/api/auth/profile", map[string]any{"display_name": "共享次数"}, 200)
	f.accountForm(t, 3, "POST", "/api/auth/avatar", nil, true, 400)
	now := time.Now().UTC()
	if !profileChangeAllowed(now.Add(-profileChangeInterval).Format(time.RFC3339Nano), now) || profileChangeAllowed(now.Add(-profileChangeInterval+time.Second).Format(time.RFC3339Nano), now) {
		t.Fatal("7天边界校验不正确")
	}
}

func TestNoPasswordAccountLifecycleAndOwnerGuard(t *testing.T) {
	f := newWorkflowFixture(t)
	f.accountForm(t, 1, "POST", "/api/users", map[string]string{
		"username": "password-free", "display_name": "无密码账号", "role": "user", "no_password": "true",
	}, false, 200)
	var accountID int64
	if err := f.app.db.QueryRow(`SELECT id FROM users WHERE username='password-free'`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=? AND password_hash='' AND session_token IS NULL`, accountID)
	f.request(t, 0, "POST", "/api/auth/login", map[string]any{"username": "password-free", "password": "test1234"}, 401)
	f.accountForm(t, 1, "PATCH", fmt.Sprintf("/api/users/%d", accountID), map[string]string{"no_password": "false"}, false, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=? AND password_hash=''`, accountID)
	f.accountForm(t, 1, "POST", "/api/users", map[string]string{
		"username": "conflicting-password", "role": "user", "no_password": "true", "new_password": "test1234",
	}, false, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM users WHERE username='conflicting-password'`)
	f.accountForm(t, 1, "POST", "/api/users", map[string]string{
		"username": "passwordless-admin", "role": "superadmin", "no_password": "true",
	}, false, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM users WHERE username='passwordless-admin'`)
	f.accountForm(t, 1, "POST", "/api/users", map[string]string{
		"username": "managed-admin", "role": "superadmin", "new_password": "admin-password", "new_password_confirm": "admin-password",
	}, false, 200)
	var superadminID int64
	if err := f.app.db.QueryRow(`SELECT id FROM users WHERE username='managed-admin'`).Scan(&superadminID); err != nil {
		t.Fatal(err)
	}
	f.accountForm(t, 1, "PATCH", fmt.Sprintf("/api/users/%d", superadminID), map[string]string{"no_password": "true"}, false, 400)
	f.accountForm(t, 1, "PATCH", fmt.Sprintf("/api/users/%d", accountID), map[string]string{"role": "superadmin", "no_password": "true"}, false, 400)
	f.accountForm(t, 1, "PATCH", fmt.Sprintf("/api/users/%d", accountID), map[string]string{"role": "superadmin"}, false, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=? AND role='user' AND password_hash=''`, accountID)

	if _, err := f.app.db.Exec(`UPDATE users SET session_token='existing-password-free-session' WHERE id=?`, accountID); err != nil {
		t.Fatal(err)
	}
	f.tokens[int(accountID)] = "existing-password-free-session"
	f.accountForm(t, 1, "PATCH", fmt.Sprintf("/api/users/%d", accountID), map[string]string{"no_password": "true"}, false, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=? AND password_hash='' AND session_token IS NULL`, accountID)
	f.request(t, int(accountID), "GET", "/api/auth/me", nil, 401)
	f.accountForm(t, 1, "PATCH", fmt.Sprintf("/api/users/%d", accountID), map[string]string{
		"new_password": "restored-password", "new_password_confirm": "restored-password",
	}, false, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=? AND length(password_hash)>0`, accountID)
	f.request(t, 0, "POST", "/api/auth/login", map[string]any{"username": "password-free", "password": "restored-password"}, 200)

	f.request(t, 1, "PATCH", "/api/users/1", map[string]any{"no_password": true}, 400)
	f.request(t, 1, "PATCH", "/api/auth/profile", map[string]any{"no_password": true}, 400)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"no_password": true}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=1 AND length(password_hash)>0`)

	var listed struct {
		Users []struct {
			ID          int64  `json:"id"`
			Username    string `json:"username"`
			HasPassword bool   `json:"has_password"`
			CanLogin    bool   `json:"can_login"`
		} `json:"users"`
	}
	if err := json.Unmarshal(f.request(t, 1, "GET", "/api/users", nil, 200), &listed); err != nil {
		t.Fatal(err)
	}
	for _, user := range listed.Users {
		if user.ID == accountID && (!user.HasPassword || !user.CanLogin) {
			t.Fatalf("设置密码后账号登录状态未更新: %+v", user)
		}
	}
}

func TestAccountCreateDeleteAndRecordProtection(t *testing.T) {
	f := newWorkflowFixture(t)
	f.accountForm(t, 1, "POST", "/api/users", map[string]string{"username": "created-user", "display_name": "新账号", "role": "user", "new_password": "test-pass", "new_password_confirm": "test-pass", "category_id": "0"}, true, 200)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM users WHERE username='created-user' AND length(avatar_data)>0 AND display_name_changed_at IS NULL")
	f.request(t, 1, "DELETE", "/api/users/1", nil, 400)
	f.request(t, 2, "DELETE", "/api/users/9", nil, 403)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	// 已录分的双方都不能删除，无论输赢；已抽签但未录分不算战绩。
	match := f.scalar(t, "SELECT id FROM matches WHERE tournament_id=? AND round_no=1", id)
	for _, side := range []string{"player_a_id", "player_b_id"} {
		player := f.scalar(t, "SELECT "+side+" FROM matches WHERE id=?", match)
		f.request(t, 1, "DELETE", fmt.Sprintf("/api/users/%d", player), nil, 400)
	}
	f.request(t, 9, "PATCH", "/api/auth/profile", map[string]any{"new_password": "delete-me", "new_password_confirm": "delete-me"}, 200)
	f.request(t, 1, "DELETE", "/api/users/9", nil, 200)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM users WHERE id=9 AND is_deleted=1")
	f.request(t, 9, "GET", "/api/auth/me", nil, 401)
	f.request(t, 0, "POST", "/api/auth/login", map[string]any{"username": "user9", "password": "delete-me"}, 401)
	f.request(t, 1, "PATCH", "/api/users/9", map[string]any{"display_name": "恢复"}, 404)
	f.request(t, 1, "DELETE", "/api/users/9", nil, 404)
	var listed struct {
		Users []User `json:"users"`
	}
	if err := json.Unmarshal(f.request(t, 1, "GET", "/api/users", nil, 200), &listed); err != nil {
		t.Fatal(err)
	}
	for _, u := range listed.Users {
		if u.ID == 9 || (u.ID == 1 && u.CanDelete) {
			t.Fatal("账号删除状态错误")
		}
	}
	players, err := f.app.activePlayers()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range players {
		if u.ID == 9 {
			t.Fatal("已删除账号仍可入选")
		}
	}
}
