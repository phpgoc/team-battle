package app

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHTTPConcurrencyLimitsAreIndependent(t *testing.T) {
	app := &App{httpLimits: httpConcurrencyLimits{reads: 1, writes: 2}}
	router := app.router()
	readStarted := make(chan struct{}, 1)
	writeStarted := make(chan struct{}, 2)
	releaseRead, releaseWrite := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(releaseRead); close(releaseWrite) }) }
	defer release()
	read := func(c *gin.Context) {
		readStarted <- struct{}{}
		<-releaseRead
		c.Status(http.StatusOK)
	}
	write := func(c *gin.Context) {
		writeStarted <- struct{}{}
		<-releaseWrite
		c.Status(http.StatusOK)
	}
	router.GET("/read", read)
	router.POST("/write", write)

	request := func(method, path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		return response
	}
	done := make(chan int, 3)
	go func() { done <- request(http.MethodGet, "/read").Code }()
	go func() { done <- request(http.MethodPost, "/write").Code }()
	go func() { done <- request(http.MethodPost, "/write").Code }()
	wait := func(started <-chan struct{}) {
		t.Helper()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("请求没有进入处理器")
		}
	}
	wait(readStarted)
	wait(writeStarted)
	wait(writeStarted)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/read"
		if method == http.MethodPost {
			path = "/write"
		}
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() { result <- request(method, path) }()
		select {
		case response := <-result:
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
				t.Fatalf("%s 超额响应错误: %d %v", method, response.Code, response.Header())
			}
		case <-time.After(3 * time.Second):
			t.Fatal("超额请求未及时返回")
		}
	}
	// 无效赛事编号触发 SSE 自身的校验，即便普通读名额已满。
	if response := request(http.MethodGet, "/api/public/tournaments/bad/stream"); response.Code != http.StatusBadRequest {
		t.Fatalf("SSE 受普通读配额影响: %d", response.Code)
	}
	release()
	for i := 0; i < 3; i++ {
		if code := <-done; code != http.StatusOK {
			t.Fatalf("配额内的请求失败: %d", code)
		}
	}
	if response := request(http.MethodGet, "/read"); response.Code != http.StatusOK {
		t.Fatalf("读名额未释放: %d", response.Code)
	}
	if response := request(http.MethodPost, "/write"); response.Code != http.StatusOK {
		t.Fatalf("写名额未释放: %d", response.Code)
	}
}
