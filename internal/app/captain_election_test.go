package app

import (
	"fmt"
	"testing"
)

func TestCaptainElectionAutoConfirmsAtHalf(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/captains/confirm", id), map[string]any{"team_a_player_id": 0, "team_b_player_id": 0}, 200)
	vote := fmt.Sprintf("/api/events/%d/captain-votes", id)
	f.request(t, 3, "POST", vote, map[string]any{"target_id": 3}, 400)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 3}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=(SELECT MIN(id) FROM teams WHERE event_id=?) AND is_captain=1`, id, id)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 3}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=3 AND is_captain=1`, id)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 4}, 400)
	f.request(t, 8, "POST", vote, map[string]any{"target_id": 6}, 200)
	f.request(t, 7, "POST", vote, map[string]any{"target_id": 6}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=6 AND is_captain=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=? AND status='lineup_confirm'`, id)

	// 赛事管理员可以直接指定或更换队长，超级管理员同样使用该接口。
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/captains/confirm", id), map[string]any{"team_a_player_id": 4, "team_b_player_id": 7}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=4 AND is_captain=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=7 AND is_captain=1`, id)
	f.request(t, 1, "POST", fmt.Sprintf("/api/events/%d/captains/confirm", id), map[string]any{"team_a_player_id": 5, "team_b_player_id": 8}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=5 AND is_captain=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=8 AND is_captain=1`, id)
}
