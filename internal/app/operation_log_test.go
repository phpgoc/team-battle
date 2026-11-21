package app

import (
	"bytes"
	"database/sql/driver"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOperationLogWritesExpandedSQLInStableFields(t *testing.T) {
	var output bytes.Buffer
	logger := newOperationLogger(&output, "zh")
	db, err := openDatabase(t.TempDir()+"/operation-log.sqlite", logger.capture)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	operation := logger.begin("/api/site-settings/tournament-defaults", "PATCH")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, "draw_animation", "slide's mode"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	logger.finish(operation, 200, User{ID: 7, DisplayName: "管理员"})

	logged := output.String()
	for _, expected := range []string{`who="管理员##7"`, `tag="修改赛事默认设置"`, `sql="INSERT INTO settings(key,value) VALUES('draw_animation','slide''s mode')"`} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("日志缺少 %q: %s", expected, logged)
		}
	}
	if strings.Index(logged, "who=") > strings.Index(logged, "time=") || strings.Index(logged, "time=") > strings.Index(logged, "tag=") || strings.Index(logged, "tag=") > strings.Index(logged, "sql=") {
		t.Fatalf("日志字段顺序不正确: %s", logged)
	}
	timeField := strings.Index(logged, `time="`)
	if timeField < 0 {
		t.Fatalf("日志缺少time字段: %s", logged)
	}
	timeValueStart := timeField + len(`time="`)
	timeValueEnd := strings.IndexByte(logged[timeValueStart:], '"')
	if timeValueEnd < 0 {
		t.Fatalf("time字段没有结束引号: %s", logged)
	}
	if _, err := time.Parse("2006-01-02 15:04:05", logged[timeValueStart:timeValueStart+timeValueEnd]); err != nil {
		t.Fatalf("时间格式不正确: %s", logged)
	}
}

func TestOperationLogMiddlewareCapturesAuthenticatedWrite(t *testing.T) {
	var output bytes.Buffer
	logger := newOperationLogger(&output, "zh")
	db, err := openDatabase(t.TempDir()+"/operation-log-request.sqlite", logger.capture)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(username,display_name,password_hash,role,session_token) VALUES('admin','管理员','hash','superadmin','admin-session')`); err != nil {
		t.Fatal(err)
	}
	application := &App{db: db, rng: rand.New(rand.NewSource(1)), operationLogger: logger}
	request := httptest.NewRequest(http.MethodPatch, "/api/site-settings/tournament-defaults", strings.NewReader(`{"random_draw_seconds":8}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "tb_session", Value: "admin-session"})
	response := httptest.NewRecorder()
	application.router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("更新网站默认值失败: %d %s", response.Code, response.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("一次操作应合并为一行日志: %s", output.String())
	}
	for _, expected := range []string{`who="管理员##1"`, `tag="修改赛事默认设置"`, `INSERT INTO config(key,value) VALUES('random_draw_seconds','8')`} {
		if !strings.Contains(lines[0], expected) {
			t.Fatalf("请求日志缺少 %q: %s", expected, lines[0])
		}
	}
}

func TestTournamentSettingsLogUsesStandardSQLWithoutBeforeValues(t *testing.T) {
	var output bytes.Buffer
	logger := newOperationLogger(&output, "zh")
	db, err := openDatabase(t.TempDir()+"/operation-log-settings.sqlite", logger.capture)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE tournaments(id INTEGER PRIMARY KEY,name TEXT,rules_json TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE tournament_actions(id INTEGER PRIMARY KEY,payload TEXT)`); err != nil {
		t.Fatal(err)
	}
	operation := logger.begin("/api/tournaments/:id/settings", "PATCH")
	fullRules := `{"round1_mode":"random","kof_starter":"captain","extra_info":{"round1":[],"round2":[],"ace":[]}}`
	if _, err := db.Exec(`UPDATE tournaments SET name=? WHERE id=?`, "Finals", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tournaments SET rules_json=? WHERE id=?`, fullRules, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tournament_actions(payload) VALUES(?)`, `{"name":{"from":"Old","to":"Finals"},"rules":{"round1_mode":{"from":"manual","to":"random"}}}`); err != nil {
		t.Fatal(err)
	}
	logger.captureTournamentOperation(tournamentOperation{
		Action:  "tournament_settings_updated",
		Payload: `{"name":{"from":"Old","to":"Finals"},"rules":{"round1_mode":{"from":"manual","to":"random"}}}`,
	})
	logger.finish(operation, 200, User{ID: 2, DisplayName: "管理员"})

	logged := output.String()
	for _, expected := range []string{
		`UPDATE tournaments SET name='Finals' WHERE id=1`,
		`UPDATE tournaments SET rules_json='` + strings.ReplaceAll(fullRules, `"`, `\"`) + `' WHERE id=1`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("设置日志缺少标准SQL %q: %s", expected, logged)
		}
	}
	if strings.Contains(logged, "/* changed:") || strings.Contains(logged, "\"from\"") || strings.Contains(logged, "INSERT INTO tournament_actions") {
		t.Fatalf("设置日志不应附加旧值摘要或重复输出审计写入: %s", logged)
	}
}

func TestImageAndAuthenticationLogsOmitSQL(t *testing.T) {
	var output bytes.Buffer
	logger := newOperationLogger(&output, "en")
	db, err := openDatabase(t.TempDir()+"/operation-log-image.sqlite", logger.capture)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE images (id INTEGER PRIMARY KEY, icon_data BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	operation := logger.begin("/api/auth/avatar", "POST")
	if _, err := db.Exec(`INSERT INTO images(icon_data) VALUES(?)`, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	logger.finish(operation, 200, User{ID: 2, DisplayName: "选手"})
	operation = logger.begin("/api/site-settings/extra-infos/:extraInfoID", "PATCH")
	if _, err := db.Exec(`UPDATE images SET icon_data=X'' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	logger.finish(operation, 200, User{ID: 2, DisplayName: "选手"})
	logger.logLoginSuccess(User{ID: 2, DisplayName: "选手"})
	logger.logLoginFailure("unknown")
	logger.logLogout(User{ID: 2, DisplayName: "选手"})

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("预期5条日志，得到%d条: %s", len(lines), output.String())
	}
	if !strings.Contains(lines[0], `tag="Update account profile"`) || !strings.Contains(lines[0], `sql=""`) {
		t.Fatalf("图片操作日志应省略SQL: %s", lines[0])
	}
	if !strings.Contains(lines[1], `tag="Update extra info"`) || !strings.Contains(lines[1], `sql=""`) {
		t.Fatalf("清除图片日志应省略SQL: %s", lines[1])
	}
	for _, line := range lines[1:] {
		if !strings.Contains(line, `sql=""`) {
			t.Fatalf("认证日志应省略SQL: %s", line)
		}
	}
}

func TestOperationLogsUseLanguageOnlyForTagsAndSkipFailures(t *testing.T) {
	tests := []struct {
		language string
		expected string
	}{
		{"zh", "修改账号密码"},
		{"en", "Change account password"},
		{"ko", "계정 비밀번호 변경"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			var output bytes.Buffer
			logger := newOperationLogger(&output, test.language)
			operation := logger.begin("/api/users/:id/password", "PATCH")
			logger.captureAccountOperation("重置账号密码")
			operation.statements = []string{"UPDATE users SET password_hash='hash' WHERE id=4"}
			logger.finish(operation, 200, User{ID: 1, DisplayName: "admin"})
			if !strings.Contains(output.String(), `tag="`+test.expected+`"`) {
				t.Fatalf("日志标签未切换为%s: %s", test.language, output.String())
			}
			output.Reset()
			operation = logger.begin("/api/users/:id/password", "PATCH")
			operation.statements = []string{"UPDATE users SET password_hash='hash' WHERE id=4"}
			logger.finish(operation, 400, User{ID: 1, DisplayName: "admin"})
			if output.Len() != 0 {
				t.Fatalf("失败操作不应输出日志: %s", output.String())
			}
		})
	}
}

func TestRenderSQLExpandsSQLiteArguments(t *testing.T) {
	query := `UPDATE users SET display_name=?,active=?,category_id=?,avatar_data=? WHERE id=?`
	args := []driver.NamedValue{
		{Ordinal: 1, Value: "O'Reilly"},
		{Ordinal: 2, Value: int64(1)},
		{Ordinal: 3, Value: nil},
		{Ordinal: 4, Value: []byte{0x01, 0xff}},
		{Ordinal: 5, Value: int64(9)},
	}
	rendered, err := renderSQL(query, args)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `UPDATE users SET display_name='O''Reilly',active=1,category_id=NULL,avatar_data=X'01ff' WHERE id=9`
	if rendered != expected {
		t.Fatalf("SQL展开不正确\n实际: %s\n预期: %s", rendered, expected)
	}
}

func TestTournamentSettingsTagUsesChangedFields(t *testing.T) {
	tests := []struct {
		payload string
		tag     string
	}{
		{`{"name":"","start_time":"","player_ids":[2,3],"fixed_assignments":null,"background_id":null,"manager_id":null,"rules":null,"round1_target":0,"round2_target":0}`, "tournament_roster_updated"},
		{`{"name":"","start_time":"","player_ids":null,"fixed_assignments":null,"background_id":3,"manager_id":null,"rules":null,"round1_target":0,"round2_target":0}`, "tournament_background_updated"},
		{`{"name":"Finals","start_time":"","player_ids":null,"fixed_assignments":null,"background_id":null,"manager_id":null,"rules":null,"round1_target":0,"round2_target":0}`, "tournament_settings_updated"},
	}
	for _, test := range tests {
		if tag := tournamentSettingsLogTag(test.payload); tag != test.tag {
			t.Errorf("payload %s 得到标签 %s，预期 %s", test.payload, tag, test.tag)
		}
	}
}
