package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestTournamentRequiresExplicitRegisteredManager(t *testing.T) {
	f := newWorkflowFixture(t)
	if _, err := f.app.db.Exec(`UPDATE users SET status='disabled' WHERE id=8`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`UPDATE users SET is_deleted=1 WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`INSERT INTO users(id,username,display_name,password_hash,role) VALUES(10,'other-admin','其他超管','unused','superadmin')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{0, -1, 8, 9, 999, 1, 2, 3, 10} {
		want := 400
		if id == 1 || id == 2 || id == 3 || id == 10 {
			want = 200
		}
		f.request(t, 1, "POST", "/api/tournaments", map[string]any{"name": "管理员验证", "start_time": "2026-11-19T20", "manager_id": id}, want)
	}
	f.request(t, 1, "POST", "/api/tournaments", map[string]any{"name": "缺管理员", "start_time": "2026-11-19T20"}, 400)
	f.assertCount(t, 4, `SELECT COUNT(*) FROM tournaments`)
}

func TestManagerCanBeSuperAdminAndCannotBeCleared(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "random", "captain", nil)
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 0}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND manager_id=2`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 1}, 200)
	f.request(t, 2, "PATCH", path, map[string]any{"description": "已失去管理权"}, 403)
	f.request(t, 1, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	var view struct {
		ManagerID int64 `json:"manager_id"`
	}
	if err := json.Unmarshal(f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200), &view); err != nil || view.ManagerID != 1 {
		t.Fatal("管理员未正确保存", err)
	}
}
