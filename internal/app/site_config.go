package app

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

type tournamentDefaults struct {
	RandomDrawSeconds int `json:"random_draw_seconds"`
	AceDrawSeconds    int `json:"ace_draw_seconds"`
}

// 网站默认值只用于创建时取快照，不能参与已有赛事规则的缺省解码。
func loadTournamentDefaults(db sqlRunner) (tournamentDefaults, error) {
	defaults := tournamentDefaults{RandomDrawSeconds: 4, AceDrawSeconds: 5}
	rows, err := db.Query(`SELECT key,value FROM config WHERE key IN ('random_draw_seconds','ace_draw_seconds')`)
	if err != nil {
		return defaults, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return defaults, err
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return defaults, errors.New("网站默认抽签时长无效")
		}
		if key == "random_draw_seconds" {
			defaults.RandomDrawSeconds = value
		} else {
			defaults.AceDrawSeconds = value
		}
	}
	return defaults, rows.Err()
}

func (a *App) updateTournamentDefaults(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	var req struct {
		RandomDrawSeconds *int `json:"random_draw_seconds"`
		AceDrawSeconds    *int `json:"ace_draw_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.RandomDrawSeconds == nil && req.AceDrawSeconds == nil) {
		fail(c, 400, "请输入默认抽签时长")
		return
	}
	values := map[string]*int{"random_draw_seconds": req.RandomDrawSeconds, "ace_draw_seconds": req.AceDrawSeconds}
	for _, value := range values {
		if value != nil && *value < 0 {
			fail(c, 400, "抽签时长不能为负数")
			return
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, 500, "保存网站设置失败")
		return
	}
	defer tx.Rollback()
	for key, value := range values {
		if value != nil {
			if _, err := tx.Exec(`INSERT INTO config(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, strconv.Itoa(*value)); err != nil {
				fail(c, 500, "保存网站设置失败")
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, "保存网站设置失败")
		return
	}
	ok(c, gin.H{"saved": true})
}
