package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type apiPermissionCase struct {
	method string
	path   string
	scope  string
}

// 权限按接口职责列出；新增受保护接口时必须补充分类，不能漏过访客检查。
var apiPermissionCases = []apiPermissionCase{
	{"GET", "/api/users", "superadmin"},
	{"POST", "/api/users", "superadmin"},
	{"PATCH", "/api/users/:id", "superadmin"},
	{"POST", "/api/users/:id/avatar", "superadmin"},
	{"DELETE", "/api/users/:id", "superadmin"},
	{"PATCH", "/api/users/:id/password", "superadmin"},
	{"POST", "/api/tournaments", "superadmin"},
	{"POST", "/api/tournaments/archive", "superadmin"},
	{"DELETE", "/api/tournaments/:id", "superadmin"},
	{"POST", "/api/tournaments/:id/restore", "superadmin"},
	{"GET", "/api/site-settings", "superadmin"},
	{"PATCH", "/api/site-settings/tournament-defaults", "superadmin"},
	{"POST", "/api/site-settings/backgrounds", "superadmin"},
	{"PATCH", "/api/site-settings/backgrounds/:backgroundID", "superadmin"},
	{"DELETE", "/api/site-settings/backgrounds/:backgroundID", "superadmin"},
	{"POST", "/api/site-settings/tournament-names", "superadmin"},
	{"PATCH", "/api/site-settings/tournament-names/:nameID", "superadmin"},
	{"DELETE", "/api/site-settings/tournament-names/:nameID", "superadmin"},
	{"POST", "/api/site-settings/categories", "superadmin"},
	{"PATCH", "/api/site-settings/categories/:categoryID", "superadmin"},
	{"DELETE", "/api/site-settings/categories/:categoryID", "superadmin"},
	{"POST", "/api/tournaments/:id/players", "manager"},
	{"GET", "/api/tournaments/:id/eligible-users", "manager"},
	{"PATCH", "/api/tournaments/:id/settings", "manager"},
	{"POST", "/api/tournaments/:id/setup-draw", "manager"},
	{"PATCH", "/api/tournaments/:id/draw-settings", "manager"},
	{"PATCH", "/api/tournaments/:id/background", "manager"},
	{"POST", "/api/tournaments/:id/captains/confirm", "manager"},
	{"POST", "/api/tournaments/:id/captains/select", "manager"},
	{"POST", "/api/tournaments/:id/lineup/generate", "manager"},
	{"POST", "/api/tournaments/:id/start", "manager"},
	{"POST", "/api/tournaments/:id/opening-result", "manager"},
	{"POST", "/api/tournaments/:id/matches/:matchID/result", "manager"},
	{"PATCH", "/api/tournaments/:id/matches/:matchID/result", "manager"},
	{"POST", "/api/tournaments/:id/ace/confirm", "manager"},
	{"POST", "/api/tournaments/:id/ace/draw", "manager"},
	{"POST", "/api/tournaments/:id/cancel", "manager"},
	{"POST", "/api/tournaments/:id/confirm", "manager"},
	{"GET", "/api/auth/me", "authenticated"},
	{"PATCH", "/api/auth/profile", "authenticated"},
	{"POST", "/api/auth/avatar", "authenticated"},
	{"GET", "/api/settings/catalog", "authenticated"},
	{"GET", "/api/tournaments", "authenticated"},
	{"GET", "/api/tournaments/:id", "authenticated"},
	{"POST", "/api/tournaments/:id/captain-votes", "authenticated"},
	{"POST", "/api/tournaments/:id/lineup/submit", "authenticated"},
	{"GET", "/api/tournaments/:id/lineup", "authenticated"},
	{"POST", "/api/tournaments/:id/kof/next-player", "authenticated"},
	{"GET", "/api/tournaments/:id/kof/options", "authenticated"},
	{"POST", "/api/tournaments/:id/kof/starter", "authenticated"},
}

func TestAPIPermissionMatrixCoversRegisteredRoutes(t *testing.T) {
	f := newWorkflowFixture(t)
	classified := map[string]bool{}
	for _, item := range apiPermissionCases {
		key := item.method + " " + item.path
		if classified[key] {
			t.Fatalf("权限矩阵重复接口: %s", key)
		}
		classified[key] = true
	}
	public := map[string]bool{
		"POST /api/auth/login":                   true,
		"POST /api/auth/logout":                  true,
		"GET /api/backgrounds":                   true,
		"GET /api/backgrounds/:id/image":         true,
		"GET /api/backgrounds/:id/thumbnail":     true,
		"GET /api/categories/:id/icon":           true,
		"GET /api/users/:id/avatar":              true,
		"GET /api/public/tournaments":            true,
		"GET /api/public/history":                true,
		"GET /api/public/users/:id/records":      true,
		"GET /api/public/tournaments/:id":        true,
		"GET /api/public/tournaments/:id/stream": true,
	}
	for _, route := range f.handler.(*gin.Engine).Routes() {
		if !strings.HasPrefix(route.Path, "/api/") {
			continue
		}
		key := route.Method + " " + route.Path
		if !classified[key] && !public[key] {
			t.Errorf("接口未纳入权限矩阵或公开白名单: %s", key)
		}
		delete(classified, key)
	}
	for key := range classified {
		t.Errorf("权限矩阵中的接口未注册: %s", key)
	}
}

// 对比应用表全部字段，确保越权响应没有伴随数据库写入。
func permissionDatabaseSnapshot(t *testing.T, f *workflowFixture) map[string]string {
	t.Helper()
	rows, err := f.app.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	result := map[string]string{}
	for _, table := range tables {
		rows, err := f.app.db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		data := [][]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			data = append(data, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		result[table] = string(raw)
	}
	return result
}

func apiPermissionPath(item apiPermissionCase, tournamentID int64) string {
	path := strings.NewReplacer(":matchID", "1", ":backgroundID", "1", ":nameID", "1", ":categoryID", "1").Replace(item.path)
	id := tournamentID
	if strings.HasPrefix(path, "/api/users/") {
		id = 4
	}
	return strings.ReplaceAll(path, ":id", fmt.Sprint(id))
}

func TestProtectedAPIsRejectAnonymousAndInvalidSessions(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	stale := &http.Cookie{Name: "tb_session", Value: f.tokens[1]}
	if _, err := f.app.db.Exec(`UPDATE users SET session_token='replacement-admin-session' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	before := permissionDatabaseSnapshot(t, f)
	for _, identity := range []struct {
		name   string
		cookie *http.Cookie
	}{
		{"未登录", nil},
		{"伪造会话", &http.Cookie{Name: "tb_session", Value: "invalid-session"}},
		{"已被顶掉的超管会话", stale},
	} {
		t.Run(identity.name, func(t *testing.T) {
			for _, item := range apiPermissionCases {
				t.Run(item.method+" "+item.path, func(t *testing.T) {
					response := sessionRequest(f.handler, item.method, apiPermissionPath(item, id), map[string]any{}, identity.cookie)
					if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
						t.Fatalf("未授权接口必须在处理数据前拒绝请求: %d %s", response.Code, response.Body.String())
					}
					var failure struct{ Error string }
					if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || failure.Error == "" {
						t.Fatalf("拒绝访问必须返回错误信息，不能返回业务数据: %s", response.Body.String())
					}
				})
			}
		})
	}
	if !reflect.DeepEqual(before, permissionDatabaseSnapshot(t, f)) {
		t.Fatal("未授权请求修改了数据库")
	}
}

func TestAdministrativeAPIsRejectRoleAndTournamentEscalation(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	other := f.create(t, "random")
	f.request(t, 1, "PATCH", fmt.Sprintf("/api/tournaments/%d/settings", other), map[string]any{"manager_id": 9}, 200)
	before := permissionDatabaseSnapshot(t, f)
	for _, identity := range []struct {
		name string
		id   int
	}{
		{"本赛事管理员", 2}, {"队长", 3}, {"普通选手", 4}, {"其他赛事管理员", 9},
	} {
		t.Run(identity.name, func(t *testing.T) {
			for _, item := range apiPermissionCases {
				if item.scope != "superadmin" && (item.scope != "manager" || identity.id == 2) {
					continue
				}
				t.Run(item.method+" "+item.path, func(t *testing.T) {
					f.request(t, identity.id, item.method, apiPermissionPath(item, id), map[string]any{}, http.StatusForbidden)
				})
			}
		})
	}
	if !reflect.DeepEqual(before, permissionDatabaseSnapshot(t, f)) {
		t.Fatal("越权请求修改了数据库")
	}
	// 正向验证相同接口确实可用：本赛事管理员与超管均能修改，其他赛事管理员只能改自己的赛事。
	for _, allowed := range []struct {
		actor int
		id    int64
	}{{2, id}, {9, other}, {1, id}, {1, other}} {
		f.request(t, allowed.actor, "PATCH", fmt.Sprintf("/api/tournaments/%d/draw-settings", allowed.id), map[string]any{"random_draw_seconds": 6, "ace_draw_seconds": 7}, 200)
	}
	f.request(t, 1, "GET", "/api/users", nil, 200)
	f.request(t, 1, "GET", "/api/site-settings", nil, 200)
}
