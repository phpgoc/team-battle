package app

import (
	"testing"
)

func TestTournamentTableNamePreservesExistingEvents(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧数据库名称，启动初始化应保留ID、参赛者、对局和所有外键。
	if _, err := f.app.db.Exec(`ALTER TABLE tournaments RENAME TO events`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tournaments'`)
	after, err := f.app.publicEventPayload(id)
	if err != nil || string(before) != string(after) {
		t.Fatalf("主表改名改变了赛事: %s %v", after, err)
	}
	rows, err := f.app.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("主表改名后存在无效外键")
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
}

func TestTournamentRelatedNamesPreserveDataAndReferences(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`CREATE INDEX tournament_player_lookup ON tournament_players(tournament_id,user_id)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`UPDATE sqlite_sequence SET seq=10000 WHERE name='tournament_actions'`); err != nil {
		t.Fatal(err)
	}
	// 模拟完整旧库，不只改主表：关联表、外键、索引和字段都使用旧名称。
	for _, table := range []string{"tournament_players", "tournament_actions", "tournament_setup_roster", "captain_votes", "lineup_entries", "rounds", "matches", "player_selections", "kof_reservations"} {
		if _, err := f.app.db.Exec(`ALTER TABLE ` + quoteSQLName(table) + ` RENAME COLUMN tournament_id TO event_id`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.app.db.Exec(`ALTER TABLE tournaments RENAME COLUMN start_time TO event_time`); err != nil {
		t.Fatal(err)
	}
	for _, names := range [][2]string{{"tournaments", "events"}, {"tournament_players", "event_players"}, {"tournament_actions", "event_actions"}, {"tournament_setup_roster", "event_setup_roster"}, {"site_tournament_names", "site_event_names"}} {
		if _, err := f.app.db.Exec(`ALTER TABLE ` + quoteSQLName(names[0]) + ` RENAME TO ` + quoteSQLName(names[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	after, err := f.app.publicEventPayload(id)
	if err != nil || string(before) != string(after) {
		t.Fatalf("表和字段改名改变了公开赛事数据: %s %v", after, err)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('events','event_players','event_actions','event_setup_roster','site_event_names')`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master m,pragma_table_info(m.name) p WHERE m.type='table' AND p.name IN ('event_id','event_time')`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='tournament_player_lookup' AND sql LIKE '%tournament_id%'`)
	rows, err := f.app.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	invalid := rows.Next()
	rows.Close()
	if invalid {
		t.Fatal("字段改名后外键关联无效")
	}
	if _, err := f.app.db.Exec(`INSERT INTO tournament_actions(tournament_id,action,payload) VALUES(?,'schema_checked','{}')`, id); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_actions WHERE id=10001`)
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournament_actions WHERE id=10001`)
}

func TestTournamentNameCollisionRollsBackInitialization(t *testing.T) {
	for _, conflict := range []string{"table", "column"} {
		t.Run(conflict, func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := f.create(t, "random")
			if _, err := f.app.db.Exec(`ALTER TABLE tournaments RENAME TO events`); err != nil {
				t.Fatal(err)
			}
			if conflict == "table" {
				if _, err := f.app.db.Exec(`CREATE TABLE event_players(id INTEGER)`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.app.db.Exec(`ALTER TABLE captain_votes ADD COLUMN event_id INTEGER`); err != nil {
					t.Fatal(err)
				}
			}
			if err := initializeTournamentNames(f.app.db); err == nil {
				t.Fatal("名称冲突时应保留原数据并拒绝继续")
			}
			f.assertCount(t, 1, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`)
			f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tournaments'`)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=?`, id)
			f.assertCount(t, 6, `SELECT COUNT(*) FROM tournament_players WHERE tournament_id=?`, id)
		})
	}
}
