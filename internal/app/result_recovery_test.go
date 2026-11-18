package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func createRecoveryTournament(t *testing.T, f *workflowFixture, firstTarget, secondTarget int) int64 {
	t.Helper()
	rules := defaultRules()
	rules.Round1Mode, rules.KOFStarter = "random", "random"
	rules.RandomDrawSeconds, rules.AceDrawSeconds = 0, 0
	raw := f.request(t, 1, "POST", "/api/tournaments", map[string]any{
		"name": "录分纠错验证", "start_time": "2026-11-18T20", "manager_id": 2,
		"player_ids":        []int{3, 4, 5, 6, 7, 8},
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"round1_target":     firstTarget, "round2_target": secondTarget, "rules": rules,
	}, 200)
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", result.ID), nil, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", result.ID), nil, 200)
	return result.ID
}

func TestSettingsCorrectionDeletesUnneededRound1Matches(t *testing.T) {
	for _, laterScored := range []bool{false, true} {
		t.Run(fmt.Sprintf("后续对局已录分=%t", laterScored), func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := createRecoveryTournament(t, f, 4, 5)
			teams := f.teams(t, id)
			var fifth int64
			for index, side := range []int{0, 0, 0, 1, 1} {
				match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1 AND status='pending'`, id)
				f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[side]}, 200)
				if index == 4 {
					fifth = match
				}
			}
			f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE tournament_id=? AND stage='round1' AND score_a=3 AND score_b=2 AND status='active'`, id)
			sixth := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1 AND seq=6`, id)
			if laterScored {
				f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, sixth), map[string]any{"winner_team_id": teams[1]}, 200)
			}
			f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{
				"random_draw_seconds": 2, "ace_draw_seconds": 3, "background_id": 0,
				"correction": map[string]any{"match_id": fifth, "winner_team_id": teams[0]},
			}, 200)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE tournament_id=? AND stage='round1' AND score_a=4 AND score_b=1 AND winner_team_id=? AND status='finished'`, id, teams[0])
			f.assertCount(t, 5, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=1`, id)
			f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=1 AND seq>=6`, id)
			f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE id=?`, sixth)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='round2'`, id)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND seq=1 AND status='pending'`, id)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND who_win='a' AND winner_team_id=?`, fifth, teams[0])
			if rules := mustTournamentRules(f.app.db, id); rules.RandomDrawSeconds != 2 || rules.AceDrawSeconds != 3 {
				t.Fatal("纠错未与设置一起保存")
			}
		})
	}
}

func TestSettingsCorrectionPermissionsAndRollback(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	first := f.enterKOF(t, id)
	teams := f.teams(t, id)
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	body := map[string]any{"random_draw_seconds": 9, "correction": map[string]any{"match_id": first, "winner_team_id": teams[1]}}
	f.request(t, 0, "PATCH", path, body, 403)
	f.request(t, 3, "PATCH", path, body, 403)
	other := f.create(t, "random")
	otherMatch := f.enterKOF(t, other)
	before, _ := f.app.publicTournamentPayload(id)
	for _, correction := range []map[string]any{
		{"match_id": first, "winner_team_id": teams[0]},
		{"match_id": first, "winner_team_id": 99999},
		{"match_id": 99999, "winner_team_id": teams[1]},
		{"match_id": otherMatch, "winner_team_id": teams[1]},
	} {
		f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 9, "correction": correction}, 400)
		after, _ := f.app.publicTournamentPayload(id)
		if !bytes.Equal(before, after) {
			t.Fatalf("非法纠错未完整回滚: %+v", correction)
		}
	}
	f.request(t, 1, "PATCH", path, body, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND winner_team_id=?`, first, teams[1])
	kof := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, kof), map[string]any{"winner_team_id": teams[1]}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/confirm", id), nil, 200)
	f.request(t, 1, "PATCH", path, map[string]any{"correction": map[string]any{"match_id": first, "winner_team_id": teams[0]}}, 400)
}

func TestKOFFinalCorrectionReturnsToKOFWithoutDrawingAce(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createRecoveryTournament(t, f, 1, 2)
	teams := f.teams(t, id)
	first := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, first), map[string]any{"winner_team_id": teams[0]}, 200)
	kof := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, kof), map[string]any{"winner_team_id": teams[1]}, 200)
	options := []int64{3, 4, 5}
	firstLoser := f.scalar(t, `SELECT player_a_id FROM matches WHERE id=?`, kof)
	for _, player := range options {
		if player != firstLoser {
			f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/kof/next-player", id), map[string]any{"match_id": kof, "team_id": teams[0], "player_id": player}, 200)
			break
		}
	}
	last := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND seq=2`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, last), map[string]any{"winner_team_id": teams[1]}, 200)
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"correction": map[string]any{"match_id": last, "winner_team_id": teams[0]}}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='round2'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE tournament_id=? AND stage='round2' AND score_a=1 AND score_b=1 AND status='active'`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=3`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action='ace_drawn'`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/ace/draw", id), nil, 400)
}
