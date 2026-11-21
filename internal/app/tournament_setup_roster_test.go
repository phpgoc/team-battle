package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type rosterEntryView struct {
	ID          int64  `json:"id"`
	FixedTeam   string `json:"fixed_team"`
	DisplayName string `json:"display_name"`
}

func setupRosterView(t *testing.T, f *workflowFixture, id int64) []rosterEntryView {
	t.Helper()
	var tournament struct {
		Roster []rosterEntryView `json:"setup_roster"`
	}
	if err := json.Unmarshal(f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 200), &tournament); err != nil {
		t.Fatal(err)
	}
	return tournament.Roster
}

func TestSetupRosterPreservesEntryOrderAndChoices(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "random", "random", nil)
	path := fmt.Sprintf("/api/tournaments/%d", id)
	ids := []int64{8, 3, 6, 4, 7, 5}
	fixed := map[string]string{"8": "B", "3": "A"}
	f.request(t, 2, "PATCH", path+"/settings", map[string]any{"player_ids": ids, "fixed_assignments": fixed}, 200)
	want := []rosterEntryView{{8, "B", "选手8"}, {3, "A", "选手3"}, {6, "", "选手6"}, {4, "", "选手4"}, {7, "", "选手7"}, {5, "", "选手5"}}
	assertRoster := func() {
		t.Helper()
		if got := setupRosterView(t, f, id); !reflect.DeepEqual(got, want) {
			t.Fatalf("原始录入被分队或状态变化覆盖: got %+v, want %+v", got, want)
		}
	}
	assertRoster()
	f.request(t, 2, "POST", path+"/setup-draw", nil, 200)
	assertRoster()
	// 抽签后不能删除或重排成员，其他设置保存也不能覆盖原始选择。
	f.request(t, 2, "PATCH", path+"/settings", map[string]any{"player_ids": ids[:5], "fixed_assignments": fixed}, 400)
	f.request(t, 2, "POST", path+"/players", map[string]any{"player_ids": ids[:5]}, 400)
	f.request(t, 2, "PATCH", path+"/settings", map[string]any{"name": "抽签后不能修改", "start_time": "2026-11-18T20"}, 403)
	f.request(t, 2, "POST", path+"/start", nil, 200)
	assertRoster()
	// 选手停用后仍有公开资料，设置页不能因可选账号目录变化而丢失录入行。
	if _, err := f.app.db.Exec(`UPDATE users SET status='disabled' WHERE id=6`); err != nil {
		t.Fatal(err)
	}
	f.app.invalidateCategoryDirectory()
	assertRoster()
}

func TestArchivePreservesOriginalRoster(t *testing.T) {
	f := newWorkflowFixture(t)
	f.request(t, 1, "POST", "/api/tournaments/archive", map[string]any{
		"manager_id": 1, "name": "历史原始录入", "start_time": "2026-11-17T19",
		"player_ids":        []int64{8, 3, 6, 4, 7, 5},
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"matches":           []archivedMatch{{1, 3, 6, "a"}, {2, 4, 7, "a"}},
	}, 200)
	id := f.scalar(t, `SELECT id FROM tournaments WHERE name='历史原始录入'`)
	want := []rosterEntryView{{8, "B", "选手8"}, {3, "A", "选手3"}, {6, "B", "选手6"}, {4, "A", "选手4"}, {7, "B", "选手7"}, {5, "A", "选手5"}}
	if got := setupRosterView(t, f, id); !reflect.DeepEqual(got, want) {
		t.Fatalf("历史录入没有保留顺序和选择: %+v", got)
	}
	f.request(t, 1, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"player_ids": []int64{}}, 400)
}

func TestRosterEditorAssetsEmbedded(t *testing.T) {
	f := newWorkflowFixture(t)
	index := f.request(t, 0, "GET", "/", nil, 200)
	for _, asset := range []string{"roster-editor.js", "roster-editor.css"} {
		path := "/assets/" + asset
		if !bytes.Contains(index, []byte(path)) || len(f.request(t, 0, "GET", path, nil, 200)) == 0 {
			t.Fatalf("正式二进制缺少选手录入资源 %s", path)
		}
	}
}
