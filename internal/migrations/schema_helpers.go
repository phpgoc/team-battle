package migrations

import (
	"database/sql"
	"fmt"
)

type sqlRunner interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func ensureColumn(tx *sql.Tx, table, column, definition string) error {
	columns, err := tableColumnNames(tx, table)
	if err != nil || columns[column] {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE ` + quoteSQLName(table) + ` ADD COLUMN ` + quoteSQLName(column) + ` ` + definition)
	return err
}

func checkForeignKeys(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var key int
		if err := rows.Scan(&table, &rowID, &parent, &key); err != nil {
			return err
		}
		return fmt.Errorf("迁移后外键校验失败：表 %s，行 %v，关联表 %s", table, rowID, parent)
	}
	return rows.Err()
}
