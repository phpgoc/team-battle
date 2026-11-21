package app

import (
	"context"
	"database/sql"

	"team-battle/internal/migrations"
)

// 服务监听端口前完成版本升级；失败时禁止继续启动。
func migrate(db *sql.DB, loggers ...*operationLogger) error {
	var logger *operationLogger
	if len(loggers) > 0 {
		logger = loggers[0]
	}
	if logger == nil {
		return migrations.Up(context.Background(), db)
	}
	return migrations.UpWithLogger(context.Background(), db, func(applied int) {
		logger.lifecyclef(operationLogText(logger.language,
			"数据库迁移完成，共应用 %d 项",
			"Database migrations complete: %d applied",
			"데이터베이스 마이그레이션 완료: %d개 적용"), applied)
	})
}
