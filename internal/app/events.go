package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultSSEMaxConnections = 500
	sseWriteTimeout          = 5 * time.Second
)

func limitSSEConnections(limit int) gin.HandlerFunc {
	if limit == 0 {
		limit = defaultSSEMaxConnections
	}
	slots := make(chan struct{}, limit)
	return func(c *gin.Context) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			c.Next()
		default:
			c.Abort()
			setStreamHeaders(c)
			// EventSource 收到 SSE 重连间隔后会自动重试；这里不登记订阅。
			writeStreamFrame(c.Writer, "retry: 10000\n\n")
		}
	}
}

func setStreamHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
}

func writeStreamFrame(writer http.ResponseWriter, format string, args ...any) bool {
	underlying := writer
	if wrapped, ok := writer.(interface{ Unwrap() http.ResponseWriter }); ok {
		underlying = wrapped.Unwrap()
	}
	controller := http.NewResponseController(underlying)
	deadlineErr := controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	if deadlineErr != nil && !errors.Is(deadlineErr, http.ErrNotSupported) {
		return false
	}
	// 通过 Gin 写入以记录响应状态，通过底层连接刷新以检查写入错误。
	if _, err := fmt.Fprintf(writer, format, args...); err != nil || controller.Flush() != nil {
		return false
	}
	if deadlineErr == nil {
		return controller.SetWriteDeadline(time.Time{}) == nil
	}
	return true
}

// 公开推送地址同时服务登录用户和访客，登录状态只影响写操作权限。
func (a *App) publicStream(c *gin.Context) {
	tournamentID, err := parseID(c.Param("id"))
	if err != nil || tournamentID <= 0 {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	initial, updates, unsubscribe, err := a.subscribeTournament(tournamentID)
	if err != nil {
		if errors.Is(err, errServerStopping) {
			fail(c, http.StatusServiceUnavailable, "服务正在停止")
			return
		}
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
	select {
	case <-a.streamsDone:
		fail(c, http.StatusServiceUnavailable, errServerStopping.Error())
		return
	default:
	}
	setStreamHeaders(c)
	if !writeStreamFrame(c.Writer, "retry: 2000\ndata: %s\n\n", initial.payload) {
		return
	}

	heartbeats := time.NewTicker(15 * time.Second)
	defer heartbeats.Stop()
	for {
		select {
		case <-a.streamsDone:
			return
		case <-c.Request.Context().Done():
			return
		case <-heartbeats.C:
			if !writeStreamFrame(c.Writer, ": heartbeat\n\n") {
				return
			}
		case next := <-updates:
			if !next.publicReady || len(next.payload) == 0 {
				writeStreamFrame(c.Writer, "data: {\"unavailable\":true}\n\n")
				return
			}
			if !writeStreamFrame(c.Writer, "data: %s\n\n", next.payload) {
				return
			}
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
