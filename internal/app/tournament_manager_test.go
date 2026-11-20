package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestTournamentRosterRequiresCategoryForNewPlayers(t *testing.T) {
	f := newWorkflowFixture(t)
	if _, err := f.app.db.Exec("UPDATE users SET category_id=NULL WHERE id IN (2,3)"); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "分类校验", "start_time": "2026-11-19T20", "manager_id": 2, "player_ids": []int{2}}
	body["player_ids"] = []int{3}
	f.request(t, 1, "POST", "/api/tournaments", body, 400)
	f.assertCount(t, 0, "SELECT COUNT(*) FROM tournaments")
	body["player_ids"] = []int{2}
	var created struct{ ID int64 }
	if err := json.Unmarshal(f.request(t, 1, "POST", "/api/tournaments", body, 200), &created); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/tournaments/%d", created.ID)
	if raw := f.request(t, 2, "GET", path+"/eligible-users", nil, 200); !strings.Contains(string(raw), `"username":"user2"`) {
		t.Fatal("未分类的赛事管理员没有出现在候选中")
	}
	f.request(t, 2, "POST", path+"/players", map[string]any{"player_ids": []int{2, 5}}, 200)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM tournament_setup_roster WHERE tournament_id=? AND user_id=2", created.ID)

	if _, err := f.app.db.Exec("UPDATE users SET category_id=NULL WHERE id=4"); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "POST", path+"/players", map[string]any{"player_ids": []int{2, 3, 4}}, 400)
	f.request(t, 2, "PATCH", path+"/settings", map[string]any{"player_ids": []int{2, 3, 4}}, 400)
	f.assertCount(t, 2, "SELECT COUNT(*) FROM tournament_setup_roster WHERE tournament_id=?", created.ID)
	f.request(t, 1, "PATCH", path+"/settings", map[string]any{"manager_id": 4, "player_ids": []int{2, 3, 4}}, 400)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM tournaments WHERE id=? AND manager_id=2", created.ID)
	f.request(t, 1, "PATCH", path+"/settings", map[string]any{"manager_id": 4, "player_ids": []int{4}}, 200)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM tournament_setup_roster WHERE tournament_id=? AND user_id=4", created.ID)

	body["player_ids"] = []int{2, 3, 4, 5, 6, 7}
	body["round1_target"], body["round2_target"] = 1, 1
	body["matches"] = []archivedMatch{{1, 2, 6, "a"}, {2, 2, 6, "a"}}
	f.request(t, 1, "POST", "/api/tournaments/archive", body, 400)
	f.assertCount(t, 1, "SELECT COUNT(*) FROM tournaments")

	archive := map[string]any{
		"manager_id": 2, "name": "未分类管理员补录", "start_time": "2026-11-19T20",
		"round1_target": 1, "round2_target": 1,
		"player_ids":        []int{2, 5, 6, 7, 8, 9},
		"fixed_assignments": map[string]string{"2": "A", "5": "A", "6": "A", "7": "B", "8": "B", "9": "B"},
		"matches":           []archivedMatch{{1, 2, 7, "a"}, {2, 2, 7, "a"}},
	}
	f.request(t, 1, "POST", "/api/tournaments/archive", archive, 200)
	f.assertCount(t, 2, "SELECT COUNT(*) FROM tournaments")
}

func TestSetupDrawRejectsPlayerWithoutCategory(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "random", "random", nil)
	if _, err := f.app.db.Exec(`UPDATE users SET category_id=NULL WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
}

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
