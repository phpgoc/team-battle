package app

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

func TestHomeVisibilityDependsOnRosterAndManagement(t *testing.T) {
	f := newWorkflowFixture(t)
	for count := 0; count <= 6; count++ {
		createSetupTournament(t, f, count, "random", "random", nil)
	}
	for _, actor := range []int{0, 1, 2, 3, 9} {
		var list struct {
			Tournaments []struct {
				ID          int64
				PlayerCount int `json:"player_count"`
			} `json:"tournaments"`
		}
		if err := json.Unmarshal(f.request(t, actor, "GET", "/api/public/tournaments", nil, 200), &list); err != nil {
			t.Fatal(err)
		}
		want := 1
		if actor == 1 || actor == 2 {
			want = 7
		}
		if len(list.Tournaments) != want {
			t.Fatalf("身份%d应看到%d场，实际%d", actor, want, len(list.Tournaments))
		}
		if list.Tournaments[0].PlayerCount != 6 {
			t.Fatal("未分队人数未计入摘要")
		}
		for count := 0; count <= 6; count++ {
			id := int64(count + 1)
			want := 404
			if count == 6 || actor == 1 || actor == 2 {
				want = 200
			}
			f.request(t, actor, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, want)
			page := 404
			if count == 6 {
				page = 200
			} else if actor == 1 || actor == 2 {
				page = 302
			}
			f.request(t, actor, "GET", fmt.Sprintf("/live/%d", id), nil, page)
			if count < 6 {
				f.request(t, actor, "GET", fmt.Sprintf("/api/public/tournaments/%d/stream", id), nil, 404)
			}
		}
	}
}

func TestUngroupedSSEPublishesGroupingAndHidesShrunkRoster(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "random", "random", nil)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/public/tournaments/%d/stream", server.URL, id), nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("分队前SSE不可用: %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	read := func() map[string]any {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var v map[string]any
				if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &v); err != nil {
					t.Fatal(err)
				}
				return v
			}
		}
	}
	initial := read()
	if initial["public_ready"] != true || len(initial["setup_roster"].([]any)) != 6 {
		t.Fatal("未分队公开名单不完整")
	}
	// 名单不足时不再推送新的私有内容，通知全部观众退出。
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7}}, 200)
	if next := read(); next["unavailable"] != true || len(next) != 1 {
		t.Fatal("撤回公开范围时泄露了赛事内容", next)
	}
	f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 404)
	// 重新补全名单后可以订阅，正式分组能在同一公开快照中读取。
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}}, 200)
	_, updates, leave, err := f.app.subscribeTournament(id)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	select {
	case update := <-updates:
		var view struct{ Teams []struct{ Players []any } }
		if err := json.Unmarshal(update.payload, &view); err != nil {
			t.Fatal(err)
		}
		if !update.publicReady || len(view.Teams) != 2 || len(view.Teams[0].Players) != 3 || len(view.Teams[1].Players) != 3 {
			t.Fatal("分组结果没有公开推送")
		}
	default:
		t.Fatal("分组后没有推送")
	}
}
