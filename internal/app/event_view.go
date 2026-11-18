package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) eventDetail(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	eventID, err := parseID(c.Param("id"))
	if err != nil || !a.canViewEvent(user.ID, user.Role, eventID) {
		fail(c, http.StatusForbidden, "无权查看该赛事")
		return
	}
	// 预报名审计含尚未公开的选手，完整审计仅供赛事管理员查看。
	data, err := a.buildEvent(eventID, a.canManageEvent(user.ID, user.Role, eventID))
	if err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	ok(c, data)
}

func (a *App) publicEvent(c *gin.Context) {
	eventID, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	data, err := a.buildEvent(eventID, false)
	if err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	ok(c, data)
}

func (a *App) buildEvent(eventID int64, includePrivate bool) (map[string]any, error) {
	var event = map[string]any{}
	var name, description, eventTime, status, createdAt string
	var round1Target, round2Target, managerID int64
	var backgroundID sql.NullInt64
	if err := a.db.QueryRow(`SELECT e.name,e.description,e.event_time,e.status,e.round1_target,e.round2_target,e.manager_id,e.created_at,e.background_id FROM events e WHERE e.id=?`, eventID).Scan(&name, &description, &eventTime, &status, &round1Target, &round2Target, &managerID, &createdAt, &backgroundID); err != nil {
		return nil, err
	}
	event["id"] = eventID
	event["name"] = name
	event["description"] = description
	event["event_time"] = eventTime
	event["status"] = status
	event["round1_target"] = round1Target
	event["round2_target"] = round2Target
	event["manager_id"] = managerID
	// 管理员可能不在参赛名单中，通过账号目录读取公开显示名。
	event["manager_name"] = ""
	if manager, err := a.cachedUser(managerID); err == nil && !manager.IsDeleted && isNonSuperAdmin(manager.Role) {
		event["manager_name"] = manager.DisplayName
	}
	event["created_at"] = createdAt
	if backgroundID.Valid {
		var backgroundName, updatedAt string
		if err := a.db.QueryRow(`SELECT name,updated_at FROM backgrounds WHERE id=?`, backgroundID.Int64).Scan(&backgroundName, &updatedAt); err == nil {
			event["background_id"] = backgroundID.Int64
			event["background_name"] = backgroundName
			event["background_url"] = "/api/backgrounds/" + strconv.FormatInt(backgroundID.Int64, 10) + "/image?v=" + url.QueryEscape(updatedAt)
		}
	}
	teams, err := a.loadTeams(eventID)
	if err != nil {
		return nil, err
	}
	event["teams"] = teams
	var raw string
	if err := a.db.QueryRow(`SELECT rules_json FROM events WHERE id=?`, eventID).Scan(&raw); err != nil {
		return nil, err
	}
	rules, err := decodeRules(raw)
	if err != nil {
		return nil, err
	}
	event["rules"] = rules
	starters := []map[string]any{}
	selectionRows, err := a.db.Query(`SELECT team_id,player_id FROM player_selections WHERE event_id=? AND stage='round2' AND EXISTS (SELECT 1 FROM matches WHERE event_id=? AND round_no=2) ORDER BY team_id`, eventID, eventID)
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
	event["kof_starters"] = starters
	lineups := []map[string]any{}
	lineupRows, err := a.db.Query(`SELECT team_id,position,player_id,source FROM lineup_entries WHERE event_id=? ORDER BY team_id,position`, eventID)
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
	event["lineups"] = lineups
	votes := []map[string]any{}
	voteRows, err := a.db.Query(`SELECT target_id,COUNT(*) FROM captain_votes WHERE event_id=? GROUP BY target_id`, eventID)
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
	event["votes"] = votes
	rounds, err := a.loadRounds(eventID)
	if err != nil {
		return nil, err
	}
	event["rounds"] = rounds
	matches, err := a.loadMatches(eventID)
	if err != nil {
		return nil, err
	}
	event["matches"] = matches
	if source, special, err := a.loadAceDrawSource(eventID); err == nil && source != "" {
		event["ace_draw_source"] = source
		if special > 0 {
			event["ace_draw_special"] = special
		}
	}
	if includePrivate {
		actions, _ := a.loadActions(eventID)
		event["actions"] = actions
	}
	return event, nil
}

// 大将战来源只公开规则名称，不公开抽签时的选手候选和内部审计数据。
func (a *App) loadAceDrawSource(eventID int64) (string, int, error) {
	var payload string
	if err := a.db.QueryRow(`SELECT payload FROM event_actions WHERE event_id=? AND action='ace_drawn' ORDER BY id DESC LIMIT 1`, eventID).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return "", 0, nil
		}
		return "", 0, err
	}
	var data struct {
		Source string   `json:"source"`
		Rules  []string `json:"rules"`
	}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return "", 0, err
	}
	source := data.Source
	if source == "" && len(data.Rules) > 0 {
		source = data.Rules[0]
		for _, item := range data.Rules[1:] {
			if item != source {
				source = "mixed"
				break
			}
		}
	}
	if source == "random_fallback" {
		source = "random"
	}
	special := 0
	if strings.HasPrefix(source, "special:") {
		if value, err := strconv.Atoi(strings.TrimPrefix(source, "special:")); err == nil && value >= 0 {
			special = value + 1
			source = "special"
		}
	}
	return source, special, nil
}

func (a *App) loadTeams(eventID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,name,color FROM teams WHERE event_id=? ORDER BY id`, eventID)
	if err != nil {
		return nil, err
	}
	type teamRow struct {
		id    int64
		name  string
		color string
	}
	teamRows := []teamRow{}
	for rows.Next() {
		var id int64
		var name, color string
		if err := rows.Scan(&id, &name, &color); err != nil {
			rows.Close()
			return nil, err
		}
		teamRows = append(teamRows, teamRow{id: id, name: name, color: color})
	}
	rows.Close()
	teams := []map[string]any{}
	for _, team := range teamRows {
		players, err := a.loadPlayers(eventID, team.id)
		if err != nil {
			return nil, err
		}
		teams = append(teams, gin.H{"id": team.id, "name": team.name, "color": team.color, "players": players})
	}
	return teams, nil
}

func (a *App) loadPlayers(eventID, teamID int64) ([]map[string]any, error) {
	users, err := a.userDirectory()
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`SELECT user_id,is_captain,active FROM event_players WHERE event_id=? AND team_id=? ORDER BY is_captain DESC,user_id`, eventID, teamID)
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
		items = append(items, map[string]any{"id": u.ID, "username": u.Username, "display_name": u.DisplayName, "avatar_url": u.AvatarURL, "category_id": u.CategoryID, "category_name": u.CategoryName, "category_icon": u.CategoryIcon, "is_captain": r.captain == 1, "active": r.active == 1})
	}
	return items, nil
}

func (a *App) loadRounds(eventID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,stage,target_score,score_a,score_b,winner_team_id,status,updated_at FROM rounds WHERE event_id=? ORDER BY CASE stage WHEN 'round1' THEN 1 WHEN 'round2' THEN 2 ELSE 3 END`, eventID)
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

func (a *App) loadMatches(eventID int64) ([]map[string]any, error) {
	users, err := a.userDirectory()
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`SELECT m.id,m.round_id,r.stage,m.seq,m.team_a_id,ta.name,m.team_b_id,tb.name,m.player_a_id,m.player_b_id,m.winner_team_id,m.next_player_id,m.status,m.note,m.created_at,m.finished_at FROM matches m JOIN rounds r ON r.id=m.round_id JOIN teams ta ON ta.id=m.team_a_id JOIN teams tb ON tb.id=m.team_b_id WHERE m.event_id=? ORDER BY m.round_id,m.seq`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, roundID, seq, teamA, teamB, playerA, playerB int64
		var winner, next sql.NullInt64
		var stage, teamAName, teamBName, playerAName, playerBName, status, note, created string
		var finished sql.NullString
		if err := rows.Scan(&id, &roundID, &stage, &seq, &teamA, &teamAName, &teamB, &teamBName, &playerA, &playerB, &winner, &next, &status, &note, &created, &finished); err != nil {
			return nil, err
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
		item := gin.H{"id": id, "match_id": id, "round_id": roundID, "round": roundNo, "stage": stage, "seq": seq, "match_no": seq, "team_a_id": teamA, "team_a_name": teamAName, "team_b_id": teamB, "team_b_name": teamBName, "player_a_id": playerA, "player_a_name": playerAName, "player_b_id": playerB, "player_b_name": playerBName, "a_id": playerA, "b_id": playerB, "who_win": whoWin, "status": status, "note": note, "created_at": created, "finished_at": finished.String}
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

func (a *App) loadActions(eventID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT ea.id,ea.action,ea.payload,ea.created_at,COALESCE(u.display_name,'系统') FROM event_actions ea LEFT JOIN users u ON u.id=ea.actor_id WHERE ea.event_id=? ORDER BY ea.id DESC LIMIT 30`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var action, payload, created, actor string
		if err := rows.Scan(&id, &action, &payload, &created, &actor); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": id, "action": action, "payload": payload, "created_at": created, "actor": actor})
	}
	return items, nil
}

func (a *App) roundInfo(eventID int64, stage string) (int64, int, int, int, string, error) {
	var target, scoreA, scoreB int
	var roundID int64
	var status string
	err := a.db.QueryRow(`SELECT id,target_score,score_a,score_b,status FROM rounds WHERE event_id=? AND stage=?`, eventID, stage).Scan(&roundID, &target, &scoreA, &scoreB, &status)
	return roundID, target, scoreA, scoreB, status, err
}

func (a *App) teamIDs(eventID int64) ([]int64, error) {
	rows, err := a.db.Query(`SELECT id FROM teams WHERE event_id=? ORDER BY id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 {
		return nil, errors.New("赛事队伍配置不完整")
	}
	return ids, nil
}
