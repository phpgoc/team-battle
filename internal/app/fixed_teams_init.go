package app

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// 开发期旧结构转换：保留原始数据和自增上限，移除固定A/B队的独立表。
func initializeFixedTeams(db *sql.DB) error {
	var legacy int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`).Scan(&legacy); err != nil {
		return err
	}
	if legacy == 0 {
		return nil
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 队名固定，只有双方提交状态需要保留；旧队伍ID只用于本次关联转换。
	if _, err := tx.Exec(`CREATE TEMP TABLE fixed_team_map AS SELECT t.id AS old_id,t.tournament_id*2-CASE WHEN t.id=(SELECT MIN(id) FROM teams WHERE tournament_id=t.tournament_id) THEN 1 ELSE 0 END AS new_id FROM teams t`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `DROP TABLE IF EXISTS temp.fixed_team_map`)
	if _, err := tx.Exec(`UPDATE tournaments SET lineup_a_submitted=COALESCE((SELECT lineup_submitted FROM teams WHERE id=(SELECT MIN(id) FROM teams WHERE tournament_id=tournaments.id)),0),lineup_b_submitted=COALESCE((SELECT lineup_submitted FROM teams WHERE id=(SELECT MAX(id) FROM teams WHERE tournament_id=tournaments.id)),0)`); err != nil {
		return err
	}
	// 重建父表时暂时卸下关联触发器，避免SQLite校验尚未恢复的父表名称。
	triggerRows, err := tx.Query(`SELECT name,sql FROM sqlite_master WHERE type='trigger' AND tbl_name IN ('tournament_players','captain_votes','lineup_entries','rounds','matches','player_selections','kof_reservations')`)
	if err != nil {
		return err
	}
	type trigger struct{ name, sql string }
	var triggers []trigger
	for triggerRows.Next() {
		var item trigger
		if err := triggerRows.Scan(&item.name, &item.sql); err != nil {
			triggerRows.Close()
			return err
		}
		triggers = append(triggers, item)
	}
	if err := triggerRows.Err(); err != nil {
		triggerRows.Close()
		return err
	}
	triggerRows.Close()
	for _, item := range triggers {
		if _, err := tx.Exec(`DROP TRIGGER ` + quoteSQLName(item.name)); err != nil {
			return err
		}
	}
	for _, table := range []string{"tournament_players", "captain_votes", "lineup_entries", "rounds", "matches", "player_selections", "kof_reservations"} {
		if err := rebuildWithoutTeamTable(tx, table); err != nil {
			return fmt.Errorf("转换%s: %w", table, err)
		}
	}
	if _, err := tx.Exec(`DROP TABLE teams`); err != nil {
		return err
	}
	for _, item := range triggers {
		if _, err := tx.Exec(item.sql); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	invalid := rows.Next()
	rows.Close()
	if invalid {
		return fmt.Errorf("固定队伍转换后关联校验失败")
	}
	return tx.Commit()
}

func rebuildWithoutTeamTable(tx *sql.Tx, table string) error {
	var schema string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&schema); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger') AND sql IS NOT NULL`, table)
	if err != nil {
		return err
	}
	var extras []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		extras = append(extras, statement)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	columns, err := tx.Query(`PRAGMA table_info(` + quoteSQLName(table) + `)`)
	if err != nil {
		return err
	}
	var names, values []string
	for columns.Next() {
		var cid, notNull, pk int
		var name, typ string
		var def sql.NullString
		if err := columns.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			columns.Close()
			return err
		}
		names = append(names, quoteSQLName(name))
		value := quoteSQLName(name)
		if name == "team_id" || name == "team_a_id" || name == "team_b_id" || name == "winner_team_id" {
			value = `(SELECT new_id FROM fixed_team_map WHERE old_id=` + quoteSQLName(table) + `.` + value + `)`
		}
		values = append(values, value)
	}
	if err := columns.Err(); err != nil {
		columns.Close()
		return err
	}
	columns.Close()
	var highWater int64
	if err := tx.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name=?`, table).Scan(&highWater); err != nil && err != sql.ErrNoRows {
		return err
	}
	// 原表其余列、约束、索引和触发器均保留，只移除指向teams的外键。
	schema = regexp.MustCompile(`(?i)FOREIGN KEY\([^)]*\)\s+REFERENCES\s+teams\(id\),?`).ReplaceAllString(schema, "")
	schema = regexp.MustCompile(`(?i)REFERENCES\s+teams\(id\)`).ReplaceAllString(schema, "")
	schema = regexp.MustCompile(`,\s*\)`).ReplaceAllString(schema, ")")
	schema = regexp.MustCompile(`(?i)^CREATE TABLE\s+(?:IF NOT EXISTS\s+)?[^\s(]+`).ReplaceAllString(schema, `CREATE TABLE `+quoteSQLName("fixed_"+table))
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO ` + quoteSQLName("fixed_"+table) + `(` + strings.Join(names, ",") + `) SELECT ` + strings.Join(values, ",") + ` FROM ` + quoteSQLName(table)); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE ` + quoteSQLName(table)); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE ` + quoteSQLName("fixed_"+table) + ` RENAME TO ` + quoteSQLName(table)); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sqlite_sequence SET seq=MAX(seq,?) WHERE name=?`, highWater, table); err != nil {
		return err
	}
	if highWater > 0 {
		if _, err := tx.Exec(`INSERT INTO sqlite_sequence(name,seq) SELECT ?,? WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name=?)`, table, highWater, table); err != nil {
			return err
		}
	}
	for _, statement := range extras {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func quoteSQLName(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
