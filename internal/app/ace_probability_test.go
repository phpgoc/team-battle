package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestSpecialProbabilityRejectsZeroWithoutChangingTournament(t *testing.T) {
	f := newWorkflowFixture(t)
	rules := defaultRules()
	rules.AceMostWins, rules.AceMostLosses = 0, 0
	rules.AceSpecials = []AceSpecial{{TeamAIDs: []int64{3}, TeamBIDs: []int64{6}, Probability: 0}}
	body := map[string]any{"name": "概率校验", "start_time": "2026-11-19T20", "manager_id": 2, "player_ids": []int{3, 4, 5, 6, 7, 8}, "rules": rules}
	f.request(t, 1, "POST", "/api/tournaments", body, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE name='概率校验'`)
	id := f.create(t, "random")
	before, err := f.app.publicTournamentPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"description": "不能部分保存", "rules": rules}, 400)
	after, err := f.app.publicTournamentPayload(id)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("非法概率修改了赛事")
	}
	// 多胜、多败都为零合法，特殊组边界允许1%和100%。
	for _, probability := range []int{1, 100} {
		rules.AceSpecials[0].Probability = probability
		f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"rules": rules}, 200)
		got := mustTournamentRules(f.app.db, id)
		if got.AceMostWins != 0 || got.AceMostLosses != 0 || got.AceSpecials[0].Probability != probability {
			t.Fatalf("合法概率保存错误: %+v", got)
		}
	}
}

func TestLegacyZeroSpecialProbabilityPreservesHistoryAndTiming(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	rules := mustTournamentRules(f.app.db, id)
	rules.AceSpecials = []AceSpecial{{PlayerIDs: []int64{3, 6}, Probability: 0}, {PlayerIDs: []int64{4, 7}, Probability: 10}}
	raw, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`UPDATE tournaments SET rules_json=? WHERE id=?`, string(raw), id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"random_draw_seconds": 4, "ace_draw_seconds": 6}, 200)
	got := mustTournamentRules(f.app.db, id)
	if len(got.AceSpecials) != 2 || got.AceSpecials[0].Probability != 0 || got.AceSpecials[1].Probability != 10 || got.RandomDrawSeconds != 4 || got.AceDrawSeconds != 6 {
		t.Fatalf("旧规则的组号或时长错误: %+v", got)
	}
}
