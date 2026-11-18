package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func createSetupEvent(t *testing.T, f *workflowFixture, count int, round1, kof string, fixed map[string]string) int64 {
	t.Helper()
	ids := []int{}
	for id := 3; id < 3+count; id++ {
		ids = append(ids, id)
	}
	rules := defaultRules()
	rules.Round1Mode = round1
	rules.KOFStarter = kof
	rules.RandomDrawSeconds = 0
	raw := f.request(t, 1, "POST", "/api/events", map[string]any{"name": "连续抽签", "event_time": "2026-11-18T20", "manager_id": 2, "player_ids": ids, "fixed_assignments": fixed, "round1_target": 1, "round2_target": 1, "rules": rules}, 200)
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
					id := createSetupEvent(t, f, count, mode, kof, nil)
					path := fmt.Sprintf("/api/events/%d/setup-draw", id)
					f.assertCount(t, 0, `SELECT COUNT(*) FROM event_players WHERE event_id=?`, id)
					f.assertCount(t, int64(count), `SELECT COUNT(*) FROM event_setup_roster WHERE event_id=?`, id)
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
								f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=? AND team_id=?`, id, step.ExtraA, teams[0])
							}
						case "round1":
							roundCount++
							for side, player := range []int64{step.A, step.B} {
								f.assertCount(t, 1, `SELECT COUNT(*) FROM lineup_entries WHERE event_id=? AND team_id=? AND position=? AND player_id=? AND source='setup_draw'`, id, teams[side], step.Position, player)
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
					f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=?`, id)
					before, _ := f.app.publicEventPayload(id)
					f.request(t, 1, "POST", path, nil, 400)
					after, _ := f.app.publicEventPayload(id)
					if !bytes.Equal(before, after) {
						t.Fatal("重复抽签修改了结果")
					}
					f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
					if mode == "random" {
						f.assertCount(t, 1, `SELECT COUNT(*) FROM matches m WHERE m.event_id=? AND m.player_a_id=(SELECT player_id FROM lineup_entries WHERE event_id=m.event_id AND team_id=m.team_a_id AND position=1) AND m.player_b_id=(SELECT player_id FROM lineup_entries WHERE event_id=m.event_id AND team_id=m.team_b_id AND position=1)`, id)
					}
					f.request(t, 2, "POST", path, nil, 400)
					match := f.scalar(t, `SELECT id FROM matches WHERE event_id=? AND round_no=1`, id)
					f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": teams[0]}, 200)
					if kof == "random" {
						f.assertCount(t, 1, `SELECT COUNT(*) FROM matches m JOIN tournaments e ON e.id=m.event_id WHERE m.event_id=? AND m.round_no=2 AND m.player_a_id=e.kof_initial_a AND m.player_b_id=e.kof_initial_b`, id)
					} else {
						f.assertCount(t, 0, `SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=2`, id)
					}
				})
			}
		}
	}
}

func TestSetupDrawFixedTeamsAndAtomicFailure(t *testing.T) {
	f := newWorkflowFixture(t)
	fixed := map[string]string{"3": "A", "4": "A", "5": "A", "6": "A", "7": "A", "8": "A"}
	id := createSetupEvent(t, f, 6, "random", "random", fixed)
	path := fmt.Sprintf("/api/events/%d/setup-draw", id)
	f.request(t, 2, "POST", path, nil, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM event_players WHERE event_id=?`, id)
	fixed["8"] = "B"
	f.request(t, 2, "PATCH", fmt.Sprintf("/api/events/%d/settings", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}, "fixed_assignments": fixed}, 200)
	f.request(t, 2, "POST", path, nil, 200)
	teams := f.teams(t, id)
	f.assertCount(t, 5, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=?`, id, teams[0])
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=?`, id, teams[1])
	f.assertCount(t, 6, `SELECT COUNT(*) FROM lineup_entries WHERE event_id=?`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/players", id), map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8}}, 400)
}

func TestSetupDraftRulesAndSelectionRollback(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupEvent(t, f, 7, "manual", "captain", nil)
	path := fmt.Sprintf("/api/events/%d/settings", id)
	rules := defaultRules()
	rules.Round1Mode = "random"
	rules.AceSpecials = []AceSpecial{{PlayerIDs: []int64{3, 4}, Probability: 20}}
	f.request(t, 2, "PATCH", path, map[string]any{"round1_target": 4, "round2_target": 5, "rules": rules}, 200)
	f.request(t, 2, "PATCH", path, map[string]any{"player_ids": []int{3, 5, 6, 7, 8, 9}}, 400)
	f.assertCount(t, 7, `SELECT COUNT(*) FROM event_setup_roster WHERE event_id=?`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"player_ids": []int{3, 4, 5, 6, 7, 8, 8}}, 400)
	f.request(t, 2, "PATCH", path, map[string]any{"player_ids": []int{1, 3, 4, 5, 6, 7}}, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM event_players WHERE event_id=?`, id)
	f.request(t, 1, "POST", fmt.Sprintf("/api/events/%d/setup-draw", id), nil, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/lineup/generate", id), nil, 400)
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{}}, 400)
	rules.AceMostWins = 25
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 200)
	before, _ := f.app.publicEventPayload(id)
	rules.Round1Mode = "manual"
	f.request(t, 2, "PATCH", path, map[string]any{"rules": rules}, 400)
	after, _ := f.app.publicEventPayload(id)
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
