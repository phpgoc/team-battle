package app

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// 测试服务使用的种族图片只在测试构建中打包。
//
//go:embed testdata/demo/*.png testdata/demo/avatars/*.png
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
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='admin'`).Scan(&existing); err != nil {
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
		if _, err := tx.Exec(`INSERT INTO site_tournament_names(name,sort_order) VALUES(?,?)`, name, i); err != nil {
			return err
		}
	}
	for _, background := range []struct {
		id       int
		name     string
		filePath string
	}{
		{1, "星际战场·暮光前线", "background-1.png"},
		{2, "星际战场·轨道风暴", "background-2.png"},
	} {
		data, err := readDemoBackground(background.filePath)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO backgrounds(id,name,image_data,image_mime,file_path,image_size,sort_order) VALUES(?,?,X'', 'image/png', ?,?,?)`, background.id, background.name, background.filePath, len(data), background.id-1); err != nil {
			return err
		}
	}
	accounts := []testAccount{{"admin", "超级管理员", roleSuperAdmin, "人族"}, {"2", "Flash", roleUser, "人族"}, {"3", "Jaedong", roleUser, "虫族"}}
	players := []testAccount{
		{"4", "Bisu", roleUser, "神族"},
		{"5", "Stork", roleUser, "神族"},
		{"6", "sAviOr", roleUser, "虫族"},
		{"7", "iloveoov", roleUser, "人族"},
		{"8", "NaDa", roleUser, "人族"},
		{"9", "Rain", roleUser, "神族"},
		{"10", "soO", roleUser, "虫族"},
	}
	accounts = append(accounts, players...)
	hash, err := bcrypt.GenerateFromPassword([]byte("test1234"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	ids := map[string]int64{}
	for _, account := range accounts {
		// 选手使用显示名登录，原编号仅用于关联演示图片和赛程。
		username := account.username
		if account.role != roleSuperAdmin {
			username = account.display
		}
		avatarData, err := demoAvatar(account.username)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO users(username,display_name,password_hash,role,category_id,avatar_data,avatar_mime) VALUES(?,?,?,?,?,?,?)`, username, account.display, string(hash), account.role, categoryIDs[account.category], avatarData, "image/png"); err != nil {
			return err
		}
		var userID int64
		if err := tx.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&userID); err != nil {
			return err
		}
		ids[account.username] = userID
	}
	playerIDs := []int64{ids["2"], ids["3"]}
	for _, username := range []string{"4", "5", "6", "7", "8", "9", "10"} {
		playerIDs = append(playerIDs, ids[username])
	}
	managerID := ids["2"]
	adminID := ids["admin"]
	for i, winners := range []string{"aa", "ba", "ab"} {
		if err := seedCompletedTestTournament(tx, managerID, adminID, playerIDs, []string{"OSL 经典赛", "MSL KOF 夜", "ASL 邀请赛"}[i], time.Now().AddDate(0, 0, -i-1).Format("2006-01-02T15"), winners); err != nil {
			return err
		}
		if i > 0 {
			source := "most_wins"
			if i == 2 {
				source = "most_losses"
			}
			var tournamentID int64
			if err := tx.QueryRow(`SELECT id FROM tournaments WHERE name=?`, []string{"OSL 经典赛", "MSL KOF 夜", "ASL 邀请赛"}[i]).Scan(&tournamentID); err != nil {
				return err
			}
			if err := seedAceSourceAction(tx, tournamentID, source); err != nil {
				return err
			}
		}
	}
	if err := seedSpecialTestTournament(tx, managerID, adminID, ids, "特殊对战1", time.Now().AddDate(0, 0, -10).Format("2006-01-02T15"), "special:0", true); err != nil {
		return err
	}
	if err := seedSpecialTestTournament(tx, managerID, adminID, ids, "特殊对战2", time.Now().AddDate(0, 0, -11).Format("2006-01-02T15"), "special:1", false); err != nil {
		return err
	}
	if err := seedLiveTestTournament(tx, managerID, adminID, playerIDs); err != nil {
		return err
	}
	if err := seedWorkflowDemoTournaments(tx, managerID, adminID, ids); err != nil {
		return err
	}
	if err := seedKOFRevivalDemo(tx, managerID, adminID, playerIDs[:6]); err != nil {
		return err
	}
	return tx.Commit()
}

func readDemoBackground(name string) ([]byte, error) {
	path := filepath.Join("..", "..", "uploads", "backgrounds", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, fmt.Errorf("演示背景不是有效的 PNG 图片: %s", path)
	}
	return data, nil
}

// 特殊历史数据包含不对称候选组，便于检查两队抽签人数不同的展示。
func seedSpecialTestTournament(tx *sql.Tx, managerID, creatorID int64, ids map[string]int64, name, startTime, source string, skewed bool) error {
	teamAPlayers := []int64{ids["2"], ids["4"], ids["5"], ids["6"]}
	teamBPlayers := []int64{ids["3"], ids["7"], ids["8"], ids["9"]}
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
	tournamentID, teamA, teamB, err := seedTournamentShell(tx, managerID, creatorID, name, startTime, statusCompleted)
	if err != nil {
		return err
	}
	backgroundID := 2
	if skewed {
		backgroundID = 1
	}
	if _, err := tx.Exec(`UPDATE tournaments SET round1_target=2,round2_target=2,rules_json=?,background_id=? WHERE id=?`, string(raw), backgroundID, tournamentID); err != nil {
		return err
	}
	for _, playerID := range teamAPlayers {
		if _, err := tx.Exec(`INSERT INTO tournament_players(tournament_id,user_id,team_id) VALUES(?,?,?)`, tournamentID, playerID, teamA); err != nil {
			return err
		}
	}
	for _, playerID := range teamBPlayers {
		if _, err := tx.Exec(`INSERT INTO tournament_players(tournament_id,user_id,team_id) VALUES(?,?,?)`, tournamentID, playerID, teamB); err != nil {
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
		result, err := tx.Exec(`INSERT INTO rounds(tournament_id,stage,target_score,score_a,score_b,winner_team_id,status) VALUES(?,?,?,?,?,?,'finished')`, tournamentID, stage, target, scoreA, scoreB, winner)
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
			if _, err := tx.Exec(`INSERT INTO matches(tournament_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,winner_team_id,status,created_by,finished_at) VALUES(?,?,?,?,?,?,?,?, 'finished',?,CURRENT_TIMESTAMP)`, tournamentID, roundID, seq+1, teamA, teamB, teamAPlayers[pair[0]], teamBPlayers[pair[1]], win, creatorID); err != nil {
				return err
			}
		}
	}
	return seedAceSourceAction(tx, tournamentID, source)
}

func seedAceSourceAction(tx *sql.Tx, tournamentID int64, source string) error {
	sources := []string{source, source}
	if strings.HasPrefix(source, "special:") {
		sources[0] = "special:0"
		sources[1] = "special:1"
	}
	payload, err := json.Marshal(map[string]any{"source": source, "rules": sources})
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO tournament_actions(tournament_id,action,payload) VALUES(?,'ace_drawn',?)`, tournamentID, string(payload))
	return err
}

// 用正式比赛引擎生成演示战绩，确保分队、胜者留场、阶段比分与对局一致。
func seedCompletedTestTournament(tx *sql.Tx, managerID, creatorID int64, players []int64, name, startTime, winners string) error {
	tournamentID, teamA, teamB, err := seedTournamentShell(tx, managerID, creatorID, name, startTime, statusDraft)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, tournamentID, teamA, teamB, players); err != nil {
		return err
	}
	rules := defaultRules()
	rules.Round1Mode = "random"
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	backgroundID := any(nil)
	switch name {
	case "MSL KOF 夜":
		backgroundID = 1
	case "ASL 邀请赛":
		backgroundID = 2
	}
	if _, err := tx.Exec(`UPDATE tournaments SET background_id=?,rules_json=? WHERE id=?`, backgroundID, string(rulesJSON), tournamentID); err != nil {
		return err
	}
	engine := &matchEngine{db: tx, tournamentID: tournamentID, actorID: creatorID, rng: mathrand.New(mathrand.NewSource(tournamentID))}
	for _, stage := range []string{stageRound1, stageRound2, stageAce} {
		target := 1
		if stage == stageRound1 {
			target = 4
		}
		if stage == stageRound2 {
			target = 5
		}
		if _, err := tx.Exec("INSERT INTO rounds(tournament_id,stage,target_score) VALUES(?,?,?)", tournamentID, stage, target); err != nil {
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
		err = tx.QueryRow("SELECT id FROM matches WHERE tournament_id=? AND status='pending'", tournamentID).Scan(&matchID)
		if err == sql.ErrNoRows && status == stageAce {
			if err := engine.beginAceDraw(); err != nil {
				return err
			}
			continue
		}
		if err == sql.ErrNoRows && status == stageRound2 {
			var lastID int64
			if err := tx.QueryRow("SELECT id FROM matches WHERE tournament_id=? AND round_no=2 ORDER BY seq DESC LIMIT 1", tournamentID).Scan(&lastID); err != nil {
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

func seedLiveTestTournament(tx *sql.Tx, managerID, creatorID int64, players []int64) error {
	tournamentID, teamA, teamB, err := seedTournamentShell(tx, managerID, creatorID, "GSL 直播赛", time.Now().Format("2006-01-02T15"), statusDraft)
	if err != nil {
		return err
	}
	if err := seedPlayers(tx, tournamentID, teamA, teamB, players); err != nil {
		return err
	}
	rules := defaultRules()
	rules.Round1Mode = "random"
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE tournaments SET background_id=1,rules_json=? WHERE id=?`, string(rulesJSON), tournamentID); err != nil {
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
		if _, err := tx.Exec("INSERT INTO rounds(tournament_id,stage,target_score) VALUES(?,?,?)", tournamentID, stage, target); err != nil {
			return err
		}
	}
	engine := &matchEngine{db: tx, tournamentID: tournamentID, actorID: creatorID, rng: mathrand.New(mathrand.NewSource(42))}
	if err := engine.start(); err != nil {
		return err
	}
	// 直播演示直接停在KOF阶段，管理员登录后可以为下一位置报名或派人。
	for n := 0; n < 4; n++ {
		var id int64
		if err := tx.QueryRow("SELECT id FROM matches WHERE tournament_id=? AND status='pending'", tournamentID).Scan(&id); err != nil {
			return err
		}
		match, err := engine.match(id)
		if err != nil {
			return err
		}
		winner := teamA
		if err := engine.record(match, winner, ""); err != nil {
			return err
		}
	}
	return nil
}

// 不同阶段的实时赛事用于试用队长选举、排阵、KOF、大将战和结束确认。
func seedWorkflowDemoTournaments(tx *sql.Tx, managerID, creatorID int64, ids map[string]int64) error {
	tournamentID, _, _, err := seedTournamentShell(tx, managerID, creatorID, "待分队示例", time.Now().Format("2006-01-02T15"), statusDraft)
	if err != nil {
		return err
	}
	for _, stage := range []string{stageRound1, stageRound2, stageAce} {
		target := 1
		if stage == stageRound1 {
			target = 4
		} else if stage == stageRound2 {
			target = 5
		}
		if _, err := tx.Exec(`INSERT INTO rounds(tournament_id,stage,target_score) VALUES(?,?,?)`, tournamentID, stage, target); err != nil {
			return err
		}
	}
	// 工作流示例统一使用4v4，避免用3v3抢1分展示不合理的赛事配置。
	players := []int64{ids["2"], ids["3"], ids["4"], ids["5"], ids["6"], ids["7"], ids["8"], ids["9"]}
	newTournament := func(name, status string) (int64, int64, int64, error) {
		tournamentID, teamA, teamB, err := seedTournamentShell(tx, managerID, creatorID, name, time.Now().Format("2006-01-02T15"), status)
		if err != nil {
			return 0, 0, 0, err
		}
		for index, playerID := range players {
			teamID := teamB
			if index%2 == 0 {
				teamID = teamA
			}
			if _, err := tx.Exec(`INSERT INTO tournament_players(tournament_id,user_id,team_id) VALUES(?,?,?)`, tournamentID, playerID, teamID); err != nil {
				return 0, 0, 0, err
			}
		}
		for _, stage := range []string{stageRound1, stageRound2, stageAce} {
			target := 1
			if stage == stageRound1 {
				target = 4
			} else if stage == stageRound2 {
				target = 5
			}
			if _, err := tx.Exec(`INSERT INTO rounds(tournament_id,stage,target_score) VALUES(?,?,?)`, tournamentID, stage, target); err != nil {
				return 0, 0, 0, err
			}
		}
		rules := defaultRules()
		rules.Round1Mode = "random"
		rulesJSON, err := json.Marshal(rules)
		if err != nil {
			return 0, 0, 0, err
		}
		if _, err := tx.Exec(`UPDATE tournaments SET background_id=2,rules_json=? WHERE id=?`, string(rulesJSON), tournamentID); err != nil {
			return 0, 0, 0, err
		}
		return tournamentID, teamA, teamB, nil
	}
	addMatch := func(tournamentID, roundID, teamA, teamB, playerA, playerB int64, seq int, winner int64, status string) error {
		_, err := tx.Exec(`INSERT INTO matches(tournament_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,winner_team_id,status,created_by,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,CASE WHEN ?='finished' THEN CURRENT_TIMESTAMP END)`, tournamentID, roundID, seq, teamA, teamB, playerA, playerB, nullableInt64(winner), status, creatorID, status)
		return err
	}
	getRoundID := func(tournamentID int64, stage string) (int64, error) {
		var id int64
		err := tx.QueryRow(`SELECT id FROM rounds WHERE tournament_id=? AND stage=?`, tournamentID, stage).Scan(&id)
		return id, err
	}

	tournamentID, teamA, teamB, err := newTournament("队长投票示例", statusCaptainVote)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO captain_votes(tournament_id,team_id,voter_id,target_id) VALUES(?,?,?,?)`, tournamentID, teamA, players[0], players[2]); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO captain_votes(tournament_id,team_id,voter_id,target_id) VALUES(?,?,?,?)`, tournamentID, teamB, players[1], players[3]); err != nil {
		return err
	}

	tournamentID, teamA, teamB, err = newTournament("第一轮排阵示例", statusLineup)
	if err != nil {
		return err
	}
	for index, captain := range []int64{players[0], players[1]} {
		if _, err := tx.Exec(`UPDATE tournament_players SET is_captain=1 WHERE tournament_id=? AND user_id=?`, tournamentID, captain); err != nil {
			return err
		}
		teamID := teamA
		if index == 1 {
			teamID = teamB
		}
		if _, err := tx.Exec(`INSERT INTO lineup_entries(tournament_id,team_id,position,player_id,source) VALUES(?,?,1,?,'manual')`, tournamentID, teamID, captain); err != nil {
			return err
		}
	}

	tournamentID, teamA, teamB, err = newTournament("KOF 阶段示例", statusRound2)
	if err != nil {
		return err
	}
	r1, err := getRoundID(tournamentID, stageRound1)
	if err != nil {
		return err
	}
	r2, err := getRoundID(tournamentID, stageRound2)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE rounds SET target_score=4,score_a=4,winner_team_id=?,status='finished' WHERE id=?`, teamA, r1); err != nil {
		return err
	}
	for seq := 1; seq <= 4; seq++ {
		if err := addMatch(tournamentID, r1, teamA, teamB, players[(seq-1)*2], players[(seq-1)*2+1], seq, teamA, statusFinished); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE rounds SET status='active' WHERE id=?`, r2); err != nil {
		return err
	}
	if err := addMatch(tournamentID, r2, teamA, teamB, players[2], players[3], 1, 0, statusPending); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE tournament_players SET is_captain=1 WHERE tournament_id=? AND user_id IN (?,?)`, tournamentID, players[0], players[1]); err != nil {
		return err
	}

	tournamentID, teamA, teamB, err = newTournament("大将战示例", statusAce)
	if err != nil {
		return err
	}
	r1, err = getRoundID(tournamentID, stageRound1)
	if err != nil {
		return err
	}
	r2, err = getRoundID(tournamentID, stageRound2)
	if err != nil {
		return err
	}
	r3, err := getRoundID(tournamentID, stageAce)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE rounds SET target_score=4,score_a=4,winner_team_id=?,status='finished' WHERE id=?`, teamA, r1); err != nil {
		return err
	}
	for seq := 1; seq <= 4; seq++ {
		if err := addMatch(tournamentID, r1, teamA, teamB, players[(seq-1)*2], players[(seq-1)*2+1], seq, teamA, statusFinished); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE rounds SET target_score=5,score_b=5,winner_team_id=?,status='finished' WHERE id=?`, teamB, r2); err != nil {
		return err
	}
	for seq := 1; seq <= 5; seq++ {
		index := ((seq - 1) % 4) * 2
		if err := addMatch(tournamentID, r2, teamA, teamB, players[index], players[1], seq, teamB, statusFinished); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE rounds SET status='active' WHERE id=?`, r3); err != nil {
		return err
	}
	if err := addMatch(tournamentID, r3, teamA, teamB, players[4], players[5], 1, 0, statusPending); err != nil {
		return err
	}
	if err := seedAceSourceAction(tx, tournamentID, "most_wins"); err != nil {
		return err
	}

	tournamentID, teamA, teamB, err = newTournament("结束确认示例", statusFinalConfirm)
	if err != nil {
		return err
	}
	r1, err = getRoundID(tournamentID, stageRound1)
	if err != nil {
		return err
	}
	r2, err = getRoundID(tournamentID, stageRound2)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE rounds SET target_score=4,score_a=4,winner_team_id=?,status='finished' WHERE id=?`, teamA, r1); err != nil {
		return err
	}
	for seq := 1; seq <= 4; seq++ {
		if err := addMatch(tournamentID, r1, teamA, teamB, players[(seq-1)*2], players[(seq-1)*2+1], seq, teamA, statusFinished); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE rounds SET target_score=5,score_a=5,winner_team_id=?,status='finished' WHERE id=?`, teamA, r2); err != nil {
		return err
	}
	for seq := 1; seq <= 5; seq++ {
		index := ((seq - 1) % 4) * 2
		if err := addMatch(tournamentID, r2, teamA, teamB, players[0], players[index+1], seq, teamA, statusFinished); err != nil {
			return err
		}
	}
	return nil
}

func seedTournamentShell(tx *sql.Tx, managerID, creatorID int64, name, startTime, status string) (int64, int64, int64, error) {
	result, err := tx.Exec(`INSERT INTO tournaments(name,start_time,status,round1_target,round2_target,manager_id,created_by) VALUES(?,?,?,?,?,?,?)`, name, startTime, status, 4, 5, managerID, creatorID)
	if err != nil {
		return 0, 0, 0, err
	}
	tournamentID, _ := result.LastInsertId()
	return tournamentID, tournamentID*2 - 1, tournamentID * 2, nil
}

func seedPlayers(tx *sql.Tx, tournamentID, teamA, teamB int64, players []int64) error {
	for i, playerID := range players {
		teamID := teamB
		if i%2 == 0 {
			teamID = teamA
		}
		if _, err := tx.Exec(`INSERT INTO tournament_players(tournament_id,user_id,team_id) VALUES(?,?,?)`, tournamentID, playerID, teamID); err != nil {
			return err
		}
	}
	return nil
}

// 演示头像使用官方游戏角色裁图，不代表同名选手的真实肖像。
func demoAvatar(username string) ([]byte, error) {
	return demoFS.ReadFile("testdata/demo/avatars/" + username + ".png")
}
