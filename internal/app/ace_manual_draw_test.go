package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAceDrawRequiresManualConfirmation(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	teams := f.teams(t, id)
	draw := fmt.Sprintf("/api/events/%d/ace/draw", id)
	f.request(t, 2, "POST", draw, nil, 400)
	f.enterKOF(t, id)
	match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=2`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[1]}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='ace'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE event_id=? AND stage='ace' AND status='pending'`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=3`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM event_actions WHERE event_id=? AND action='ace_drawn'`, id)
	f.request(t, 0, "POST", draw, nil, 403)
	f.request(t, 3, "POST", draw, nil, 403)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/ace/confirm", id), map[string]any{"player_a_id": 3, "player_b_id": 6}, 400)
	f.request(t, 2, "POST", draw, nil, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=3 AND status='pending'`, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, 1, "POST", draw, nil, 400)
	after, _ := f.app.publicEventPayload(id)
	if string(before) != string(after) {
		t.Fatal("重复抽签改变了赛事")
	}
	ace := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=3`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, ace), map[string]any{"winner_team_id": teams[0]}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/confirm", id), nil, 200)
	f.request(t, 1, "POST", draw, nil, 400)
}

func TestKOFResultCorrectionClearsAceDraw(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	teams := f.teams(t, id)
	match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=2`, id)
	result := fmt.Sprintf("/api/events/%d/matches/%d/result", id, match)
	draw := fmt.Sprintf("/api/events/%d/ace/draw", id)
	f.request(t, 2, "POST", result, map[string]any{"winner_team_id": teams[1]}, 200)
	f.request(t, 2, "POST", draw, nil, 200)
	f.request(t, 2, "PATCH", result, map[string]any{"winner_team_id": teams[0]}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='final_confirm'`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=3`, id)
	// 保留审计轨迹，但已失效的大将抽签来源不能继续显示。
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_actions WHERE event_id=? AND action='ace_drawn'`, id)
	assertNoAceSource := func() {
		t.Helper()
		raw := f.request(t, 0, "GET", fmt.Sprintf("/api/public/events/%d", id), nil, 200)
		var view map[string]any
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatal(err)
		}
		if view["ace_draw_source"] != nil || view["ace_draw"] != nil {
			t.Fatalf("仍公开已失效的抽签来源: %s", raw)
		}
	}
	assertNoAceSource()
	f.request(t, 2, "POST", draw, nil, 400)
	f.request(t, 1, "PATCH", result, map[string]any{"winner_team_id": teams[1]}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='ace'`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=3`, id)
	assertNoAceSource()
	f.request(t, 1, "POST", draw, nil, 200)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM event_actions WHERE event_id=? AND action='ace_drawn'`, id)
	recorded, err := f.app.loadAceDrawResult(id)
	if err != nil || recorded == nil || recorded.MatchID != f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=3`, id) {
		t.Fatalf("重抽后没有绑定新的大将对局: %+v %v", recorded, err)
	}
}
