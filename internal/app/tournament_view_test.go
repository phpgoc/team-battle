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
