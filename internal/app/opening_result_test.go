package app

import (
	"fmt"
	"testing"
)

// 首次直接录分必须原子开赛，并拒绝旧阵容、越权和重复录入。
func TestOpeningResultStartsAndRecordsAtomically(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupEvent(t, f, 6, "random", "captain", nil)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/setup-draw", id), nil, 200)
	if _, err := f.app.db.Exec(`UPDATE rounds SET target_score=4 WHERE event_id=? AND stage='round1'`, id); err != nil {
		t.Fatal(err)
	}
	teams := f.teams(t, id)
	first := func(team int64) int64 {
		return f.scalar(t, `SELECT player_id FROM lineup_entries WHERE event_id=? AND team_id=? AND position=1`, id, team)
	}
	a, b := first(teams[0]), first(teams[1])
	path := fmt.Sprintf("/api/events/%d/opening-result", id)
	body := map[string]any{"player_a_id": a, "player_b_id": b, "winner_team_id": teams[0]}
	f.request(t, 0, "POST", path, body, 403)
	f.request(t, 3, "POST", path, body, 403)
	f.request(t, 2, "POST", path, map[string]any{"player_a_id": b, "player_b_id": a, "winner_team_id": teams[0]}, 400)
	f.request(t, 2, "POST", path, map[string]any{"player_a_id": a, "player_b_id": b, "winner_team_id": 9999}, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=?`, id)
	f.request(t, 2, "POST", path, body, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE event_id=? AND seq=1 AND player_a_id=? AND player_b_id=? AND status='finished' AND winner_team_id=?`, id, a, b, teams[0])
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE event_id=? AND seq=2 AND status='pending'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE event_id=? AND stage='round1' AND score_a=1 AND score_b=0`, id)
	f.request(t, 1, "POST", path, body, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_actions WHERE event_id=? AND action='match_result_submitted'`, id)
	if _, err := f.app.db.Exec(`UPDATE events SET status='completed' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 1, "POST", path, body, 400)
}

func TestOpeningResultWaitsForBothManualLineups(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	teams := f.teams(t, id)
	submit := fmt.Sprintf("/api/events/%d/lineup/submit", id)
	f.request(t, 2, "POST", submit, map[string]any{"team_id": teams[0], "player_ids": []int{3}}, 200)
	path := fmt.Sprintf("/api/events/%d/opening-result", id)
	body := map[string]any{"player_a_id": 3, "player_b_id": 6, "winner_team_id": teams[0]}
	f.request(t, 2, "POST", path, body, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=?`, id)
	f.request(t, 2, "POST", submit, map[string]any{"team_id": teams[1], "player_ids": []int{6}}, 200)
	f.assertCount(t, 6, `SELECT COUNT(*) FROM lineup_entries WHERE event_id=?`, id)
	f.request(t, 1, "POST", path, body, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=1 AND status='finished'`, id)
}

func TestOpeningResultPreservesLegacyRandomLineups(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupEvent(t, f, 6, "random", "captain", nil)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/setup-draw", id), nil, 200)
	if _, err := f.app.db.Exec(`UPDATE lineup_entries SET source='random' WHERE event_id=?`, id); err != nil {
		t.Fatal(err)
	}
	teams := f.teams(t, id)
	a := f.scalar(t, `SELECT player_id FROM lineup_entries WHERE event_id=? AND team_id=? AND position=1`, id, teams[0])
	b := f.scalar(t, `SELECT player_id FROM lineup_entries WHERE event_id=? AND team_id=? AND position=1`, id, teams[1])
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/opening-result", id), map[string]any{"player_a_id": a, "player_b_id": b, "winner_team_id": teams[0]}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=1 AND seq=1 AND player_a_id=? AND player_b_id=? AND status='finished'`, id, a, b)
}
