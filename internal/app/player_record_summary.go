package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
)

type playerRecordSummary struct {
	overall, ace, team recordScore
	opponents          map[int64][2]int
}

func loadPlayerRecord(db sqlRunner, id int64) (playerRecordSummary, error) {
	var record playerRecordSummary
	var opponents string
	err := db.QueryRow(`SELECT wins,losses,ace_wins,ace_losses,team_wins,team_losses,opponents_json FROM player_records WHERE user_id=?`, id).Scan(
		&record.overall.Wins, &record.overall.Losses, &record.ace.Wins, &record.ace.Losses, &record.team.Wins, &record.team.Losses, &opponents)
	if errors.Is(err, sql.ErrNoRows) {
		record.opponents = make(map[int64][2]int)
		return record, nil
	}
	if err != nil {
		return record, err
	}
	err = json.Unmarshal([]byte(opponents), &record.opponents)
	return record, err
}

// 只计算当前赛事的贡献，以差额更新受影响的选手。
func tournamentRecordContributions(tx *sql.Tx, id int64) (map[int64]playerRecordSummary, error) {
	rows, err := tx.Query(`SELECT user_id,opponent_id,SUM(wins),SUM(losses),SUM(ace_wins),SUM(ace_losses),SUM(team_wins),SUM(team_losses)
		FROM player_record_contributions WHERE tournament_id=? GROUP BY user_id,opponent_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	contributions := make(map[int64]playerRecordSummary)
	for rows.Next() {
		var userID int64
		var opponent sql.NullInt64
		var wins, losses, aceWins, aceLosses, teamWins, teamLosses int
		if err := rows.Scan(&userID, &opponent, &wins, &losses, &aceWins, &aceLosses, &teamWins, &teamLosses); err != nil {
			return nil, err
		}
		record := contributions[userID]
		record.overall.Wins += wins
		record.overall.Losses += losses
		record.ace.Wins += aceWins
		record.ace.Losses += aceLosses
		record.team.Wins += teamWins
		record.team.Losses += teamLosses
		if opponent.Valid {
			if record.opponents == nil {
				record.opponents = make(map[int64][2]int)
			}
			record.opponents[opponent.Int64] = [2]int{wins, losses}
		}
		contributions[userID] = record
	}
	return contributions, rows.Err()
}

func saveTournamentRecordChanges(tx *sql.Tx, id int64, before map[int64]playerRecordSummary) error {
	after, err := tournamentRecordContributions(tx, id)
	if err != nil {
		return err
	}
	// 纠错和删除后，原选手可能完全退出本场赛事的战绩。
	for userID := range before {
		if _, exists := after[userID]; !exists {
			after[userID] = playerRecordSummary{}
		}
	}
	for userID, next := range after {
		previous := before[userID]
		if previous.overall == next.overall && previous.ace == next.ace && previous.team == next.team && maps.Equal(previous.opponents, next.opponents) {
			continue
		}
		record, err := loadPlayerRecord(tx, userID)
		if err != nil {
			return err
		}
		record.accumulate(previous, -1)
		record.accumulate(next, 1)
		if err := savePlayerRecord(tx, userID, record); err != nil {
			return err
		}
	}
	return nil
}

func (r *playerRecordSummary) accumulate(change playerRecordSummary, sign int) {
	r.overall.Wins += sign * change.overall.Wins
	r.overall.Losses += sign * change.overall.Losses
	r.ace.Wins += sign * change.ace.Wins
	r.ace.Losses += sign * change.ace.Losses
	r.team.Wins += sign * change.team.Wins
	r.team.Losses += sign * change.team.Losses
	for opponentID, delta := range change.opponents {
		score := r.opponents[opponentID]
		score[0] += sign * delta[0]
		score[1] += sign * delta[1]
		if score == [2]int{} {
			delete(r.opponents, opponentID)
		} else {
			r.opponents[opponentID] = score
		}
	}
}

func savePlayerRecord(tx *sql.Tx, id int64, record playerRecordSummary) error {
	if record.overall == (recordScore{}) && record.team == (recordScore{}) {
		_, err := tx.Exec(`DELETE FROM player_records WHERE user_id=?`, id)
		return err
	}
	opponents, err := json.Marshal(record.opponents)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO player_records(user_id,wins,losses,ace_wins,ace_losses,team_wins,team_losses,opponents_json)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET wins=excluded.wins,losses=excluded.losses,
		ace_wins=excluded.ace_wins,ace_losses=excluded.ace_losses,team_wins=excluded.team_wins,team_losses=excluded.team_losses,opponents_json=excluded.opponents_json`,
		id, record.overall.Wins, record.overall.Losses, record.ace.Wins, record.ace.Losses, record.team.Wins, record.team.Losses, string(opponents))
	return err
}
