package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestDrawDurationsApplyToRoundAndAceSelection(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	var waits []time.Duration
	f.app.drawSleep = func(d time.Duration) { waits = append(waits, d) }
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/events/%d/draw-settings", id), map[string]any{"random_draw_seconds": 2, "ace_draw_seconds": 7}, 200)
	f.enterKOF(t, id)
	match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=2`, id)
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[1]}, 200)
	if want := []time.Duration{2 * time.Second, 2 * time.Second, 7 * time.Second}; !reflect.DeepEqual(waits, want) {
		t.Fatalf("排阵、KOF首发、大将战抽选时间不正确: got %v, want %v", waits, want)
	}
}

func TestDrawTimingDefaultsAndMutableSettings(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	rules := mustEventRules(f.app.db, id)
	if rules.RandomDrawSeconds != 5 || rules.AceDrawSeconds != 5 {
		t.Fatalf("默认抽选时长不正确: %+v", rules)
	}
	path := fmt.Sprintf("/api/events/%d/draw-settings", id)
	f.request(t, 3, "PATCH", path, map[string]any{"random_draw_seconds": 8}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 8, "ace_draw_seconds": 12}, 200)
	rules = mustEventRules(f.app.db, id)
	if rules.RandomDrawSeconds != 8 || rules.AceDrawSeconds != 12 {
		t.Fatalf("抽选时长未保存: %+v", rules)
	}
	f.enterKOF(t, id)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 1, "ace_draw_seconds": 2}, 200)
	rules = mustEventRules(f.app.db, id)
	if rules.RandomDrawSeconds != 1 || rules.AceDrawSeconds != 2 {
		t.Fatalf("进行中抽选时长未保存: %+v", rules)
	}
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/events/%d/settings", id), map[string]any{"name": "不能改", "event_time": "2026-11-18T20", "round1_target": 4, "round2_target": 5, "rules": defaultRules()}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": -1}, 400)
}

func TestEmptyRosterLiveRouteRequiresManagerSettings(t *testing.T) {
	f := newWorkflowFixture(t)
	raw := f.request(t, 1, "POST", "/api/events", map[string]any{
		"name": "待分队赛事", "event_time": "2026-11-17T20", "manager_id": 2,
		"round1_target": 4, "round2_target": 5, "rules": defaultRules(),
	}, 200)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if got := f.request(t, 0, "GET", fmt.Sprintf("/live/%d", created.ID), nil, http.StatusNotFound); len(got) != 0 {
		t.Fatalf("访客不应看到未分队赛事页面: %s", got)
	}
	f.request(t, 2, "GET", fmt.Sprintf("/live/%d", created.ID), nil, http.StatusFound)
	f.request(t, 2, "GET", fmt.Sprintf("/live/%d/setting", created.ID), nil, http.StatusOK)
	f.request(t, 3, "GET", fmt.Sprintf("/live/%d/setting", created.ID), nil, http.StatusForbidden)
}

func TestBothTeamsRequiredBeforeOpeningLivePage(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	path := fmt.Sprintf("/live/%d", id)
	team := f.teams(t, id)[0]
	if _, err := f.app.db.Exec(`UPDATE event_players SET team_id=? WHERE event_id=?`, team, id); err != nil {
		t.Fatal(err)
	}
	// 选手全部放在一队时仍未完成分队，不能把半成品看板公开给访客。
	f.request(t, 0, "GET", path, nil, http.StatusNotFound)
	f.request(t, 0, "GET", fmt.Sprintf("/api/public/events/%d/stream", id), nil, http.StatusNotFound)
	f.request(t, 2, "GET", path, nil, http.StatusFound)
	f.request(t, 2, "GET", path+"/setting", nil, http.StatusOK)
	f.request(t, 3, "GET", path, nil, http.StatusNotFound)
	otherTeam := f.teams(t, id)[1]
	if _, err := f.app.db.Exec(`UPDATE event_players SET team_id=? WHERE event_id=? AND user_id=6`, otherTeam, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 0, "GET", path, nil, http.StatusOK)
	f.request(t, 2, "GET", path, nil, http.StatusOK)
}
