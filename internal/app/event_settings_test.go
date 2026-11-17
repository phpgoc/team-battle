package app

import (
	"fmt"
	"testing"
)

func TestSettingsLockAtFormalStart(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	path := fmt.Sprintf("/api/events/%d/settings", id)
	f.request(t, 4, "PATCH", path, map[string]any{"rules": defaultRules()}, 403)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=? AND status='finished'`, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int{1, 2} {
		f.request(t, actor, "PATCH", path, map[string]any{"name": "不允许改名", "event_time": "2026-11-19T20", "round1_target": 8, "round2_target": 8, "rules": defaultRules()}, 400)
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
	f.request(t, 2, "PATCH", path, map[string]any{"rules": defaultRules(), "player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": fixed}, 200)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND is_captain=1`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"manager_id": 9}, 403)
	// 后续校验失败时，之前的删除、分队、资料与规则变更全部回滚。
	f.request(t, 2, "PATCH", path, map[string]any{"name": "不应保存", "event_time": "2026-11-19T21", "round1_target": 4, "round2_target": 5, "player_ids": []int{3, 4, 5, 6, 7, 9}, "rules": map[string]any{"ace_specials": []map[string]any{{"player_ids": []int{8}, "probability": 10}}}}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=8`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM events WHERE id=? AND name='不应保存'`, id)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND is_captain=1`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"name": "完整赛事设置", "description": "更新说明", "event_time": "2026-11-19T21", "round1_target": 4, "round2_target": 5, "player_ids": []int{3, 4, 5, 6, 7, 9}, "fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "9": "B"}, "rules": map[string]any{"kof_starter": "captain", "round1_mode": "random", "ace_specials": []map[string]any{{"player_ids": []int{9}, "probability": 10}}}}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=? AND name='完整赛事设置' AND event_time='2026-11-19T21:00:00' AND round1_target=4 AND round2_target=5`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=9`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND (user_id=8 OR is_captain=1)`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 9}, 200)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": defaultRules()}, 403)
	f.request(t, 9, "PATCH", path, map[string]any{"player_ids": []int{}, "rules": defaultRules()}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM event_players WHERE event_id=?`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 0}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=? AND manager_id=1`, id)
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
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=? AND status='round2'`, id)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 4}, 400)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 5}, 400)
	f.request(t, 7, "POST", vote, map[string]any{"target_id": 6}, 200)
	starter := fmt.Sprintf("/api/events/%d/kof/starter", id)
	f.request(t, 4, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 4}, 400)
	f.request(t, 5, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 4}, 200)
	f.request(t, 6, "POST", starter, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
}
