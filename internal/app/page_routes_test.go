package app

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"
)

func TestWorkspacePageRoutesAndPermissions(t *testing.T) {
	f := newWorkflowFixture(t)
	for _, path := range []string{"/events/new", "/events/archive", "/accounts", "/accounts/new", "/accounts/3", "/site-settings", "/backgrounds", "/account"} {
		t.Run(path, func(t *testing.T) {
			response := sessionRequest(f.handler, http.MethodGet, path, nil, nil)
			if response.Code != http.StatusFound || response.Header().Get("Location") != "/login?return="+url.QueryEscape(path) {
				t.Fatalf("未登录应返回登录页并保留原地址: %d %s", response.Code, response.Header().Get("Location"))
			}
			cookie := &http.Cookie{Name: "tb_session", Value: f.tokens[1]}
			response = sessionRequest(f.handler, http.MethodGet, path, nil, cookie)
			if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("/assets/app.js")) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("管理员直接打开页面失败: %d %s", response.Code, response.Body.String())
			}
			want := http.StatusForbidden
			if path == "/account" {
				want = http.StatusOK
			}
			f.request(t, 3, http.MethodGet, path, nil, want)
		})
	}
	for _, path := range []string{"/accounts/0", "/accounts/-1", "/accounts/no-such-user", "/accounts/999999", "/workspace/unknown", "/unknown-page"} {
		f.request(t, 1, http.MethodGet, path, nil, http.StatusNotFound)
	}
	for _, path := range []string{"/", "/login", "/?name=test&p=2"} {
		f.request(t, 0, http.MethodGet, path, nil, http.StatusOK)
	}
	// 会话失效后刷新私有页面，不能继续返回管理页。
	if _, err := f.app.db.Exec(`UPDATE users SET session_token=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	f.request(t, 1, http.MethodGet, "/backgrounds", nil, http.StatusFound)
}

func TestLegacyPageBookmarksRedirectToPaths(t *testing.T) {
	f := newWorkflowFixture(t)
	for _, item := range []struct{ old, target string }{
		{"/workspace", "/"},
		{"/workspace?page=home&name=test&p=2", "/?name=test&p=2"},
		{"/workspace?page=createEvent", "/events/new"},
		{"/workspace?page=events", "/events/new"},
		{"/workspace?page=settings", "/account"},
		{"/workspace?page=accounts", "/accounts"},
		{"/workspace?page=accounts&edit=new", "/accounts/new"},
		{"/workspace?page=accounts&edit=3", "/accounts/3"},
		{"/workspace?page=createUser", "/accounts/new"},
		{"/workspace?page=users", "/accounts"},
		{"/workspace?page=passwords", "/accounts"},
		{"/workspace?page=archive", "/events/archive"},
		{"/workspace?page=siteSettings", "/site-settings"},
		{"/workspace?page=site-settings", "/site-settings"},
		{"/workspace?page=backgrounds", "/backgrounds"},
		{"/history?name=test&p=2", "/?name=test&p=2"},
		{"/live", "/"},
	} {
		response := sessionRequest(f.handler, http.MethodGet, item.old, nil, nil)
		if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != item.target {
			t.Fatalf("旧书签 %s 应跳转 %s: %d %s", item.old, item.target, response.Code, response.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/workspace?page=unknown", "/workspace?page=accounts&edit=0", "/workspace?page=accounts&edit=https://example.com"} {
		f.request(t, 0, http.MethodGet, path, nil, http.StatusNotFound)
	}
}
