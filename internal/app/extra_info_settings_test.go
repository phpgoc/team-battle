package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTournamentExtraInfoIsPublicAndKeepsItsSnapshot(t *testing.T) {
	f := newWorkflowFixture(t)
	icon, err := demoFS.ReadFile("testdata/demo/map.png")
	if err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	for name, value := range map[string]string{"name": "地图", "options": "Circuit Breaker\nDestination"} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	iconPart, err := writer.CreateFormFile("icon", "map.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iconPart.Write(icon); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.multipartRequest(t, 2, http.MethodPost, "/api/site-settings/extra-infos", writer.FormDataContentType(), form.Bytes(), http.StatusForbidden)
	f.multipartRequest(t, 1, http.MethodPost, "/api/site-settings/extra-infos", writer.FormDataContentType(), form.Bytes(), http.StatusOK)
	settings := f.request(t, 1, http.MethodGet, "/api/site-settings", nil, http.StatusOK)
	var catalog struct {
		ExtraInfos []extraInfoDefinition `json:"extra_infos"`
	}
	if err := json.Unmarshal(settings, &catalog); err != nil || len(catalog.ExtraInfos) != 1 {
		t.Fatalf("读取地图类型失败: %s, %v", settings, err)
	}
	definition := catalog.ExtraInfos[0]
	if definition.Name != "地图" || definition.IconURL == "" || len(definition.Options) != 2 {
		t.Fatalf("地图类型数据不完整: %+v", definition)
	}
	iconResponse := f.request(t, 0, http.MethodGet, definition.IconURL, nil, http.StatusOK)
	if !bytes.Equal(iconResponse, icon) {
		t.Fatal("公开地图图标接口没有返回已上传图片")
	}
	players := []int64{3, 4, 5, 6, 7, 8}
	assignments := map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}
	created := f.request(t, 1, http.MethodPost, "/api/tournaments", map[string]any{
		"name": "额外信息赛事", "start_time": "2026-11-20T12", "manager_id": 2,
		"player_ids": players, "fixed_assignments": assignments, "round1_target": 1, "round2_target": 1,
		"rules": map[string]any{
			"round1_mode": "random", "kof_starter": "random",
			"extra_info": map[string]any{"round1": []map[string]any{{"definition_id": definition.ID, "selected": []string{"Destination"}}}},
		},
	}, http.StatusOK)
	var tournament struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created, &tournament); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, http.MethodPost, fmt.Sprintf("/api/tournaments/%d/setup-draw", tournament.ID), nil, http.StatusOK)
	f.request(t, 2, http.MethodPost, fmt.Sprintf("/api/tournaments/%d/captains/confirm", tournament.ID), map[string]any{"team_a_player_id": 3, "team_b_player_id": 6}, http.StatusOK)
	f.request(t, 2, http.MethodPost, fmt.Sprintf("/api/tournaments/%d/start", tournament.ID), nil, http.StatusOK)
	public := f.request(t, 0, http.MethodGet, fmt.Sprintf("/api/public/tournaments/%d", tournament.ID), nil, http.StatusOK)
	iconURL := fmt.Sprintf(`"icon_url":"/api/extra-infos/%d/icon"`, definition.ID)
	if !bytes.Contains(public, []byte(`"extra_info":{"地图":"Destination"}`)) || !bytes.Contains(public, []byte(iconURL)) {
		t.Fatalf("公开赛事缺少地图值或图标快照: %s", public)
	}
	f.request(t, 1, http.MethodDelete, fmt.Sprintf("/api/site-settings/extra-infos/%d", definition.ID), nil, http.StatusConflict)
}

func (f *workflowFixture) multipartRequest(t *testing.T, user int, method, path, contentType string, body []byte, want int) []byte {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if user > 0 {
		req.AddCookie(&http.Cookie{Name: "tb_session", Value: f.tokens[user]})
	}
	res := httptest.NewRecorder()
	f.handler.ServeHTTP(res, req)
	if res.Code != want {
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, res.Code, res.Body.String())
	}
	return res.Body.Bytes()
}
