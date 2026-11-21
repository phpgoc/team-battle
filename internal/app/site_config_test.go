package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func readSiteDefaults(t *testing.T, f *workflowFixture) tournamentDefaults {
	t.Helper()
	var settings struct {
		Defaults tournamentDefaults `json:"tournament_defaults"`
	}
	if err := json.Unmarshal(f.request(t, 1, "GET", "/api/site-settings", nil, 200), &settings); err != nil {
		t.Fatal(err)
	}
	return settings.Defaults
}

func TestSiteDefaultsPersistAcrossInitialization(t *testing.T) {
	f := newWorkflowFixture(t)
	if got := readSiteDefaults(t, f); got != (tournamentDefaults{4, 5, "slide"}) {
		t.Fatalf("初始网站默认时长错误: %+v", got)
	}
	f.assertCount(t, 2, `SELECT COUNT(*) FROM config WHERE (key='random_draw_seconds' AND value='4') OR (key='ace_draw_seconds' AND value='5')`)
	f.request(t, 1, "PATCH", "/api/site-settings/tournament-defaults", map[string]any{"random_draw_seconds": 7, "ace_draw_seconds": 9}, 200)
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	if got := readSiteDefaults(t, f); got != (tournamentDefaults{7, 9, "slide"}) {
		t.Fatalf("重复初始化覆盖了已保存配置: %+v", got)
	}
	// 单项更新保留另一项，显式零秒也应保存。
	f.request(t, 1, "PATCH", "/api/site-settings/tournament-defaults", map[string]any{"random_draw_seconds": 0}, 200)
	if got := readSiteDefaults(t, f); got != (tournamentDefaults{0, 9, "slide"}) {
		t.Fatalf("部分更新未保留配置: %+v", got)
	}
}

func TestSiteDefaultsRejectUnauthorizedAndInvalidUpdates(t *testing.T) {
	f := newWorkflowFixture(t)
	path := "/api/site-settings/tournament-defaults"
	f.request(t, 0, "PATCH", path, map[string]any{"random_draw_seconds": 8}, 401)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 8}, 403)
	for _, body := range []any{
		nil, map[string]any{}, map[string]any{"random_draw_seconds": nil},
		map[string]any{"random_draw_seconds": 8, "ace_draw_seconds": -1},
		map[string]any{"random_draw_seconds": -1, "ace_draw_seconds": 8},
		map[string]any{"random_draw_seconds": 8, "ace_draw_seconds": 1.5},
		map[string]any{"random_draw_seconds": "8"},
	} {
		f.request(t, 1, "PATCH", path, body, 400)
		if got := readSiteDefaults(t, f); got != (tournamentDefaults{4, 5, "slide"}) {
			t.Fatalf("非法请求部分写入了配置: %+v, 请求: %+v", got, body)
		}
	}
}

func TestSiteDrawAnimationDefaultIsValidatedAndSnapshotted(t *testing.T) {
	f := newWorkflowFixture(t)
	f.request(t, 1, "PATCH", "/api/site-settings/tournament-defaults", map[string]any{"draw_animation": "gradient"}, 200)
	if got := readSiteDefaults(t, f); got.DrawAnimation != "gradient" {
		t.Fatalf("网站默认抽签动画未保存: %+v", got)
	}
	id := f.create(t, "random")
	if rules := mustTournamentRules(f.app.db, id); rules.DrawAnimation != "gradient" {
		t.Fatalf("新赛事未保存抽签动画快照: %+v", rules)
	}
	f.request(t, 1, "PATCH", "/api/site-settings/tournament-defaults", map[string]any{"draw_animation": "unknown"}, 400)
	if got := readSiteDefaults(t, f); got.DrawAnimation != "gradient" {
		t.Fatalf("非法动画设置覆盖了已有值: %+v", got)
	}
}

func TestSiteDefaultsOnlyApplyToNewTournaments(t *testing.T) {
	f := newWorkflowFixture(t)
	history := f.create(t, "random")
	f.enterKOF(t, history)
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, history)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", history, match), map[string]any{"winner_team_id": f.teams(t, history)[0]}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/confirm", history), nil, 200)
	ongoing := f.create(t, "random")
	legacy := f.create(t, "random")
	if _, err := f.app.db.Exec(`UPDATE tournaments SET rules_json='{}' WHERE id=?`, legacy); err != nil {
		t.Fatal(err)
	}
	snapshots := map[int64]string{}
	for _, id := range []int64{history, ongoing, legacy} {
		var raw string
		if err := f.app.db.QueryRow(`SELECT rules_json FROM tournaments WHERE id=?`, id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		snapshots[id] = raw
	}
	f.request(t, 1, "PATCH", "/api/site-settings/tournament-defaults", map[string]any{"random_draw_seconds": 7, "ace_draw_seconds": 9}, 200)
	for id, before := range snapshots {
		var after string
		if err := f.app.db.QueryRow(`SELECT rules_json FROM tournaments WHERE id=?`, id).Scan(&after); err != nil || after != before {
			t.Fatalf("已有赛事%d被网站配置改写: %v", id, err)
		}
		want := 4
		if id == legacy {
			want = 5
		}
		if rules := mustTournamentRules(f.app.db, id); rules.RandomDrawSeconds != want || rules.AceDrawSeconds != 5 {
			t.Fatalf("已有赛事%d的缺省解释发生变化: %+v", id, rules)
		}
	}
	for _, test := range []struct {
		rules any
		want  tournamentDefaults
	}{
		{nil, tournamentDefaults{7, 9, "slide"}},
		{map[string]any{}, tournamentDefaults{7, 9, "slide"}},
		{map[string]any{"round1_mode": "random"}, tournamentDefaults{7, 9, "slide"}},
		{map[string]any{"random_draw_seconds": 0}, tournamentDefaults{0, 9, "slide"}},
		{map[string]any{"ace_draw_seconds": 0}, tournamentDefaults{7, 0, "slide"}},
		{map[string]any{"random_draw_seconds": 2, "ace_draw_seconds": 3}, tournamentDefaults{2, 3, "slide"}},
	} {
		body := map[string]any{"name": "网站默认值验证", "start_time": "2026-11-19T20", "manager_id": 2}
		if test.rules != nil {
			body["rules"] = test.rules
		}
		var result struct{ ID int64 }
		if err := json.Unmarshal(f.request(t, 1, "POST", "/api/tournaments", body, 200), &result); err != nil {
			t.Fatal(err)
		}
		rules := mustTournamentRules(f.app.db, result.ID)
		if got := (tournamentDefaults{rules.RandomDrawSeconds, rules.AceDrawSeconds, rules.DrawAnimation}); got != test.want {
			t.Fatalf("新赛事时长错误: %+v, 期望: %+v, 请求: %+v", got, test.want, test.rules)
		}
	}
}
