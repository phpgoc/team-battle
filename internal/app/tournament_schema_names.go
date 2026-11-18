package app

import (
	"database/sql"
	"fmt"
)

// 开发期名称调整在建表前原位完成；保留ID、数据、自增上限和外键关联。
func initializeTournamentNames(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, names := range [][2]string{
		{"events", "tournaments"},
		{"event_players", "tournament_players"},
		{"event_actions", "tournament_actions"},
		{"event_setup_roster", "tournament_setup_roster"},
		{"site_event_names", "site_tournament_names"},
	} {
		var oldTable, newTable int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, names[0]).Scan(&oldTable); err != nil {
			return err
		}
		if oldTable == 0 {
			continue
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, names[1]).Scan(&newTable); err != nil {
			return err
		}
		if newTable != 0 {
			return fmt.Errorf("旧表%s和新表%s同时存在，请先核对数据", names[0], names[1])
		}
		if _, err := tx.Exec(`ALTER TABLE ` + quoteSQLName(names[0]) + ` RENAME TO ` + quoteSQLName(names[1])); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, table := range tables {
		columns, err := tableColumnNames(tx, table)
		if err != nil {
			return err
		}
		for _, names := range [][2]string{{"event_id", "tournament_id"}, {"event_time", "start_time"}} {
			if !columns[names[0]] {
				continue
			}
			if columns[names[1]] {
				return fmt.Errorf("表%s同时存在旧字段%s和新字段%s，请先核对数据", table, names[0], names[1])
			}
			if _, err := tx.Exec(`ALTER TABLE ` + quoteSQLName(table) + ` RENAME COLUMN ` + quoteSQLName(names[0]) + ` TO ` + quoteSQLName(names[1])); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func tableColumnNames(db sqlRunner, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + quoteSQLName(table) + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}
