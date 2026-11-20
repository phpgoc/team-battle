package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

func createSetupTournament(t *testing.T, f *workflowFixture, count int, round1, kof string, fixed map[string]string) int64 {
	t.Helper()
	ids := []int{}
	for id := 3; id < 3+count; id++ {
		ids = append(ids, id)
	}
	rules := defaultRules()
	rules.Round1Mode = round1
	rules.KOFStarter = kof
	rules.RandomDrawSeconds = 0
	raw := f.request(t, 1, "POST", "/api/tournaments", map[string]any{"name": "连续抽签", "start_time": "2026-11-18T20", "manager_id": 2, "player_ids": ids, "fixed_assignments": fixed, "round1_target": 1, "round2_target": 1, "rules": rules}, 200)
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result.ID
}

func TestSetupDrawCombinationsAndOddRemainder(t *testing.T) {
	for _, count := range []int{6, 7} {
		for _, mode := range []string{"manual", "random"} {
			for _, kof := range []string{"captain", "random"} {
				t.Run(fmt.Sprintf("%d/%s/%s", count, mode, kof), func(t *testing.T) {
					f := newWorkflowFixture(t)
					id := createSetupTournament(t, f, count, mode, kof, nil)
					path := fmt.Sprintf("/api/tournaments/%d/setup-draw", id)
					f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
					f.assertCount(t, int64(count), `SELECT COUNT(*) FROM tournament_setup_roster WHERE tournament_id=?`, id)
					f.request(t, 4, "POST", path, nil, 403)
					f.request(t, 0, "POST", path, nil, 403)
					var result struct {
						Steps []setupDrawStep `json:"steps"`
					}
					if err := json.Unmarshal(f.request(t, 2, "POST", path, nil, 200), &result); err != nil {
						t.Fatal(err)
					}
					teams := f.teams(t, id)
					seen := map[int64]bool{}
					groupCount, roundCount, kofCount := 0, 0, 0
					for _, step := range result.Steps {
						switch step.Kind {
						case "group":
							groupCount++
							for _, player := range []int64{step.A, step.B, step.ExtraA} {
								if player != 0 {
									if seen[player] {
										t.Fatal("抽签重复")
									}
									seen[player] = true
								}
							}
							if step.ExtraA != 0 {
								f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=? AND team_id=?`, id, step.ExtraA, teams[0])
							}
						case "round1":
							roundCount++
							for side, player := range []int64{step.A, step.B} {
								f.assertCount(t, 1, `SELECT COUNT(*) FROM lineup_entries WHERE tournament_id=? AND team_id=? AND position=? AND player_id=? AND source='setup_draw'`, id, teams[side], step.Position, player)
							}
						case "round2":
							kofCount++
							f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND kof_initial_a=? AND kof_initial_b=?`, id, step.A, step.B)
						}
					}
					if groupCount != count/2 || len(seen) != count {
						t.Fatalf("奇数末位必须随末轮进入: %+v", result)
					}
					if (mode == "random" && roundCount != count/2) || (mode == "manual" && roundCount != 0) || (kof == "random" && kofCount != 1) || (kof == "captain" && kofCount != 0) {
						t.Fatalf("抽签步骤不符: %+v", result)
					}
					f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=?`, id)
					before, _ := f.app.publicTournamentPayload(id)
					f.request(t, 1, "POST", path, nil, 400)
					after, _ := f.app.publicTournamentPayload(id)
					if !bytes.Equal(before, after) {
						t.Fatal("重复抽签修改了结果")
					}
					f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
					if mode == "random" {
						f.assertCount(t, 1, `SELECT COUNT(*) FROM matches m WHERE m.tournament_id=? AND m.player_a_id=(SELECT player_id FROM lineup_entries WHERE tournament_id=m.tournament_id AND team_id=m.team_a_id AND position=1) AND m.player_b_id=(SELECT player_id FROM lineup_entries WHERE tournament_id=m.tournament_id AND team_id=m.team_b_id AND position=1)`, id)
					}
					f.request(t, 2, "POST", path, nil, 400)
					match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1`, id)
					f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
					if kof == "random" {
						f.assertCount(t, 1, `SELECT COUNT(*) FROM matches m JOIN tournaments e ON e.id=m.tournament_id WHERE m.tournament_id=? AND m.round_no=2 AND m.player_a_id=e.kof_initial_a AND m.player_b_id=e.kof_initial_b`, id)
					} else {
						f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2`, id)
					}
				})
			}
		}
	}
}

func TestSetupDrawKeepsExtraInfoWithItsMatch(t *testing.T) {
	f := newWorkflowFixture(t)
	options, err := json.Marshal([]string{"地图甲", "地图乙"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.app.db.Exec(`INSERT INTO site_extra_infos(name,options_json) VALUES('地图',?)`, string(options))
	if err != nil {
		t.Fatal(err)
	}
	definitionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	id := createSetupTournament(t, f, 6, "random", "random", nil)
	rules := defaultRules()
	rules.Round1Mode = "random"
	rules.KOFStarter = "random"
	rules.ExtraInfo = map[string][]TournamentExtraInfo{
		stageRound1: {{DefinitionID: definitionID, Name: "地图", Options: []string{"地图甲", "地图乙"}, Selected: []string{"地图乙"}}},
		stageRound2: {{DefinitionID: definitionID, Name: "地图", Options: []string{"地图甲", "地图乙"}, Selected: []string{"地图乙"}}},
	}
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"rules": rules}, 200)
	var draw struct {
		Steps []setupDrawStep `json:"steps"`
	}
	if err := json.Unmarshal(f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200), &draw); err != nil {
		t.Fatal(err)
	}
	for _, step := range draw.Steps {
		if step.Kind == stageRound1 || step.Kind == stageRound2 {
			if step.ExtraInfo["地图"] != "地图乙" {
				t.Fatalf("赛前抽签步骤 %s #%d 未选定额外信息: %+v", step.Kind, step.Position, step.ExtraInfo)
			}
		}
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
	teams := f.teams(t, id)
	firstMatch := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=1 AND status='pending'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND json_extract(extra_info_json,'$."地图"')='地图乙'`, firstMatch)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, firstMatch), map[string]any{"winner_team_id": teams[0]}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE tournament_id=? AND round_no=2 AND json_extract(extra_info_json,'$."地图"')='地图乙'`, id)
}

func TestSetupDrawFixedTeamsAndAtomicFailure(t *testing.T) {
	f := newWorkflowFixture(t)
	fixed := map[string]string{"3": "A", "4": "A", "5": "A", "6": "A", "7": "A", "8": "A"}
	id := createSetupTournament(t, f, 6, "random", "random", fixed)
	path := fmt.Sprintf("/api/tournaments/%d/setup-draw", id)
	f.request(t, 2, "POST", path, nil, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
	fixed["8"] = "B"
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": fixed}, 200)
	f.request(t, 2, "POST", path, nil, 200)
	teams := f.teams(t, id)
	f.assertCount(t, 5, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=?`, id, teams[0])
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=?`, id, teams[1])
	f.assertCount(t, 6, `SELECT COUNT(*) FROM lineup_entries WHERE tournament_id=?`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/players", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}}, 400)
}

func TestSetupDrawSkipsDeterminedTeams(t *testing.T) {
	for _, tc := range []struct {
		name, round1, kof string
		fixed             map[string]string
		kinds             []string
	}{
		{"全员固定", "random", "random", map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"}, []string{"round1", "round1", "round1", "round2"}},
		{"剩余全补B队", "manual", "random", map[string]string{"3": "A", "4": "A", "5": "A"}, []string{"round2"}},
		{"剩余全补A队且后续手动", "manual", "captain", map[string]string{"6": "B", "7": "B", "8": "B"}, nil},
		{"剩余需要随机分到两队", "manual", "random", map[string]string{"3": "A", "4": "A"}, []string{"group", "group", "group", "round2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := createSetupTournament(t, f, 6, tc.round1, tc.kof, tc.fixed)
			path := fmt.Sprintf("/api/tournaments/%d/setup-draw", id)
			var result struct {
				Steps []setupDrawStep `json:"steps"`
			}
			if err := json.Unmarshal(f.request(t, 2, "POST", path, nil, 200), &result); err != nil {
				t.Fatal(err)
			}
			kinds := make([]string, 0, len(result.Steps))
			for _, step := range result.Steps {
				kinds = append(kinds, step.Kind)
			}
			if !slices.Equal(kinds, tc.kinds) {
				t.Fatalf("抽签顺序为 %v，预期 %v", kinds, tc.kinds)
			}
			for _, team := range f.teams(t, id) {
				f.assertCount(t, 3, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=?`, id, team)
			}
		})
	}
}

func TestSetupDraftRulesAndSelectionRollback(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 7, "manual", "captain", nil)
	path := fmt.Sprintf("/api/tournaments/%d/settings", id)
	rules := defaultRules()
	rules.Round1Mode = "random"
	rules.AceSpecials = []AceSpecial{{PlayerIDs: []int64{3, 4}, Probability: 20}}
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 4, "round2_target": 5, "rules": rules}, 200)
	f.request(t, 2, "PATCH", path, map[string]any{"player_ids": []int{3, 5, 6, 7, 8, 9}}, 400)
	f.assertCount(t, 7, `SELECT COUNT(*) FROM tournament_setup_roster WHERE tournament_id=?`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8, 8}}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"player_ids": []int{1, 3, 4, 5, 6, 7}}, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
	f.request(t, 1, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/generate", id), nil, 400)
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{}}, 400)
	rules.AceMostWins = 25
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 200)
	before, _ := f.app.publicTournamentPayload(id)
	rules.Round1Mode = "manual"
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 400)
	after, _ := f.app.publicTournamentPayload(id)
	if !bytes.Equal(before, after) {
		t.Fatal("抽签后修改规则未回滚")
	}
}

func TestSetupAnimationAssetIsEmbedded(t *testing.T) {
	f := newWorkflowFixture(t)
	asset := f.request(t, 0, "GET", "/assets/setup-draw.js", nil, 200)
	if !bytes.Contains(asset, []byte("TBSetupDraw")) {
		t.Fatal("正式二进制缺少赛前抽签动画资源")
	}
	index := f.request(t, 0, "GET", "/", nil, 200)
	if !bytes.Contains(index, []byte("/assets/setup-draw.js")) {
		t.Fatal("页面未加载赛前抽签动画")
	}
}
