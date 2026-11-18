package app

import (
	"database/sql"
	"embed"
	"flag"
	"log"
	mathrand "math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web/index.html web/styles.css web/app.js web/i18n.js web/player-records.js web/setup-draw.js web/roster-editor.js web/roster-editor.css web/lineup-page.js web/lineup-page.css web/kof-board.js web/kof-board.css web/archive-page.js web/archive-page.css web/confirm-dialog.js web/confirm-dialog.css web/result-correction.css web/broadcast-layout.css web/match-draw.js web/match-draw.css
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
	db          *sql.DB
	writeMu     sync.Mutex
	thumbnailMu sync.Mutex
	rng         *mathrand.Rand
	storageDir  string
	history     historyCache
	directory   categoryDirectoryCache
	broadcast   streamHub
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
	Round1Target     int               `json:"round1_target"`
	Round2Target     int               `json:"round2_target"`
	Matches          []archivedMatch   `json:"matches"`
	FixedAssignments map[string]string `json:"fixed_assignments"`
	BackgroundID     int64             `json:"background_id"`
	Name             string            `json:"name" binding:"required"`
	Description      string            `json:"description"`
	StartTime        string            `json:"start_time" binding:"required"`
	PlayerIDs        []int64           `json:"player_ids"`
	Round1ScoreA     int               `json:"round1_score_a"`
	Round1ScoreB     int               `json:"round1_score_b"`
	Round2ScoreA     int               `json:"round2_score_a"`
	Round2ScoreB     int               `json:"round2_score_b"`
	Winner           string            `json:"winner"`
	AceSource        string            `json:"ace_source"`
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
	Rules            *TournamentRules  `json:"rules"`
	BackgroundID     int64             `json:"background_id"`
}

type tournamentSettingsRequest struct {
	Correction        *tournamentResultCorrection `json:"correction"`
	AcePlayers        *tournamentAcePlayers       `json:"ace_players"`
	BackgroundID      *int64                      `json:"background_id"`
	RandomDrawSeconds *int                        `json:"random_draw_seconds"`
	AceDrawSeconds    *int                        `json:"ace_draw_seconds"`
	ManagerID         *int64                      `json:"manager_id"`
	PlayerIDs         *[]int64                    `json:"player_ids"`
	FixedAssignments  map[string]string           `json:"fixed_assignments"`
	Name              string                      `json:"name"`
	StartTime         string                      `json:"start_time"`
	Description       string                      `json:"description"`
	Round1Target      int                         `json:"round1_target"`
	Round2Target      int                         `json:"round2_target"`
	Rules             *TournamentRules            `json:"rules"`
}

type tournamentAcePlayers struct {
	MatchID   int64 `json:"match_id"`
	PlayerAID int64 `json:"player_a_id"`
	PlayerBID int64 `json:"player_b_id"`
}

type tournamentResultCorrection struct {
	MatchID      int64  `json:"match_id"`
	WinnerTeamID int64  `json:"winner_team_id"`
	Note         string `json:"note"`
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
	flag.Parse()

	if *dbPath == "" {
		*dbPath = *sqlPath
	}
	if *dbPath == "" {
		*dbPath = defaultDatabasePath()
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0755); err != nil {
		log.Fatal(err)
	}

	db, err := openDatabase(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}
	if err := ensureDefaultAdmin(db); err != nil {
		log.Fatal(err)
	}
	if err := migrateBackgroundFiles(db, filepath.Dir(*dbPath)); err != nil {
		log.Fatal(err)
	}

	app := &App{
		db:         db,
		rng:        mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
		storageDir: filepath.Dir(*dbPath),
	}
	r := app.router()
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	log.Printf("team-battle listening on http://%s, db=%s", addr, *dbPath)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
