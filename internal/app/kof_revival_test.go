package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func prepareRevivalKOF(t *testing.T, noRepeat bool) (*workflowFixture, int64, []int64) {
	t.Helper()
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	rules := mustTournamentRules(f.app.db, id)
	rules.KOFRevivalNoRepeat = noRepeat
	raw, _ := json.Marshal(rules)
	if _, err := f.app.db.Exec(`UPDATE tournaments SET rules_json=?,round2_target=12 WHERE id=?`, string(raw), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`UPDATE rounds SET target_score=12 WHERE tournament_id=? AND stage='round2'`, id); err != nil {
		t.Fatal(err)
	}
	f.enterKOF(t, id)
	teams := f.teams(t, id)
	for side, player := range []int64{3, 6} {
		f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/kof/starter", id), map[string]any{"team_id": teams[side], "player_id": player}, 200)
	}
	return f, id, teams
}

func TestKOFRevivalCyclesAndCorrection(t *testing.T) {
	f, id, teams := prepareRevivalKOF(t, true)
	seen := map[int64]bool{}
	for seq := 1; seq <= 9; seq++ {
		match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=?`, id, seq)
		player := f.scalar(t, `SELECT player_b_id FROM matches WHERE id=?`, match)
		if seq >= 4 {
			if (seq-4)%3 == 0 {
				seen = map[int64]bool{}
			}
			if seen[player] {
				t.Fatalf("第%d局本轮重复复活%d", seq, player)
			}
			seen[player] = true
		}
		if seq < 3 {
			f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/kof/next-player", id), map[string]any{"match_id": match, "team_id": teams[1], "player_id": 6 + seq}, 200)
		}
		path := fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, match)
		f.request(t, 2, "POST", path, map[string]any{"winner_team_id": teams[0]}, 200)
		if seq == 3 {
			f.request(t, 2, "POST", path, map[string]any{"winner_team_id": teams[0]}, 400)
		}
	}
	// 改掉第二轮复活首局，后续抽签删除；胜者留场后再败，不能再次抽中该人。
	seventh := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=7`, id)
	previous := f.scalar(t, `SELECT player_b_id FROM matches WHERE id=?`, seventh)
	path := fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, seventh)
	f.request(t, 4, "PATCH", path, map[string]any{"winner_team_id": teams[1]}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"winner_team_id": teams[1]}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq>7`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/kof/next-player", id), map[string]any{"match_id": seventh, "team_id": teams[0], "player_id": 4}, 200)
	eighth := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=8`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND player_b_id=?`, eighth, previous)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, eighth), map[string]any{"winner_team_id": teams[0]}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=9 AND player_b_id=?`, id, previous)
}

func TestKOFRevivalDisabledKeepsFullPool(t *testing.T) {
	f, id, teams := prepareRevivalKOF(t, false)
	engine := &matchEngine{db: f.app.db, tournamentID: id}
	round, err := engine.round(stageRound2)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := engine.kofRevivalCandidates(round.id, teams[1])
	if err != nil || len(ids) != 3 {
		t.Fatalf("关闭规则应保留全队: %v %v", ids, err)
	}
}

func TestKOFRevivalRuleLocksAfterGrouping(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "random", "captain", nil)
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	rules := mustTournamentRules(f.app.db, id)
	rules.KOFRevivalNoRepeat = true
	f.request(t, 4, "PATCH", path, map[string]any{"rules": rules}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules, "round2_target": 5}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	rules.KOFRevivalNoRepeat = false
	for _, actor := range []int{1, 2} {
		f.request(t, actor, "PATCH", path, map[string]any{"rules": rules}, 400)
	}
	if !mustTournamentRules(f.app.db, id).KOFRevivalNoRepeat {
		t.Fatal("分组后规则被改动")
	}
}

func TestArchivedKOFRevivalRejectsRepeatsAndAllowsNewCycle(t *testing.T) {
	f := newWorkflowFixture(t)
	for _, tc := range []struct {
		enabled bool
		order   []int64
		want    int
	}{
		{false, []int64{6, 7, 8, 6, 6}, 200},
		{true, []int64{6, 7, 8, 6, 6}, 400},
		{true, []int64{6, 7, 8, 6, 7, 8, 6, 7}, 200},
	} {
		matches := []archivedMatch{{Round: 1, AID: 3, BID: 6, WhoWin: "a"}}
		for _, id := range tc.order {
			matches = append(matches, archivedMatch{Round: 2, AID: 3, BID: id, WhoWin: "a"})
		}
		f.request(t, 1, "POST", "/api/tournaments/archive", map[string]any{
			"manager_id": 1, "name": "历史复活校验", "start_time": "2026-11-19T20", "round1_target": 1, "round2_target": len(tc.order), "kof_revival_no_repeat": tc.enabled,
			"player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}, "matches": matches,
		}, tc.want)
	}
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournaments`)
}
