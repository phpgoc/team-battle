package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// 公开推送地址同时服务登录用户和访客，登录状态只影响写操作权限。
func (a *App) publicStream(c *gin.Context) {
	eventID, err := parseID(c.Param("id"))
	if err != nil || eventID <= 0 {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	var assignedTeams int
	if err := a.db.QueryRow(`SELECT COUNT(DISTINCT team_id) FROM event_players WHERE event_id=? AND active=1`, eventID).Scan(&assignedTeams); err != nil || assignedTeams != 2 {
		fail(c, http.StatusNotFound, "赛事尚未完成参赛分队")
		return
	}
	payload, err := a.publicEventPayload(eventID)
	if err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	var initialStatus string
	if err := a.db.QueryRow(`SELECT status FROM events WHERE id=?`, eventID).Scan(&initialStatus); err != nil {
		fail(c, 404, "赛事不存在")
		return
	}
	if initialStatus == statusCompleted || initialStatus == statusCancelled {
		c.Status(http.StatusNoContent)
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	if _, err := fmt.Fprintf(c.Writer, "retry: 2000\ndata: %s\n\n", payload); err != nil {
		return
	}
	c.Writer.Flush()

	updates := time.NewTicker(time.Second)
	heartbeats := time.NewTicker(15 * time.Second)
	defer updates.Stop()
	defer heartbeats.Stop()
	previous := payload
	var status string
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeats.C:
			if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-updates.C:
			payload, err = a.publicEventPayload(eventID)
			if err != nil {
				return
			}
			if bytes.Equal(previous, payload) {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
				return
			}
			previous = payload
			c.Writer.Flush()
			if err := a.db.QueryRow(`SELECT status FROM events WHERE id=?`, eventID).Scan(&status); err == nil && (status == statusCompleted || status == statusCancelled) {
				return
			}
		}
	}
}

func (a *App) publicEventPayload(eventID int64) ([]byte, error) {
	data, err := a.buildEvent(eventID, false)
	if err != nil {
		return nil, err
	}
	return json.Marshal(data)
}
