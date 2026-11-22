package migrations

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func testDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	return db
}

func scalar(t *testing.T, db *sql.DB, query string) int64 {
	t.Helper()
	var value int64
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFreshDatabaseAndRepeatedStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "新数据库.sqlite")
	db := testDatabase(t, path)
	if err := Up(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, db, `SELECT MAX(version_id) FROM goose_db_version`); got != 3 {
		t.Fatalf("全新库版本为 %d", got)
	}
	if got := scalar(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('users','tournaments','matches','rounds','categories','backgrounds','config')`); got != 7 {
		t.Fatal("基线未建齐核心表")
	}
	// 已升级后不再重放旧数据修复；管理员保存的默认值也不能被启动覆盖。
	if _, err := db.Exec(`INSERT INTO users(username,display_name,password_hash,role,session_token,avatar_data) VALUES('本人账号','显示名','原有密码哈希','user','永久会话',X'010203');
		UPDATE users SET display_name_changed_at='2026-01-01',profile_changed_at=NULL;
		UPDATE config SET value='9' WHERE key='random_draw_seconds'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testDatabase(t, path)
	for i := 0; i < 2; i++ {
		if err := Up(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if got := scalar(t, db, `SELECT COUNT(*) FROM users WHERE username='本人账号' AND display_name='显示名' AND password_hash='原有密码哈希' AND session_token='永久会话' AND avatar_data=X'010203' AND profile_changed_at IS NULL`); got != 1 {
		t.Fatal("重启改变了账号、图片或重复执行了旧回填")
	}
	if got := scalar(t, db, `SELECT CAST(value AS INTEGER) FROM config WHERE key='random_draw_seconds'`); got != 9 {
		t.Fatal("重启覆盖了已有网站设置")
	}
	if got := scalar(t, db, `SELECT COUNT(*) FROM goose_db_version`); got != 4 {
		t.Fatal("重复启动重复记录了版本")
	}
}

func TestIncrementalMigrationRollbackRetryAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	db := testDatabase(t, path)
	baseline := goose.NewGoMigration(1, &goose.GoFunc{RunTx: upBaseline}, nil)
	mapInfo := goose.NewGoMigration(2, &goose.GoFunc{RunTx: upExtraInfo}, nil)
	if err := up(context.Background(), db, baseline, mapInfo); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(username,display_name,password_hash,role) VALUES('keep','原显示名','保留密码','user')`); err != nil {
		t.Fatal(err)
	}
	third := func(fail bool) *goose.Migration {
		return goose.NewGoMigration(3, &goose.GoFunc{RunTx: func(_ context.Context, tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN new_value TEXT NOT NULL DEFAULT ''; UPDATE users SET display_name='升级后',new_value='新值'`); err != nil {
				return err
			}
			if fail {
				return errors.New("模拟升级中断")
			}
			return nil
		}}, nil)
	}
	if err := up(context.Background(), db, baseline, mapInfo, third(true)); err == nil || !strings.Contains(err.Error(), "模拟升级中断") {
		t.Fatalf("迁移故障未报告: %v", err)
	}
	if scalar(t, db, `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='new_value'`) != 0 || scalar(t, db, `SELECT COUNT(*) FROM users WHERE display_name='原显示名'`) != 1 || scalar(t, db, `SELECT MAX(version_id) FROM goose_db_version`) != 2 {
		t.Fatal("失败迁移未同时回滚表结构、业务数据和版本记录")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testDatabase(t, path)
	for i := 0; i < 2; i++ {
		if err := up(context.Background(), db, baseline, mapInfo, third(false)); err != nil {
			t.Fatalf("修复后无法重试或重复启动: %v", err)
		}
	}
	if scalar(t, db, `SELECT COUNT(*) FROM users WHERE username='keep' AND display_name='升级后' AND password_hash='保留密码' AND new_value='新值'`) != 1 || scalar(t, db, `SELECT COUNT(*) FROM goose_db_version WHERE version_id=3`) != 1 {
		t.Fatal("增量升级结果或版本记录不正确")
	}
	// 用旧程序打开已经升级的库必须直接拒绝，不能悄悄降级。
	if err := up(context.Background(), db, baseline, mapInfo); err == nil || !strings.Contains(err.Error(), "高于") {
		t.Fatalf("旧程序未拒绝新库: %v", err)
	}
	if scalar(t, db, `SELECT MAX(version_id) FROM goose_db_version`) != 3 {
		t.Fatal("旧程序改变了数据库版本")
	}
}

func TestInvalidMigrationHistoryIsRejected(t *testing.T) {
	for _, item := range []struct{ name, sql string }{
		{"未来版本", `INSERT INTO goose_db_version(version_id,is_applied) VALUES(99,1)`},
		{"重复版本", `INSERT INTO goose_db_version(version_id,is_applied) VALUES(1,1)`},
		{"失效记录", `UPDATE goose_db_version SET is_applied=0 WHERE version_id=1`},
		{"缺少初始版本", `DELETE FROM goose_db_version WHERE version_id=0`},
	} {
		t.Run(item.name, func(t *testing.T) {
			db := testDatabase(t, filepath.Join(t.TempDir(), "invalid.sqlite"))
			if err := Up(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(item.sql); err != nil {
				t.Fatal(err)
			}
			count := scalar(t, db, `SELECT COUNT(*) FROM goose_db_version`)
			if err := Up(context.Background(), db); err == nil {
				t.Fatal("迁移元数据异常仍然允许启动")
			}
			if scalar(t, db, `SELECT COUNT(*) FROM goose_db_version`) != count {
				t.Fatal("版本检查修改了异常记录")
			}
		})
	}
}
