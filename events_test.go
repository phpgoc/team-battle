package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicSSEMatchesAcrossIdentitiesAndPushesResults(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	if _, err := f.app.db.Exec(`INSERT INTO backgrounds(name,image_data,image_mime) VALUES(?,?,?)`, "SSE 背景", []byte{1, 2, 3}, "image/png"); err != nil {
		t.Fatal(err)
	}
	backgroundID := f.scalar(t, `SELECT id FROM backgrounds WHERE name='SSE 背景'`)
	if _, err := f.app.db.Exec(`UPDATE events SET background_id=? WHERE id=?`, backgroundID, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	path := fmt.Sprintf("/api/public/events/%d/stream", id)
	connect := func(token string) *bufio.Reader {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			request.AddCookie(&http.Cookie{Name: "tb_session", Value: token})
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") || response.Header.Get("X-Accel-Buffering") != "no" {
			t.Fatalf("公开推送响应无效: %d %v", response.StatusCode, response.Header)
		}
		return bufio.NewReader(response.Body)
	}
	read := func(reader *bufio.Reader) string {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("未收到推送数据: %v", err)
			}
			if strings.HasPrefix(line, "data: ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
	}
	guest, manager := connect(""), connect(f.tokens[2])
	initial := read(guest)
	if initial != read(manager) {
		t.Fatal("访客与赛事管理员必须收到相同的公开推送")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(initial), &payload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload["background_id"], &backgroundID); err != nil || backgroundID == 0 {
		t.Fatalf("初始公开推送应包含赛事背景: %s", initial)
	}
	for _, key := range []string{"actions", "sessions", "kof_reservations", "password_hash"} {
		if _, exists := payload[key]; exists {
			t.Fatalf("公开推送不应含内部字段 %s", key)
		}
	}
	f.request(t, 0, "PATCH", fmt.Sprintf("/api/events/%d/background", id), map[string]any{"background_id": 0}, 200)
	updated := read(guest)
	if updated == initial || updated != read(manager) {
		t.Fatal("切换背景后访客与管理员应收到相同的 SSE 更新")
	}
	var changedBackground struct {
		BackgroundID *int64 `json:"background_id"`
	}
	if err := json.Unmarshal([]byte(updated), &changedBackground); err != nil || changedBackground.BackgroundID != nil {
		t.Fatalf("清除背景后 SSE 应同步清空背景: %s", updated)
	}
	match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND status='pending'`, id)
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
	updated = read(guest)
	if updated == initial || updated != read(manager) {
		t.Fatal("录分后应向两个身份推送相同的新状态")
	}
	var event struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(updated), &event); err != nil || event.Status != statusRound2 {
		t.Fatalf("推送未包含录分后的KOF阶段: %s", updated)
	}
	// 历史详情仍可读取，但已结束赛事不再建立SSE连接。
	match = f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND status='pending'`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/confirm", id), nil, 200)
	f.request(t, 0, "GET", path, nil, http.StatusNoContent)
}
