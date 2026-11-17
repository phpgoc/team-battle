package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	mathrand "math/rand"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// 测试服务使用的种族图片只在测试构建中打包。
//
//go:embed testdata/demo/*.png
var demoFS embed.FS

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
		iconData, err := demoFS.ReadFile("testdata/demo/" + category.asset + ".png")
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
	for i, name := range []string{"OSL 经典赛", "MSL KOF 夜", "ASL 邀请赛"} {
		if _, err := tx.Exec(`INSERT INTO site_event_names(name,sort_order) VALUES(?,?)`, name, i); err != nil {
			return err
		}
	}
	accounts := []testAccount{{"test-admin", "BoxeR", roleSuperAdmin, "人族"}, {"test-manager-1", "Flash", roleUser, "人族"}, {"test-manager-2", "Jaedong", roleUser, "虫族"}}
	players := []testAccount{
		{"test-player-01", "Bisu", roleUser, "神族"},
		{"test-player-02", "Stork", roleUser, "神族"},
		{"test-player-03", "sAviOr", roleUser, "虫族"},
		{"test-player-04", "iloveoov", roleUser, "人族"},
		{"test-player-05", "NaDa", roleUser, "人族"},
		{"test-player-06", "Rain", roleUser, "神族"},
		{"test-player-07", "soO", roleUser, "虫族"},
	}
	accounts = append(accounts, players...)
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
	playerIDs := []int64{ids["test-manager-1"], ids["test-manager-2"]}
	for i := 1; i <= 7; i++ {
		playerIDs = append(playerIDs, ids[fmt.Sprintf("test-player-%02d", i)])
	}
	managerID := ids["test-manager-1"]
	adminID := ids["test-admin"]
	for i, winners := range []string{"aa", "ba", "ab"} {
		if err := seedCompletedTestEvent(tx, managerID, adminID, playerIDs, []string{"OSL 经典赛", "MSL KOF 夜", "ASL 邀请赛"}[i], time.Now().AddDate(0, 0, -i-1).Format("2006-01-02T15"), winners); err != nil {
			return err
		}
		if i > 0 {
			source := "most_wins"
			if i == 2 {
				source = "most_losses"
			}
			var eventID int64
			if err := tx.QueryRow(`SELECT id FROM events WHERE name=?`, []string{"OSL 经典赛", "MSL KOF 夜", "ASL 邀请赛"}[i]).Scan(&eventID); err != nil {
				return err
			}
			if err := seedAceSourceAction(tx, eventID, source); err != nil {
				return err
			}
		}
	}
	if err := seedSpecialTestEvent(tx, managerID, adminID, ids, "特殊对战1", time.Now().AddDate(0, 0, -10).Format("2006-01-02T15"), "special:0", true); err != nil {
		return err
	}
	if err := seedSpecialTestEvent(tx, managerID, adminID, ids, "特殊对战2", time.Now().AddDate(0, 0, -11).Format("2006-01-02T15"), "special:1", false); err != nil {
		return err
	}
	if err := seedLiveTestEvent(tx, managerID, adminID, playerIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// 特殊历史数据包含不对称候选组，便于检查两队抽签人数不同的展示。
func seedSpecialTestEvent(tx *sql.Tx, managerID, creatorID int64, ids map[string]int64, name, eventTime, source string, skewed bool) error {
	teamAPlayers := []int64{ids["test-manager-1"], ids["test-player-01"], ids["test-player-02"], ids["test-player-03"]}
	teamBPlayers := []int64{ids["test-manager-2"], ids["test-player-04"], ids["test-player-05"], ids["test-player-06"]}
	candidatesA := [][]int64{{teamAPlayers[0]}, {teamAPlayers[1], teamAPlayers[2], teamAPlayers[3]}}
	candidatesB := [][]int64{{teamBPlayers[0], teamBPlayers[1]}, {teamBPlayers[2], teamBPlayers[3]}}
	if !skewed {
		candidatesA = [][]int64{{teamAPlayers[0], teamAPlayers[1]}, {teamAPlayers[2], teamAPlayers[3]}}
		candidatesB = [][]int64{{teamBPlayers[0], teamBPlayers[1]}, {teamBPlayers[2], teamBPlayers[3]}}
	}
	specials := []AceSpecial{
		{PlayerIDs: append(append([]int64{}, candidatesA[0]...), candidatesB[0]...), Probability: 30},
		{PlayerIDs: append(append([]int64{}, candidatesA[1]...), candidatesB[1]...), Probability: 30},
	}
	rules := defaultRules()
	rules.AceSpecials = specials
	rules.AceMostWins = 10
	rules.AceMostLosses = 10
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	eventID, teamA, teamB, err := seedEventShell(tx, managerID, creatorID, name, eventTime, statusCompleted)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE events SET round1_target=2,round2_target=2,rules_json=? WHERE id=?`, string(raw), eventID); err != nil {
		return err
	}
	for _, playerID := range teamAPlayers {
		if _, err := tx.Exec(`INSERT INTO event_players(event_id,user_id,team_id) VALUES(?,?,?)`, eventID, playerID, teamA); err != nil {
			return err
		}
	}
	for _, playerID := range teamBPlayers {
		if _, err := tx.Exec(`INSERT INTO event_players(event_id,user_id,team_id) VALUES(?,?,?)`, eventID, playerID, teamB); err != nil {
			return err
		}
	}
	for _, stage := range []string{stageRound1, stageRound2, stageAce} {
		scoreA, scoreB := 0, 0
		winner := teamA
		target := 2
		if stage == stageRound1 {
			scoreA, scoreB, winner = 2, 1, teamA
		} else if stage == stageRound2 {
			scoreA, scoreB, winner = 1, 2, teamB
		} else {
			scoreA, target = 1, 1
		}
		result, err := tx.Exec(`INSERT INTO rounds(event_id,stage,target_score,score_a,score_b,winner_team_id,status) VALUES(?,?,?,?,?,?,'finished')`, eventID, stage, target, scoreA, scoreB, winner)
		if err != nil {
			return err
		}
		roundID, _ := result.LastInsertId()
		pairs := [][2]int{{0, 0}, {1, 1}, {2, 2}}
		winners := []int64{teamA, teamB, teamA}
		if stage == stageRound2 {
			winners = []int64{teamB, teamA, teamB}
		}
		if skewed && stage == stageRound1 {
			pairs = [][2]int{{0, 0}, {0, 1}, {1, 2}}
		}
		if skewed && stage == stageRound2 {
			pairs = [][2]int{{1, 2}, {2, 3}, {3, 3}}
		}
		if stage == stageAce {
			pairs = [][2]int{{0, 0}}
			winners = []int64{teamA}
		}
		for seq, pair := range pairs {
			win := winners[seq]
			if skewed && stage == stageRound1 && seq == 1 {
				win = teamB
			}
			if _, err := tx.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,winner_team_id,status,created_by,finished_at) VALUES(?,?,?,?,?,?,?,?, 'finished',?,CURRENT_TIMESTAMP)`, eventID, roundID, seq+1, teamA, teamB, teamAPlayers[pair[0]], teamBPlayers[pair[1]], win, creatorID); err != nil {
				return err
			}
		}
	}
	return seedAceSourceAction(tx, eventID, source)
}

func seedAceSourceAction(tx *sql.Tx, eventID int64, source string) error {
	sources := []string{source, source}
	if strings.HasPrefix(source, "special:") {
		sources[0] = "special:0"
		sources[1] = "special:1"
	}
	payload, err := json.Marshal(map[string]any{"source": source, "rules": sources})
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event_actions(event_id,action,payload) VALUES(?,'ace_drawn',?)`, eventID, string(payload))
	return err
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
	eventID, teamA, teamB, err := seedEventShell(tx, managerID, creatorID, "GSL 直播赛", time.Now().Format("2006-01-02T15"), statusDraft)
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
