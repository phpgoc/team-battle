package app

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	sqlite "modernc.org/sqlite"
)

const defaultDBMaxConnections = 4

func openDatabase(path string, captures ...*sqlWriteCapture) (*sql.DB, error) {
	var capture *sqlWriteCapture
	if len(captures) > 0 {
		capture = captures[0]
	}
	return openDatabaseWithLimit(path, defaultDBMaxConnections, capture)
}

// 每条连接都启用外键和锁等待；WAL允许读写同时进行。
func openDatabaseWithLimit(path string, maxConnections int, capture *sqlWriteCapture) (*sql.DB, error) {
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
	driverName := "sqlite"
	if capture != nil {
		driverName = fmt.Sprintf("sqlite-operation-log-%d", nextLoggingDriverID.Add(1))
		sql.Register(driverName, &operationLogDriver{base: &sqlite.Driver{}, capture: capture})
	}
	db, err := sql.Open(driverName, uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
