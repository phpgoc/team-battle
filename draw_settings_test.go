package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestDrawTimingDefaultsAndMutableSettings(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	rules := mustEventRules(f.app.db, id)
	if rules.RandomDrawSeconds != 3 || rules.AceDrawSeconds != 5 {
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
