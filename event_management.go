package main

// Tournament creation, participant allocation, and event listings.

import (
	"database/sql"
	"errors"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *App) listEvents(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	query := `SELECT e.id, e.name, e.description, e.event_time, e.status, e.round1_target, e.round2_target, e.manager_id, COALESCE(u.display_name,''), e.created_at,
		(SELECT COUNT(*) FROM event_players ep WHERE ep.event_id=e.id) AS player_count,
		(SELECT COALESCE(SUM(r.score_a),0) FROM rounds r WHERE r.event_id=e.id) AS score_a,
		(SELECT COALESCE(SUM(r.score_b),0) FROM rounds r WHERE r.event_id=e.id) AS score_b
		FROM events e LEFT JOIN users u ON u.id=e.manager_id`
	args := []any{}
	if user.Role != roleSuperAdmin {
		query += ` WHERE e.manager_id = ? OR EXISTS (SELECT 1 FROM event_players ep2 WHERE ep2.event_id=e.id AND ep2.user_id=?)`
		args = append(args, user.ID, user.ID)
	}
	query += ` ORDER BY CASE e.status WHEN 'round1' THEN 0 WHEN 'round2' THEN 0 WHEN 'ace' THEN 0 WHEN 'draft' THEN 1 ELSE 2 END, e.updated_at DESC`
	rows, err := a.db.Query(query, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var item = map[string]any{}
		var id, managerID, playerCount, round1Target, round2Target, scoreA, scoreB int64
		var name, description, eventTime, status, managerName, createdAt string
		if err := rows.Scan(&id, &name, &description, &eventTime, &status, &round1Target, &round2Target, &managerID, &managerName, &createdAt, &playerCount, &scoreA, &scoreB); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		item["id"] = id
		item["name"] = name
		item["description"] = description
		item["event_time"] = eventTime
		item["status"] = status
		item["round1_target"] = round1Target
		item["round2_target"] = round2Target
		item["manager_id"] = managerID
		item["manager_name"] = managerName
		item["player_count"] = playerCount
		item["score_a"] = scoreA
		item["score_b"] = scoreB
		item["created_at"] = createdAt
		items = append(items, item)
	}
	ok(c, gin.H{"events": items})
}

func (a *App) publicEvents(c *gin.Context) {
	rows, err := a.db.Query(`SELECT id, name, description, event_time, status, round1_target, round2_target, created_at FROM events WHERE status IN ('round1','round2','ace','final_confirm') ORDER BY updated_at DESC`)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, round1, round2 int64
		var name, description, eventTime, status, createdAt string
		if err := rows.Scan(&id, &name, &description, &eventTime, &status, &round1, &round2, &createdAt); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, gin.H{"id": id, "name": name, "description": description, "event_time": eventTime, "status": status, "round1_target": round1, "round2_target": round2, "created_at": createdAt})
	}
	ok(c, gin.H{"events": items})
}

func (a *App) publicHistory(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	page := queryInt(c.Query("page"), 1)
	pageSize := queryInt(c.Query("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	items, err := a.historyItems()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取赛事历史失败")
		return
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		itemName, _ := item["name"].(string)
		eventTime, _ := item["event_time"].(string)
		eventTime = strings.ReplaceAll(eventTime, "T", " ")
		if name != "" && !strings.Contains(strings.ToLower(itemName), strings.ToLower(name)) {
			continue
		}
		if from != "" && eventTime < from {
			continue
		}
		if to != "" && eventTime > to+" 23:59:59" {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	ok(c, gin.H{"events": filtered[start:end], "page": page, "page_size": pageSize, "total": total})
}

func (a *App) historyItems() ([]map[string]any, error) {
	a.history.mu.RLock()
	if len(a.history.items) > 0 && time.Since(a.history.loadedAt) < 30*time.Second {
		items := append([]map[string]any(nil), a.history.items...)
		a.history.mu.RUnlock()
		return items, nil
	}
	a.history.mu.RUnlock()
	rows, err := a.db.Query(`SELECT id,name,description,event_time,status,round1_target,round2_target,created_at FROM events WHERE status='completed' ORDER BY event_time DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, round1, round2 int64
		var name, description, eventTime, status, createdAt string
		if err := rows.Scan(&id, &name, &description, &eventTime, &status, &round1, &round2, &createdAt); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": id, "name": name, "description": description, "event_time": eventTime, "status": status, "round1_target": round1, "round2_target": round2, "created_at": createdAt})
	}
	a.history.mu.Lock()
	a.history.items = items
	a.history.loadedAt = time.Now()
	a.history.mu.Unlock()
	return items, nil
}

func (a *App) invalidateHistoryCache() {
	a.history.mu.Lock()
	a.history.items = nil
	a.history.loadedAt = time.Time{}
	a.history.mu.Unlock()
}

func (a *App) createEvent(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil || actor.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以创建赛事")
		return
	}
	var req createEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "赛事信息不完整")
		return
	}
	if len(req.PlayerIDs) != 0 && (len(req.PlayerIDs) < 6 || len(req.PlayerIDs) > 12) {
		fail(c, http.StatusBadRequest, "赛事总人数必须在6到12人之间")
		return
	}
	if strings.TrimSpace(req.EventTime) == "" {
		fail(c, http.StatusBadRequest, "请设置比赛开始时间")
		return
	}
	if req.Round1Target == 0 {
		req.Round1Target = 4
	}
	if req.Round2Target == 0 {
		req.Round2Target = 5
	}
	minTarget := len(req.PlayerIDs) / 2
	if req.Round1Target < minTarget || req.Round2Target < minTarget {
		fail(c, http.StatusBadRequest, fmt.Sprintf("抢分目标不能小于%d", minTarget))
		return
	}
	if req.ManagerID == 0 {
		req.ManagerID = actor.ID
	} else {
		var managerRole string
		if err := a.db.QueryRow(`SELECT role FROM users WHERE id=? AND status='active'`, req.ManagerID).Scan(&managerRole); err != nil || !isNonSuperAdmin(managerRole) {
			fail(c, http.StatusBadRequest, "赛事管理员账号无效")
			return
		}
	}
	if strings.TrimSpace(req.TeamAName) == "" {
		req.TeamAName = "赤队"
	}
	if strings.TrimSpace(req.TeamBName) == "" {
		req.TeamBName = "蓝队"
	}

	transaction, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer transaction.Rollback()
	result, err := transaction.Exec(`INSERT INTO events(name, description, event_time, status, round1_target, round2_target, manager_id, created_by, kof_initial_a, kof_initial_b) VALUES(?,?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.EventTime, statusDraft, req.Round1Target, req.Round2Target, req.ManagerID, actor.ID, nullableInt64(req.KOFInitialA), nullableInt64(req.KOFInitialB))
	if err != nil {
		fail(c, http.StatusBadRequest, "创建赛事失败")
		return
	}
	eventID, _ := result.LastInsertId()
	teamAResult, err := transaction.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, req.TeamAName, "#ef8354")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	teamA, _ := teamAResult.LastInsertId()
	teamBResult, err := transaction.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, req.TeamBName, "#3578e5")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	teamB, _ := teamBResult.LastInsertId()
	if len(req.PlayerIDs) > 0 {
		if err := assignPlayers(transaction, eventID, teamA, teamB, req.PlayerIDs, req.FixedAssignments, a.rng); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, round := range []struct {
		stage  string
		target int
	}{{stageRound1, req.Round1Target}, {stageRound2, req.Round2Target}, {stageAce, 1}} {
		if _, err := transaction.Exec(`INSERT INTO rounds(event_id,stage,target_score) VALUES(?,?,?)`, eventID, round.stage, round.target); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := transaction.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "event_created", gin.H{"name": req.Name})
	ok(c, gin.H{"id": eventID})
}

func (a *App) updateEventSettings(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var req eventSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "赛事设置格式不正确")
		return
	}
	var scoreA, scoreB int
	if err := a.db.QueryRow(`SELECT COALESCE(SUM(score_a),0),COALESCE(SUM(score_b),0) FROM rounds WHERE event_id=?`, eventID).Scan(&scoreA, &scoreB); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if scoreA != 0 || scoreB != 0 {
		fail(c, http.StatusBadRequest, "已有比分后不能修改随机规则")
		return
	}
	if req.KOFInitialA != 0 {
		var exists int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players ep JOIN teams t ON t.id=ep.team_id WHERE ep.event_id=? AND t.id=(SELECT id FROM teams WHERE event_id=? ORDER BY id LIMIT 1) AND ep.user_id=?`, eventID, eventID, req.KOFInitialA).Scan(&exists); err != nil || exists == 0 {
			fail(c, http.StatusBadRequest, "KOF A队首发不属于该队")
			return
		}
	}
	if req.KOFInitialB != 0 {
		var exists int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players ep JOIN teams t ON t.id=ep.team_id WHERE ep.event_id=? AND t.id=(SELECT id FROM teams WHERE event_id=? ORDER BY id LIMIT 1 OFFSET 1) AND ep.user_id=?`, eventID, eventID, req.KOFInitialB).Scan(&exists); err != nil || exists == 0 {
			fail(c, http.StatusBadRequest, "KOF B队首发不属于该队")
			return
		}
	}
	if _, err := a.db.Exec(`UPDATE events SET round1_no_repeat=?,round1_all_first=?,kof_initial_a=?,kof_initial_b=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, boolInt(req.Round1NoRepeat), boolInt(req.Round1AllFirst), nullableInt64(req.KOFInitialA), nullableInt64(req.KOFInitialB), eventID); err != nil {
		fail(c, http.StatusInternalServerError, "保存赛事设置失败")
		return
	}
	a.logAction(eventID, actor.ID, "event_settings_updated", req)
	ok(c, gin.H{"ok": true})
}

func (a *App) confirmEvent(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM events WHERE id=?`, eventID).Scan(&status); err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	if status != statusFinalConfirm {
		fail(c, http.StatusBadRequest, "当前赛事还不能确认结束")
		return
	}
	if _, err := a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusCompleted, eventID); err != nil {
		fail(c, http.StatusInternalServerError, "确认赛事结束失败")
		return
	}
	a.logAction(eventID, actor.ID, "event_completed", nil)
	a.invalidateHistoryCache()
	ok(c, gin.H{"ok": true})
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func assignPlayers(tx *sql.Tx, eventID, teamA, teamB int64, ids []int64, fixed map[string]string, rng *mathrand.Rand) error {
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return errors.New("参赛选手不能重复")
		}
		seen[id] = true
	}
	var placeholders []string
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id IN (`+strings.Join(placeholders, ",")+`) AND role<>'superadmin' AND status='active'`, args...).Scan(&count); err != nil || count != len(ids) {
		return errors.New("参赛选手列表中存在无效账号")
	}
	remaining := append([]int64(nil), ids...)
	rng.Shuffle(len(remaining), func(i, j int) { remaining[i], remaining[j] = remaining[j], remaining[i] })
	assignments := map[int64]int64{}
	for _, id := range ids {
		switch strings.ToUpper(strings.TrimSpace(fixed[strconv.FormatInt(id, 10)])) {
		case "A":
			assignments[id] = teamA
		case "B":
			assignments[id] = teamB
		}
	}
	countA, countB := 0, 0
	for _, team := range assignments {
		if team == teamA {
			countA++
		} else {
			countB++
		}
	}
	for _, id := range remaining {
		if _, fixed := assignments[id]; fixed {
			continue
		}
		if countA <= countB {
			assignments[id] = teamA
			countA++
		} else {
			assignments[id] = teamB
			countB++
		}
	}
	for _, id := range ids {
		if _, err := tx.Exec(`INSERT INTO event_players(event_id,user_id,team_id) VALUES(?,?,?)`, eventID, id, assignments[id]); err != nil {
			return err
		}
	}
	return nil
}
