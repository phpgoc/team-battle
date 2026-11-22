package migrations

import (
	"context"
	"database/sql"
)

func upPlayerRecords(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE player_records (
			user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			wins INTEGER NOT NULL CHECK(wins >= 0),
			losses INTEGER NOT NULL CHECK(losses >= 0),
			ace_wins INTEGER NOT NULL CHECK(ace_wins >= 0),
			ace_losses INTEGER NOT NULL CHECK(ace_losses >= 0),
			team_wins INTEGER NOT NULL CHECK(team_wins >= 0),
			team_losses INTEGER NOT NULL CHECK(team_losses >= 0),
			opponents_json TEXT NOT NULL CHECK(json_type(opponents_json)='object')
		)`,
		`CREATE INDEX matches_finished_by_tournament ON matches(tournament_id) WHERE status='finished'`,
		// 按对手 ID 保存个人单局贡献，由读接口按对手当前分类归类。
		`CREATE VIEW player_record_contributions(tournament_id, user_id, opponent_id, wins, losses, ace_wins, ace_losses, team_wins, team_losses) AS
			SELECT m.tournament_id, m.player_a_id, m.player_b_id,
				m.winner_team_id=m.team_a_id, m.winner_team_id<>m.team_a_id,
				m.round_no IS 3 AND m.winner_team_id=m.team_a_id, m.round_no IS 3 AND m.winner_team_id<>m.team_a_id, 0, 0
			FROM matches m JOIN tournaments t ON t.id=m.tournament_id
			WHERE m.status='finished' AND m.winner_team_id IN (m.team_a_id,m.team_b_id) AND t.status<>'cancelled' AND t.is_deleted=0
			UNION ALL
			SELECT m.tournament_id, m.player_b_id, m.player_a_id,
				m.winner_team_id=m.team_b_id, m.winner_team_id<>m.team_b_id,
				m.round_no IS 3 AND m.winner_team_id=m.team_b_id, m.round_no IS 3 AND m.winner_team_id<>m.team_b_id, 0, 0
			FROM matches m JOIN tournaments t ON t.id=m.tournament_id
			WHERE m.status='finished' AND m.winner_team_id IN (m.team_a_id,m.team_b_id) AND t.status<>'cancelled' AND t.is_deleted=0
			UNION ALL
			SELECT p.tournament_id, p.user_id, NULL, 0, 0, 0, 0,
				COALESCE(SUM(r.winner_team_id=p.team_id)>=2,0), COALESCE(SUM(r.winner_team_id<>p.team_id)>=2,0)
			FROM tournament_players p JOIN tournaments t ON t.id=p.tournament_id JOIN rounds r ON r.tournament_id=t.id
			WHERE t.status='completed' AND t.is_deleted=0 AND p.active=1 AND r.status='finished' AND r.stage IN ('round1','round2','ace')
			GROUP BY p.tournament_id, p.user_id`,
		`WITH by_opponent AS (
			SELECT user_id, opponent_id, SUM(wins) AS wins, SUM(losses) AS losses,
				SUM(ace_wins) AS ace_wins, SUM(ace_losses) AS ace_losses, SUM(team_wins) AS team_wins, SUM(team_losses) AS team_losses
			FROM player_record_contributions GROUP BY user_id, opponent_id
		)
		INSERT INTO player_records(user_id,wins,losses,ace_wins,ace_losses,team_wins,team_losses,opponents_json)
		SELECT user_id, SUM(wins), SUM(losses), SUM(ace_wins), SUM(ace_losses), SUM(team_wins), SUM(team_losses),
			json_group_object(opponent_id,json_array(wins,losses)) FILTER (WHERE opponent_id IS NOT NULL)
		FROM by_opponent GROUP BY user_id HAVING SUM(wins + losses + team_wins + team_losses)>0`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
