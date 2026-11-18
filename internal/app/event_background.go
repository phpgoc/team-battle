package app

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

func backgroundExists(db sqlRunner, backgroundID int64) (bool, error) {
	if backgroundID == 0 {
		return true, nil
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM backgrounds WHERE id=?`, backgroundID).Scan(&count)
	return count == 1, err
}

// 赛事背景用于广告和赞助展示，仅赛事管理员及超级管理员可修改，所有观众通过 SSE 同步。
func (a *App) updateEventBackground(c *gin.Context) {
	actor, eventID, access := a.managerEvent(c)
	if !access {
		return
	}
	var req eventBackgroundRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.BackgroundID < 0 {
		fail(c, 400, "背景编号无效")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, 500, "更新赛事背景失败")
		return
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM tournaments WHERE id=?`, eventID).Scan(&status); err != nil {
		fail(c, 404, "赛事不存在")
		return
	}
	if status == statusCompleted || status == statusCancelled {
		fail(c, 400, "已结束赛事不能修改背景")
		return
	}
	valid, err := backgroundExists(tx, req.BackgroundID)
	if err != nil {
		fail(c, 500, "读取赛事背景失败")
		return
	}
	if !valid {
		fail(c, 400, "背景不存在")
		return
	}
	if _, err := tx.Exec(`UPDATE tournaments SET background_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, nullableInt64(req.BackgroundID), eventID); err != nil {
		fail(c, 500, "保存赛事背景失败")
		return
	}
	payload, _ := json.Marshal(req)
	if _, err := tx.Exec(`INSERT INTO event_actions(event_id,actor_id,action,payload) VALUES(?,?,?,?)`, eventID, nullableInt64(actor.ID), "event_background_updated", string(payload)); err != nil {
		fail(c, 500, "记录赛事背景变更失败")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, "保存赛事背景失败")
		return
	}
	ok(c, gin.H{"ok": true})
}
