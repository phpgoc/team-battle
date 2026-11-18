package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func sessionRequest(handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func sessionCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("登录失败: %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tb_session" {
			return cookie
		}
	}
	t.Fatal("响应没有会话Cookie")
	return nil
}

func prepareSessionLogin(t *testing.T, f *workflowFixture) map[string]any {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`UPDATE users SET password_hash=? WHERE id=3`, string(hash)); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"username": "user3", "password": "test-password"}
}

func TestPersistentSessionReplacementAndLogout(t *testing.T) {
	f := newWorkflowFixture(t)
	credentials := prepareSessionLogin(t, f)
	first := sessionCookie(t, sessionRequest(f.handler, "POST", "/api/auth/login", credentials, nil))
	if !first.HttpOnly || first.MaxAge < 365*24*60*60 || first.SameSite != http.SameSiteLaxMode {
		t.Fatalf("登录必须设置长期HttpOnly Cookie: %+v", first)
	}
	if response := sessionRequest(f.handler, "GET", "/api/auth/me", nil, first); response.Code != 200 {
		t.Fatalf("首次登录不可用: %d", response.Code)
	}
	second := sessionCookie(t, sessionRequest(f.handler, "POST", "/api/auth/login", credentials, nil))
	if first.Value == second.Value {
		t.Fatal("重新登录必须替换会话标识")
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sessions WHERE user_id=3`)
	if response := sessionRequest(f.handler, "GET", "/api/auth/me", nil, first); response.Code != 401 {
		t.Fatal("旧设备必须立即失效")
	}
	// 其他账号的会话不受影响，旧设备退出也不能删除新设备的会话。
	f.request(t, 4, "GET", "/api/auth/me", nil, 200)
	if response := sessionRequest(f.handler, "POST", "/api/auth/logout", nil, first); response.Code != 200 {
		t.Fatal("退出旧会话失败")
	}
	if _, err := f.app.db.Exec(`UPDATE sessions SET created_at='2000-01-01 00:00:00' WHERE user_id=3`); err != nil {
		t.Fatal(err)
	}
	// 重新打开同一个SQLite文件，模拟重启后没有任何进程内状态的应用。
	var sequence int
	var name, path string
	if err := f.app.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := migrate(reopened); err != nil {
		t.Fatal(err)
	}
	restarted := (&App{db: reopened}).router()
	response := sessionRequest(restarted, "GET", "/api/auth/me", nil, second)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("重启后应继续保持登录: %d %s", response.Code, response.Body.String())
	}
	if renewed := sessionCookie(t, response); renewed.MaxAge != sessionCookieMaxAge {
		t.Fatal("鉴权后未续期持久Cookie")
	}
	third := sessionCookie(t, sessionRequest(f.handler, "POST", "/api/auth/login", credentials, nil))
	if response := sessionRequest(restarted, "GET", "/api/auth/me", nil, second); response.Code != 401 {
		t.Fatal("另一个进程也必须识别被替换的会话")
	}
	response = sessionRequest(restarted, "POST", "/api/auth/logout", nil, third)
	if response.Code != 200 || sessionCookie(t, response).MaxAge >= 0 {
		t.Fatal("退出应清除持久Cookie")
	}
	if response := sessionRequest(f.handler, "GET", "/api/auth/me", nil, third); response.Code != 401 {
		t.Fatal("退出后服务端会话仍然有效")
	}
}

func TestFailedLoginPreservesCurrentSession(t *testing.T) {
	f := newWorkflowFixture(t)
	credentials := prepareSessionLogin(t, f)
	cookie := sessionCookie(t, sessionRequest(f.handler, "POST", "/api/auth/login", credentials, nil))
	if response := sessionRequest(f.handler, "POST", "/api/auth/login", map[string]any{"username": "user3", "password": "wrong"}, nil); response.Code != 401 {
		t.Fatal("错误密码必须拒绝登录")
	}
	if _, err := f.app.db.Exec(`CREATE TRIGGER reject_session_update BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := sessionRequest(f.handler, "POST", "/api/auth/login", credentials, nil)
	if response.Code != 500 || len(response.Result().Cookies()) != 0 {
		t.Fatalf("写入失败时不能签发Cookie: %d", response.Code)
	}
	if response := sessionRequest(f.handler, "GET", "/api/auth/me", nil, cookie); response.Code != 200 {
		t.Fatal("登录失败不应撤销原会话")
	}
}

func TestReplacedSessionReturnsUnauthorizedOnProtectedActions(t *testing.T) {
	f := newWorkflowFixture(t)
	cookie := &http.Cookie{Name: "tb_session", Value: f.tokens[1]}
	if _, err := f.app.db.Exec(`UPDATE sessions SET token='replacement-token' WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/users"}, {"POST", "/api/users"}, {"PATCH", "/api/users/3/password"},
		{"POST", "/api/events"}, {"POST", "/api/events/archive"}, {"POST", "/api/events/1/confirm"},
		{"GET", "/api/site-settings"}, {"PATCH", "/api/auth/profile"},
	} {
		response := sessionRequest(f.handler, endpoint.method, endpoint.path, nil, cookie)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("旧会话访问 %s %s 应返回401: %d %s", endpoint.method, endpoint.path, response.Code, response.Body.String())
		}
	}
	// 真正的越权仍然返回403，前端不能因此把有效会话退出。
	f.request(t, 3, "GET", "/api/users", nil, http.StatusForbidden)
	f.request(t, 3, "GET", "/api/auth/me", nil, http.StatusOK)
}

func TestConcurrentLoginsLeaveOneValidSession(t *testing.T) {
	f := newWorkflowFixture(t)
	credentials := prepareSessionLogin(t, f)
	responses := make(chan *httptest.ResponseRecorder, 4)
	for i := 0; i < cap(responses); i++ {
		go func() { responses <- sessionRequest(f.handler, "POST", "/api/auth/login", credentials, nil) }()
	}
	valid := 0
	cookies := []*http.Cookie{}
	for i := 0; i < cap(responses); i++ {
		cookies = append(cookies, sessionCookie(t, <-responses))
	}
	for _, cookie := range cookies {
		response := sessionRequest(f.handler, "GET", "/api/auth/me", nil, cookie)
		if response.Code == 200 {
			valid++
		} else if response.Code != 401 {
			t.Fatalf("意外鉴权状态: %d", response.Code)
		}
	}
	if valid != 1 {
		t.Fatalf("并发登录后只应有一个有效会话，实际为 %d", valid)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sessions WHERE user_id=3`)
}

func TestSessionMigrationKeepsLatestLogin(t *testing.T) {
	f := newWorkflowFixture(t)
	if _, err := f.app.db.Exec(`DROP INDEX sessions_one_per_user`); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"older", "latest", "latest-tie"} {
		created := "2000-01-01 00:00:00"
		if token != "older" {
			created = "2030-01-01 00:00:00"
		}
		if _, err := f.app.db.Exec(`INSERT INTO sessions(token,user_id,created_at) VALUES(?,3,?)`, token, created); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sessions WHERE user_id=3 AND token='latest-tie'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sessions WHERE user_id=3`)
	if _, err := f.app.db.Exec(`INSERT INTO sessions(token,user_id) VALUES('duplicate',3)`); err == nil {
		t.Fatal("数据库必须拒绝同一账号的第二个会话")
	}
}
