package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type playerRecordsResponse struct {
	User       map[string]any   `json:"user"`
	Overall    recordScore      `json:"overall"`
	Ace        recordScore      `json:"ace"`
	TeamBattle recordScore      `json:"team_battle"`
	ByCategory []categoryRecord `json:"by_category"`
}

func (f *workflowFixture) records(t *testing.T, user, player int) playerRecordsResponse {
	t.Helper()
	raw := f.request(t, user, "GET", fmt.Sprintf("/api/public/users/%d/records", player), nil, 200)
	var response playerRecordsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestPlayerRecordsIncludeBothSidesAndOpponentCategories(t *testing.T) {
	f := newWorkflowFixture(t)
	for _, query := range []string{
		`INSERT INTO categories(id,name) VALUES(1,'虫族'),(2,'神族')`,
		`UPDATE users SET category_id=1 WHERE id=6`,
		`UPDATE users SET category_id=2 WHERE id=7`,
	} {
		if _, err := f.app.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	f.app.invalidateCategoryDirectory()
	for _, reversed := range []bool{false, true} {
		assignments := map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}
		matches := []archivedMatch{{1, 3, 6, "a"}, {1, 3, 7, "b"}, {1, 4, 6, "a"}, {2, 3, 6, "b"}, {3, 3, 8, "a"}}
		if reversed {
			assignments = map[string]string{"3": "B", "4": "B", "5": "B", "6": "A", "7": "A", "8": "A"}
			matches = []archivedMatch{{1, 6, 3, "b"}, {2, 7, 3, "a"}, {3, 8, 3, "a"}}
		}
		f.request(t, 1, "POST", "/api/events/archive", map[string]any{
			"name": "战绩统计", "start_time": "2026-11-17T19", "player_ids": []int{3, 4, 5, 6, 7, 8},
			"fixed_assignments": assignments, "matches": matches,
		}, 200)
	}
	got := f.records(t, 0, 3)
	if got.Overall != (recordScore{3, 4}) || got.Ace != (recordScore{1, 1}) {
		t.Fatalf("总战绩或大将战绩不正确: %+v", got)
	}
	if got.TeamBattle != (recordScore{1, 1}) {
		t.Fatalf("A/B双方参赛的团战胜负统计错误: %+v", got.TeamBattle)
	}
	if reserve := f.records(t, 0, 5); reserve.TeamBattle != (recordScore{1, 1}) || reserve.Overall != (recordScore{}) {
		t.Fatalf("未实际出场的正式队员也应计入团战胜负: %+v", reserve)
	}
	want := []categoryRecord{
		{CategoryID: 0, recordScore: recordScore{1, 1}},
		{CategoryID: 1, Name: "虫族", recordScore: recordScore{2, 1}},
		{CategoryID: 2, Name: "神族", recordScore: recordScore{0, 2}},
	}
	if !reflect.DeepEqual(got.ByCategory, want) {
		t.Fatalf("对手分类战绩不正确: got %+v, want %+v", got.ByCategory, want)
	}
	if self := f.records(t, 3, 3); !reflect.DeepEqual(got, self) {
		t.Fatalf("本人和访客看到的战绩必须相同: guest %+v, self %+v", got, self)
	}
	for _, private := range []string{"username", "password_hash", "role", "status", "can_delete"} {
		if _, exists := got.User[private]; exists {
			t.Fatalf("公开资料包含账号设置字段: %s", private)
		}
	}
	// 对手更换分类后，下一次查询直接按当前内存目录重新归类。
	if _, err := f.app.db.Exec(`UPDATE users SET category_id=2 WHERE id=6`); err != nil {
		t.Fatal(err)
	}
	f.app.invalidateCategoryDirectory()
	got = f.records(t, 0, 3)
	if len(got.ByCategory) != 2 || got.ByCategory[1].recordScore != (recordScore{2, 3}) {
		t.Fatalf("未使用当前分类汇总: %+v", got.ByCategory)
	}
}

func TestPlayerRecordsReflectLiveCorrectionsAndCancellation(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	teams := f.teams(t, id)
	for side, players := range [][]int{{3, 4, 5}, {6, 7, 8}} {
		f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/lineup/submit", id), map[string]any{"team_id": teams[side], "player_ids": players}, 200)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	if got := f.records(t, 0, 3); got.Overall != (recordScore{}) || len(got.ByCategory) != 0 {
		t.Fatalf("待定对局不应计入战绩: %+v", got)
	}
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1`, id)
	result := fmt.Sprintf("/api/events/%d/matches/%d/result", id, match)
	f.request(t, 2, "POST", result, map[string]any{"winner_team_id": teams[0]}, 200)
	if got := f.records(t, 0, 3); got.Overall != (recordScore{1, 0}) || got.Ace != (recordScore{}) {
		t.Fatalf("进行中对局未计入战绩: %+v", got)
	}
	f.request(t, 2, "PATCH", result, map[string]any{"winner_team_id": teams[1]}, 200)
	if got := f.records(t, 0, 3); got.Overall != (recordScore{0, 1}) {
		t.Fatalf("比分修正后统计未更新: %+v", got)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/cancel", id), nil, 200)
	if got := f.records(t, 0, 3); got.Overall != (recordScore{}) {
		t.Fatalf("取消赛事不应计入战绩: %+v", got)
	}
}

func TestPlayerRecordsUnavailableAndEmptyAccounts(t *testing.T) {
	f := newWorkflowFixture(t)
	if raw := f.request(t, 0, "GET", "/api/public/users/9/records", nil, 200); !bytes.Contains(raw, []byte(`"by_category":[]`)) {
		t.Fatalf("无战绩账号应返回空数组: %s", raw)
	}
	f.request(t, 0, "GET", "/api/public/users/999/records", nil, 404)
	f.request(t, 0, "GET", "/api/public/users/invalid/records", nil, 400)
	f.request(t, 1, "DELETE", "/api/users/9", nil, 200)
	f.request(t, 0, "GET", "/api/public/users/9/records", nil, 404)
}

func TestTeamBattleRecordsWaitForConfirmation(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	teams := f.teams(t, id)
	for side, players := range [][]int{{3, 4, 5}, {6, 7, 8}} {
		f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/lineup/submit", id), map[string]any{"team_id": teams[side], "player_ids": players}, 200)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	for _, stage := range []string{stageRound1, stageRound2} {
		match := f.scalar(t, `SELECT m.id FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.tournament_id=? AND r.stage=? AND m.status='pending'`, id, stage)
		f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
		if got := f.records(t, 0, 3); got.TeamBattle != (recordScore{}) {
			t.Fatalf("进行中和待确认结束赛事不能提前计算团战胜负: %+v", got)
		}
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/confirm", id), nil, 200)
	for _, player := range []int{3, 4, 5, 6, 7, 8} {
		want := recordScore{1, 0}
		if player >= 6 {
			want = recordScore{0, 1}
		}
		for read := 0; read < 2; read++ {
			if got := f.records(t, 0, player); got.TeamBattle != want {
				t.Fatalf("确认结束后每位正式队员每场只计一次: player=%d got=%+v want=%+v", player, got.TeamBattle, want)
			}
		}
	}
	for _, observer := range []int{1, 2, 9} {
		if got := f.records(t, 0, observer); got.TeamBattle != (recordScore{}) {
			t.Fatalf("非参赛管理员和观众不计入团战: player=%d %+v", observer, got)
		}
	}
	// 取消赛事即使保留了对局与阶段胜者也不计入。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET status='cancelled' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if got := f.records(t, 0, 3); got.TeamBattle != (recordScore{}) {
		t.Fatalf("取消赛事不能计入团战胜负: %+v", got)
	}
}
