package main

import "testing"

func TestDemoDataHasConsistentCompleteMatches(t *testing.T) {
	f := newWorkflowFixture(t)
	if err := seedTestData(f.app.db); err != nil {
		t.Fatal(err)
	}
	// 再次初始化不覆盖已有比赛和账号。
	if err := seedTestData(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 10, `SELECT COUNT(*) FROM users WHERE username LIKE 'test-%'`)
	f.assertCount(t, 3, `SELECT COUNT(*) FROM events WHERE status='completed'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE status='round1'`)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM matches WHERE round_no=3 AND status='finished'`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches m WHERE NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=m.event_id AND p.user_id=m.player_a_id AND p.team_id=m.team_a_id) OR NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=m.event_id AND p.user_id=m.player_b_id AND p.team_id=m.team_b_id)`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM rounds r WHERE score_a<>(SELECT COUNT(*) FROM matches m WHERE m.round_id=r.id AND m.status='finished' AND m.winner_team_id=m.team_a_id) OR score_b<>(SELECT COUNT(*) FROM matches m WHERE m.round_id=r.id AND m.status='finished' AND m.winner_team_id=m.team_b_id)`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches current JOIN matches previous ON previous.round_id=current.round_id AND previous.seq=current.seq-1 WHERE current.round_no=2 AND ((previous.winner_team_id=previous.team_a_id AND previous.player_a_id<>current.player_a_id) OR (previous.winner_team_id=previous.team_b_id AND previous.player_b_id<>current.player_b_id))`)
}
