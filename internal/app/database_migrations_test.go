package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func migrationBusinessSnapshot(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	data := permissionDatabaseSnapshot(t, &workflowFixture{app: &App{db: db}})
	delete(data, "goose_db_version")
	rows, err := db.Query(`SELECT name,seq FROM sqlite_sequence WHERE name<>'goose_db_version' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var sequences []any
	for rows.Next() {
		var name string
		var seq int64
		if err := rows.Scan(&name, &seq); err != nil {
			t.Fatal(err)
		}
		sequences = append(sequences, []any{name, seq})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sequences)
	if err != nil {
		t.Fatal(err)
	}
	data["sqlite_sequence"] = string(raw)
	return data
}

func assertMigrationConnections(t *testing.T, db *sql.DB) {
	t.Helper()
	if db.Stats().MaxOpenConnections != 4 {
		t.Fatal("迁移没有恢复应用连接池")
	}
	for i := 0; i < 4; i++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var foreignKeys int
		if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
			t.Fatalf("迁移后连接外键未恢复：%d，%v", foreignKeys, err)
		}
	}
}

func TestUnversionedBaselineFailureRollsBackAndCanRetry(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	simulateUnversionedDatabase(t, f.app.db)
	// 让失败发生在名称、增列及会话转换之后，验证整个基线是同一事务。
	for _, statement := range []string{
		`CREATE TABLE teams(id INTEGER PRIMARY KEY,tournament_id INTEGER,lineup_submitted INTEGER)`,
		`INSERT INTO teams SELECT id*2-1,id,lineup_a_submitted FROM tournaments UNION ALL SELECT id*2,id,lineup_b_submitted FROM tournaments`,
		`ALTER TABLE tournaments RENAME TO events`,
		`ALTER TABLE users DROP COLUMN profile_changed_at`,
		`UPDATE users SET session_token=NULL,role='manager' WHERE id=2`,
		`CREATE TABLE sessions(token TEXT PRIMARY KEY,user_id INTEGER,created_at TEXT)`,
		`INSERT INTO sessions VALUES('legacy-session-token',2,'2026-01-01')`,
		`CREATE TRIGGER fail_baseline BEFORE UPDATE OF lineup_a_submitted ON events BEGIN SELECT RAISE(ABORT,'模拟队伍转换失败'); END`,
	} {
		if _, err := f.app.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	before := migrationBusinessSnapshot(t, f.app.db)
	if err := migrate(f.app.db); err == nil || !strings.Contains(err.Error(), "模拟队伍转换失败") {
		t.Fatalf("未报告迁移故障：%v", err)
	}
	if !reflect.DeepEqual(before, migrationBusinessSnapshot(t, f.app.db)) {
		t.Fatal("失败基线改变了旧库业务数据或自增上限")
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='profile_changed_at'`)
	f.assertCount(t, 0, `SELECT MAX(version_id) FROM goose_db_version`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tournaments'`)
	assertMigrationConnections(t, f.app.db)
	if _, err := f.app.db.Exec(`DROP TRIGGER fail_baseline`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT MAX(version_id) FROM goose_db_version`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=2 AND role='user' AND session_token='legacy-session-token'`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('events','teams','sessions')`)
	assertMigrationConnections(t, f.app.db)
	// 迁移完成的赛事可继续录分，触发器和权限不能因升级失效。
	f.tokens[2] = "legacy-session-token"
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND status='pending'`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": id*2 - 1}, 200)
}

func TestTrackedDemoDatabaseSurvivesMigration(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/"), RawQuery: "mode=ro"}
	source, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	copyPath := filepath.Join(t.TempDir(), "demo.sqlite")
	// SQLite在线快照包含WAL内容；所有升级写入只作用于临时副本。
	if _, err := source.Exec(`VACUUM INTO ?`, copyPath); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var versioned int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'`).Scan(&versioned); err != nil {
		t.Fatal(err)
	}
	if versioned != 0 {
		simulateUnversionedDatabase(t, db)
	}
	before := migrationBusinessSnapshot(t, db)
	for i := 0; i < 2; i++ {
		if err := migrate(db); err != nil {
			t.Fatal(err)
		}
		if after := migrationBusinessSnapshot(t, db); !reflect.DeepEqual(before, after) {
			for table := range before {
				if before[table] != after[table] {
					t.Errorf("演示库升级改变了 %s 的数据", table)
				}
			}
			t.Fatal("演示库升级改变了业务表、图片或自增上限")
		}
	}
	assertMigrationConnections(t, db)
}
