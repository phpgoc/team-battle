package main

import (
	"database/sql"
)

func migrate(db *sql.DB) error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('superadmin','user','manager','player')),
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			event_time TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS teams (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '#2b6de5',
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS event_players (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			team_id INTEGER NOT NULL,
			is_captain INTEGER NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 1,
			UNIQUE(event_id, user_id),
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY(user_id) REFERENCES users(id),
			FOREIGN KEY(team_id) REFERENCES teams(id)
		)`,
		`CREATE TABLE IF NOT EXISTS captain_votes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			team_id INTEGER NOT NULL,
			voter_id INTEGER NOT NULL,
			target_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(event_id, voter_id),
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY(team_id) REFERENCES teams(id),
			FOREIGN KEY(voter_id) REFERENCES users(id),
			FOREIGN KEY(target_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS lineup_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			team_id INTEGER NOT NULL,
			position INTEGER NOT NULL,
			player_id INTEGER NOT NULL,
			source TEXT NOT NULL DEFAULT 'manual',
			UNIQUE(event_id, team_id, position),
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY(team_id) REFERENCES teams(id),
			FOREIGN KEY(player_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS rounds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			stage TEXT NOT NULL,
			target_score INTEGER NOT NULL DEFAULT 0,
			score_a INTEGER NOT NULL DEFAULT 0,
			score_b INTEGER NOT NULL DEFAULT 0,
			winner_team_id INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(event_id, stage),
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY(winner_team_id) REFERENCES teams(id)
		)`,
		`CREATE TABLE IF NOT EXISTS matches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
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
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY(round_id) REFERENCES rounds(id) ON DELETE CASCADE,
			FOREIGN KEY(team_a_id) REFERENCES teams(id),
			FOREIGN KEY(team_b_id) REFERENCES teams(id),
			FOREIGN KEY(player_a_id) REFERENCES users(id),
			FOREIGN KEY(player_b_id) REFERENCES users(id),
			FOREIGN KEY(winner_team_id) REFERENCES teams(id),
			FOREIGN KEY(next_player_id) REFERENCES users(id),
			FOREIGN KEY(created_by) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS event_actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			actor_id INTEGER,
			action TEXT NOT NULL,
			payload TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY(actor_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS player_selections (
			event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			stage TEXT NOT NULL,
			team_id INTEGER NOT NULL REFERENCES teams(id),
			player_id INTEGER NOT NULL REFERENCES users(id),
			PRIMARY KEY(event_id, stage, team_id)
		)`,
		`CREATE TABLE IF NOT EXISTS site_event_names (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
	if err := ensureColumn(db, "users", "avatar_data", "BLOB"); err != nil {
		return err
	}
	if err := ensureColumn(db, "users", "avatar_mime", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, definition string }{
		{"users", "avatar_version", "INTEGER NOT NULL DEFAULT 0"},
		{"users", "category_id", "INTEGER"},
		{"categories", "icon_data", "BLOB"},
		{"categories", "icon_mime", "TEXT NOT NULL DEFAULT ''"},
		{"events", "round1_no_repeat", "INTEGER NOT NULL DEFAULT 1"},
		{"events", "round1_all_first", "INTEGER NOT NULL DEFAULT 1"},
		{"events", "kof_initial_a", "INTEGER"},
		{"events", "kof_initial_b", "INTEGER"},
		{"events", "rules_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"matches", "round_no", "INTEGER"},
		{"matches", "a_id", "INTEGER"},
		{"matches", "b_id", "INTEGER"},
		{"matches", "who_win", "TEXT NOT NULL DEFAULT '0'"},
	} {
		if err := ensureColumn(db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`UPDATE matches SET round_no=CASE (SELECT stage FROM rounds WHERE id=matches.round_id) WHEN 'round1' THEN 1 WHEN 'round2' THEN 2 WHEN 'ace' THEN 3 ELSE 0 END,a_id=player_a_id,b_id=player_b_id,who_win=CASE WHEN winner_team_id IS NULL THEN '0' WHEN winner_team_id=team_a_id THEN 'a' ELSE 'b' END WHERE round_no IS NULL OR a_id IS NULL OR b_id IS NULL`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE users SET role='user' WHERE role IN ('manager','player')`); err != nil {
		return err
	}
	return nil
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
