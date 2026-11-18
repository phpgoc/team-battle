package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// 公开推送地址同时服务登录用户和访客，登录状态只影响写操作权限。
func (a *App) publicStream(c *gin.Context) {
	tournamentID, err := parseID(c.Param("id"))
	if err != nil || tournamentID <= 0 {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	initial, updates, unsubscribe, err := a.subscribeTournament(tournamentID)
	if err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	defer unsubscribe()
	if !initial.publicReady {
		fail(c, http.StatusNotFound, "赛事暂未公开")
		return
	}
	if initial.status == statusCompleted || initial.status == statusCancelled {
		c.Status(http.StatusNoContent)
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	if _, err := fmt.Fprintf(c.Writer, "retry: 2000\ndata: %s\n\n", initial.payload); err != nil {
		return
	}
	c.Writer.Flush()

	heartbeats := time.NewTicker(15 * time.Second)
	defer heartbeats.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeats.C:
			if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case next := <-updates:
			if !next.publicReady || len(next.payload) == 0 {
				fmt.Fprint(c.Writer, "data: {\"unavailable\":true}\n\n")
				c.Writer.Flush()
				return
			}
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", next.payload); err != nil {
				return
			}
			c.Writer.Flush()
			if next.status == statusCompleted || next.status == statusCancelled {
				return
			}
		}
	}
}

func (a *App) publicTournamentPayload(tournamentID int64) ([]byte, error) {
	data, err := a.buildTournament(tournamentID, false)
	if err != nil {
		return nil, err
	}
	return json.Marshal(data)
}
