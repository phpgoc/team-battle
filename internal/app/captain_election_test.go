package app

import (
	"fmt"
	"testing"
)

func TestCaptainElectionAutoConfirmsAtHalf(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/captains/confirm", id), map[string]any{"team_a_player_id": 0, "team_b_player_id": 0}, 200)
	vote := fmt.Sprintf("/api/tournaments/%d/captain-votes", id)
	f.request(t, 3, "POST", vote, map[string]any{"target_id": 3}, 400)
	f.request(t, 4, "POST", vote, map[string]any{"target_id": 3}, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=(?*2-1) AND is_captain=1`, id, id)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 3}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=3 AND is_captain=1`, id)
	f.request(t, 5, "POST", vote, map[string]any{"target_id": 4}, 400)
	f.request(t, 8, "POST", vote, map[string]any{"target_id": 6}, 200)
	f.request(t, 7, "POST", vote, map[string]any{"target_id": 6}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=6 AND is_captain=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='lineup_confirm'`, id)

	// 赛事管理员可以直接指定或更换队长，超级管理员同样使用该接口。
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/captains/confirm", id), map[string]any{"team_a_player_id": 4, "team_b_player_id": 7}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=4 AND is_captain=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=7 AND is_captain=1`, id)
	f.request(t, 1, "POST", fmt.Sprintf("/api/tournaments/%d/captains/confirm", id), map[string]any{"team_a_player_id": 5, "team_b_player_id": 8}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=5 AND is_captain=1`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=8 AND is_captain=1`, id)
}

func TestManagerSelectsOneTeamCaptain(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	teams := f.teams(t, id)
	path := fmt.Sprintf("/api/tournaments/%d/captains/select", id)
	request := func(user int, team int64, player, want int) {
		t.Helper()
		f.request(t, user, "POST", path, map[string]any{"team_id": team, "player_id": player}, want)
	}
	request(0, teams[0], 4, 403)
	request(3, teams[0], 4, 403)
	request(2, teams[0], 6, 400)
	request(2, 999999, 4, 400)
	request(2, teams[0], 4, 200)
	request(2, teams[0], 4, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=? AND user_id=4 AND is_captain=1`, id, teams[0])
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=? AND user_id=6 AND is_captain=1`, id, teams[1])
	// 指派立即取得本队排阵权；原队长及队员不能继续排阵或投票。
	f.request(t, 4, "GET", fmt.Sprintf("/api/tournaments/%d/lineup", id), nil, 200)
	f.request(t, 3, "GET", fmt.Sprintf("/api/tournaments/%d/lineup", id), nil, 403)
	f.request(t, 3, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{3, 4, 5}}, 400)
	f.request(t, 4, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{5, 4, 3}}, 200)
	for _, user := range []int{3, 4, 5} {
		f.request(t, user, "POST", fmt.Sprintf("/api/tournaments/%d/captain-votes", id), map[string]any{"target_id": 5}, 400)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM captain_votes WHERE tournament_id=? AND team_id=?`, id, teams[0])
	request(1, teams[0], 0, 200)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=? AND is_captain=1`, id, teams[0])
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND team_id=? AND user_id=6 AND is_captain=1`, id, teams[1])
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
	request(2, teams[1], 7, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND status='round1'`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=7 AND is_captain=1`, id)
	if _, err := f.app.db.Exec(`UPDATE tournaments SET status='completed' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	request(1, teams[0], 5, 400)
	f.assertCount(t, 4, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action='team_captain_selected'`, id)
}
