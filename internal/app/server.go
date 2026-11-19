package app

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var errServerStopping = errors.New("服务正在停止")

func (a *App) rejectDuringShutdown(c *gin.Context) {
	if a.shutdownStarted.Load() {
		c.Header("Connection", "close")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": errServerStopping.Error()})
	}
}

func (a *App) serveHTTP(ctx context.Context, listener net.Listener) error {
	return a.serveHTTPHandler(ctx, listener, a.router())
}

func (a *App) serveHTTPHandler(ctx context.Context, listener net.Listener, handler http.Handler) error {
	streamsDone := make(chan struct{})
	a.streamsDone = streamsDone
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	// Shutdown 先关闭监听，再调用回调，唤醒正在等待心跳或更新的 SSE。
	server.RegisterOnShutdown(func() { close(streamsDone) })
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	// 监听异常退出时也要等待已经接受的请求。
	var serveErr error
	serveFinished := false
	select {
	case <-ctx.Done():
	case serveErr = <-serveDone:
		serveFinished = true
	}
	a.shutdownStarted.Store(true)
	log.Print("停止接受请求，等待活动请求结束")
	// 已接收的请求继续使用自己的上下文，允许数据库操作提交或回滚。
	shutdownErr := server.Shutdown(context.Background())
	<-streamsDone
	if !serveFinished {
		serveErr = <-serveDone
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}
