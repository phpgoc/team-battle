package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestAceRecordPriority(t *testing.T) {
	tests := []struct {
		name      string
		mostWins  bool
		candidate aceRecord
		current   aceRecord
		better    bool
	}{
		{name: "多胜先比较胜负差", mostWins: true, candidate: aceRecord{wins: 1, losses: 0}, current: aceRecord{wins: 2, losses: 2}, better: true},
		{name: "多胜相同差值比较胜场", mostWins: true, candidate: aceRecord{wins: 3, losses: 3}, current: aceRecord{wins: 2, losses: 2}, better: true},
		{name: "多败先比较负胜差", mostWins: false, candidate: aceRecord{wins: 0, losses: 1}, current: aceRecord{wins: 2, losses: 2}, better: true},
		{name: "多败相同差值优先较少胜场", mostWins: false, candidate: aceRecord{wins: 2, losses: 2}, current: aceRecord{wins: 3, losses: 3}, better: true},
		{name: "相同战绩进入第二轮抽签", mostWins: true, candidate: aceRecord{wins: 2, losses: 1}, current: aceRecord{wins: 2, losses: 1}, better: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := betterAceRecord(test.candidate, test.current, test.mostWins); got != test.better {
				t.Fatalf("betterAceRecord() = %v, want %v", got, test.better)
			}
		})
	}
}

func TestAceSpecialGroupsAndFullRandomFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		groups []AceSpecial
		source string
		counts []int
		poolA  []int64
		poolB  []int64
	}{
		{"全随机包含特殊候选", []AceSpecial{{PlayerIDs: []int64{3, 4, 5, 6, 7, 8}, Probability: 0}}, "random", []int{3, 3}, []int64{3, 4, 5}, []int64{6, 7, 8}},
		{"其他选手显式录入第二组", []AceSpecial{{PlayerIDs: []int64{3, 6}, Probability: 0}, {PlayerIDs: []int64{4, 5, 7, 8}, Probability: 100}}, "special:1", []int{2, 2}, []int64{4, 5}, []int64{7, 8}},
		{"双侧候选独立且排除对侧记录", []AceSpecial{{TeamAIDs: []int64{3, 4, 8}, TeamBIDs: []int64{5, 6}, Probability: 100}}, "special:0", []int{2, 1}, []int64{3, 4}, []int64{6}},
		{"空侧从本队全随机", []AceSpecial{{TeamAIDs: []int64{3}, TeamBIDs: []int64{}, Probability: 100}}, "special:0", []int{1, 3}, []int64{3}, []int64{6, 7, 8}},
		{"双侧规则不缩小全随机候选", []AceSpecial{{TeamAIDs: []int64{3}, TeamBIDs: []int64{6}, Probability: 0}}, "random", []int{3, 3}, []int64{3, 4, 5}, []int64{6, 7, 8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := f.create(t, "random")
			rules := mustEventRules(f.app.db, id)
			rules.AceMostWins, rules.AceMostLosses = 0, 0
			rules.RandomDrawSeconds, rules.AceDrawSeconds = 0, 0
			rules.AceSpecials = test.groups
			f.request(t, 2, "PATCH", fmt.Sprintf("/api/events/%d/settings", id), map[string]any{"rules": rules}, 200)
			f.enterKOF(t, id)
			match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=2`, id)
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": f.teams(t, id)[1]}, 200)
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/ace/draw", id), nil, 200)
			var payload string
			if err := f.app.db.QueryRow(`SELECT payload FROM event_actions WHERE event_id=? AND action='ace_drawn' ORDER BY id DESC LIMIT 1`, id).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var draw struct {
				Source     string    `json:"source"`
				Counts     []int     `json:"candidate_counts"`
				A          int64     `json:"player_a_id"`
				B          int64     `json:"player_b_id"`
				MatchID    int64     `json:"match_id"`
				Candidates [][]int64 `json:"candidate_ids"`
			}
			if err := json.Unmarshal([]byte(payload), &draw); err != nil {
				t.Fatal(err)
			}
			if draw.Source != test.source || !reflect.DeepEqual(draw.Counts, test.counts) || !containsID(test.poolA, draw.A) || !containsID(test.poolB, draw.B) {
				t.Fatalf("大将战候选池不符合配置: %+v", draw)
			}
			if draw.MatchID != f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=3`, id) || !reflect.DeepEqual(draw.Candidates, [][]int64{test.poolA, test.poolB}) {
				t.Fatalf("没有保存准确的对局与候选快照: %+v", draw)
			}
			view, err := f.app.loadAceDrawResult(id)
			if err != nil || view == nil || !reflect.DeepEqual(view.CandidateIDs, draw.Candidates) || view.PlayerAID != draw.A || view.PlayerBID != draw.B {
				t.Fatalf("公开抽签结果与审计不一致: %+v, %v", view, err)
			}
			if test.name == "空侧从本队全随机" && view.Rules[1] != "random_fallback" {
				t.Fatalf("没有保存空侧的全随机来源: %+v", view)
			}
		})
	}
}

func TestAceSideCandidatesValidationAndRosterCleanup(t *testing.T) {
	f := newWorkflowFixture(t)
	base := func() map[string]any {
		return map[string]any{
			"name": "双侧特殊对战", "event_time": "2026-11-18T20", "manager_id": 2,
			"player_ids": []int64{3, 4, 5, 6, 7, 8},
			"rules":      map[string]any{"ace_specials": []map[string]any{{"team_a_ids": []int64{3}, "team_b_ids": []int64{6, 7}, "probability": 20}}},
		}
	}
	for _, test := range []struct {
		name string
		a, b []int64
	}{
		{"同侧重复", []int64{3, 3}, []int64{6}},
		{"非参赛选手", []int64{3}, []int64{9}},
		{"两侧皆空", []int64{}, []int64{}},
		{"非法ID", []int64{-1}, []int64{6}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := base()
			body["rules"].(map[string]any)["ace_specials"] = []map[string]any{{"team_a_ids": test.a, "team_b_ids": test.b, "probability": 20}}
			f.request(t, 1, "POST", "/api/events", body, 400)
			f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE name='双侧特殊对战'`)
		})
	}
	body := base()
	// 未分组的同一选手可分别列为两侧候选，不能当作同侧重复。
	body["rules"].(map[string]any)["ace_specials"] = []map[string]any{{"team_a_ids": []int64{3, 4}, "team_b_ids": []int64{3, 6}, "probability": 20}}
	f.request(t, 1, "POST", "/api/events", body, 200)
	id := f.scalar(t, `SELECT id FROM tournaments WHERE name='双侧特殊对战'`)
	path := fmt.Sprintf("/api/events/%d", id)
	f.request(t, 3, "PATCH", path+"/settings", map[string]any{"rules": body["rules"]}, 403)
	f.request(t, 2, "PATCH", path+"/settings", map[string]any{"rules": map[string]any{"ace_specials": []map[string]any{{"team_a_ids": []int64{9}, "team_b_ids": []int64{6}, "probability": 20}}}}, 400)
	// 旧阵容接口删除参赛者时，也必须清理两侧名单。
	f.request(t, 2, "POST", path+"/players", map[string]any{"player_ids": []int64{3, 5, 6, 7, 8, 9}}, 200)
	rules := mustEventRules(f.app.db, id)
	if len(rules.AceSpecials) != 1 || !reflect.DeepEqual(rules.AceSpecials[0].TeamAIDs, []int64{3}) || !reflect.DeepEqual(rules.AceSpecials[0].TeamBIDs, []int64{3, 6}) {
		t.Fatalf("修改阵容后双侧候选不正确: %+v", rules.AceSpecials)
	}
}
