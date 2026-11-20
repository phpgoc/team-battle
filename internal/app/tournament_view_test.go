package app

import (
	"bytes"
	"fmt"
	"testing"
)

func TestPublicTournamentIncludesManagerDisplayName(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	path := fmt.Sprintf("/api/public/tournaments/%d", id)
	// 管理员不在两队名单中，公开详情仍然需要显示其名称。
	for _, status := range []string{statusDraft, statusCompleted} {
		if _, err := f.app.db.Exec(`UPDATE tournaments SET status=? WHERE id=?`, status, id); err != nil {
			t.Fatal(err)
		}
		raw := f.request(t, 0, "GET", path, nil, 200)
		if !bytes.Contains(raw, []byte(`"manager_name":"选手2"`)) {
			t.Fatalf("%s 详情缺少赛事管理员显示名: %s", status, raw)
		}
		if bytes.Contains(raw, []byte(`"password_hash"`)) {
			t.Fatal("公开管理员资料不得包含密码哈希")
		}
	}
	// 已删除或找不到的管理员允许显示空名称，详情仍正常访问。
	if _, err := f.app.db.Exec(`UPDATE users SET is_deleted=1 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	f.app.invalidateCategoryDirectory()
	raw := f.request(t, 0, "GET", path, nil, 200)
	if !bytes.Contains(raw, []byte(`"manager_name":""`)) {
		t.Fatalf("空管理员名称未保留: %s", raw)
	}
	// 超级管理员也可被明确指定为赛事管理员，应显示其姓名。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET manager_id=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	raw = f.request(t, 0, "GET", path, nil, 200)
	if !bytes.Contains(raw, []byte(`"manager_name":"选手1"`)) {
		t.Fatalf("超级管理员姓名未显示: %s", raw)
	}
}

func TestTournamentViewOmitsLoginNames(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "random", "random", nil)
	path := fmt.Sprintf("/api/tournaments/%d", id)
	assertView := func(isGrouped bool) {
		t.Helper()
		for _, actor := range []int{0, 1, 2, 3} {
			raw := f.request(t, actor, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200)
			assertNoLoginNames(t, raw)
			if !bytes.Contains(raw, []byte(`"display_name":"选手3"`)) {
				t.Fatalf("身份%d的公开名单缺少选手显示名，已分队=%t", actor, isGrouped)
			}
		}
		managerView := f.request(t, 2, "GET", path, nil, 200)
		assertNoLoginNames(t, managerView)
		if bytes.Contains(managerView, []byte(`"actions"`)) {
			t.Fatal("赛事详情不提供操作日志查询")
		}
	}
	assertView(false)
	f.request(t, 2, "POST", path+"/setup-draw", nil, 200)
	assertView(true)
	for actor, path := range map[int]string{1: "/api/users", 3: "/api/auth/me"} {
		if raw := f.request(t, actor, "GET", path, nil, 200); !bytes.Contains(raw, []byte(`"username":"user3"`)) {
			t.Fatalf("%s 缺少授权可见的登录名", path)
		}
	}
}

func assertNoLoginNames(t *testing.T, raw []byte) {
	t.Helper()
	if bytes.Contains(raw, []byte(`"username":`)) || bytes.Contains(raw, []byte(`"user3"`)) {
		t.Fatalf("观赛数据包含登录名: %s", raw)
	}
}
