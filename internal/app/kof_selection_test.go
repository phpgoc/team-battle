package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func prepareSignupKOF(t *testing.T, f *workflowFixture) (int64, []int64, int64) {
	t.Helper()
	id := f.create(t, "captain")
	if _, err := f.app.db.Exec(`UPDATE rounds SET target_score=5 WHERE tournament_id=? AND stage='round2'`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/captains/confirm", id), map[string]any{"team_a_player_id": 0, "team_b_player_id": 0}, 200)
	first := f.enterKOF(t, id)
	return id, f.teams(t, id), first
}

func TestCaptainlessKOFStartersAndPrivacy(t *testing.T) {
	f := newWorkflowFixture(t)
	id, teams, _ := prepareSignupKOF(t, f)
	path := fmt.Sprintf("/api/events/%d/kof/starter", id)
	f.request(t, 4, "POST", path, map[string]any{"team_id": teams[0], "player_id": 3}, 400)
	f.request(t, 9, "POST", path, map[string]any{"team_id": teams[0], "player_id": 9}, 400)
	f.request(t, 3, "POST", path, map[string]any{"team_id": teams[0], "player_id": 3}, 200)
	f.request(t, 4, "POST", path, map[string]any{"team_id": teams[0], "player_id": 4}, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
	for _, user := range []int{0, 3, 6} {
		endpoint := fmt.Sprintf("/api/public/events/%d", id)
		if user != 0 {
			endpoint = fmt.Sprintf("/api/events/%d", id)
		}
		raw := f.request(t, user, "GET", endpoint, nil, 200)
		if bytes.Contains(raw, []byte("kof_starter_selected")) || !bytes.Contains(raw, []byte(`"kof_starters":[]`)) {
			t.Fatalf("未就绪的首发泄露: %s", raw)
		}
	}
	f.request(t, 6, "POST", path, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND player_a_id=3 AND player_b_id=6`, id)
}

func TestKOFReservationsAdvanceOnlyAfterResult(t *testing.T) {
	f := newWorkflowFixture(t)
	id, teams, _ := prepareSignupKOF(t, f)
	starter := fmt.Sprintf("/api/events/%d/kof/starter", id)
	f.request(t, 3, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 3}, 200)
	f.request(t, 6, "POST", starter, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	path := fmt.Sprintf("/api/events/%d/kof/next-player", id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(actor int, team, player int64, want int) {
		f.request(t, actor, "POST", path, map[string]any{"match_id": match, "team_id": team, "player_id": player}, want)
	}
	reserve(3, teams[0], 3, 400) // 当前对局选手不能占下一位置。
	reserve(4, teams[1], 7, 400)
	reserve(4, teams[0], 5, 400)
	reserve(4, teams[0], 4, 200)
	reserve(7, teams[1], 7, 200)
	reserve(8, teams[1], 8, 400)
	reserve(1, teams[1], 8, 200) // 超级管理员可以覆盖报名。
	reserve(2, teams[1], 7, 200)
	after, err := f.app.publicEventPayload(id)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("预报名改变了公开 SSE 数据")
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
	options := fmt.Sprintf("/api/events/%d/kof/options", id)
	f.request(t, 0, "GET", options, nil, 401)
	f.request(t, 9, "GET", options, nil, 403)
	var own struct {
		Teams []struct {
			TeamID int64 `json:"team_id"`
			Player int64 `json:"player_id"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(f.request(t, 4, "GET", options, nil, 200), &own); err != nil || len(own.Teams) != 1 || own.Teams[0].TeamID != teams[0] || own.Teams[0].Player != 4 {
		t.Fatal("报名查询没有隔离双方队伍")
	}
	// 除候选本人外，队友、队长和管理员均只看到占位状态，不能读取候选身份。
	for _, actor := range []int{1, 2, 5, 6, 7} {
		var view struct {
			Teams []struct {
				Player   int64 `json:"player_id"`
				Occupied bool  `json:"occupied"`
			} `json:"teams"`
		}
		if err := json.Unmarshal(f.request(t, actor, "GET", options, nil, 200), &view); err != nil {
			t.Fatal(err)
		}
		for _, choice := range view.Teams {
			want := int64(0)
			if actor == 7 {
				want = 7
			}
			if !choice.Occupied || choice.Player != want {
				t.Fatalf("用户%d读取到错误候选: %+v", actor, choice)
			}
		}
	}
	result := fmt.Sprintf("/api/events/%d/matches/%d/result", id, match)
	f.request(t, 2, "POST", result, map[string]any{"winner_team_id": teams[0]}, 200)
	f.request(t, 2, "POST", result, map[string]any{"winner_team_id": teams[0]}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=2 AND player_a_id=3 AND player_b_id=7 AND status='pending'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM kof_reservations WHERE tournament_id=? AND player_id=4`, id)
	reserve(8, teams[1], 8, 400) // 旧场次报名无效。
	second := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=2`, id)
	f.request(t, 6, "POST", path, map[string]any{"match_id": second, "team_id": teams[1], "player_id": 6}, 400)
	f.request(t, 8, "POST", path, map[string]any{"match_id": second, "team_id": teams[1], "player_id": 8}, 200)
	f.request(t, 2, "PATCH", result, map[string]any{"winner_team_id": teams[0]}, 400)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM kof_reservations WHERE tournament_id=?`, id)
	f.request(t, 2, "PATCH", result, map[string]any{"winner_team_id": teams[1]}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM kof_reservations WHERE tournament_id=?`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND status='pending'`, id)
	// 未提前报名时，上一局结束后可由本人补报名并公开对阵。
	reserve(4, teams[0], 4, 200)
	reserve(5, teams[0], 5, 400)
}

func TestKOFWinningQueueSurvivesWhileWaiting(t *testing.T) {
	f := newWorkflowFixture(t)
	id, teams, _ := prepareSignupKOF(t, f)
	starter := fmt.Sprintf("/api/events/%d/kof/starter", id)
	f.request(t, 2, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 3}, 200)
	f.request(t, 2, "POST", starter, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
	first := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	next := fmt.Sprintf("/api/events/%d/kof/next-player", id)
	f.request(t, 4, "POST", next, map[string]any{"match_id": first, "team_id": teams[0], "player_id": 4}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, first), map[string]any{"winner_team_id": teams[0]}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND status='pending'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM kof_reservations WHERE match_id=? AND player_id=4`, first)
	var own struct {
		Teams []struct {
			Player     int64   `json:"player_id"`
			Candidates []int64 `json:"candidate_ids"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(f.request(t, 4, "GET", fmt.Sprintf("/api/events/%d/kof/options", id), nil, 200), &own); err != nil || len(own.Teams) != 1 || own.Teams[0].Player != 4 || len(own.Teams[0].Candidates) != 0 {
		t.Fatal("等待败方派人时，本人候选应保留但胜方不能重复派人")
	}
	f.request(t, 2, "POST", next, map[string]any{"match_id": first, "team_id": teams[0], "player_id": 5}, 400)
	f.request(t, 2, "POST", next, map[string]any{"match_id": first, "team_id": teams[1], "player_id": 7}, 200)
	second := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=2`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM kof_reservations WHERE match_id=? AND player_id=4`, second)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, second), map[string]any{"winner_team_id": teams[1]}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=3 AND player_a_id=4 AND player_b_id=7 AND status='pending'`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM kof_reservations WHERE tournament_id=?`, id)
}

func TestCaptainSelectionAndAutomaticRevival(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	if _, err := f.app.db.Exec(`UPDATE rounds SET target_score=5 WHERE tournament_id=? AND stage='round2'`, id); err != nil {
		t.Fatal(err)
	}
	teams := f.teams(t, id)
	f.enterKOF(t, id)
	for i, player := range []int64{3, 6} {
		f.request(t, int(player), "POST", fmt.Sprintf("/api/events/%d/kof/starter", id), map[string]any{"team_id": teams[i], "player_id": player}, 200)
	}
	path := fmt.Sprintf("/api/events/%d/kof/next-player", id)
	for seq := int64(1); seq <= 5; seq++ {
		match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=?`, id, seq)
		if seq < 3 {
			body := map[string]any{"match_id": match, "team_id": teams[1], "player_id": 6 + seq}
			f.request(t, int(6+seq), "POST", path, body, 400)
			f.request(t, 6, "POST", path, body, 200)
		}
		f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
		if seq < 5 {
			f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=? AND status='pending'`, id, seq+1)
		}
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=4 AND player_b_id IN (6,7,8)`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=5 AND player_b_id IN (6,7,8)`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/confirm", id), nil, 200)
	last := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=5`, id)
	f.request(t, 1, "POST", path, map[string]any{"match_id": last, "team_id": teams[1], "player_id": 8}, 400)
	f.request(t, 1, "PATCH", fmt.Sprintf("/api/events/%d/matches/%d/result", id, last), map[string]any{"winner_team_id": teams[1]}, 400)
}
