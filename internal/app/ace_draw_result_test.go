package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestAceDrawRecordPersistsAndSurvivesOverride(t *testing.T) {
	for _, source := range []string{"most_wins", "most_losses", "random"} {
		t.Run(source, func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := f.create(t, "random")
			rules := mustEventRules(f.app.db, id)
			rules.AceMostWins, rules.AceMostLosses = 0, 0
			rules.AceSpecials = nil
			if source == "most_wins" {
				rules.AceMostWins = 100
			}
			if source == "most_losses" {
				rules.AceMostLosses = 100
			}
			f.request(t, 2, "PATCH", fmt.Sprintf("/api/events/%d/settings", id), map[string]any{"rules": rules}, 200)
			f.enterKOF(t, id)
			match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=2`, id)
			teams := f.teams(t, id)
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[1]}, 200)
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/ace/draw", id), nil, 200)
			before, err := f.app.loadAceDrawResult(id)
			if err != nil || before == nil || before.Source != source || len(before.CandidateIDs) != 2 || before.MatchID == 0 {
				t.Fatalf("抽签快照缺失: %+v %v", before, err)
			}
			// 非管理员和访客读取相同的公开快照，不包含操作者等内部审计字段。
			for _, user := range []int{0, 3, 1} {
				raw := f.request(t, user, "GET", fmt.Sprintf("/api/public/events/%d", id), nil, 200)
				var view struct {
					Draw aceDrawResult `json:"ace_draw"`
				}
				if err := json.Unmarshal(raw, &view); err != nil || !reflect.DeepEqual(view.Draw, *before) {
					t.Fatalf("公开快照不一致: %s %v", raw, err)
				}
			}
			otherA := int64(3)
			if otherA == before.PlayerAID {
				otherA = 4
			}
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/ace/confirm", id), map[string]any{"player_a_id": otherA, "player_b_id": before.PlayerBID}, 200)
			after, err := f.app.loadAceDrawResult(id)
			before.Adjusted = true
			if err != nil || after == nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("管理员覆盖后丢失原始抽签结果: %+v %v", after, err)
			}
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, before.MatchID), map[string]any{"winner_team_id": teams[0]}, 200)
			f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/confirm", id), nil, 200)
			history, _ := f.app.loadAceDrawResult(id)
			if !reflect.DeepEqual(history, after) {
				t.Fatalf("完赛丢失抽签记录: %+v", history)
			}
		})
	}
}

func TestAceDrawRecordLegacyAndWrongMatch(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=2`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": f.teams(t, id)[1]}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/ace/draw", id), nil, 200)
	for _, test := range []struct {
		payload, source string
		special         int
	}{
		{`{"source":"special:1","rules":["special:1","random_fallback"]}`, "special", 2},
		{`{"rules":["most_wins","most_losses"]}`, "mixed", 0},
		{`{"source":"random_fallback"}`, "random", 0},
	} {
		if _, err := f.app.db.Exec(`UPDATE event_actions SET payload=? WHERE event_id=? AND action='ace_drawn'`, test.payload, id); err != nil {
			t.Fatal(err)
		}
		draw, err := f.app.loadAceDrawResult(id)
		if err != nil || draw == nil || draw.Source != test.source || draw.Special != test.special || len(draw.CandidateIDs) != 0 || draw.PlayerAID != 0 || draw.PlayerBID != 0 {
			t.Fatalf("旧记录不能补造候选与结果: %+v %v", draw, err)
		}
	}
	if _, err := f.app.db.Exec(`UPDATE event_actions SET payload='{"match_id":999999,"source":"most_wins"}' WHERE event_id=? AND action='ace_drawn'`, id); err != nil {
		t.Fatal(err)
	}
	if draw, err := f.app.loadAceDrawResult(id); err != nil || draw != nil {
		t.Fatalf("错误关联了其他对局: %+v %v", draw, err)
	}
}
