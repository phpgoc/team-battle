// Package migrations 管理编入程序的数据库升级，只在 HTTP 服务启动前调用。
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"
)

// Up 自动执行尚未应用的迁移；不提供降级，恢复旧版本须使用升级前的备份。
func Up(ctx context.Context, db *sql.DB, languages ...string) error {
	return upWithLogger(ctx, db, nil, migrationList()...)
}

// UpWithLogger 在成功应用迁移后通知调用方，日志目的地由应用控制。
func UpWithLogger(ctx context.Context, db *sql.DB, onApplied func(int)) error {
	return upWithLogger(ctx, db, onApplied, migrationList()...)
}

func migrationList() []*goose.Migration {
	baseline := goose.NewGoMigration(1, &goose.GoFunc{RunTx: upBaseline}, nil)
	baseline.Source = "00001_baseline.go"
	extraInfo := goose.NewGoMigration(2, &goose.GoFunc{RunTx: upExtraInfo}, nil)
	extraInfo.Source = "00002_extra_info.go"
	playerRecords := goose.NewGoMigration(3, &goose.GoFunc{RunTx: upPlayerRecords}, nil)
	playerRecords.Source = "00003_player_records.go"
	return []*goose.Migration{baseline, extraInfo, playerRecords}
}

func up(ctx context.Context, db *sql.DB, migrations ...*goose.Migration) error {
	return upWithLogger(ctx, db, nil, migrations...)
}

func upWithLogger(ctx context.Context, db *sql.DB, onApplied func(int), migrations ...*goose.Migration) (retErr error) {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, nil,
		goose.WithDisableGlobalRegistry(true), goose.WithGoMigrations(migrations...),
		goose.WithLogger(goose.NopLogger()))
	if err != nil {
		return fmt.Errorf("初始化数据库迁移: %w", err)
	}
	// Provider.Close 会关闭调用方的数据库，这里由应用统一管理连接生命周期。
	current, target, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("读取数据库版本: %w", err)
	}
	if current > target {
		return fmt.Errorf("数据库版本 %d 高于程序支持的版本 %d，请使用较新的程序", current, target)
	}
	if err := validateHistory(ctx, db, provider.ListSources()); err != nil {
		return err
	}
	var legacyTeams int
	if current == 0 {
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`).Scan(&legacyTeams); err != nil {
			return err
		}
	}
	if legacyTeams != 0 {
		restore, err := prepareLegacyRebuild(ctx, db)
		if err != nil {
			return err
		}
		defer func() { retErr = errors.Join(retErr, restore()) }()
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("数据库升级失败（当前版本 %d，目标版本 %d）: %w", current, target, err)
	}
	if len(results) != 0 {
		if onApplied != nil {
			onApplied(len(results))
		}
	}
	return nil
}

// Goose负责执行和记录版本；额外拒绝未知、重复或被手工损坏的版本记录。
func validateHistory(ctx context.Context, db *sql.DB, sources []*goose.Source) error {
	known := map[int64]bool{0: true}
	for _, source := range sources {
		known[source.Version] = true
	}
	rows, err := db.QueryContext(ctx, `SELECT version_id,is_applied FROM goose_db_version ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil {
			return err
		}
		if !known[version] || seen[version] || !applied {
			return fmt.Errorf("数据库迁移记录异常，版本 %d；请核对备份和程序版本", version)
		}
		seen[version] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !seen[0] {
		return errors.New("数据库迁移记录缺少初始版本，请核对备份")
	}
	return nil
}

// SQLite重建旧teams关联表必须在事务外暂停外键；启动期间只留同一条连接。
// 迁移结束前在事务内执行foreign_key_check，无论成功失败都恢复连接配置。
func prepareLegacyRebuild(ctx context.Context, db *sql.DB) (func() error, error) {
	maxOpen := db.Stats().MaxOpenConnections
	db.SetMaxOpenConns(1)
	var foreignKeys int
	restorePool := func() {
		db.SetMaxOpenConns(maxOpen)
		if maxOpen > 0 {
			db.SetMaxIdleConns(maxOpen)
		} else {
			db.SetMaxIdleConns(2)
		}
	}
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		restorePool()
		return nil, err
	}
	restore := func() error {
		defer restorePool()
		_, err := db.ExecContext(context.Background(), fmt.Sprintf("PRAGMA foreign_keys=%d", foreignKeys))
		return err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return nil, errors.Join(err, restore())
	}
	return restore, nil
}
