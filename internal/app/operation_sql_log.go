package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type operationLogger struct {
	writer   io.Writer
	language string
	capture  *sqlWriteCapture
	mu       sync.Mutex
}

type sqlWriteCapture struct {
	mu     sync.RWMutex
	active *sqlOperationCapture
}

type sqlOperationCapture struct {
	path        string
	tag         string
	tagPriority int
	statements  []string
	hasBlob     bool
	mu          sync.Mutex
}

func newOperationLogger(writer io.Writer, language string) *operationLogger {
	return &operationLogger{
		writer:   writer,
		language: language,
		capture:  &sqlWriteCapture{},
	}
}

func (l *operationLogger) Write(p []byte) (int, error) {
	if l == nil || l.writer == nil {
		return len(p), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writer.Write(p)
}

func (l *operationLogger) lifecyclef(format string, values ...any) {
	if l == nil {
		return
	}
	_, _ = fmt.Fprintf(l, format+"\n", values...)
}

func (l *operationLogger) logEvent(who, tag, sqlText string) {
	if l == nil {
		return
	}
	line := fmt.Sprintf("who=%q time=%q tag=%q sql=%q\n", who, time.Now().Format("2006-01-02 15:04:05"), operationTagLabel(l.language, tag), sqlText)
	_, _ = io.WriteString(l, line)
}

func (l *operationLogger) begin(path, method string) *sqlOperationCapture {
	if l == nil || l.capture == nil {
		return nil
	}
	tag, priority := operationTagForRoute(method, path)
	if tag == "" {
		return nil
	}
	operation := &sqlOperationCapture{path: path, tag: tag, tagPriority: priority}
	l.capture.mu.Lock()
	l.capture.active = operation
	l.capture.mu.Unlock()
	return operation
}

func (l *operationLogger) finish(operation *sqlOperationCapture, status int, actor User) {
	if l == nil || operation == nil {
		return
	}
	l.capture.mu.Lock()
	if l.capture.active == operation {
		l.capture.active = nil
	}
	l.capture.mu.Unlock()

	if status < 200 || status >= 300 || actor.ID == 0 {
		return
	}
	operation.mu.Lock()
	tag := operation.tag
	statements := append([]string(nil), operation.statements...)
	hasBlob := operation.hasBlob
	path := operation.path
	operation.mu.Unlock()
	if tag == "" || (len(statements) == 0 && !hasBlob) {
		return
	}
	if path == "/api/tournaments/:id/settings" {
		filtered := statements[:0]
		for _, statement := range statements {
			upper := strings.ToUpper(statement)
			if strings.Contains(upper, "INSERT INTO TOURNAMENT_ACTIONS") {
				continue
			}
			filtered = append(filtered, statement)
		}
		statements = filtered
	}
	sqlText := strings.Join(statements, "; ")
	if hasBlob {
		sqlText = ""
	}
	l.logEvent(operationLogWho(actor), tag, sqlText)
}

func (l *operationLogger) setCurrentTag(tag string) {
	if l == nil || l.capture == nil || tag == "" {
		return
	}
	l.capture.mu.RLock()
	operation := l.capture.active
	l.capture.mu.RUnlock()
	if operation == nil {
		return
	}
	operation.mu.Lock()
	if operation.tagPriority < 2 {
		operation.tag = tag
		operation.tagPriority = 2
	}
	operation.mu.Unlock()
}

func (l *operationLogger) captureTournamentOperation(operation tournamentOperation) {
	if l == nil {
		return
	}
	l.capture.mu.RLock()
	active := l.capture.active
	l.capture.mu.RUnlock()
	if operation.Action == "round1_started" && active != nil && active.path == "/api/tournaments/:id/opening-result" {
		return
	}
	if operation.Action == "tournament_settings_updated" {
		l.setCurrentTag(tournamentSettingsLogTag(operation.Payload))
		return
	}
	l.setCurrentTag(operation.Action)
}

func (l *operationLogger) captureAccountOperation(action string) {
	if l == nil {
		return
	}
	switch action {
	case "修改账号密码", "重置账号密码":
		l.setCurrentTag("account_password_updated")
	case "修改账号资料", "创建账号", "删除账号":
		l.setCurrentTag(map[string]string{"修改账号资料": "account_profile_updated", "创建账号": "account_created", "删除账号": "account_deleted"}[action])
	}
}

func (l *operationLogger) logLoginFailure(username string) {
	if l != nil {
		l.logEvent(username, "login_failed", "")
	}
}

func (l *operationLogger) logLoginSuccess(user User) {
	if l != nil {
		l.logEvent(operationLogWho(user), "login_success", "")
	}
}

func (l *operationLogger) logLogout(user User) {
	if l != nil && user.ID != 0 {
		l.logEvent(operationLogWho(user), "logout", "")
	}
}

func operationLogWho(user User) string {
	return fmt.Sprintf("%s##%d", user.DisplayName, user.ID)
}

func (a *App) operationLogActor(c *gin.Context) User {
	token, err := c.Cookie("tb_session")
	if err != nil || token == "" {
		return User{}
	}
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE session_token=? AND is_deleted=0 AND status='active'`, token).Scan(&userID); err != nil {
		return User{}
	}
	user, err := a.cachedUser(userID)
	if err != nil {
		return User{}
	}
	return user
}

func (c *sqlWriteCapture) record(ctx context.Context, query string, args []driver.NamedValue) {
	if c == nil || ctx.Value(skipSQLLogKey{}) == true || !isWriteStatement(query) {
		return
	}
	c.mu.RLock()
	operation := c.active
	c.mu.RUnlock()
	if operation == nil {
		return
	}
	for _, arg := range args {
		if data, ok := arg.Value.([]byte); ok && len(data) > 0 {
			c.omitImageSQL(operation)
			return
		}
	}
	upperQuery := strings.ToUpper(query)
	for _, imageColumn := range []string{"AVATAR_DATA", "ICON_DATA", "IMAGE_DATA", "THUMBNAIL_DATA"} {
		if strings.Contains(upperQuery, imageColumn+"=X''") {
			c.omitImageSQL(operation)
			return
		}
	}
	operation.mu.Lock()
	hasBlob := operation.hasBlob
	operation.mu.Unlock()
	if hasBlob {
		return
	}
	rendered, err := renderSQL(query, args)
	if err != nil {
		rendered = query + " /* parameters unavailable: " + err.Error() + " */"
	}
	operation.mu.Lock()
	operation.statements = append(operation.statements, rendered)
	operation.mu.Unlock()
}

func (c *sqlWriteCapture) omitImageSQL(operation *sqlOperationCapture) {
	operation.mu.Lock()
	operation.hasBlob = true
	operation.statements = nil
	operation.mu.Unlock()
}

type skipSQLLogKey struct{}

func withoutSQLLog(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipSQLLogKey{}, true)
}

func operationTagForRoute(method, path string) (string, int) {
	if method == "PATCH" || method == "POST" || method == "DELETE" {
		switch path {
		case "/api/auth/profile", "/api/auth/avatar", "/api/users/:id/avatar":
			return "account_profile_updated", 1
		case "/api/users":
			return "account_created", 1
		case "/api/users/:id":
			if method == "DELETE" {
				return "account_deleted", 1
			}
			return "account_profile_updated", 1
		case "/api/users/:id/password":
			return "account_password_updated", 1
		case "/api/tournaments":
			return "tournament_created", 1
		case "/api/tournaments/archive":
			return "historical_tournament_created", 1
		case "/api/tournaments/:id/settings":
			return "tournament_settings_updated", 1
		case "/api/tournaments/:id/players":
			return "tournament_roster_updated", 1
		case "/api/tournaments/:id/draw-settings":
			return "draw_settings_updated", 1
		case "/api/tournaments/:id/background":
			return "tournament_background_updated", 1
		case "/api/tournaments/:id/setup-draw":
			return "tournament_setup_drawn", 1
		case "/api/tournaments/:id/restore":
			return "tournament_restored", 1
		case "/api/tournaments/:id/permanent":
			return "tournament_permanently_deleted", 1
		case "/api/tournaments/:id":
			return "tournament_deleted", 1
		case "/api/tournaments/:id/captain-votes":
			return "captain_vote_cast", 1
		case "/api/tournaments/:id/captains/confirm":
			return "captains_confirmed", 1
		case "/api/tournaments/:id/captains/select":
			return "team_captain_selected", 1
		case "/api/tournaments/:id/lineup/generate":
			return "lineup_generated", 1
		case "/api/tournaments/:id/lineup/submit":
			return "lineup_submitted", 1
		case "/api/tournaments/:id/start":
			return "round1_started", 1
		case "/api/tournaments/:id/opening-result", "/api/tournaments/:id/matches/:matchID/result":
			return "match_result_submitted", 1
		case "/api/tournaments/:id/kof/next-player":
			return "kof_next_reserved", 1
		case "/api/tournaments/:id/kof/starter":
			return "kof_starter_selected", 1
		case "/api/tournaments/:id/ace/confirm":
			return "ace_players_overridden", 1
		case "/api/tournaments/:id/ace/draw":
			return "ace_drawn", 1
		case "/api/tournaments/:id/cancel":
			return "tournament_cancelled", 1
		case "/api/tournaments/:id/confirm":
			return "tournament_completed", 1
		case "/api/site-settings/tournament-defaults":
			return "site_defaults_updated", 1
		case "/api/site-settings/tournament-names":
			return "site_tournament_name_created", 1
		case "/api/site-settings/tournament-names/:nameID":
			if method == "DELETE" {
				return "site_tournament_name_deleted", 1
			}
			return "site_tournament_name_updated", 1
		case "/api/site-settings/categories":
			return "site_category_created", 1
		case "/api/site-settings/categories/:categoryID":
			if method == "DELETE" {
				return "site_category_deleted", 1
			}
			return "site_category_updated", 1
		case "/api/site-settings/extra-infos":
			return "site_extra_info_created", 1
		case "/api/site-settings/extra-infos/:extraInfoID":
			if method == "DELETE" {
				return "site_extra_info_deleted", 1
			}
			return "site_extra_info_updated", 1
		case "/api/site-settings/backgrounds":
			return "site_background_created", 1
		case "/api/site-settings/backgrounds/:backgroundID":
			if method == "DELETE" {
				return "site_background_deleted", 1
			}
			return "site_background_updated", 1
		}
		if method == "PATCH" && strings.HasPrefix(path, "/api/tournaments/:id/matches/") && strings.HasSuffix(path, "/result") {
			return "match_result_corrected", 1
		}
	}
	return "", 0
}

func tournamentSettingsLogTag(payload string) string {
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &values) != nil {
		return "tournament_settings_updated"
	}
	present := func(key string) bool {
		value, exists := values[key]
		return exists && string(value) != "null"
	}
	nonEmptyString := func(key string) bool {
		var value string
		return json.Unmarshal(values[key], &value) == nil && strings.TrimSpace(value) != ""
	}
	nonZeroNumber := func(key string) bool {
		var value int
		return json.Unmarshal(values[key], &value) == nil && value != 0
	}
	rosterChanged := present("roster") || present("player_ids") || (present("fixed_assignments") && string(values["fixed_assignments"]) != "{}")
	backgroundChanged := present("background_id")
	generalChanged := nonEmptyString("name") || nonEmptyString("start_time") || present("manager_id") || present("rules") || present("random_draw_seconds") || present("ace_draw_seconds") || nonZeroNumber("round1_target") || nonZeroNumber("round2_target")
	if rosterChanged && !backgroundChanged && !generalChanged {
		return "tournament_roster_updated"
	}
	if backgroundChanged && !rosterChanged && !generalChanged {
		return "tournament_background_updated"
	}
	return "tournament_settings_updated"
}

var nextLoggingDriverID atomic.Uint64

type operationLogDriver struct {
	base    driver.Driver
	capture *sqlWriteCapture
}

func (d *operationLogDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &operationLogConn{Conn: conn, capture: d.capture}, nil
}

type operationLogConn struct {
	driver.Conn
	capture *sqlWriteCapture
}

func (c *operationLogConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &operationLogStmt{Stmt: stmt, query: query, capture: c.capture}, nil
}

func (c *operationLogConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var stmt driver.Stmt
	var err error
	if preparer, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err = preparer.PrepareContext(ctx, query)
	} else {
		stmt, err = c.Conn.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &operationLogStmt{Stmt: stmt, query: query, capture: c.capture}, nil
}

func (c *operationLogConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	if opts.Isolation != driver.IsolationLevel(sql.LevelDefault) || opts.ReadOnly {
		return nil, errors.New("sqlite driver does not support requested transaction options")
	}
	return c.Conn.Begin()
}

func (c *operationLogConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if executor, ok := c.Conn.(driver.ExecerContext); ok {
		result, err := executor.ExecContext(ctx, query, args)
		if !errors.Is(err, driver.ErrSkip) {
			if err == nil {
				c.capture.record(ctx, query, args)
			}
			return result, err
		}
	}
	stmt, err := c.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	execer, ok := stmt.(driver.StmtExecContext)
	if !ok {
		values := make([]driver.Value, len(args))
		for i, arg := range args {
			values[i] = arg.Value
		}
		return stmt.Exec(values)
	}
	return execer.ExecContext(ctx, args)
}

func (c *operationLogConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		rows, err := queryer.QueryContext(ctx, query, args)
		if !errors.Is(err, driver.ErrSkip) {
			return rows, err
		}
	}
	stmt, err := c.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	querier, ok := stmt.(driver.StmtQueryContext)
	if !ok {
		stmt.Close()
		return nil, driver.ErrSkip
	}
	rows, err := querier.QueryContext(ctx, args)
	if err != nil {
		stmt.Close()
		return nil, err
	}
	return &operationLogRows{Rows: rows, stmt: stmt}, nil
}

type operationLogStmt struct {
	driver.Stmt
	query   string
	capture *sqlWriteCapture
}

func (s *operationLogStmt) Exec(args []driver.Value) (driver.Result, error) {
	result, err := s.Stmt.Exec(args)
	if err == nil {
		named := make([]driver.NamedValue, len(args))
		for i, value := range args {
			named[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
		}
		s.capture.record(context.Background(), s.query, named)
	}
	return result, err
}

func (s *operationLogStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	var result driver.Result
	var err error
	if execer, ok := s.Stmt.(driver.StmtExecContext); ok {
		result, err = execer.ExecContext(ctx, args)
	} else {
		values := make([]driver.Value, len(args))
		for i, arg := range args {
			values[i] = arg.Value
		}
		result, err = s.Stmt.Exec(values)
	}
	if err == nil {
		s.capture.record(ctx, s.query, args)
	}
	return result, err
}

func (s *operationLogStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if querier, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return querier.QueryContext(ctx, args)
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	return s.Stmt.Query(values)
}

type operationLogRows struct {
	driver.Rows
	stmt driver.Stmt
}

func (r *operationLogRows) Close() error {
	return errors.Join(r.Rows.Close(), r.stmt.Close())
}

func isWriteStatement(query string) bool {
	query = strings.TrimSpace(query)
	for strings.HasPrefix(query, "--") {
		if newline := strings.IndexByte(query, '\n'); newline >= 0 {
			query = strings.TrimSpace(query[newline+1:])
		} else {
			return false
		}
	}
	upper := strings.ToUpper(query)
	return strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(upper, "INSERT\n") || upper == "INSERT" ||
		strings.HasPrefix(upper, "UPDATE ") || strings.HasPrefix(upper, "UPDATE\n") || upper == "UPDATE" ||
		strings.HasPrefix(upper, "DELETE ") || strings.HasPrefix(upper, "DELETE\n") || upper == "DELETE"
}

func renderSQL(query string, args []driver.NamedValue) (string, error) {
	if len(args) == 0 {
		return query, nil
	}
	var output strings.Builder
	output.Grow(len(query) + len(args)*8)
	argByPosition := make(map[int]driver.Value, len(args))
	for _, arg := range args {
		argByPosition[arg.Ordinal] = arg.Value
	}
	nextPosition := 1
	for i := 0; i < len(query); {
		char := query[i]
		if char == '\'' || char == '"' || char == '`' {
			quote := char
			output.WriteByte(char)
			i++
			for i < len(query) {
				output.WriteByte(query[i])
				if query[i] == quote {
					if i+1 < len(query) && query[i+1] == quote {
						output.WriteByte(query[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if char == '[' {
			end := strings.IndexByte(query[i+1:], ']')
			if end < 0 {
				output.WriteString(query[i:])
				break
			}
			end += i + 1
			output.WriteString(query[i : end+1])
			i = end + 1
			continue
		}
		if char == '-' && i+1 < len(query) && query[i+1] == '-' {
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				output.WriteString(query[i:])
				break
			}
			output.WriteString(query[i : i+end+1])
			i += end + 1
			continue
		}
		if char == '/' && i+1 < len(query) && query[i+1] == '*' {
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				output.WriteString(query[i:])
				break
			}
			end += i + 4
			output.WriteString(query[i:end])
			i = end
			continue
		}
		if char != '?' {
			output.WriteByte(char)
			i++
			continue
		}
		i++
		position := nextPosition
		if i < len(query) && query[i] >= '0' && query[i] <= '9' {
			start := i
			for i < len(query) && query[i] >= '0' && query[i] <= '9' {
				i++
			}
			number, err := strconv.Atoi(query[start:i])
			if err != nil || number < 1 {
				return "", fmt.Errorf("invalid numbered placeholder")
			}
			position = number
			nextPosition = number + 1
		} else {
			nextPosition++
		}
		value, exists := argByPosition[position]
		if !exists {
			return "", fmt.Errorf("missing argument %d", position)
		}
		literal, err := sqliteLiteral(value)
		if err != nil {
			return "", err
		}
		output.WriteString(literal)
	}
	if len(argByPosition) != 0 && nextPosition-1 < len(argByPosition) {
		return "", fmt.Errorf("unused SQL arguments")
	}
	return output.String(), nil
}

func sqliteLiteral(value driver.Value) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "NULL", nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "NULL", nil
		}
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	case bool:
		if typed {
			return "1", nil
		}
		return "0", nil
	case string:
		return "'" + strings.ReplaceAll(typed, "'", "''") + "'", nil
	case []byte:
		return "X'" + hex.EncodeToString(typed) + "'", nil
	case time.Time:
		return "'" + strings.ReplaceAll(typed.Format(time.RFC3339Nano), "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("unsupported parameter type %T", value)
	}
}
