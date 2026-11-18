package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
)

func TestHistoryPlayerFilterUsesCachedParticipants(t *testing.T) {
	f := newWorkflowFixture(t)
	archive := func(name, date string, roster []int) int64 {
		t.Helper()
		assignments := map[string]string{}
		for index, id := range roster {
			side := "A"
			if index >= 3 {
				side = "B"
			}
			assignments[fmt.Sprint(id)] = side
		}
		var result struct{ ID int64 }
		if err := json.Unmarshal(f.request(t, 1, "POST", "/api/tournaments/archive", map[string]any{
			"name": name, "start_time": date, "player_ids": roster, "fixed_assignments": assignments,
			"round1_target": 1, "round2_target": 1,
			"matches": []archivedMatch{{1, int64(roster[0]), int64(roster[3]), "a"}, {2, int64(roster[0]), int64(roster[3]), "a"}},
		}, 200), &result); err != nil {
			t.Fatal(err)
		}
		return result.ID
	}
	first := archive("选手筛选甲", "2026-11-17T20", []int{3, 4, 5, 6, 7, 8})
	second := archive("选手筛选乙", "2026-11-18T20", []int{3, 4, 5, 6, 7, 9})
	archive("其他赛事", "2026-11-19T20", []int{2, 4, 5, 6, 7, 9})
	type listing struct {
		Tournaments []struct{ ID int64 } `json:"tournaments"`
		Players     []map[string]any     `json:"players"`
		Total       int                  `json:"total"`
	}
	read := func(query string) listing {
		t.Helper()
		var result listing
		if err := json.Unmarshal(f.request(t, 0, "GET", "/api/public/history?"+query, nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	// 热缓存后仍应按参赛资格筛选，包括未实际出场的队员。
	read("")
	for _, player := range []int{3, 4} {
		result := read(fmt.Sprintf("player_id=%d&page_size=1", player))
		want := 2
		if player == 4 {
			want = 3
		}
		if result.Total != want || len(result.Tournaments) != 1 || len(result.Players) != 8 {
			t.Fatalf("参赛选手筛选及候选列表错误: player=%d %+v", player, result)
		}
	}
	result := read("player_id=3&page=2&page_size=1")
	if result.Total != 2 || len(result.Tournaments) != 1 || result.Tournaments[0].ID != first {
		t.Fatalf("选手筛选后分页错误: %+v", result)
	}
	query := url.Values{"player_id": {"3"}, "name": {"选手筛选"}, "from": {"2026-11-18"}, "to": {"2026-11-18"}}
	result = read(query.Encode())
	if result.Total != 1 || result.Tournaments[0].ID != second {
		t.Fatalf("选手、赛事名与开始日期组合筛选错误: %+v", result)
	}
	if result = read("player_id=999999"); result.Total != 0 || len(result.Tournaments) != 0 || len(result.Players) != 8 {
		t.Fatalf("空结果不能清空筛选候选: %+v", result)
	}
	for _, player := range result.Players {
		if _, leaked := player["username"]; leaked {
			t.Fatal("公开筛选候选不得包含登录名")
		}
	}
	for _, raw := range []string{"bad", "-1", "9223372036854775808"} {
		f.request(t, 0, "GET", "/api/public/history?player_id="+raw, nil, 400)
	}
	// 导入新历史后使缓存失效，候选及总数立即更新。
	archive("新导入", "2026-11-20T20", []int{3, 4, 5, 6, 7, 8})
	if result = read("player_id=3"); result.Total != 3 {
		t.Fatalf("新增历史后筛选缓存未更新: %+v", result)
	}
}
