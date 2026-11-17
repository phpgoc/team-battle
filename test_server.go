package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
	name  string
	asset string
}{
	{"虫族", "zerg"}, {"神族", "protoss"}, {"人族", "terran"}, {"随机", "random"}}

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
	app := &App{db: db, rng: mathrand.New(mathrand.NewSource(time.Now().UnixNano())), drawSleep: time.Sleep}
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
	for _, category := range testCategories {
		iconData, err := demoFS.ReadFile("web/demo/" + category.asset + ".png")
		if err != nil {
			return err
		}
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
	ids := map[string]int64{}
	for _, account := range accounts {
		avatarData, err := demoAvatar(account.username)
		if err != nil {
			return err
		}
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
	for i, winners := range []string{"aa", "ba", "ab"} {
		if err := seedCompletedTestEvent(tx, managerID, adminID, players, fmt.Sprintf("测试历史赛事 %d", i+1), time.Now().AddDate(0, 0, -i-1).Format("2006-01-02T15"), winners); err != nil {
			return err
		}
	}
	if err := seedLiveTestEvent(tx, managerID, adminID, players); err != nil {
		return err
	}
	return tx.Commit()
}

// 用正式比赛引擎生成演示战绩，确保分队、胜者留场、阶段比分与对局一致。
func seedCompletedTestEvent(tx *sql.Tx, managerID, creatorID int64, players []int64, name, eventTime, winners string) error {
	eventID, teamA, teamB, err := seedEventShell(tx, managerID, creatorID, name, eventTime, statusDraft)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, eventID, teamA, teamB, players); err != nil {
		return err
	}
	engine := &matchEngine{db: tx, eventID: eventID, actorID: creatorID, rng: mathrand.New(mathrand.NewSource(eventID))}
	for _, stage := range []string{stageRound1, stageRound2, stageAce} {
		target := 1
		if stage == stageRound1 {
			target = 4
		}
		if stage == stageRound2 {
			target = 5
		}
		if _, err := tx.Exec("INSERT INTO rounds(event_id,stage,target_score) VALUES(?,?,?)", eventID, stage, target); err != nil {
			return err
		}
	}
	if err := engine.start(); err != nil {
		return err
	}
	for n := 0; n < 60; n++ {
		status, err := engine.status()
		if err != nil {
			return err
		}
		if status == statusFinalConfirm {
			return engine.setStage(statusCompleted)
		}
		var matchID int64
		err = tx.QueryRow("SELECT id FROM matches WHERE event_id=? AND status='pending'", eventID).Scan(&matchID)
		if err == sql.ErrNoRows && status == stageRound2 {
			var lastID int64
			if err := tx.QueryRow("SELECT id FROM matches WHERE event_id=? AND round_no=2 ORDER BY seq DESC LIMIT 1", eventID).Scan(&lastID); err != nil {
				return err
			}
			previous, err := engine.match(lastID)
			if err != nil {
				return err
			}
			_, loser := previous.loser()
			ids, err := engine.players(loser)
			if err != nil {
				return err
			}
			for _, player := range ids {
				lost, err := engine.lost(previous.roundID, loser, player)
				if err != nil {
					return err
				}
				if !lost {
					if err := engine.chooseKOF(previous, player); err != nil {
						return err
					}
					break
				}
			}
			continue
		}
		if err != nil {
			return err
		}
		match, err := engine.match(matchID)
		if err != nil {
			return err
		}
		side := byte('a')
		if status == stageRound1 {
			side = winners[0]
		}
		if status == stageRound2 {
			side = winners[1]
		}
		// 第一局由阶段败方获胜，让演示中同时出现胜负和KOF换人。
		if match.seq == 1 && status != stageAce {
			if side == 'a' {
				side = 'b'
			} else {
				side = 'a'
			}
		}
		winner := teamA
		if side == 'b' {
			winner = teamB
		}
		if err := engine.record(match, winner, ""); err != nil {
			return err
		}
	}
	return fmt.Errorf("测试赛事推进未能结束")
}

func seedLiveTestEvent(tx *sql.Tx, managerID, creatorID int64, players []int64) error {
	eventID, teamA, teamB, err := seedEventShell(tx, managerID, creatorID, "测试直播赛事", time.Now().Format("2006-01-02T15"), statusDraft)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, eventID, teamA, teamB, players); err != nil {
		return err
	}
	for _, stage := range []string{stageRound1, stageRound2, stageAce} {
		target := 1
		if stage == stageRound1 {
			target = 4
		}
		if stage == stageRound2 {
			target = 5
		}
		if _, err := tx.Exec("INSERT INTO rounds(event_id,stage,target_score) VALUES(?,?,?)", eventID, stage, target); err != nil {
			return err
		}
	}
	engine := &matchEngine{db: tx, eventID: eventID, actorID: creatorID, rng: mathrand.New(mathrand.NewSource(42))}
	if err := engine.start(); err != nil {
		return err
	}
	for n := 0; n < 3; n++ {
		var id int64
		if err := tx.QueryRow("SELECT id FROM matches WHERE event_id=? AND status='pending'", eventID).Scan(&id); err != nil {
			return err
		}
		match, err := engine.match(id)
		if err != nil {
			return err
		}
		winner := teamA
		if n == 1 {
			winner = teamB
		}
		if err := engine.record(match, winner, ""); err != nil {
			return err
		}
	}
	return nil
}

func seedEventShell(tx *sql.Tx, managerID, creatorID int64, name, eventTime, status string) (int64, int64, int64, error) {
	result, err := tx.Exec(`INSERT INTO events(name,event_time,status,round1_target,round2_target,manager_id,created_by) VALUES(?,?,?,?,?,?,?)`, name, eventTime, status, 4, 5, managerID, creatorID)
	if err != nil {
		return 0, 0, 0, err
	}
	eventID, _ := result.LastInsertId()
	a, err := tx.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "A 队", "#ef8354")
	if err != nil {
		return 0, 0, 0, err
	}
	teamA, _ := a.LastInsertId()
	b, err := tx.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "B 队", "#3578e5")
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

// 根据账号生成对称像素头像，演示选手能够一眼区分且不使用外部图片。
func demoAvatar(username string) ([]byte, error) {
	hash := sha256.Sum256([]byte(username))
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	foreground := color.RGBA{70 + hash[0]/2, 80 + hash[1]/2, 90 + hash[2]/2, 255}
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			shade := color.RGBA{237, 242, 247, 255}
			col, row := (x-8)/16, (y-8)/16
			if x >= 8 && x < 88 && y >= 8 && y < 88 {
				if col > 2 {
					col = 4 - col
				}
				if hash[row*3+col]&1 == 1 {
					shade = foreground
				}
			}
			img.SetRGBA(x, y, shade)
		}
	}
	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}
