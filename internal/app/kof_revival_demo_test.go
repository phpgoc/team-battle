package app

import (
	"database/sql"
	"strconv"
	"time"
)

// 3打3抢5，随机示例已抽出第一名复活选手，手动示例等待队长或队员报名。
func seedKOFRevivalDemo(tx *sql.Tx, managerID, creatorID int64, players []int64, manual bool) error {
	name := "KOF 不重复复活示例"
	if manual {
		name = "KOF 手动复活示例"
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tournaments WHERE name=?`, name).Scan(&count); err != nil || count > 0 {
		return err
	}
	id, teamA, teamB, err := seedTournamentShell(tx, managerID, creatorID, name, time.Now().Format("2006-01-02T15:00:00"), statusRound2)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, id, teamA, teamB, players); err != nil {
		return err
	}
	fixed := map[string]string{}
	for index, player := range players {
		side := "A"
		if index%2 == 1 {
			side = "B"
		}
		fixed[strconv.FormatInt(player, 10)] = side
	}
	if err := saveSetupRoster(tx, id, players, fixed); err != nil {
		return err
	}
	rules := defaultRules()
	rules.KOFRevivalNoRepeat = true
	captainB := players[1]
	if manual {
		rules.KOFStarter = "captain"
		captainB = 0 // B队没有队长，队员可直接报名复活。
	}
	if err := saveRules(tx, id, rules); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE tournament_players SET is_captain=1 WHERE tournament_id=? AND user_id IN (?,?)`, id, players[0], captainB); err != nil {
		return err
	}
	for index, stage := range []string{stageRound1, stageRound2, stageAce} {
		target, score, state, winner := []int{4, 5, 1}[index], []int{4, 3, 0}[index], []string{statusFinished, statusActive, statusPending}[index], int64(0)
		if index == 0 {
			winner = teamA
		}
		result, err := tx.Exec(`INSERT INTO rounds(tournament_id,stage,target_score,score_a,score_b,winner_team_id,status) VALUES(?,?,?,?,0,?,?)`, id, stage, target, score, nullableInt64(winner), state)
		if err != nil {
			return err
		}
		round, _ := result.LastInsertId()
		if index == 2 {
			continue
		}
		for seq := 1; seq <= 4; seq++ {
			a, b, resultWinner, state := players[(seq-1)%3*2], players[(seq-1)%3*2+1], teamA, statusFinished
			if index == 1 {
				a = players[0]
				if seq == 4 {
					if manual {
						break
					}
					resultWinner, state = 0, statusPending
				}
			}
			if _, err := tx.Exec(`INSERT INTO matches(tournament_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,winner_team_id,status,created_by,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,CASE WHEN ?='finished' THEN CURRENT_TIMESTAMP ELSE NULL END)`, id, round, seq, teamA, teamB, a, b, nullableInt64(resultWinner), state, creatorID, state); err != nil {
				return err
			}
		}
	}
	return nil
}
