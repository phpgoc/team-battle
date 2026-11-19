package migrations

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// 将旧版本的分钟精度统一为整点时间。
func normalizeLegacyTournamentMetadata(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id,start_time FROM tournaments`)
	if err != nil {
		return err
	}
	type startTime struct {
		id    int64
		value string
	}
	times := []startTime{}
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return err
		}
		normalized, err := normalizeLegacyStartTime(value)
		if err == nil && normalized != value {
			times = append(times, startTime{id, normalized})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range times {
		if _, err := tx.Exec(`UPDATE tournaments SET start_time=? WHERE id=?`, item.value, item.id); err != nil {
			return err
		}
	}
	return nil
}

// 历史格式转换保持独立，后续业务格式变化不能改变已发布迁移。
func normalizeLegacyStartTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02T15", "2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02T15:00:00"), nil
		}
	}
	return "", errors.New("比赛开始时间格式不正确")
}
