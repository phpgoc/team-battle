package app

import (
	"database/sql"
	"errors"

	"github.com/gin-gonic/gin"
)

type drawSettingsRequest struct {
	RandomDrawSeconds *int `json:"random_draw_seconds"`
	AceDrawSeconds    *int `json:"ace_draw_seconds"`
}

// 抽选时长是赛事进行中唯一允许继续修改的赛事设置。
func (a *App) updateDrawSettings(c *gin.Context) {
	actor, tournamentID, access := a.managerEvent(c)
	if !access {
		return
	}
	var req drawSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.RandomDrawSeconds == nil && req.AceDrawSeconds == nil) {
		fail(c, 400, "抽选时间设置格式不正确")
		return
	}
	err := a.runMatchMutation(tournamentID, actor.ID, func(e *matchEngine) error {
		rules, err := e.rules()
		if err != nil {
			return err
		}
		if req.RandomDrawSeconds != nil {
			rules.RandomDrawSeconds = *req.RandomDrawSeconds
		}
		if req.AceDrawSeconds != nil {
			rules.AceDrawSeconds = *req.AceDrawSeconds
		}
		if err := rules.validate(); err != nil {
			return err
		}
		tx, ok := e.db.(*sql.Tx)
		if !ok {
			return errors.New("赛事设置事务不可用")
		}
		if err := saveRules(tx, tournamentID, rules); err != nil {
			return err
		}
		return e.log("draw_settings_updated", req)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "rules": mustEventRules(a.db, tournamentID)})
}

func mustEventRules(db *sql.DB, tournamentID int64) EventRules {
	var raw string
	if err := db.QueryRow(`SELECT rules_json FROM tournaments WHERE id=?`, tournamentID).Scan(&raw); err != nil {
		return defaultRules()
	}
	rules, err := decodeRules(raw)
	if err != nil {
		return defaultRules()
	}
	return rules
}
