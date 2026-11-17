package app

import (
	"bytes"
	"fmt"
	"testing"
)

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
	f.assertCount(t, 5, `SELECT COUNT(*) FROM events WHERE status='completed'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='特殊对战1' AND id IN (SELECT event_id FROM event_players GROUP BY event_id HAVING COUNT(*)=8)`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='特殊对战2' AND id IN (SELECT event_id FROM event_players GROUP BY event_id HAVING COUNT(*)=8)`)
	specialID := f.scalar(t, `SELECT id FROM events WHERE name='特殊对战1'`)
	detail := f.request(t, 0, "GET", fmt.Sprintf("/api/public/events/%d", specialID), nil, 200)
	if !bytes.Contains(detail, []byte(`"ace_draw_source":"special"`)) || !bytes.Contains(detail, []byte(`"ace_draw_special":1`)) {
		t.Fatalf("历史大将战没有显示特殊抽选来源: %s", detail)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM (SELECT m.player_a_id FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.event_id=? AND r.stage='round1' GROUP BY m.player_a_id HAVING COUNT(*)>1)`, specialID)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE status='round1'`)
	f.assertCount(t, 4, `SELECT COUNT(*) FROM matches WHERE round_no=3 AND status='finished'`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches m WHERE NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=m.event_id AND p.user_id=m.player_a_id AND p.team_id=m.team_a_id) OR NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=m.event_id AND p.user_id=m.player_b_id AND p.team_id=m.team_b_id)`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM rounds r WHERE score_a<>(SELECT COUNT(*) FROM matches m WHERE m.round_id=r.id AND m.status='finished' AND m.winner_team_id=m.team_a_id) OR score_b<>(SELECT COUNT(*) FROM matches m WHERE m.round_id=r.id AND m.status='finished' AND m.winner_team_id=m.team_b_id)`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches current JOIN matches previous ON previous.round_id=current.round_id AND previous.seq=current.seq-1 JOIN events e ON e.id=current.event_id WHERE e.name NOT LIKE '特殊对战%' AND current.round_no=2 AND ((previous.winner_team_id=previous.team_a_id AND previous.player_a_id<>current.player_a_id) OR (previous.winner_team_id=previous.team_b_id AND previous.player_b_id<>current.player_b_id))`)
}
