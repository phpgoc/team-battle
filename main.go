package main

import (
	"database/sql"
	"embed"
	"flag"
	"log"
	mathrand "math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web/index.html web/styles.css web/app.js
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
	db        *sql.DB
	sessions  map[string]int64
	mu        sync.RWMutex
	writeMu   sync.Mutex
	rng       *mathrand.Rand
	history   historyCache
	directory categoryDirectoryCache
}

type historyCache struct {
	mu       sync.RWMutex
	items    []map[string]any
	loadedAt time.Time
}

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	Status       string `json:"status"`
	AvatarURL    string `json:"avatar_url,omitempty"`
	CreatedAt    string `json:"created_at"`
	CategoryID   int64  `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	CategoryIcon string `json:"category_icon,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type profileRequest struct {
	DisplayName     string `json:"display_name"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	CategoryID      *int64 `json:"category_id"`
}

type catalogNameRequest struct {
	Name string `json:"name" binding:"required"`
}

type categoryRequest struct {
	Name string `json:"name" binding:"required"`
	Icon string `json:"icon"`
}

type archiveEventRequest struct {
	Name         string  `json:"name" binding:"required"`
	Description  string  `json:"description"`
	EventTime    string  `json:"event_time" binding:"required"`
	TeamAName    string  `json:"team_a_name"`
	TeamBName    string  `json:"team_b_name"`
	PlayerIDs    []int64 `json:"player_ids"`
	Round1ScoreA int     `json:"round1_score_a"`
	Round1ScoreB int     `json:"round1_score_b"`
	Round2ScoreA int     `json:"round2_score_a"`
	Round2ScoreB int     `json:"round2_score_b"`
	Winner       string  `json:"winner"`
}

type passwordResetRequest struct {
	Password string `json:"password" binding:"required"`
}

type createUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type createEventRequest struct {
	Name             string            `json:"name" binding:"required"`
	Description      string            `json:"description"`
	EventTime        string            `json:"event_time"`
	ManagerID        int64             `json:"manager_id"`
	PlayerIDs        []int64           `json:"player_ids"`
	TeamAName        string            `json:"team_a_name"`
	TeamBName        string            `json:"team_b_name"`
	FixedAssignments map[string]string `json:"fixed_assignments"`
	Round1Target     int               `json:"round1_target"`
	Round2Target     int               `json:"round2_target"`
	KOFInitialA      int64             `json:"kof_initial_a"`
	KOFInitialB      int64             `json:"kof_initial_b"`
}

type eventSettingsRequest struct {
	Round1NoRepeat bool  `json:"round1_no_repeat"`
	Round1AllFirst bool  `json:"round1_all_first"`
	KOFInitialA    int64 `json:"kof_initial_a"`
	KOFInitialB    int64 `json:"kof_initial_b"`
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

type addEventPlayersRequest struct {
	PlayerIDs []int64 `json:"player_ids"`
	TeamID    int64   `json:"team_id"`
}

type aceRequest struct {
	PlayerAID int64 `json:"player_a_id" binding:"required"`
	PlayerBID int64 `json:"player_b_id" binding:"required"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "test" {
		if err := runTestServer(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	host := flag.String("host", "localhost", "HTTP listen host")
	port := flag.Int("port", 9527, "HTTP listen port")
	dbPath := flag.String("db", "", "SQLite database path")
	flag.Parse()

	if *dbPath == "" {
		*dbPath = defaultDatabasePath()
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	// 使用小连接池支持赛事详情的嵌套读取，写操作由路由互斥锁串行化。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		log.Fatal(err)
	}
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}

	app := &App{
		db:       db,
		sessions: make(map[string]int64),
		rng:      mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
	}
	r := app.router()
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	log.Printf("team-battle listening on http://%s, db=%s", addr, *dbPath)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
