package app

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func TestHomeSummariesAndCachedHistoryFilters(t *testing.T) {
	f := newWorkflowFixture(t)
	if err := seedTestData(f.app.db); err != nil {
		t.Fatal(err)
	}
	type summary struct {
		Name        string `json:"name"`
		PlayerCount int    `json:"player_count"`
		WinsA       int    `json:"round_wins_a"`
		WinsB       int    `json:"round_wins_b"`
	}
	type listing struct {
		Tournaments []summary `json:"tournaments"`
		Total       int       `json:"total"`
	}
	read := func(path string) listing {
		t.Helper()
		var result listing
		if err := json.Unmarshal(f.request(t, 0, "GET", path, nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	// 尚未确认结束的赛事均属于正在进行，包含分队、选队长和排阵阶段。
	live := read("/api/public/tournaments")
	if len(live.Tournaments) != 9 {
		t.Fatalf("实时赛事摘要有误: %+v", live)
	}
	var foundLive bool
	for _, tournament := range live.Tournaments {
		if tournament.Name == "GSL 直播赛" {
			foundLive = tournament.PlayerCount == 9 && tournament.WinsA == 1 && tournament.WinsB == 0
		}
	}
	if !foundLive {
		t.Fatalf("直播赛事摘要有误: %+v", live)
	}
	history := read("/api/public/history?page_size=1")
	if history.Total != 5 || len(history.Tournaments) != 1 || history.Tournaments[0].Name != "OSL 经典赛" || history.Tournaments[0].WinsA != 2 || history.Tournaments[0].WinsB != 0 {
		t.Fatalf("首轮与KOF获胜后应显示2:0: %+v", history)
	}
	second := read("/api/public/history?page_size=1&page=2")
	if len(second.Tournaments) != 1 || second.Tournaments[0].Name != "MSL KOF 夜" || second.Tournaments[0].PlayerCount != 9 || second.Tournaments[0].WinsA != 2 || second.Tournaments[0].WinsB != 1 {
		t.Fatalf("缓存分页应保留大将战与参赛人数: %+v", second)
	}
	date := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	filtered := read("/api/public/history?" + url.Values{"name": {"MSL KOF 夜"}, "from": {date}, "to": {date}}.Encode())
	if filtered.Total != 1 || len(filtered.Tournaments) != 1 || filtered.Tournaments[0].Name != "MSL KOF 夜" {
		t.Fatalf("缓存应按赛事名称和开始日期筛选: %+v", filtered)
	}
	missing := read("/api/public/history?name=" + url.QueryEscape("不存在的赛事"))
	if missing.Total != 0 || len(missing.Tournaments) != 0 {
		t.Fatalf("无匹配赛事时应返回空列表: %+v", missing)
	}
}
