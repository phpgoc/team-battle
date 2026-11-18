package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// SSE持有读取快照时，设置仍能提交；连接池中的每条连接使用相同约束。
func TestDatabaseConcurrentReadersAndConnectionPragmas(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "赛事 空格.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY,value INTEGER NOT NULL); INSERT INTO sample VALUES(1,1)`); err != nil {
		t.Fatal(err)
	}
	reader, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, connection := range []*sql.Conn{reader, writer} {
		var foreignKeys, timeout int
		var mode string
		if err := connection.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(context.Background(), `PRAGMA journal_mode`).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || timeout != 5000 || mode != "wal" {
			t.Fatalf("连接配置不一致: %d %d %s", foreignKeys, timeout, mode)
		}
	}
	if _, err := reader.ExecContext(context.Background(), `BEGIN`); err != nil {
		t.Fatal(err)
	}
	defer reader.ExecContext(context.Background(), `ROLLBACK`)
	var before int
	if err := reader.QueryRowContext(context.Background(), `SELECT value FROM sample WHERE id=1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(context.Background(), `UPDATE sample SET value=2 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var snapshot int
	if err := reader.QueryRowContext(context.Background(), `SELECT value FROM sample WHERE id=1`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if before != 1 || snapshot != 1 {
		t.Fatal("读快照不一致")
	}
	if _, err := reader.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := reader.QueryRowContext(context.Background(), `SELECT value FROM sample WHERE id=1`).Scan(&after); err != nil || after != 2 {
		t.Fatalf("保存结果不可见: %d %v", after, err)
	}
}
