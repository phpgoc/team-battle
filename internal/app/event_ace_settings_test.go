package app

import (
	"fmt"
	"reflect"
	"testing"
)

// 设置页调整大将须与时长设置一起提交或回滚，不能改动已结束或失效对局。
func TestAcePlayersSavedWithEventSettings(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	path := fmt.Sprintf("/api/events/%d/settings", id)
	pick := func(matchID, a, b int64) map[string]any {
		return map[string]any{"random_draw_seconds": 9, "ace_players": eventAcePlayers{MatchID: matchID, PlayerAID: a, PlayerBID: b}}
	}
	f.request(t, 2, "PATCH", path, pick(1, 4, 7), 400)
	f.enterKOF(t, id)
	kof := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, kof), map[string]any{"winner_team_id": teams[1]}, 200)
	f.request(t, 2, "PATCH", path, pick(kof, 4, 7), 400)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/ace/draw", id), nil, 200)
	before, err := f.app.loadAceDrawResult(id)
	if err != nil || before == nil {
		t.Fatalf("缺少大将抽签记录: %+v %v", before, err)
	}
	rules := mustEventRules(f.app.db, id)
	for _, user := range []int{0, 3, 9} {
		f.request(t, user, "PATCH", path, pick(before.MatchID, 4, 7), 403)
	}
	for _, test := range []struct {
		name string
		body map[string]any
	}{
		{"另一场对局", pick(kof, 4, 7)},
		{"缺失对局", pick(0, 4, 7)},
		{"对调队伍", pick(before.MatchID, 7, 4)},
		{"未参赛者", pick(before.MatchID, 9, 7)},
		{"非法时长", map[string]any{"random_draw_seconds": -1, "ace_players": eventAcePlayers{before.MatchID, 4, 7}}},
		{"同时修正比分", map[string]any{"random_draw_seconds": 9, "ace_players": eventAcePlayers{before.MatchID, 4, 7}, "correction": eventResultCorrection{MatchID: kof, WinnerTeamID: teams[0]}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.request(t, 2, "PATCH", path, test.body, 400)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND player_a_id=? AND player_b_id=? AND status='pending'`, before.MatchID, before.PlayerAID, before.PlayerBID)
			if got := mustEventRules(f.app.db, id); !reflect.DeepEqual(got, rules) {
				t.Fatalf("非法调整不应保存其他设置: %+v", got)
			}
			f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action='ace_players_overridden'`, id)
		})
	}
	f.request(t, 2, "PATCH", path, pick(before.MatchID, 4, 7), 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND player_a_id=4 AND player_b_id=7 AND status='pending'`, before.MatchID)
	if got := mustEventRules(f.app.db, id); got.RandomDrawSeconds != 9 {
		t.Fatalf("合法调整应一并保存时长: %+v", got)
	}
	before.Adjusted = true
	after, err := f.app.loadAceDrawResult(id)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("调整后应保留原抽签快照: %+v %v", after, err)
	}
	// 超级管理员也可操作；大将战结束后立即拒绝继续调整。
	f.request(t, 1, "PATCH", path, pick(before.MatchID, 5, 8), 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, before.MatchID), map[string]any{"winner_team_id": teams[0]}, 200)
	f.request(t, 2, "PATCH", path, pick(before.MatchID, 4, 7), 400)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/confirm", id), nil, 200)
	f.request(t, 1, "PATCH", path, pick(before.MatchID, 4, 7), 400)
}
