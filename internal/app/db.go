package app

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
)

// 每条连接都启用外键和锁等待；WAL使SSE读取不会阻塞保存事务提交。
func openDatabase(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(absolute), "/")}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Set("_txlock", "immediate")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	if err := initializeTournamentNames(db); err != nil {
		return err
	}
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS config(key TEXT PRIMARY KEY,value TEXT NOT NULL)`,
		`INSERT OR IGNORE INTO config(key,value) VALUES('random_draw_seconds','4'),('ace_draw_seconds','5')`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('superadmin','user','manager','player')),
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tournaments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			start_time TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft',
			round1_target INTEGER NOT NULL DEFAULT 4,
			round2_target INTEGER NOT NULL DEFAULT 5,
			manager_id INTEGER NOT NULL,
			created_by INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(manager_id) REFERENCES users(id),
			FOREIGN KEY(created_by) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS tournament_players (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tournament_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			team_id INTEGER NOT NULL,
			is_captain INTEGER NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 1,
			UNIQUE(tournament_id, user_id),
			FOREIGN KEY(tournament_id) REFERENCES tournaments(id) ON DELETE CASCADE,
			FOREIGN KEY(user_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS captain_votes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tournament_id INTEGER NOT NULL,
			team_id INTEGER NOT NULL,
			voter_id INTEGER NOT NULL,
			target_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tournament_id, voter_id),
			FOREIGN KEY(tournament_id) REFERENCES tournaments(id) ON DELETE CASCADE,
			FOREIGN KEY(voter_id) REFERENCES users(id),
			FOREIGN KEY(target_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS lineup_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tournament_id INTEGER NOT NULL,
			team_id INTEGER NOT NULL,
			position INTEGER NOT NULL,
			player_id INTEGER NOT NULL,
			source TEXT NOT NULL DEFAULT 'manual',
			UNIQUE(tournament_id, team_id, position),
			FOREIGN KEY(tournament_id) REFERENCES tournaments(id) ON DELETE CASCADE,
			FOREIGN KEY(player_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS rounds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tournament_id INTEGER NOT NULL,
			stage TEXT NOT NULL,
			target_score INTEGER NOT NULL DEFAULT 0,
			score_a INTEGER NOT NULL DEFAULT 0,
			score_b INTEGER NOT NULL DEFAULT 0,
			winner_team_id INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tournament_id, stage),
			FOREIGN KEY(tournament_id) REFERENCES tournaments(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS matches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tournament_id INTEGER NOT NULL,
			round_id INTEGER NOT NULL,
			seq INTEGER NOT NULL,
			team_a_id INTEGER NOT NULL,
			team_b_id INTEGER NOT NULL,
			player_a_id INTEGER NOT NULL,
			player_b_id INTEGER NOT NULL,
			winner_team_id INTEGER,
			next_player_id INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			note TEXT NOT NULL DEFAULT '',
			created_by INTEGER,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			UNIQUE(round_id, seq),
			FOREIGN KEY(tournament_id) REFERENCES tournaments(id) ON DELETE CASCADE,
			FOREIGN KEY(round_id) REFERENCES rounds(id) ON DELETE CASCADE,
			FOREIGN KEY(player_a_id) REFERENCES users(id),
			FOREIGN KEY(player_b_id) REFERENCES users(id),
			FOREIGN KEY(next_player_id) REFERENCES users(id),
			FOREIGN KEY(created_by) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS tournament_actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tournament_id INTEGER NOT NULL,
			actor_id INTEGER,
			action TEXT NOT NULL,
			payload TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(tournament_id) REFERENCES tournaments(id) ON DELETE CASCADE,
			FOREIGN KEY(actor_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS matches_player_a_records ON matches(player_a_id) WHERE status='finished'`,
		`CREATE INDEX IF NOT EXISTS matches_player_b_records ON matches(player_b_id) WHERE status='finished'`,
		`CREATE TABLE IF NOT EXISTS player_selections (
			tournament_id INTEGER NOT NULL REFERENCES tournaments(id) ON DELETE CASCADE,
			stage TEXT NOT NULL,
			team_id INTEGER NOT NULL,
			player_id INTEGER NOT NULL REFERENCES users(id),
			PRIMARY KEY(tournament_id, stage, team_id)
		)`,
		`CREATE TABLE IF NOT EXISTS site_tournament_names (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS backgrounds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			image_data BLOB NOT NULL,
			image_mime TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS kof_reservations (
			tournament_id INTEGER NOT NULL REFERENCES tournaments(id) ON DELETE CASCADE,
			match_id INTEGER NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
			team_id INTEGER NOT NULL,
			player_id INTEGER NOT NULL REFERENCES users(id),
			PRIMARY KEY(match_id,team_id)
		)`,
		`CREATE TABLE IF NOT EXISTS categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			icon TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TRIGGER IF NOT EXISTS matches_sync_after_insert
		AFTER INSERT ON matches
		BEGIN
			UPDATE matches SET round_no=CASE NEW.round_id WHEN (SELECT id FROM rounds WHERE id=NEW.round_id AND stage='round1') THEN 1 WHEN (SELECT id FROM rounds WHERE id=NEW.round_id AND stage='round2') THEN 2 WHEN (SELECT id FROM rounds WHERE id=NEW.round_id AND stage='ace') THEN 3 ELSE 0 END,
				a_id=NEW.player_a_id,b_id=NEW.player_b_id,who_win=CASE WHEN NEW.winner_team_id IS NULL THEN '0' WHEN NEW.winner_team_id=NEW.team_a_id THEN 'a' ELSE 'b' END WHERE id=NEW.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS matches_sync_after_update
		AFTER UPDATE OF round_id,player_a_id,player_b_id,team_a_id,team_b_id,winner_team_id ON matches
		BEGIN
			UPDATE matches SET round_no=CASE NEW.round_id WHEN (SELECT id FROM rounds WHERE id=NEW.round_id AND stage='round1') THEN 1 WHEN (SELECT id FROM rounds WHERE id=NEW.round_id AND stage='round2') THEN 2 WHEN (SELECT id FROM rounds WHERE id=NEW.round_id AND stage='ace') THEN 3 ELSE 0 END,
				a_id=NEW.player_a_id,b_id=NEW.player_b_id,who_win=CASE WHEN NEW.winner_team_id IS NULL THEN '0' WHEN NEW.winner_team_id=NEW.team_a_id THEN 'a' ELSE 'b' END WHERE id=NEW.id;
		END`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tournament_setup_roster(tournament_id INTEGER NOT NULL REFERENCES tournaments(id) ON DELETE CASCADE,user_id INTEGER NOT NULL REFERENCES users(id),fixed_team TEXT NOT NULL DEFAULT '',position INTEGER NOT NULL,PRIMARY KEY(tournament_id,user_id))`); err != nil {
		return err
	}
	if err := ensureColumn(db, "users", "avatar_data", "BLOB"); err != nil {
		return err
	}
	if err := ensureColumn(db, "users", "avatar_mime", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, definition string }{
		{"users", "session_token", "TEXT"},
		{"users", "is_deleted", "INTEGER NOT NULL DEFAULT 0"},
		{"users", "display_name_changed_at", "TEXT"},
		{"users", "avatar_changed_at", "TEXT"},
		{"users", "profile_changed_at", "TEXT"},
		{"users", "avatar_version", "INTEGER NOT NULL DEFAULT 0"},
		{"users", "category_id", "INTEGER"},
		{"categories", "icon_data", "BLOB"},
		{"categories", "icon_mime", "TEXT NOT NULL DEFAULT ''"},
		{"tournaments", "round1_no_repeat", "INTEGER NOT NULL DEFAULT 1"},
		{"tournaments", "round1_all_first", "INTEGER NOT NULL DEFAULT 1"},
		{"tournaments", "kof_initial_a", "INTEGER"},
		{"tournaments", "kof_initial_b", "INTEGER"},
		{"tournaments", "rules_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"tournaments", "lineup_a_submitted", "INTEGER NOT NULL DEFAULT 0"},
		{"tournaments", "lineup_b_submitted", "INTEGER NOT NULL DEFAULT 0"},
		{"tournaments", "background_id", "INTEGER"},
		{"tournaments", "is_imported", "INTEGER NOT NULL DEFAULT 0"},
		{"backgrounds", "file_path", "TEXT NOT NULL DEFAULT ''"},
		{"backgrounds", "image_size", "INTEGER NOT NULL DEFAULT 0"},
		{"backgrounds", "thumbnail_data", "BLOB"},
		{"matches", "round_no", "INTEGER"},
		{"matches", "a_id", "INTEGER"},
		{"matches", "b_id", "INTEGER"},
		{"matches", "who_win", "TEXT NOT NULL DEFAULT '0'"},
	} {
		if err := ensureColumn(db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	if err := initializeUserSessions(db); err != nil {
		return err
	}
	// 通过录入审计识别旧的补录赛事，正常完赛记录不套用补录标记。
	if _, err := db.Exec(`UPDATE tournaments SET is_imported=1 WHERE is_imported=0 AND id IN (SELECT tournament_id FROM tournament_actions WHERE action='historical_tournament_created')`); err != nil {
		return err
	}
	// 旧版本的头像、显示名次数合并到最近一次本人资料修改，不重置已有冷却。
	if _, err := db.Exec(`UPDATE users SET profile_changed_at=MAX(COALESCE(display_name_changed_at,''),COALESCE(avatar_changed_at,'')) WHERE profile_changed_at IS NULL AND (COALESCE(display_name_changed_at,'')<>'' OR COALESCE(avatar_changed_at,'')<>'')`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE matches SET round_no=CASE (SELECT stage FROM rounds WHERE id=matches.round_id) WHEN 'round1' THEN 1 WHEN 'round2' THEN 2 WHEN 'ace' THEN 3 ELSE 0 END,a_id=player_a_id,b_id=player_b_id,who_win=CASE WHEN winner_team_id IS NULL THEN '0' WHEN winner_team_id=team_a_id THEN 'a' ELSE 'b' END WHERE round_no IS NULL OR a_id IS NULL OR b_id IS NULL`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE users SET role='user' WHERE role IN ('manager','player')`); err != nil {
		return err
	}
	if err := initializeFixedTeams(db); err != nil {
		return err
	}
	return normalizeTournamentMetadata(db)
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	rows.Close()
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}
