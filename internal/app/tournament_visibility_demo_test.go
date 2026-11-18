package app

import (
	"database/sql"
	"time"
)

func seedUngroupedSpectatorDemo(tx *sql.Tx, manager, creator int64, players []int64) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tournaments WHERE name='未分队观赛示例'`).Scan(&count); err != nil || count > 0 {
		return err
	}
	id, _, _, err := seedTournamentShell(tx, manager, creator, "未分队观赛示例", time.Now().Format("2006-01-02T15"), statusDraft)
	if err != nil {
		return err
	}
	if err := saveSetupRoster(tx, id, players, nil); err != nil {
		return err
	}
	for index, stage := range []string{stageRound1, stageRound2, stageAce} {
		if _, err := tx.Exec(`INSERT INTO rounds(tournament_id,stage,target_score,status) VALUES(?,?,?,'pending')`, id, stage, []int{4, 5, 1}[index]); err != nil {
			return err
		}
	}
	return nil
}
