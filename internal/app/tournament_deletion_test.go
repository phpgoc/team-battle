package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type deletionList struct {
	Tournaments []struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	} `json:"tournaments"`
	Players []struct{ ID int64 } `json:"players"`
	Total   int                  `json:"total"`
}

func (f *workflowFixture) deletionList(t *testing.T, actor int, path string) deletionList {
	t.Helper()
	var list deletionList
	if err := json.Unmarshal(f.request(t, actor, "GET", path, nil, 200), &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestTournamentSoftDeletionPermissionsAndRecovery(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	teams := f.teams(t, id)
	matchID := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2`, id)
	path := fmt.Sprintf("/api/tournaments/%d", id)
	f.request(t, 2, "POST", fmt.Sprintf("%s/matches/%d/result", path, matchID), map[string]any{"winner_team_id": teams[0]}, 200)
	f.request(t, 2, "POST", path+"/confirm", nil, 200)
	before, err := f.app.buildTournament(id, false)
	if err != nil {
		t.Fatal(err)
	}
	records := f.records(t, 0, 3)
	// 先加载缓存，删除、恢复都应立即反映到首页、筛选候选和战绩。
	if f.deletionList(t, 0, "/api/public/history").Total != 1 {
		t.Fatal("历史缓存未建立")
	}
	for _, method := range []string{"DELETE", "POST"} {
		target := path
		if method == "POST" {
			target += "/restore"
		}
		f.request(t, 0, method, target, nil, 401)
		for _, actor := range []int{2, 3, 9} {
			f.request(t, actor, method, target, nil, 403)
		}
	}
	f.request(t, 0, "GET", "/tournaments", nil, 302)
	f.request(t, 3, "GET", "/tournaments", nil, 403)
	f.request(t, 1, "GET", "/tournaments", nil, 200)
	f.request(t, 1, "DELETE", "/api/tournaments/99999", nil, 404)
	f.request(t, 1, "POST", "/api/tournaments/99999/restore", nil, 404)
	f.request(t, 1, "DELETE", "/api/tournaments/bad", nil, 400)
	if _, err := f.app.db.Exec(`CREATE TRIGGER fail_record_removal BEFORE DELETE ON player_records BEGIN SELECT RAISE(ABORT,'模拟战绩更新失败'); END`); err != nil {
		t.Fatal(err)
	}
	f.request(t, 1, "DELETE", path, nil, 500)
	f.assertCount(t, 0, `SELECT is_deleted FROM tournaments WHERE id=?`, id)
	if !reflect.DeepEqual(records, f.records(t, 0, 3)) {
		t.Fatal("战绩更新失败后，原战绩应保持一致")
	}
	if _, err := f.app.db.Exec(`DROP TRIGGER fail_record_removal`); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		f.request(t, 1, "DELETE", path, nil, 200)
	}
	if list := f.deletionList(t, 0, "/api/public/history"); list.Total != 0 || len(list.Players) != 0 {
		t.Fatalf("默认历史仍含已删除赛事: %+v", list)
	}
	if list := f.deletionList(t, 0, "/api/public/history?include_deleted=1&player_id=3&name=首发&from=2026-11-17&to=2026-11-17&page_size=1"); list.Total != 1 || len(list.Tournaments) != 1 || !list.Tournaments[0].Deleted || len(list.Players) != 6 {
		t.Fatalf("显式筛选未保留已删除赛事: %+v", list)
	}
	if list := f.deletionList(t, 1, "/api/tournaments"); len(list.Tournaments) != 1 || !list.Tournaments[0].Deleted {
		t.Fatal("管理列表不能恢复已删除赛事")
	}
	if list := f.deletionList(t, 2, "/api/tournaments"); len(list.Tournaments) != 0 {
		t.Fatal("普通账号列表不应默认含已删除赛事")
	}
	if got := f.records(t, 0, 3); got.Overall != (recordScore{}) || got.TeamBattle != (recordScore{}) {
		t.Fatal("删除赛事仍纳入战绩", got)
	}
	f.request(t, 0, "GET", fmt.Sprintf("/live/%d", id), nil, 200)
	f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d/stream", id), nil, 204)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action='tournament_deleted'`, id)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM matches WHERE tournament_id=?`, id)
	// 原有阶段、名单、规则、比分必须保留，恢复也不能解锁已结束赛事。
	for n := 0; n < 2; n++ {
		f.request(t, 1, "POST", path+"/restore", nil, 200)
	}
	after, err := f.app.buildTournament(id, false)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("软删除改变了赛事原始内容: %v", err)
	}
	if f.deletionList(t, 0, "/api/public/history").Total != 1 || !reflect.DeepEqual(records, f.records(t, 0, 3)) {
		t.Fatal("恢复未刷新历史或战绩")
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action='tournament_restored'`, id)
	f.request(t, 1, "PATCH", path+"/settings", map[string]any{"name": "不能修改"}, 400)
}

func TestDeletionFiltersBeforePaginationAndKeepsPrivateDraftsHidden(t *testing.T) {
	f := newWorkflowFixture(t)
	for i := 0; i < 3; i++ {
		id := createSetupTournament(t, f, 6, "random", "random", nil)
		if _, err := f.app.db.Exec(`UPDATE tournaments SET status='completed',start_time=? WHERE id=?`, fmt.Sprintf("2026-11-%02dT20:00:00", 10+i), id); err != nil {
			t.Fatal(err)
		}
	}
	f.deletionList(t, 0, "/api/public/history")
	f.request(t, 1, "DELETE", "/api/tournaments/3", nil, 200)
	for _, test := range []struct {
		query string
		id    int64
		total int
	}{
		{"page=1", 2, 2}, {"page=2", 1, 2}, {"page=1&include_deleted=1", 3, 3},
		{"page=2&include_deleted=1", 2, 3}, {"page=1&include_deleted=1&from=2026-11-12", 3, 1},
	} {
		list := f.deletionList(t, 0, "/api/public/history?page_size=1&"+test.query)
		if list.Total != test.total || len(list.Tournaments) != 1 || list.Tournaments[0].ID != test.id {
			t.Fatalf("删除筛选应在分页前完成: %s %+v", test.query, list)
		}
	}
	id := createSetupTournament(t, f, 0, "random", "random", nil)
	f.request(t, 1, "DELETE", fmt.Sprintf("/api/tournaments/%d", id), nil, 400)
	for _, actor := range []int{0, 3, 9} {
		if list := f.deletionList(t, actor, "/api/public/tournaments?include_deleted=1"); len(list.Tournaments) != 0 {
			t.Fatal("筛选绕过了人数可见性限制")
		}
		f.request(t, actor, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 404)
	}
	for _, actor := range []int{1, 2} {
		f.request(t, actor, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200)
	}
}

func TestOngoingTournamentsCannotBeDeletedOrRestored(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	path := fmt.Sprintf("/api/tournaments/%d", id)
	for _, status := range []string{statusDraft, statusCaptainVote, statusLineup, statusRound1, statusRound2, statusAce, statusFinalConfirm} {
		if _, err := f.app.db.Exec(`UPDATE tournaments SET status=? WHERE id=?`, status, id); err != nil {
			t.Fatal(err)
		}
		f.request(t, 1, "DELETE", path, nil, 400)
		f.request(t, 1, "POST", path+"/restore", nil, 400)
		f.assertCount(t, 0, `SELECT is_deleted FROM tournaments WHERE id=?`, id)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action IN ('tournament_deleted','tournament_restored')`, id)
	// 取消的赛事也已关闭，删除和恢复仍保持取消状态。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET status='cancelled' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 1, "DELETE", path, nil, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='cancelled' AND is_deleted=1`, id)
	f.request(t, 1, "POST", path+"/restore", nil, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='cancelled' AND is_deleted=0`, id)
}
