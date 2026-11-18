package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSettingsLockAtFormalStart(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	path := fmt.Sprintf("/api/events/%d/settings", id)
	f.request(t, 4, "PATCH", path, map[string]any{"rules": defaultRules()}, 403)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND status='finished'`, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int{1, 2} {
		f.request(t, actor, "PATCH", path, map[string]any{"name": "不允许改名", "start_time": "2026-11-19T20", "round1_target": 8, "round2_target": 8, "rules": defaultRules()}, 400)
		f.request(t, actor, "POST", fmt.Sprintf("/api/events/%d/players", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 9}}, 400)
	}
	after, err := f.app.publicEventPayload(id)
	if err != nil || string(before) != string(after) {
		t.Fatal("正式开始但尚未录分时，设置或阵容仍可被修改")
	}
}

func TestUnifiedEventSettingsAtomicRoster(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	path := fmt.Sprintf("/api/events/%d/settings", id)
	fixed := map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}
	f.request(t, 2, "PATCH", path, map[string]any{"rules": map[string]any{"round1_mode": "manual", "kof_starter": "captain", "no_repeat": true}, "player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": fixed}, 200)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND is_captain=1`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"manager_id": 9}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 2}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": map[string]any{"round1_mode": "manual", "kof_starter": "random", "no_repeat": true}}, 400)
	// 后续校验失败时，之前的删除、分队、资料与规则变更全部回滚。
	f.request(t, 2, "PATCH", path, map[string]any{"name": "不应保存", "start_time": "2026-11-19T21", "round1_target": 1, "round2_target": 1, "player_ids": []int{3, 4, 5, 6, 7, 9}, "rules": map[string]any{"round1_mode": "manual", "kof_starter": "captain", "no_repeat": true, "ace_specials": []map[string]any{{"player_ids": []int{8}, "probability": 10}}}}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=8`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE id=? AND name='不应保存'`, id)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND is_captain=1`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"name": "完整赛事设置", "description": "更新说明", "start_time": "2026-11-19T21", "round1_target": 1, "round2_target": 1, "rules": map[string]any{"kof_starter": "captain", "round1_mode": "manual", "no_repeat": true, "ace_most_wins": 30, "ace_specials": []map[string]any{{"player_ids": []int{8}, "probability": 10}}}}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND name='完整赛事设置' AND start_time='2026-11-19T21:00:00' AND round1_target=1 AND round2_target=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=8`, id)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND is_captain=1`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 9}, 200)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": defaultRules()}, 403)
	f.request(t, 9, "PATCH", path, map[string]any{"player_ids": []int{}, "rules": map[string]any{"round1_mode": "manual", "kof_starter": "captain", "no_repeat": true}}, 400)
	f.assertCount(t, 6, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 0}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND manager_id=1`, id)
}

func TestRoundAndAceSettingsUseDifferentSetupStages(t *testing.T) {
	f := newWorkflowFixture(t)
	raw := f.request(t, 1, "POST", "/api/events", map[string]any{"name": "分阶段设置", "start_time": "2026-11-19T20", "manager_id": 2}, 200)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	id := created.ID
	path := fmt.Sprintf("/api/events/%d/settings", id)
	rules := defaultRules()
	rules.Round1Mode = "random"
	rules.KOFStarter = "captain"
	f.request(t, 2, "PATCH", path, map[string]any{"name": "分阶段设置", "start_time": "2026-11-19T20", "round1_target": 4, "round2_target": 5, "rules": rules}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/players", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/setup-draw", id), nil, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND round1_target=4 AND round2_target=5`, id)
	rules.AceMostWins = 35
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 200)
	if got := mustEventRules(f.app.db, id); got.AceMostWins != 35 || got.KOFStarter != "captain" {
		t.Fatalf("分组后、首场前应允许修改大将战规则并保留KOF规则: %+v", got)
	}
	rules.Round1Mode = "manual"
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 3}, 400)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	rules = mustEventRules(f.app.db, id)
	rules.AceMostWins = 40
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 400)
}

func TestCaptainlessVotingAndLiveCaptainPermissions(t *testing.T) {
	f := newWorkflowFixture(t)
	id, teams, _ := prepareSignupKOF(t, f)
	vote := fmt.Sprintf("/api/events/%d/captain-votes", id)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 6}, 400)
	f.request(t, 9, "POST", vote, map[string]any{"target_id": 5}, 400)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 5}, 200)
	confirm := fmt.Sprintf("/api/events/%d/captains/confirm", id)
	f.request(t, 4, "POST", confirm, map[string]any{"team_a_player_id": 5, "team_b_player_id": 0}, 403)
	f.request(t, 2, "POST", confirm, map[string]any{"team_a_player_id": 5, "team_b_player_id": 0}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='round2'`, id)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 4}, 400)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 5}, 400)
	f.request(t, 7, "POST", vote, map[string]any{"target_id": 6}, 200)
	starter := fmt.Sprintf("/api/events/%d/kof/starter", id)
	f.request(t, 4, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 4}, 400)
	f.request(t, 5, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 4}, 200)
	f.request(t, 6, "POST", starter, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
}
