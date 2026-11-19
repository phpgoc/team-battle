package migrations

import "database/sql"

// 启动时接收旧会话表的数据；后续只用users的一列，不建立会话消息表。
func initializeUserSessions(tx *sql.Tx) error {
	var legacy int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&legacy); err != nil {
		return err
	}
	if legacy != 0 {
		if _, err := tx.Exec(`UPDATE users SET session_token=(SELECT token FROM sessions WHERE user_id=users.id ORDER BY created_at DESC,rowid DESC LIMIT 1) WHERE session_token IS NULL AND EXISTS(SELECT 1 FROM sessions WHERE user_id=users.id)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE sessions`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS users_session_token ON users(session_token) WHERE session_token IS NOT NULL`); err != nil {
		return err
	}
	return nil
}
