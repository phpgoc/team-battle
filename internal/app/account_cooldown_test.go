package app

import (
	"fmt"
	"testing"
	"time"
)

func TestAccountAllProfileFieldsShareCooldown(t *testing.T) {
	for _, actor := range []int{1, 2} {
		for _, field := range []string{"username", "display_name", "category_id", "avatar"} {
			t.Run(fmt.Sprintf("账号%d先修改%s", actor, field), func(t *testing.T) {
				f := newWorkflowFixture(t)
				if _, err := f.app.db.Exec(`INSERT INTO categories(id,name) VALUES(1,'测试分类')`); err != nil {
					t.Fatal(err)
				}
				if field == "avatar" {
					f.accountForm(t, actor, "POST", "/api/auth/avatar", nil, true, 200)
				} else {
					value := any("首次修改")
					if field == "category_id" {
						value = 1
					}
					f.request(t, actor, "PATCH", "/api/auth/profile", map[string]any{field: value}, 200)
				}
				f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=? AND length(profile_changed_at)>0`, actor)
				before, err := f.app.userByID(int64(actor))
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range []map[string]any{
					{"username": "冷却中改登录名"}, {"display_name": "冷却中改名"}, {"category_id": 1 - before.CategoryID},
					{"username": "冷却中改登录名", "new_password": "must-fail", "new_password_confirm": "must-fail"},
				} {
					f.request(t, actor, "PATCH", "/api/auth/profile", change, 400)
				}
				f.accountForm(t, actor, "POST", "/api/auth/avatar", nil, true, 400)
				// 超级管理员不能通过账号管理中的本人入口绕过冷却。
				if actor == 1 {
					f.request(t, 1, "PATCH", "/api/users/1", map[string]any{"display_name": "绕过限制"}, 400)
				}
				f.request(t, actor, "PATCH", "/api/auth/profile", map[string]any{"username": before.Username, "display_name": before.DisplayName, "category_id": before.CategoryID, "new_password": "new-password", "new_password_confirm": "new-password"}, 200)
				after, err := f.app.userByID(int64(actor))
				if err != nil || before != after {
					t.Fatal("密码修改或失败请求改变了资料及共享冷却", before, after, err)
				}
				old := time.Now().UTC().Add(-profileChangeInterval - time.Second).Format(time.RFC3339Nano)
				if _, err := f.app.db.Exec(`UPDATE users SET profile_changed_at=? WHERE id=?`, old, actor); err != nil {
					t.Fatal(err)
				}
				f.app.invalidateCategoryDirectory()
				f.accountForm(t, actor, "PATCH", "/api/auth/profile", map[string]string{"username": "一周后登录名", "display_name": "一周后显示名", "category_id": "1"}, true, 200)
			})
		}
	}
}

func TestAdminEditsOtherProfilesWithoutConsumingCooldown(t *testing.T) {
	f := newWorkflowFixture(t)
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"display_name": "本人修改"}, 200)
	before, _ := f.app.userByID(2)
	for index := 0; index < 2; index++ {
		f.accountForm(t, 1, "PATCH", "/api/users/2", map[string]string{"display_name": fmt.Sprint("管理员代改", index)}, true, 200)
	}
	after, _ := f.app.userByID(2)
	if before.ProfileChangedAt != after.ProfileChangedAt {
		t.Fatal("管理员代改他人资料占用了本人冷却")
	}
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"username": "绕过冷却"}, 400)
	f.request(t, 1, "PATCH", "/api/users/2", map[string]any{"username": "更改他人登录名"}, 403)
}

func TestProfileCooldownMigrationKeepsLatestChange(t *testing.T) {
	f := newWorkflowFixture(t)
	older := time.Now().UTC().Add(-4 * 24 * time.Hour).Format(time.RFC3339Nano)
	newer := time.Now().UTC().Add(-2 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := f.app.db.Exec(`UPDATE users SET display_name_changed_at=?,avatar_changed_at=?,profile_changed_at=NULL WHERE id=2`, older, newer); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := migrate(f.app.db); err != nil {
			t.Fatal(err)
		}
	}
	f.app.invalidateCategoryDirectory()
	u, err := f.app.userByID(2)
	if err != nil || u.ProfileChangedAt != newer {
		t.Fatal("旧资料修改时间未合并", u, err)
	}
	f.request(t, 2, "PATCH", "/api/auth/profile", map[string]any{"username": "迁移后绕过"}, 400)
}
