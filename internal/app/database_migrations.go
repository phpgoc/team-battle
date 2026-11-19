package app

import (
	"context"
	"database/sql"

	"team-battle/internal/migrations"
)

// 服务监听端口前完成版本升级；失败时禁止继续启动。
func migrate(db *sql.DB) error {
	return migrations.Up(context.Background(), db)
}
