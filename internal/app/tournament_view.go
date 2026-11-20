package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *App) tournamentDetail(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tournamentID, err := parseID(c.Param("id"))
	if err != nil || !a.canViewTournament(user.ID, user.Role, tournamentID) {
		fail(c, http.StatusForbidden, "无权查看该赛事")
		return
	}
	// 预报名审计含尚未公开的选手，完整审计仅供赛事管理员查看。
	data, err := a.buildTournament(tournamentID, a.canManageTournament(user.ID, user.Role, tournamentID))
	if err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	if data["public_ready"] != true && !a.canManageTournament(user.ID, user.Role, tournamentID) {
		fail(c, http.StatusNotFound, "赛事暂未公开")
		return
	}
	ok(c, data)
}

func (a *App) publicTournament(c *gin.Context) {
	tournamentID, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	c.Header("Cache-Control", "no-store")
	data, err := a.buildTournament(tournamentID, false)
	if err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	user, authErr := a.currentUser(c)
	if data["public_ready"] != true && !(authErr == nil && a.canManageTournament(user.ID, user.Role, tournamentID)) {
		fail(c, http.StatusNotFound, "赛事暂未公开")
		return
	}
	ok(c, data)
}

func (a *App) buildTournament(tournamentID int64, includePrivate bool) (map[string]any, error) {
	var tournament = map[string]any{}
	var name, description, startTime, status, createdAt string
	var round1Target, round2Target, managerID int64
	var backgroundID sql.NullInt64
	var imported, deleted bool
	if err := a.db.QueryRow(`SELECT e.name,e.description,e.start_time,e.status,e.round1_target,e.round2_target,e.manager_id,e.created_at,e.background_id,e.is_imported,e.is_deleted FROM tournaments e WHERE e.id=?`, tournamentID).Scan(&name, &description, &startTime, &status, &round1Target, &round2Target, &managerID, &createdAt, &backgroundID, &imported, &deleted); err != nil {
		return nil, err
	}
	tournament["id"] = tournamentID
	tournament["name"] = name
	tournament["description"] = description
	tournament["start_time"] = startTime
	tournament["status"] = status
	tournament["is_imported"] = imported
	tournament["is_deleted"] = deleted
	tournament["round1_target"] = round1Target
	tournament["round2_target"] = round2Target
	tournament["manager_id"] = managerID
	// 管理员可能不在参赛名单中，通过账号目录读取公开显示名。
	tournament["manager_name"] = ""
	if manager, err := a.cachedUser(managerID); err == nil && !manager.IsDeleted {
		tournament["manager_name"] = manager.DisplayName
	}
	tournament["created_at"] = createdAt
	if backgroundID.Valid {
		var backgroundName, updatedAt string
		if err := a.db.QueryRow(`SELECT name,updated_at FROM backgrounds WHERE id=?`, backgroundID.Int64).Scan(&backgroundName, &updatedAt); err == nil {
			tournament["background_id"] = backgroundID.Int64
			tournament["background_name"] = backgroundName
			tournament["background_url"] = "/api/backgrounds/" + strconv.FormatInt(backgroundID.Int64, 10) + "/image?v=" + url.QueryEscape(updatedAt)
		}
	}
	teams, err := a.loadTeams(tournamentID)
	if err != nil {
		return nil, err
	}
	tournament["teams"] = teams
	directory, err := a.userDirectory()
	if err != nil {
		return nil, err
	}
	setup := []map[string]any{}
	setupRows, err := a.db.Query(`SELECT user_id,fixed_team FROM tournament_setup_roster WHERE tournament_id=? ORDER BY position,user_id`, tournamentID)
	if err != nil {
		return nil, err
	}
	for setupRows.Next() {
		var id int64
		var side string
		if err := setupRows.Scan(&id, &side); err != nil {
			setupRows.Close()
			return nil, err
		}
		player := directory[id]
		setup = append(setup, map[string]any{"id": id, "fixed_team": side, "display_name": player.DisplayName, "avatar_url": player.AvatarURL, "category_name": player.CategoryName, "category_icon": player.CategoryIcon, "selectable": player.Status == "active" && !player.IsDeleted && isNonSuperAdmin(player.Role)})
	}
	if err := setupRows.Err(); err != nil {
		setupRows.Close()
		return nil, err
	}
	setupRows.Close()
	tournament["setup_roster"] = setup
	players, assigned := 0, 0
	for _, team := range teams {
		active := 0
		for _, player := range team["players"].([]map[string]any) {
			if player["active"] != false {
				players++
				active++
			}
		}
		if active > 0 {
			assigned++
		}
	}
	tournament["public_ready"] = rosterReadiness(players, assigned, len(setup)).publicReady
	var raw string
	if err := a.db.QueryRow(`SELECT rules_json FROM tournaments WHERE id=?`, tournamentID).Scan(&raw); err != nil {
		return nil, err
	}
	rules, err := decodeRules(raw)
	if err != nil {
		return nil, err
	}
	tournament["rules"] = rules
	setupDraw, err := a.loadSetupDrawInfo(tournamentID)
	if err != nil {
		return nil, err
	}
	if setupDraw != nil {
		tournament["setup_draw"] = setupDraw
	}
	starters := []map[string]any{}
	selectionRows, err := a.db.Query(`SELECT team_id,player_id FROM player_selections WHERE tournament_id=? AND stage='round2' AND EXISTS (SELECT 1 FROM matches WHERE tournament_id=? AND round_no=2) ORDER BY team_id`, tournamentID, tournamentID)
	if err != nil {
		return nil, err
	}
	for selectionRows.Next() {
		var team, player int64
		if err := selectionRows.Scan(&team, &player); err != nil {
			selectionRows.Close()
			return nil, err
		}
		starters = append(starters, map[string]any{"team_id": team, "player_id": player})
	}
	if err := selectionRows.Err(); err != nil {
		selectionRows.Close()
		return nil, err
	}
	selectionRows.Close()
	tournament["kof_starters"] = starters
	if rules.KOFStarter == "random" {
		var aID, bID sql.NullInt64
		if err := a.db.QueryRow(`SELECT kof_initial_a,kof_initial_b FROM tournaments WHERE id=?`, tournamentID).Scan(&aID, &bID); err != nil {
			return nil, err
		}
		if aID.Valid && bID.Valid && len(teams) == 2 {
			tournament["kof_starters"] = []map[string]any{{"team_id": teams[0]["id"], "player_id": aID.Int64}, {"team_id": teams[1]["id"], "player_id": bID.Int64}}
		}
	}
	lineups := []map[string]any{}
	lineupRows, err := a.db.Query(`SELECT team_id,position,player_id,source FROM lineup_entries WHERE tournament_id=? ORDER BY team_id,position`, tournamentID)
	if err != nil {
		return nil, err
	}
	for lineupRows.Next() {
		var team, player int64
		var pos int
		var source string
		if err := lineupRows.Scan(&team, &pos, &player, &source); err != nil {
			lineupRows.Close()
			return nil, err
		}
		lineups = append(lineups, map[string]any{"team_id": team, "position": pos, "player_id": player, "source": source})
	}
	lineupRows.Close()
	// 双方都确认前只公开提交状态，避免另一队通过详情或SSE提前看到排阵。
	if rules.Round1Mode == "manual" && preMatch(status) && !includePrivate {
		ready := len(teams) == 2
		for _, team := range teams {
			ready = ready && team["lineup_submitted"] == true
		}
		if !ready {
			lineups = []map[string]any{}
		}
	}
	tournament["lineups"] = lineups
	votes := []map[string]any{}
	voteRows, err := a.db.Query(`SELECT target_id,COUNT(*) FROM captain_votes WHERE tournament_id=? GROUP BY target_id`, tournamentID)
	if err != nil {
		return nil, err
	}
	for voteRows.Next() {
		var player int64
		var count int
		if err := voteRows.Scan(&player, &count); err != nil {
			voteRows.Close()
			return nil, err
		}
		votes = append(votes, map[string]any{"player_id": player, "count": count})
	}
	voteRows.Close()
	tournament["votes"] = votes
	rounds, err := a.loadRounds(tournamentID)
	if err != nil {
		return nil, err
	}
	tournament["rounds"] = rounds
	matches, err := a.loadMatches(tournamentID)
	if err != nil {
		return nil, err
	}
	tournament["matches"] = matches
	if draw, err := a.loadAceDrawResult(tournamentID); err == nil && draw != nil {
		tournament["ace_draw"] = draw
		tournament["ace_draw_source"] = draw.Source
		if draw.Special > 0 {
			tournament["ace_draw_special"] = draw.Special
		}
	}
	return tournament, nil
}

func (a *App) loadSetupDrawInfo(tournamentID int64) ([]map[string]any, error) {
	var raw string
	err := a.db.QueryRow(`SELECT payload FROM tournament_actions WHERE tournament_id=? AND action='tournament_setup_drawn' ORDER BY id DESC LIMIT 1`, tournamentID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var steps []setupDrawStep
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, step := range steps {
		if len(step.ExtraInfo) == 0 {
			continue
		}
		result = append(result, map[string]any{"stage": step.Kind, "seq": step.Position, "extra_info": step.ExtraInfo})
	}
	return result, nil
}

func (a *App) loadTeams(tournamentID int64) ([]map[string]any, error) {
	ready, err := lineupReadiness(a.db, tournamentID)
	if err != nil {
		return nil, err
	}
	teams := []map[string]any{}
	for side, id := range fixedTeamIDs(tournamentID) {
		players, err := a.loadPlayers(tournamentID, id)
		if err != nil {
			return nil, err
		}
		teams = append(teams, gin.H{"id": id, "name": []string{"A 队", "B 队"}[side], "color": []string{"#ef8354", "#3578e5"}[side], "players": players, "lineup_submitted": ready[side]})
	}
	return teams, nil
}

func (a *App) loadPlayers(tournamentID, teamID int64) ([]map[string]any, error) {
	users, err := a.userDirectory()
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`SELECT user_id,is_captain,active FROM tournament_players WHERE tournament_id=? AND team_id=? ORDER BY is_captain DESC,user_id`, tournamentID, teamID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id              int64
		captain, active int
	}
	entries := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.captain, &r.active); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	items := []map[string]any{}
	for _, r := range entries {
		u, exists := users[r.id]
		if !exists {
			return nil, errors.New("参赛账号不存在")
		}
		items = append(items, map[string]any{"id": u.ID, "display_name": u.DisplayName, "avatar_url": u.AvatarURL, "category_id": u.CategoryID, "category_name": u.CategoryName, "category_icon": u.CategoryIcon, "is_captain": r.captain == 1, "active": r.active == 1})
	}
	return items, nil
}

func (a *App) loadRounds(tournamentID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,stage,target_score,score_a,score_b,winner_team_id,status,updated_at FROM rounds WHERE tournament_id=? ORDER BY CASE stage WHEN 'round1' THEN 1 WHEN 'round2' THEN 2 ELSE 3 END`, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, target, scoreA, scoreB int64
		var winner sql.NullInt64
		var stage, status, updated string
		if err := rows.Scan(&id, &stage, &target, &scoreA, &scoreB, &winner, &status, &updated); err != nil {
			return nil, err
		}
		item := gin.H{"id": id, "stage": stage, "target_score": target, "score_a": scoreA, "score_b": scoreB, "status": status, "updated_at": updated}
		if winner.Valid {
			item["winner_team_id"] = winner.Int64
		}
		items = append(items, item)
	}
	return items, nil
}

func (a *App) loadMatches(tournamentID int64) ([]map[string]any, error) {
	users, err := a.userDirectory()
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`SELECT m.id,m.round_id,r.stage,m.seq,m.team_a_id,'A 队',m.team_b_id,'B 队',m.player_a_id,m.player_b_id,m.winner_team_id,m.next_player_id,m.status,m.note,m.created_at,m.finished_at,m.extra_info_json FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.tournament_id=? ORDER BY m.round_id,m.seq`, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, roundID, seq, teamA, teamB, playerA, playerB int64
		var winner, next sql.NullInt64
		var stage, teamAName, teamBName, playerAName, playerBName, status, note, created string
		var extraInfoJSON string
		var finished sql.NullString
		if err := rows.Scan(&id, &roundID, &stage, &seq, &teamA, &teamAName, &teamB, &teamBName, &playerA, &playerB, &winner, &next, &status, &note, &created, &finished, &extraInfoJSON); err != nil {
			return nil, err
		}
		extraInfo := map[string]string{}
		if err := json.Unmarshal([]byte(extraInfoJSON), &extraInfo); err != nil {
			return nil, errors.New("对局额外信息格式错误")
		}
		pa, okA := users[playerA]
		pb, okB := users[playerB]
		if !okA || !okB {
			return nil, errors.New("对局账号不存在")
		}
		playerAName = pa.DisplayName
		playerBName = pb.DisplayName
		roundNo := int64(0)
		switch stage {
		case stageRound1:
			roundNo = 1
		case stageRound2:
			roundNo = 2
		case stageAce:
			roundNo = 3
		}
		whoWin := "0"
		if winner.Valid {
			if winner.Int64 == teamA {
				whoWin = "a"
			} else if winner.Int64 == teamB {
				whoWin = "b"
			}
		}
		item := gin.H{"id": id, "match_id": id, "round_id": roundID, "round": roundNo, "stage": stage, "seq": seq, "match_no": seq, "team_a_id": teamA, "team_a_name": teamAName, "team_b_id": teamB, "team_b_name": teamBName, "player_a_id": playerA, "player_a_name": playerAName, "player_b_id": playerB, "player_b_name": playerBName, "a_id": playerA, "b_id": playerB, "who_win": whoWin, "status": status, "note": note, "created_at": created, "finished_at": finished.String, "extra_info": extraInfo}
		if winner.Valid {
			item["winner_team_id"] = winner.Int64
		}
		if next.Valid {
			item["next_player_id"] = next.Int64
		}
		items = append(items, item)
	}
	return items, nil
}

func (a *App) roundInfo(tournamentID int64, stage string) (int64, int, int, int, string, error) {
	var target, scoreA, scoreB int
	var roundID int64
	var status string
	err := a.db.QueryRow(`SELECT id,target_score,score_a,score_b,status FROM rounds WHERE tournament_id=? AND stage=?`, tournamentID, stage).Scan(&roundID, &target, &scoreA, &scoreB, &status)
	return roundID, target, scoreA, scoreB, status, err
}

func (a *App) teamIDs(tournamentID int64) ([]int64, error) {
	return loadFixedTeamIDs(a.db, tournamentID)
}
