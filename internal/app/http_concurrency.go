package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	defaultHTTPReads            = 100
	defaultHTTPWrites           = 10
	publicTournamentStreamRoute = "/api/public/tournaments/:id/stream"
)

type httpConcurrencyLimits struct {
	reads, writes int
}

// 写名额包含等待应用写锁的请求，SSE 由独立配额管理。
func limitHTTPConcurrency(limits httpConcurrencyLimits) gin.HandlerFunc {
	if limits.reads == 0 {
		limits.reads = defaultHTTPReads
	}
	if limits.writes == 0 {
		limits.writes = defaultHTTPWrites
	}
	readSlots := make(chan struct{}, limits.reads)
	writeSlots := make(chan struct{}, limits.writes)
	return func(c *gin.Context) {
		if c.FullPath() == publicTournamentStreamRoute && c.Request.Method == http.MethodGet {
			c.Next()
			return
		}
		slots := writeSlots
		if isHTTPRead(c.Request.Method) {
			slots = readSlots
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			c.Next()
		default:
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "请求过多，请稍后重试"})
		}
	}
}

func isHTTPRead(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
