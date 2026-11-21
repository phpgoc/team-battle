package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSettingsLockAtFormalStart(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	f.request(t, 4, "PATCH", path, map[string]any{"rules": defaultRules()}, 403)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND status='finished'`, id)
	before, err := f.app.publicTournamentPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int{1, 2} {
		f.request(t, actor, "PATCH", path, map[string]any{"name": "不允许改名", "start_time": "2026-11-19T20", "round1_target": 8, "round2_target": 8, "rules": defaultRules()}, 400)
		f.request(t, actor, "POST", fmt.Sprintf("/api/tournaments/%d/players", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 9}}, 400)
	}
	after, err := f.app.publicTournamentPayload(id)
	if err != nil || string(before) != string(after) {
		t.Fatal("正式开始但尚未录分时，设置或阵容仍可被修改")
	}
}

func TestUnifiedTournamentSettingsAtomicRoster(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	fixed := map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}
	f.request(t, 2, "PATCH", path, map[string]any{"rules": map[string]any{"round1_mode": "manual", "kof_starter": "captain", "no_repeat": true}, "player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": fixed}, 200)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND is_captain=1`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"name": "管理员改名"}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"manager_id": 9}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 2}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": map[string]any{"round1_mode": "manual", "kof_starter": "random", "no_repeat": true}}, 400)
	// 后续校验失败时，之前的删除、分队、资料与规则变更全部回滚。
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 1, "round2_target": 1, "player_ids": []int{3, 4, 5, 6, 7, 9}, "rules": map[string]any{"round1_mode": "manual", "kof_starter": "captain", "no_repeat": true, "ace_specials": []map[string]any{{"player_ids": []int{8}, "probability": 10}}}}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=8`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE id=? AND name='管理员改名'`, id)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND is_captain=1`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": map[string]any{"kof_starter": "captain", "round1_mode": "manual", "no_repeat": true, "ace_most_wins": 30, "ace_specials": []map[string]any{{"player_ids": []int{8}, "probability": 10}}}}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND name='首发验证' AND start_time='2026-11-17T19:00:00' AND round1_target=1 AND round2_target=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=8`, id)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND is_captain=1`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 9}, 400)
	f.request(t, 4, "PATCH", path, map[string]any{"rules": defaultRules()}, 403)
	f.request(t, 9, "PATCH", path, map[string]any{"player_ids": []int{}, "rules": map[string]any{"round1_mode": "manual", "kof_starter": "captain", "no_repeat": true}}, 403)
	f.assertCount(t, 6, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
	f.request(t, 1, "PATCH", path, map[string]any{"manager_id": 0}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND manager_id=2`, id)
}

func TestBasicInfoIsSuperadminOnlyBeforeSetupDraw(t *testing.T) {
	f := newWorkflowFixture(t)
	raw := f.request(t, 1, "POST", "/api/tournaments", map[string]any{
		"name": "基本信息权限", "description": "初始说明", "start_time": "2026-11-17T19", "manager_id": 2,
		"player_ids":        []int{3, 4, 5, 6, 7, 8},
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
	}, 200)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	id := created.ID
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	f.request(t, 2, "PATCH", path, map[string]any{"name": "管理员不能改"}, 403)
	f.request(t, 1, "PATCH", path, map[string]any{
		"name": "超管修改", "description": "抽签前更新", "start_time": "2026-11-18T20", "manager_id": 9,
	}, 200)
	f.request(t, 9, "PATCH", path, map[string]any{"description": "管理员不能改说明"}, 403)
	f.request(t, 1, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	f.request(t, 1, "PATCH", path, map[string]any{"name": "抽签后不能改"}, 400)
	f.request(t, 9, "PATCH", path, map[string]any{"start_time": "2026-11-19T20"}, 403)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND name='超管修改' AND description='抽签前更新' AND start_time='2026-11-18T20:00:00' AND manager_id=9`, id)
}

func TestSavingUnchangedTournamentSettingsDoesNotWriteAudit(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	var name, description, startTime string
	var round1Target, round2Target int
	if err := f.app.db.QueryRow(`SELECT name,description,start_time,round1_target,round2_target FROM tournaments WHERE id=?`, id).Scan(&name, &description, &startTime, &round1Target, &round2Target); err != nil {
		t.Fatal(err)
	}
	var actionCount int
	if err := f.app.db.QueryRow(`SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=?`, id).Scan(&actionCount); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "PATCH", path, map[string]any{
		"name": name, "description": description, "start_time": startTime,
		"round1_target": round1Target, "round2_target": round2Target,
		"background_id": 0, "rules": mustTournamentRules(f.app.db, id),
	}, 200)
	var afterCount int
	if err := f.app.db.QueryRow(`SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=?`, id).Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if afterCount != actionCount {
		t.Fatalf("原样保存完整设置快照不应新增审计记录: before=%d after=%d", actionCount, afterCount)
	}
}

func TestSettingsAuditContainsOnlyChangedValues(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	f.request(t, 2, "PATCH", path, map[string]any{"rules": map[string]any{"ace_most_wins": 31}}, 200)
	var payload string
	if err := f.app.db.QueryRow(`SELECT payload FROM tournament_actions WHERE tournament_id=? AND action='tournament_settings_updated' ORDER BY id DESC LIMIT 1`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var changes map[string]any
	if err := json.Unmarshal([]byte(payload), &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("只修改赛事名称时应只记录名称差异: %s", payload)
	}
	ruleChanges, ok := changes["rules"].(map[string]any)
	if !ok || len(ruleChanges) != 1 {
		t.Fatalf("只修改一项规则时应只记录该规则: %s", payload)
	}
	probability, ok := ruleChanges["ace_most_wins"].(map[string]any)
	if !ok || probability["from"] != float64(20) || probability["to"] != float64(31) {
		t.Fatalf("规则差异缺少旧值和新值: %s", payload)
	}
	rules := mustTournamentRules(f.app.db, id)
	if rules.AceMostWins != 31 || rules.KOFStarter != "captain" || rules.Round1Mode != "manual" {
		t.Fatalf("局部规则保存应保留未提交的设置: %+v", rules)
	}
}

func TestRoundAndAceSettingsUseDifferentSetupStages(t *testing.T) {
	f := newWorkflowFixture(t)
	raw := f.request(t, 1, "POST", "/api/tournaments", map[string]any{"name": "分阶段设置", "start_time": "2026-11-19T20", "manager_id": 2}, 200)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	id := created.ID
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	rules := defaultRules()
	rules.Round1Mode = "random"
	rules.KOFStarter = "captain"
	f.request(t, 2, "PATCH", path, map[string]any{"name": "分阶段设置", "start_time": "2026-11-19T20", "round1_target": 4, "round2_target": 5, "rules": rules}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/players", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND round1_target=4 AND round2_target=5`, id)
	rules.AceMostWins = 35
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 200)
	if got := mustTournamentRules(f.app.db, id); got.AceMostWins != 35 || got.KOFStarter != "captain" {
		t.Fatalf("分组后、首场前应允许修改大将战规则并保留KOF规则: %+v", got)
	}
	rules.Round1Mode = "manual"
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 3}, 400)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
	rules = mustTournamentRules(f.app.db, id)
	rules.AceMostWins = 40
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 400)
}

func TestCaptainlessVotingAndLiveCaptainPermissions(t *testing.T) {
	f := newWorkflowFixture(t)
	id, teams, _ := prepareSignupKOF(t, f)
	vote := fmt.Sprintf("/api/tournaments/%d/captain-votes", id)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 6}, 400)
	f.request(t, 9, "POST", vote, map[string]any{"target_id": 5}, 400)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 5}, 200)
	confirm := fmt.Sprintf("/api/tournaments/%d/captains/confirm", id)
	f.request(t, 4, "POST", confirm, map[string]any{"team_a_player_id": 5, "team_b_player_id": 0}, 403)
	f.request(t, 2, "POST", confirm, map[string]any{"team_a_player_id": 5, "team_b_player_id": 0}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='round2'`, id)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 4}, 400)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 5}, 400)
	f.request(t, 7, "POST", vote, map[string]any{"target_id": 6}, 200)
	starter := fmt.Sprintf("/api/tournaments/%d/kof/starter", id)
	f.request(t, 4, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 4}, 400)
	f.request(t, 5, "POST", starter, map[string]any{"team_id": teams[0], "player_id": 4}, 200)
	f.request(t, 6, "POST", starter, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
}
