package app

import (
	"fmt"
	"testing"
)

func TestPermanentlyDeleteActiveTournamentCascadesRelations(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	otherID := f.create(t, "random")
	if _, err := f.app.db.Exec(`UPDATE tournaments SET manager_id=3 WHERE id=?`, otherID); err != nil {
		t.Fatal(err)
	}
	firstMatch := f.enterKOF(t, id)
	winner := int(f.scalar(t, `SELECT player_a_id FROM matches WHERE id=?`, firstMatch))
	teams := f.teams(t, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/kof/starter", id), map[string]any{"team_id": teams[0], "player_id": 3}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/kof/starter", id), map[string]any{"team_id": teams[1], "player_id": 6}, 200)
	matchID := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND status='pending'`, id)
	if _, err := f.app.db.Exec(`INSERT INTO captain_votes(tournament_id,team_id,voter_id,target_id) VALUES(?,?,?,?)`, id, teams[0], 4, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`INSERT INTO kof_reservations(tournament_id,match_id,team_id,player_id) VALUES(?,?,?,?)`, id, matchID, teams[0], 4); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"tournament_players", "tournament_setup_roster", "captain_votes", "lineup_entries", "rounds", "matches", "tournament_actions", "player_selections", "kof_reservations"} {
		if count := f.scalar(t, `SELECT COUNT(*) FROM `+table+` WHERE tournament_id=?`, id); count == 0 {
			t.Fatalf("删除前%s应含有赛事关联数据", table)
		}
	}
	path := fmt.Sprintf("/api/tournaments/%d/permanent", id)
	f.request(t, 3, "DELETE", path, nil, 403)
	f.request(t, 2, "DELETE", fmt.Sprintf("/api/tournaments/%d/permanent", otherID), nil, 403)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=?`, id)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=?`, otherID)
	if got := f.records(t, 0, winner); got.Overall != (recordScore{Wins: 1}) {
		t.Fatalf("删除前应已有一场胜局: %+v", got)
	}
	f.request(t, 2, "DELETE", path, nil, 200)
	if got := f.records(t, 0, winner); got.Overall != (recordScore{}) || len(got.ByCategory) != 0 {
		t.Fatalf("永久删除后仍有战绩: %+v", got)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournaments WHERE id=?`, id)
	for _, table := range []string{"tournament_players", "tournament_setup_roster", "captain_votes", "lineup_entries", "rounds", "matches", "tournament_actions", "player_selections", "kof_reservations"} {
		f.assertCount(t, 0, `SELECT COUNT(*) FROM `+table+` WHERE tournament_id=?`, id)
	}
	f.request(t, 0, "GET", fmt.Sprintf("/api/public/tournaments/%d", id), nil, 404)
}

func TestPermanentlyDeleteRejectsClosedTournaments(t *testing.T) {
	for _, status := range []string{statusCompleted, statusCancelled} {
		t.Run(status, func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := f.create(t, "captain")
			if _, err := f.app.db.Exec(`UPDATE tournaments SET status=? WHERE id=?`, status, id); err != nil {
				t.Fatal(err)
			}
			f.request(t, 2, "DELETE", fmt.Sprintf("/api/tournaments/%d/permanent", id), nil, 400)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=?`, id)
			f.assertCount(t, 6, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
		})
	}
}
