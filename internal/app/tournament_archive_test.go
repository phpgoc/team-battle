package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

// 覆盖6到12人的所有分队人数，非法录入不能留下赛事、名单或对局。
func TestArchiveValidatesTeamSizes(t *testing.T) {
	for total := 6; total <= 12; total++ {
		t.Run(fmt.Sprintf("%d人", total), func(t *testing.T) {
			f := newWorkflowFixture(t)
			ids := make([]int64, total)
			for index := range ids {
				id := int64(index + 3)
				ids[index] = id
				if id > 9 {
					if _, err := f.app.db.Exec(`INSERT INTO users(id,username,display_name,password_hash,role) VALUES(?,?,?,'unused','user')`, id, fmt.Sprint("user", id), fmt.Sprint("选手", id)); err != nil {
						t.Fatal(err)
					}
				}
			}
			for countA := 0; countA <= total; countA++ {
				t.Run(fmt.Sprintf("A%d_B%d", countA, total-countA), func(t *testing.T) {
					fixed := map[string]string{}
					for index, id := range ids {
						side := "B"
						if index < countA {
							side = "A"
						}
						fixed[fmt.Sprint(id)] = side
					}
					body := map[string]any{
						"manager_id": 1, "name": "历史人数校验", "start_time": "2026-11-17T19",
						"player_ids": ids, "fixed_assignments": fixed,
						"round1_target": 1, "round2_target": 1,
						"matches": []archivedMatch{{1, 3, int64(countA + 3), "a"}, {2, 3, int64(countA + 3), "a"}},
					}
					want := 400
					if countA == (total+1)/2 {
						want = 200
					}
					const savedRows = `SELECT (SELECT COUNT(*) FROM tournaments)+(SELECT COUNT(*) FROM tournament_setup_roster)+(SELECT COUNT(*) FROM tournament_players)+(SELECT COUNT(*) FROM rounds)+(SELECT COUNT(*) FROM matches)+(SELECT COUNT(*) FROM tournament_actions)`
					before := f.scalar(t, savedRows)
					raw := f.request(t, 1, "POST", "/api/tournaments/archive", body, want)
					if want == 400 {
						var result struct {
							Error string `json:"error"`
						}
						if err := json.Unmarshal(raw, &result); err != nil || result.Error != "偶数人数须两队相同，奇数人数须A队比B队多1人。" {
							t.Fatalf("应提示分队人数不合法: %s", raw)
						}
						f.assertCount(t, before, savedRows)
						return
					}
					var result struct {
						ID int64 `json:"id"`
					}
					if err := json.Unmarshal(raw, &result); err != nil {
						t.Fatal(err)
					}
					teams := fixedTeamIDs(result.ID)
					f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='completed'`, result.ID)
					f.assertCount(t, int64(countA), `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=?`, result.ID, teams[0])
					f.assertCount(t, int64(total-countA), `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=?`, result.ID, teams[1])
					f.assertCount(t, 2, `SELECT COUNT(*) FROM matches WHERE tournament_id=?`, result.ID)
				})
			}
		})
	}
}

func TestArchiveValidatesTargetsAndKOFBeforeSaving(t *testing.T) {
	f := newWorkflowFixture(t)
	base := func() map[string]any {
		return map[string]any{
			"manager_id": 1, "name": "分轮历史录入", "start_time": "2026-11-17T19",
			"player_ids":        []int64{3, 4, 5, 6, 7, 8},
			"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
			"round1_target":     2, "round2_target": 4,
			"matches": []archivedMatch{
				{1, 3, 6, "a"}, {1, 4, 7, "a"},
				{2, 3, 6, "a"}, {2, 3, 7, "b"}, {2, 4, 7, "a"}, {2, 4, 8, "a"}, {2, 4, 6, "a"},
			},
		}
	}
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"未达到目标", func(body map[string]any) { body["round1_target"] = 3 }},
		{"目标越界", func(body map[string]any) { body["round2_target"] = 100 }},
		{"胜者未留场", func(body map[string]any) { body["matches"].([]archivedMatch)[3].AID = 4 }},
		{"提前复活", func(body map[string]any) { body["matches"].([]archivedMatch)[5].BID = 6 }},
		{"结束后多录", func(body map[string]any) {
			body["matches"] = append(body["matches"].([]archivedMatch), archivedMatch{1, 5, 8, "b"})
		}},
		{"不需要的大将", func(body map[string]any) {
			body["matches"] = append(body["matches"].([]archivedMatch), archivedMatch{3, 3, 6, "a"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := base()
			test.change(body)
			f.request(t, 1, "POST", "/api/tournaments/archive", body, 400)
			f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE name='分轮历史录入'`)
		})
	}
	f.request(t, 0, "POST", "/api/tournaments/archive", base(), 403)
	f.request(t, 3, "POST", "/api/tournaments/archive", base(), 403)
	// 最后一局B队全员已败，目标4高于3人，可选择真实复活选手6。
	f.request(t, 1, "POST", "/api/tournaments/archive", base(), 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE name='分轮历史录入' AND status='completed' AND round1_target=2 AND round2_target=4`)
	f.assertCount(t, 7, `SELECT COUNT(*) FROM matches`)
}

func TestArchiveTiedRoundsRequireAce(t *testing.T) {
	f := newWorkflowFixture(t)
	body := map[string]any{
		"manager_id": 1, "name": "大将决胜历史", "start_time": "2026-11-17T19", "round1_target": 2, "round2_target": 2,
		"player_ids": []int64{3, 4, 5, 6, 7, 8}, "ace_source": "most_losses",
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"matches":           []archivedMatch{{1, 3, 6, "a"}, {1, 4, 7, "a"}, {2, 3, 6, "b"}, {2, 4, 6, "b"}},
	}
	f.request(t, 1, "POST", "/api/tournaments/archive", body, 400)
	body["matches"] = append(body["matches"].([]archivedMatch), archivedMatch{3, 5, 8, "a"})
	f.request(t, 1, "POST", "/api/tournaments/archive", body, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE stage='ace' AND score_a=1 AND score_b=0 AND status='finished'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_actions WHERE action='ace_drawn' AND payload LIKE '%most_losses%'`)
}

func TestImportedTournamentMarkerSurvivesUpgrade(t *testing.T) {
	f := newWorkflowFixture(t)
	normalID := f.create(t, "random")
	raw := f.request(t, 1, "POST", "/api/tournaments/archive", map[string]any{
		"manager_id": 1, "name": "补录标记", "start_time": "2026-11-17T19", "round1_target": 1, "round2_target": 1,
		"player_ids":        []int64{3, 4, 5, 6, 7, 8},
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"matches":           []archivedMatch{{1, 3, 6, "a"}, {2, 3, 6, "a"}},
	}, 200)
	var result struct{ ID int64 }
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT is_imported FROM tournaments WHERE id=?`, result.ID)
	simulateUnversionedDatabase(t, f.app.db)
	// 模拟旧版没有标记的补录数据，重复初始化必须保留对局并准确回填。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET is_imported=0`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrate(f.app.db); err != nil {
			t.Fatal(err)
		}
	}
	f.assertCount(t, 0, `SELECT is_imported FROM tournaments WHERE id=?`, normalID)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM matches WHERE tournament_id=?`, result.ID)
	var view struct {
		Imported bool `json:"is_imported"`
	}
	if err := json.Unmarshal(f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", result.ID), nil, 200), &view); err != nil || !view.Imported {
		t.Fatalf("补录来源未公开: %v", err)
	}
}
