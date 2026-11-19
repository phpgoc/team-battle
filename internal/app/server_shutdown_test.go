package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGracefulShutdownDrainsRequestsAndClosesSSE(t *testing.T) {
	f := newWorkflowFixture(t)
	tournamentID := createSetupTournament(t, f, 6, "random", "random", nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdStarted := make(chan struct{})
	releaseHold := make(chan struct{})
	holdFinished := make(chan error, 1)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/hold" {
			close(holdStarted)
			<-releaseHold
			var one int
			holdFinished <- f.app.db.QueryRow(`SELECT 1`).Scan(&one)
			_, _ = fmt.Fprint(writer, "finished")
			return
		}
		f.handler.ServeHTTP(writer, request)
	})
	serverDone := make(chan error, 1)
	go func() { serverDone <- f.app.serveHTTPHandler(ctx, listener, handler) }()

	baseURL := "http://" + listener.Addr().String()
	streamRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/public/tournaments/%d/stream", baseURL, tournamentID), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResponse, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResponse.Body.Close()
	reader := bufio.NewReader(streamResponse.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}

	holdResponse := make(chan error, 1)
	go func() {
		response, err := http.Get(baseURL + "/hold")
		if err != nil {
			holdResponse <- err
			return
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, response.Body)
		if err == nil && response.StatusCode != http.StatusOK {
			err = fmt.Errorf("活动请求返回 %d", response.StatusCode)
		}
		holdResponse <- err
	}()
	select {
	case <-holdStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("活动请求没有开始")
	}

	cancel()
	select {
	case err := <-serverDone:
		t.Fatalf("活动请求未完成时服务器已退出: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if !f.app.shutdownStarted.Load() {
		t.Fatal("停止信号后没有立即拒绝新请求")
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/tournaments", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("停止期间新请求应返回503，实际为%d", response.Code)
	}
	select {
	case err := <-holdFinished:
		t.Fatalf("活动请求在释放前已经操作完数据库: %v", err)
	default:
	}

	sseClosed := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, reader)
		sseClosed <- err
	}()
	select {
	case err := <-sseClosed:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("优雅关闭SSE时读取失败: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("停止信号没有关闭SSE")
	}
	close(releaseHold)
	select {
	case err := <-holdResponse:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("活动请求没有在停止后完成")
	}
	select {
	case err := <-holdFinished:
		if err != nil {
			t.Fatalf("活动请求结束前数据库不可用: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("活动请求未完成数据库操作")
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("服务器优雅退出失败: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("活动请求完成后服务器没有退出")
	}
	f.app.broadcast.mu.Lock()
	defer f.app.broadcast.mu.Unlock()
	if len(f.app.broadcast.tournaments) != 0 {
		t.Fatal("服务器退出后仍有SSE订阅")
	}
}
