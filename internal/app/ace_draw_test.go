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
				Source string `json:"source"`
				Counts []int  `json:"candidate_counts"`
				A      int64  `json:"player_a_id"`
				B      int64  `json:"player_b_id"`
			}
			if err := json.Unmarshal([]byte(payload), &draw); err != nil {
				t.Fatal(err)
			}
			if draw.Source != test.source || !reflect.DeepEqual(draw.Counts, test.counts) || !containsID(test.poolA, draw.A) || !containsID(test.poolB, draw.B) {
				t.Fatalf("大将战候选池不符合配置: %+v", draw)
			}
		})
	}
}
