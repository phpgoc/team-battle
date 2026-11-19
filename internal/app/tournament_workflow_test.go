package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

type workflowFixture struct {
	app     *App
	handler http.Handler
	tokens  map[int]string
}

func newWorkflowFixture(t *testing.T) *workflowFixture {
	t.Helper()
	db, err := openDatabase(filepath.Join(t.TempDir(), "tournaments.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { db.Close() })
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 9; id++ {
		role := "user"
		if id == 1 {
			role = "superadmin"
		}
		if _, err := db.Exec(`INSERT INTO users(id,username,display_name,password_hash,role) VALUES(?,?,?,?,?)`, id, fmt.Sprint("user", id), fmt.Sprint("选手", id), "unused", role); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{db: db, rng: rand.New(rand.NewSource(7)), storageDir: t.TempDir()}
	tokens := map[int]string{}
	for id := 1; id <= 9; id++ {
		tokens[id] = fmt.Sprint(id)
		if _, err := db.Exec(`UPDATE users SET session_token=? WHERE id=?`, tokens[id], id); err != nil {
			t.Fatal(err)
		}
	}
	return &workflowFixture{app: app, handler: app.router(), tokens: tokens}
}
func (f *workflowFixture) request(t *testing.T, user int, method, path string, body any, want int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if user > 0 {
		req.AddCookie(&http.Cookie{Name: "tb_session", Value: f.tokens[user]})
	}
	res := httptest.NewRecorder()
	f.handler.ServeHTTP(res, req)
	if res.Code != want {
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, res.Code, res.Body.String())
	}
	// 登录会替换旧会话，后续测试请求和真实浏览器一样采用响应中的新Cookie。
	if path == "/api/auth/login" && res.Code == http.StatusOK {
		var result struct {
			User User `json:"user"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		for _, cookie := range res.Result().Cookies() {
			if cookie.Name == "tb_session" {
				f.tokens[int(result.User.ID)] = cookie.Value
			}
		}
	}
	return res.Body.Bytes()
}
func (f *workflowFixture) create(t *testing.T, mode string) int64 {
	t.Helper()
	raw := f.request(t, 1, "POST", "/api/tournaments", map[string]any{
		"name": "首发验证", "start_time": "2026-11-17T19", "manager_id": 2,
		"team_a_name": "不可设置的队名", "team_b_name": "另一队名",
		"round1_target": 1, "round2_target": 1, "player_ids": []int{3, 4, 5, 6, 7, 8},
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"rules":             map[string]any{"kof_starter": mode, "ace_specials": []map[string]any{{"player_ids": []int{3, 6}, "probability": 10}}},
	}, 200)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", created.ID), nil, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/captains/confirm", created.ID), map[string]any{"team_a_player_id": 3, "team_b_player_id": 6}, 200)
	return created.ID
}
func (f *workflowFixture) teams(t *testing.T, id int64) []int64 {
	t.Helper()
	ids, err := f.app.teamIDs(id)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}
func (f *workflowFixture) scalar(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := f.app.db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func (f *workflowFixture) assertCount(t *testing.T, want int64, query string, args ...any) {
	t.Helper()
	if got := f.scalar(t, query, args...); got != want {
		t.Fatalf("want %d, got %d for %s", want, got, query)
	}
}
func (f *workflowFixture) enterKOF(t *testing.T, id int64) int64 {
	t.Helper()
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), map[string]any{}, 200)
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1`, id)
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
	return match
}

// 队长首发覆盖正常推进、越权、重复提交、错误阶段和管理员代选。
func TestCaptainKOFStarters(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	teams := f.teams(t, id)
	path := fmt.Sprintf("/api/tournaments/%d/kof/starter", id)
	pickA := map[string]any{"team_id": teams[0], "player_id": 3}
	f.request(t, 3, "POST", path, pickA, 400)
	first := f.enterKOF(t, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
	f.request(t, 0, "POST", path, pickA, 401)
	f.request(t, 4, "POST", path, pickA, 400)
	f.request(t, 6, "POST", path, pickA, 400)
	f.request(t, 9, "POST", path, pickA, 400)
	f.request(t, 3, "POST", path, map[string]any{"team_id": teams[0], "player_id": 6}, 400)
	f.request(t, 3, "POST", path, pickA, 200)
	f.request(t, 3, "POST", path, pickA, 400)
	f.request(t, 2, "POST", path, map[string]any{"team_id": teams[0], "player_id": 4}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
	raw := f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200)
	var view struct {
		Starters []struct {
			PlayerID int64 `json:"player_id"`
		} `json:"kof_starters"`
	}
	if err := json.Unmarshal(raw, &view); err != nil || len(view.Starters) != 0 {
		t.Fatalf("双方首发就绪前不应公开报名: %s", raw)
	}
	f.request(t, 6, "POST", path, map[string]any{"team_id": teams[1], "player_id": 6}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND player_a_id=4 AND player_b_id=6 AND status='pending'`, id)
	f.request(t, 1, "POST", path, pickA, 400)
	// 修正首轮会清空失效首发，下一次进入KOF必须重新派人。
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, first), map[string]any{"winner_team_id": teams[1]}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM player_selections WHERE tournament_id=?`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
	f.request(t, 1, "POST", path, pickA, 200)
	f.request(t, 6, "POST", path, map[string]any{"team_id": teams[1], "player_id": 7}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
}
func TestRandomKOFAndHistoryRules(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	teams := f.teams(t, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND status='pending'`, id)
	f.request(t, 1, "POST", fmt.Sprintf("/api/tournaments/%d/kof/starter", id), map[string]any{"team_id": teams[0], "player_id": 3}, 400)
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	result := fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, match)
	f.request(t, 2, "POST", result, map[string]any{"winner_team_id": teams[0]}, 200)
	f.request(t, 2, "POST", result, map[string]any{"winner_team_id": teams[0]}, 400)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/confirm", id), map[string]any{}, 200)
	raw := f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200)
	var view struct {
		Status string          `json:"status"`
		Rules  TournamentRules `json:"rules"`
		Time   string          `json:"start_time"`
		Teams  []struct {
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != "completed" || view.Rules.KOFStarter != "random" || len(view.Rules.AceSpecials) != 1 || view.Rules.AceSpecials[0].Probability != 10 {
		t.Fatalf("历史规则丢失: %s", raw)
	}
	if view.Time != "2026-11-17T19:00:00" || view.Teams[0].Name != "A 队" || view.Teams[1].Name != "B 队" {
		t.Fatalf("时间或固定队名不正确: %s", raw)
	}
	f.request(t, 1, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"team_a_name": "篡改"}, 400)
}
func TestTournamentHourAndFixedNames(t *testing.T) {
	for _, input := range []string{"2026-11-17T08", "2026-11-17T08:39", "2026-11-17T08:39:56", "2026-11-17 08:39:56", "2026-11-17T08:39:56+08:00"} {
		got, err := normalizeStartTime(input)
		if err != nil || got != "2026-11-17T08:00:00" {
			t.Fatalf("%s => %s, %v", input, got, err)
		}
	}
	if _, err := normalizeStartTime("2026-11-17T25"); err == nil {
		t.Fatal("无效小时被接受")
	}
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"name": "整点比赛", "start_time": "2026-11-18T21", "round1_target": 1, "round2_target": 1, "team_a_name": "任意队名", "team_b_name": "任意队名2"}, 200)
	view, err := f.app.loadTeams(id)
	if err != nil || len(view) != 2 || view[0]["name"] != "A 队" || view[1]["name"] != "B 队" {
		t.Fatalf("队名必须固定: %+v %v", view, err)
	}
	simulateUnversionedDatabase(t, f.app.db)
	if _, err := f.app.db.Exec(`UPDATE tournaments SET start_time='2026-11-17T08:35:59' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrate(f.app.db); err != nil {
			t.Fatal(err)
		}
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND start_time='2026-11-17T08:00:00'`, id)
	if rules, err := decodeRules(`{"round1_mode":"random"}`); err != nil || rules.KOFStarter != "random" {
		t.Fatal("旧规则未兼容默认随机首发")
	}
	f.request(t, 1, "POST", "/api/tournaments", map[string]any{"name": "非法首发", "start_time": "2026-11-18T20", "rules": map[string]any{"kof_starter": "invalid"}}, 400)
}
