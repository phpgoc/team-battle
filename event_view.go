package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

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
	data, err := a.buildEvent(eventID, true)
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
	if err := a.db.QueryRow(`SELECT e.name,e.description,e.event_time,e.status,e.round1_target,e.round2_target,e.manager_id,e.created_at FROM events e WHERE e.id=?`, eventID).Scan(&name, &description, &eventTime, &status, &round1Target, &round2Target, &managerID, &createdAt); err != nil {
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
	event["created_at"] = createdAt
	teams, err := a.loadTeams(eventID)
	if err != nil {
		return nil, err
	}
	event["teams"] = teams
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
	if includePrivate {
		actions, _ := a.loadActions(eventID)
		event["actions"] = actions
	}
	return event, nil
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
	rows, err := a.db.Query(`SELECT ep.user_id,u.username,u.display_name,ep.is_captain,ep.active,COALESCE(u.category_id,0),CASE WHEN u.avatar_data IS NULL OR length(u.avatar_data)=0 THEN 0 ELSE 1 END FROM event_players ep JOIN users u ON u.id=ep.user_id WHERE ep.event_id=? AND ep.team_id=? ORDER BY ep.is_captain DESC,u.display_name`, eventID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type playerRow struct {
		id, categoryID          int64
		username, displayName   string
		captain, active, avatar int
	}
	playerRows := []playerRow{}
	for rows.Next() {
		var row playerRow
		if err := rows.Scan(&row.id, &row.username, &row.displayName, &row.captain, &row.active, &row.categoryID, &row.avatar); err != nil {
			return nil, err
		}
		playerRows = append(playerRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	players := make([]map[string]any, 0, len(playerRows))
	for _, row := range playerRows {
		item := gin.H{"id": row.id, "username": row.username, "display_name": row.displayName, "is_captain": row.captain == 1, "active": row.active == 1, "category_id": row.categoryID}
		if row.avatar == 1 {
			item["avatar_url"] = "/api/users/" + strconv.FormatInt(row.id, 10) + "/avatar"
		}
		if category, ok := a.categoryInfo(row.categoryID); ok {
			item["category_name"] = category.Name
			item["category_icon"] = category.IconURL
		}
		players = append(players, item)
	}
	return players, nil
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
	rows, err := a.db.Query(`SELECT m.id,m.round_id,r.stage,m.seq,m.team_a_id,ta.name,m.team_b_id,tb.name,m.player_a_id,pa.display_name,m.player_b_id,pb.display_name,m.winner_team_id,m.next_player_id,m.status,m.note,m.created_at,m.finished_at FROM matches m JOIN rounds r ON r.id=m.round_id JOIN teams ta ON ta.id=m.team_a_id JOIN teams tb ON tb.id=m.team_b_id JOIN users pa ON pa.id=m.player_a_id JOIN users pb ON pb.id=m.player_b_id WHERE m.event_id=? ORDER BY m.round_id,m.seq`, eventID)
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
		if err := rows.Scan(&id, &roundID, &stage, &seq, &teamA, &teamAName, &teamB, &teamBName, &playerA, &playerAName, &playerB, &playerBName, &winner, &next, &status, &note, &created, &finished); err != nil {
			return nil, err
		}
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
