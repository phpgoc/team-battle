package migrations

import (
	"context"
	"database/sql"
)

func upExtraInfo(_ context.Context, tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE site_extra_infos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		options_json TEXT NOT NULL,
		icon_data BLOB NOT NULL DEFAULT X'',
		icon_mime TEXT NOT NULL DEFAULT '',
		sort_order INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return err
	}
	_, err := tx.Exec(`ALTER TABLE matches ADD COLUMN extra_info_json TEXT NOT NULL DEFAULT '{}'`)
	return err
}
