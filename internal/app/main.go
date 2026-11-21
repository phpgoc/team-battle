package app

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"log"
	mathrand "math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web/index.html web/styles.css web/app.js web/i18n.js web/player-records.js web/setup-draw.js web/roster-editor.js web/roster-editor.css web/lineup-page.js web/lineup-page.css web/kof-board.js web/kof-board.css web/kof-rules.js web/archive-page.js web/archive-page.css web/confirm-dialog.js web/confirm-dialog.css web/result-correction.css web/broadcast-layout.css web/match-draw.js web/match-draw.css web/search-select.js web/search-select.css web/date-picker.js web/date-picker.css web/site-settings.css web/management.css web/tournament-management.js web/tournament-management.css web/vendor/flatpickr
var webFS embed.FS

const (
	roleSuperAdmin = "superadmin"
	roleUser       = "user"
	roleManager    = "manager"
	rolePlayer     = "player"

	statusDraft        = "draft"
	statusCaptainVote  = "captain_vote"
	statusLineup       = "lineup_confirm"
	statusRound1       = "round1"
	statusRound2       = "round2"
	statusAce          = "ace"
	statusFinalConfirm = "final_confirm"
	statusCompleted    = "completed"
	statusCancelled    = "cancelled"
	statusPending      = "pending"
	statusActive       = "active"
	statusFinished     = "finished"

	stageRound1 = "round1"
	stageRound2 = "round2"
	stageAce    = "ace"
)

func isNonSuperAdmin(role string) bool {
	return role != "" && role != roleSuperAdmin
}

type App struct {
	db                *sql.DB
	writeMu           sync.Mutex
	thumbnailMu       sync.Mutex
	rng               *mathrand.Rand
	storageDir        string
	history           historyCache
	directory         categoryDirectoryCache
	broadcast         streamHub
	logins            loginLimiter
	sseMaxConnections int
	logLanguage       string
	operationLogger   *operationLogger
	shutdownStarted   atomic.Bool
	streamsDone       <-chan struct{}
}

type historyCache struct {
	mu       sync.RWMutex
	items    []map[string]any
	loadedAt time.Time
}

type User struct {
	ID                   int64  `json:"id"`
	Username             string `json:"username"`
	DisplayName          string `json:"display_name"`
	Role                 string `json:"role"`
	Status               string `json:"status"`
	AvatarURL            string `json:"avatar_url,omitempty"`
	CreatedAt            string `json:"created_at"`
	CategoryID           int64  `json:"category_id,omitempty"`
	CategoryName         string `json:"category_name,omitempty"`
	CategoryIcon         string `json:"category_icon,omitempty"`
	DisplayNameChangedAt string `json:"display_name_changed_at,omitempty"`
	AvatarChangedAt      string `json:"avatar_changed_at,omitempty"`
	ProfileChangedAt     string `json:"profile_changed_at,omitempty"`
	IsDeleted            bool   `json:"-"`
	CanDelete            bool   `json:"can_delete"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type profileRequest struct {
	Username           *string `json:"username"`
	DisplayName        *string `json:"display_name"`
	CurrentPassword    string  `json:"current_password"`
	NewPassword        string  `json:"new_password"`
	NewPasswordConfirm string  `json:"new_password_confirm"`
	CategoryID         *int64  `json:"category_id"`
	Role               *string `json:"role"`
	Password           string  `json:"password"`
}

type catalogNameRequest struct {
	Name string `json:"name" binding:"required"`
}

type categoryRequest struct {
	Name string `json:"name" binding:"required"`
	Icon string `json:"icon"`
}

type archivedMatch struct {
	Round  int    `json:"round"`
	AID    int64  `json:"a_id"`
	BID    int64  `json:"b_id"`
	WhoWin string `json:"who_win"`
}

type archiveTournamentRequest struct {
	ManagerID          int64             `json:"manager_id"`
	KOFRevivalNoRepeat bool              `json:"kof_revival_no_repeat"`
	Round1Target       int               `json:"round1_target"`
	Round2Target       int               `json:"round2_target"`
	Matches            []archivedMatch   `json:"matches"`
	FixedAssignments   map[string]string `json:"fixed_assignments"`
	BackgroundID       int64             `json:"background_id"`
	Name               string            `json:"name" binding:"required"`
	Description        string            `json:"description"`
	StartTime          string            `json:"start_time" binding:"required"`
	PlayerIDs          []int64           `json:"player_ids"`
	Round1ScoreA       int               `json:"round1_score_a"`
	Round1ScoreB       int               `json:"round1_score_b"`
	Round2ScoreA       int               `json:"round2_score_a"`
	Round2ScoreB       int               `json:"round2_score_b"`
	Winner             string            `json:"winner"`
	AceSource          string            `json:"ace_source"`
}

type passwordResetRequest struct {
	Password string `json:"password" binding:"required"`
}

type createUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role" binding:"required"`
}

type createTournamentRequest struct {
	Name             string            `json:"name" binding:"required"`
	Description      string            `json:"description"`
	StartTime        string            `json:"start_time"`
	ManagerID        int64             `json:"manager_id"`
	PlayerIDs        []int64           `json:"player_ids"`
	FixedAssignments map[string]string `json:"fixed_assignments"`
	Round1Target     int               `json:"round1_target"`
	Round2Target     int               `json:"round2_target"`
	Rules            json.RawMessage   `json:"rules"`
	BackgroundID     int64             `json:"background_id"`
}

type tournamentSettingsRequest struct {
	Correction        *tournamentResultCorrection  `json:"correction"`
	Corrections       []tournamentResultCorrection `json:"corrections"`
	AcePlayers        *tournamentAcePlayers        `json:"ace_players"`
	BackgroundID      *int64                       `json:"background_id"`
	RandomDrawSeconds *int                         `json:"random_draw_seconds"`
	AceDrawSeconds    *int                         `json:"ace_draw_seconds"`
	ManagerID         *int64                       `json:"manager_id"`
	PlayerIDs         *[]int64                     `json:"player_ids"`
	FixedAssignments  map[string]string            `json:"fixed_assignments"`
	Name              *string                      `json:"name"`
	StartTime         *string                      `json:"start_time"`
	Description       *string                      `json:"description"`
	Round1Target      *int                         `json:"round1_target"`
	Round2Target      *int                         `json:"round2_target"`
	Rules             json.RawMessage              `json:"rules"`
}

type tournamentAcePlayers struct {
	MatchID   int64 `json:"match_id"`
	PlayerAID int64 `json:"player_a_id"`
	PlayerBID int64 `json:"player_b_id"`
}

type tournamentResultCorrection struct {
	MatchID      int64               `json:"match_id"`
	WinnerTeamID int64               `json:"winner_team_id"`
	PlayerAID    *int64              `json:"player_a_id"`
	PlayerBID    *int64              `json:"player_b_id"`
	Note         *string             `json:"note"`
	Expected     *correctionSnapshot `json:"expected"`
}

type correctionSnapshot struct {
	MatchID            int64 `json:"match_id"`
	PlayerAID          int64 `json:"player_a_id"`
	PlayerBID          int64 `json:"player_b_id"`
	WinnerTeamID       int64 `json:"winner_team_id"`
	LatestMatchID      int64 `json:"latest_match_id"`
	LatestWinnerTeamID int64 `json:"latest_winner_team_id"`
}

type tournamentBackgroundRequest struct {
	BackgroundID int64 `json:"background_id"`
}

type resultRequest struct {
	WinnerTeamID int64  `json:"winner_team_id" binding:"required"`
	NextPlayerID int64  `json:"next_player_id"`
	Note         string `json:"note"`
}

type lineupRequest struct {
	TeamID    int64   `json:"team_id" binding:"required"`
	PlayerIDs []int64 `json:"player_ids"`
}

type addTournamentPlayersRequest struct {
	PlayerIDs []int64 `json:"player_ids"`
	TeamID    int64   `json:"team_id"`
}

type aceRequest struct {
	PlayerAID int64 `json:"player_a_id" binding:"required"`
	PlayerBID int64 `json:"player_b_id" binding:"required"`
}

// Main 启动正式服务，根目录和 cmd/team-battle 都通过它保持相同的启动行为。
func Main() {
	host := flag.String("host", "localhost", "HTTP listen host")
	port := flag.Int("port", 9527, "HTTP listen port")
	dbPath := flag.String("db", "", "SQLite database path")
	sqlPath := flag.String("sql", "", "SQLite database path alias")
	sseMaxConnections := flag.Int("sse-max-connections", defaultSSEMaxConnections, "Maximum simultaneous SSE subscriptions")
	logLanguage := flag.String("language", "zh", "Operation log language: zh, en, ko")
	logDestination := flag.String("log", "", "Log destination: stdout (omit to disable routine logs)")
	flag.Parse()
	if *logDestination != "" && *logDestination != "stdout" {
		log.Fatalf("--log 只支持 stdout")
	}
	if *sseMaxConnections < 1 {
		log.Fatal("--sse-max-connections 必须大于 0")
	}
	if *logLanguage != "zh" && *logLanguage != "en" && *logLanguage != "ko" {
		log.Fatal("--language 只支持 zh、en、ko")
	}

	if *dbPath == "" {
		*dbPath = *sqlPath
	}
	if *dbPath == "" {
		*dbPath = defaultDatabasePath()
	}
	var operationLog *operationLogger
	if *logDestination == "stdout" {
		operationLog = newOperationLogger(os.Stdout, *logLanguage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	err := run(ctx, addr, *dbPath, *sseMaxConnections, *logLanguage, operationLog)
	stop()
	if err != nil {
		log.Fatal(err)
	}
	if ctx.Err() != nil {
		operationLog.lifecyclef(operationLogText(*logLanguage,
			"服务已停止，数据库已关闭",
			"Service stopped and database closed",
			"서비스가 중지되고 데이터베이스가 닫혔습니다"))
	}
}

func run(ctx context.Context, addr, dbPath string, sseMaxConnections int, logLanguage string, operationLog *operationLogger) (err error) {
	storageDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return err
	}
	var db *sql.DB
	if operationLog != nil {
		db, err = openDatabase(dbPath, operationLog.capture)
	} else {
		db, err = openDatabase(dbPath)
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := migrate(db, operationLog); err != nil {
		return err
	}
	if err := ensureDefaultAdmin(db); err != nil {
		return err
	}
	if err := migrateBackgroundFiles(db, storageDir); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	app := &App{
		db:                db,
		rng:               mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
		storageDir:        storageDir,
		sseMaxConnections: sseMaxConnections,
		logLanguage:       logLanguage,
		operationLogger:   operationLog,
	}
	operationLog.lifecyclef(operationLogText(logLanguage,
		"服务已监听 http://%s，数据库=%s",
		"Listening on http://%s, database=%s",
		"http://%s에서 수신 중, 데이터베이스=%s"), listener.Addr(), dbPath)
	return app.serveHTTP(ctx, listener)
}
