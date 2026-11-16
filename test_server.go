package main

import (
	"database/sql"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	mathrand "math/rand"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type testAccount struct {
	username string
	display  string
	role     string
	category string
}

var testCategories = []struct {
	name string
}{
	{"虫族"}, {"神族"}, {"人族"}, {"随机"}}

func runTestServer(args []string) error {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	dbPath := flags.String("db", "", "SQLite database path")
	sqlPath := flags.String("sql", "", "SQLite database path alias")
	host := flags.String("host", "localhost", "HTTP listen host")
	port := flags.Int("port", 9528, "HTTP listen port")
	if err := flags.Parse(args); err != nil {
		return err
	}
	path := strings.TrimSpace(*sqlPath)
	if path == "" {
		path = strings.TrimSpace(*dbPath)
	}
	if path == "" {
		base := defaultDatabasePath()
		path = filepath.Join(filepath.Dir(base), "test_"+filepath.Base(base))
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	if err := migrate(db); err != nil {
		return err
	}
	if err := seedTestData(db); err != nil {
		return err
	}
	app := &App{db: db, sessions: make(map[string]int64), rng: mathrand.New(mathrand.NewSource(time.Now().UnixNano()))}
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	log.Printf("test server listening on http://%s, db=%s", addr, path)
	log.Printf("test accounts use password: test1234; superadmin=test-admin, managers=test-manager-1/test-manager-2")
	return app.router().Run(addr)
}

const testAvatarPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func seedTestData(db *sql.DB) error {
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='test-admin'`).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	categoryIDs := map[string]int64{}
	iconData, err := base64.StdEncoding.DecodeString(testAvatarPNG)
	if err != nil {
		return err
	}
	for _, category := range testCategories {
		if _, err := tx.Exec(`INSERT INTO categories(name,icon,icon_data,icon_mime,sort_order) VALUES(?,?,?, ?,?)`, category.name, "", iconData, "image/png", len(categoryIDs)); err != nil {
			return err
		}
		var categoryID int64
		if err := tx.QueryRow(`SELECT id FROM categories WHERE name=?`, category.name).Scan(&categoryID); err != nil {
			return err
		}
		categoryIDs[category.name] = categoryID
	}
	for i, name := range []string{"测试公开赛", "测试 KOF 夜", "测试邀请赛"} {
		if _, err := tx.Exec(`INSERT INTO site_event_names(name,sort_order) VALUES(?,?)`, name, i); err != nil {
			return err
		}
	}
	accounts := []testAccount{{"test-admin", "测试超级管理员", roleSuperAdmin, "随机"}, {"test-manager-1", "测试账号 1", roleUser, "随机"}, {"test-manager-2", "测试账号 2", roleUser, "随机"}}
	for i := 1; i <= 7; i++ {
		accounts = append(accounts, testAccount{fmt.Sprintf("test-player-%02d", i), fmt.Sprintf("测试选手 %02d", i), roleUser, testCategories[(i-1)%3].name})
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test1234"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	avatarData, err := base64.StdEncoding.DecodeString(testAvatarPNG)
	if err != nil {
		return err
	}
	ids := map[string]int64{}
	for _, account := range accounts {
		if _, err := tx.Exec(`INSERT INTO users(username,display_name,password_hash,role,category_id,avatar_data,avatar_mime) VALUES(?,?,?,?,?,?,?)`, account.username, account.display, string(hash), account.role, categoryIDs[account.category], avatarData, "image/png"); err != nil {
			return err
		}
		var userID int64
		if err := tx.QueryRow(`SELECT id FROM users WHERE username=?`, account.username).Scan(&userID); err != nil {
			return err
		}
		ids[account.username] = userID
	}
	players := []int64{ids["test-manager-1"], ids["test-manager-2"]}
	for i := 1; i <= 7; i++ {
		players = append(players, ids[fmt.Sprintf("test-player-%02d", i)])
	}
	managerID := ids["test-manager-1"]
	adminID := ids["test-admin"]
	for i, scores := range []struct{ a, b int }{{4, 2}, {3, 4}, {5, 3}} {
		if err := seedCompletedTestEvent(tx, managerID, adminID, players, fmt.Sprintf("测试历史赛事 %d", i+1), time.Now().AddDate(0, 0, -i-1).Format("2006-01-02T15:04"), scores.a, scores.b); err != nil {
			return err
		}
	}
	if err := seedLiveTestEvent(tx, managerID, adminID, players); err != nil {
		return err
	}
	return tx.Commit()
}

func seedCompletedTestEvent(tx *sql.Tx, managerID, creatorID int64, players []int64, name, eventTime string, scoreA, scoreB int) error {
	eventID, teamA, teamB, err := seedEventShell(tx, managerID, creatorID, name, eventTime, statusCompleted)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, eventID, teamA, teamB, players); err != nil {
		return err
	}
	roundIDs := map[string]int64{}
	for _, round := range []struct {
		stage          string
		scoreA, scoreB int
	}{{stageRound1, scoreA, scoreB}, {stageRound2, scoreA, scoreB}, {stageAce, 0, 0}} {
		var winner any
		if round.scoreA > round.scoreB {
			winner = teamA
		} else if round.scoreB > round.scoreA {
			winner = teamB
		}
		result, err := tx.Exec(`INSERT INTO rounds(event_id,stage,target_score,score_a,score_b,winner_team_id,status) VALUES(?,?,?,?,?,?,?)`, eventID, round.stage, 1, round.scoreA, round.scoreB, winner, statusFinished)
		if err != nil {
			return err
		}
		roundIDs[round.stage], _ = result.LastInsertId()
	}
	return seedFinishedMatches(tx, eventID, roundIDs[stageRound1], teamA, teamB, players[:3], players[3:], scoreA, scoreB, creatorID)
}

func seedLiveTestEvent(tx *sql.Tx, managerID, creatorID int64, players []int64) error {
	eventID, teamA, teamB, err := seedEventShell(tx, managerID, creatorID, "测试直播赛事", time.Now().Format("2006-01-02T15:04"), statusRound1)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, eventID, teamA, teamB, players); err != nil {
		return err
	}
	var roundID int64
	if err := tx.QueryRow(`INSERT INTO rounds(event_id,stage,target_score,status) VALUES(?,?,?,?) RETURNING id`, eventID, stageRound1, 4, statusActive).Scan(&roundID); err != nil {
		return err
	}
	for _, round := range []struct {
		stage  string
		target int
	}{{stageRound2, 5}, {stageAce, 1}} {
		if _, err := tx.Exec(`INSERT INTO rounds(event_id,stage,target_score,status) VALUES(?,?,?,?)`, eventID, round.stage, round.target, statusPending); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,status,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, eventID, roundID, 1, teamA, teamB, players[0], players[3], statusPending, creatorID)
	return err
}

func seedEventShell(tx *sql.Tx, managerID, creatorID int64, name, eventTime, status string) (int64, int64, int64, error) {
	result, err := tx.Exec(`INSERT INTO events(name,event_time,status,round1_target,round2_target,manager_id,created_by) VALUES(?,?,?,?,?,?,?)`, name, eventTime, status, 4, 5, managerID, creatorID)
	if err != nil {
		return 0, 0, 0, err
	}
	eventID, _ := result.LastInsertId()
	a, err := tx.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "测试 A 队", "#ef8354")
	if err != nil {
		return 0, 0, 0, err
	}
	teamA, _ := a.LastInsertId()
	b, err := tx.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "测试 B 队", "#3578e5")
	if err != nil {
		return 0, 0, 0, err
	}
	teamB, _ := b.LastInsertId()
	return eventID, teamA, teamB, nil
}

func seedPlayers(tx *sql.Tx, eventID, teamA, teamB int64, players []int64) error {
	for i, playerID := range players {
		teamID := teamB
		if i%2 == 0 {
			teamID = teamA
		}
		if _, err := tx.Exec(`INSERT INTO event_players(event_id,user_id,team_id) VALUES(?,?,?)`, eventID, playerID, teamID); err != nil {
			return err
		}
	}
	return nil
}

func seedFinishedMatches(tx *sql.Tx, eventID, roundID, teamA, teamB int64, playersA, playersB []int64, scoreA, scoreB int, creatorID int64) error {
	total := scoreA + scoreB
	for seq := 1; seq <= total; seq++ {
		winner := teamB
		if seq <= scoreA {
			winner = teamA
		}
		if _, err := tx.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,winner_team_id,status,created_by,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`, eventID, roundID, seq, teamA, teamB, playersA[(seq-1)%len(playersA)], playersB[(seq-1)%len(playersB)], winner, statusFinished, creatorID); err != nil {
			return err
		}
	}
	return nil
}
