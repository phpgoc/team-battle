package app

import "testing"

func TestArchiveValidatesTargetsAndKOFBeforeSaving(t *testing.T) {
	f := newWorkflowFixture(t)
	base := func() map[string]any {
		return map[string]any{
			"name": "分轮历史录入", "event_time": "2026-11-17T19",
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
			f.request(t, 1, "POST", "/api/events/archive", body, 400)
			f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE name='分轮历史录入'`)
		})
	}
	f.request(t, 0, "POST", "/api/events/archive", base(), 403)
	f.request(t, 3, "POST", "/api/events/archive", base(), 403)
	// 最后一局B队全员已败，目标4高于3人，可选择真实复活选手6。
	f.request(t, 1, "POST", "/api/events/archive", base(), 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE name='分轮历史录入' AND status='completed' AND round1_target=2 AND round2_target=4`)
	f.assertCount(t, 7, `SELECT COUNT(*) FROM matches`)
}

func TestArchiveTiedRoundsRequireAce(t *testing.T) {
	f := newWorkflowFixture(t)
	body := map[string]any{
		"name": "大将决胜历史", "event_time": "2026-11-17T19", "round1_target": 2, "round2_target": 2,
		"player_ids": []int64{3, 4, 5, 6, 7, 8}, "ace_source": "most_losses",
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"matches":           []archivedMatch{{1, 3, 6, "a"}, {1, 4, 7, "a"}, {2, 3, 6, "b"}, {2, 4, 6, "b"}},
	}
	f.request(t, 1, "POST", "/api/events/archive", body, 400)
	body["matches"] = append(body["matches"].([]archivedMatch), archivedMatch{3, 5, 8, "a"})
	f.request(t, 1, "POST", "/api/events/archive", body, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM rounds WHERE stage='ace' AND score_a=1 AND score_b=0 AND status='finished'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_actions WHERE action='ace_drawn' AND payload LIKE '%most_losses%'`)
}
