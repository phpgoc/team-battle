package main

// 赛事创建、参赛分配和首页历史缓存。

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	if !a.history.loadedAt.IsZero() && time.Since(a.history.loadedAt) < 30*time.Second {
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
	if len(req.PlayerIDs) > 12 {
		fail(c, http.StatusBadRequest, "赛事最多12人")
		return
	}
	eventTime, err := normalizeEventTime(req.EventTime)
	if err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, http.StatusBadRequest, "请填写赛事名称和有效的比赛开始时间")
		return
	}
	req.EventTime = eventTime
	if req.Round1Target == 0 {
		req.Round1Target = 4
	}
	if req.Round2Target == 0 {
		req.Round2Target = 5
	}
	if req.Round1Target < 1 || req.Round2Target < 1 || req.Round1Target > 99 || req.Round2Target > 99 {
		fail(c, http.StatusBadRequest, "抢分目标必须在1到99之间")
		return
	}
	rules := defaultRules()
	if req.Rules != nil {
		rules = *req.Rules
	}
	if err := rules.validate(); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	for _, group := range rules.AceSpecials {
		for _, id := range group.PlayerIDs {
			found := false
			for _, player := range req.PlayerIDs {
				if player == id {
					found = true
				}
			}
			if !found {
				fail(c, http.StatusBadRequest, "大将战特殊候选必须是参赛选手")
				return
			}
		}
	}
	rulesJSON, _ := json.Marshal(rules)
	if req.ManagerID == 0 {
		req.ManagerID = actor.ID
	} else {
		var managerRole string
		if err := a.db.QueryRow(`SELECT role FROM users WHERE id=? AND status='active' AND is_deleted=0`, req.ManagerID).Scan(&managerRole); err != nil || !isNonSuperAdmin(managerRole) {
			fail(c, http.StatusBadRequest, "赛事管理员账号无效")
			return
		}
	}

	transaction, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer transaction.Rollback()
	result, err := transaction.Exec(`INSERT INTO events(name, description, event_time, status, round1_target, round2_target, manager_id, created_by, rules_json) VALUES(?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.EventTime, statusDraft, req.Round1Target, req.Round2Target, req.ManagerID, actor.ID, string(rulesJSON))
	if err != nil {
		fail(c, http.StatusBadRequest, "创建赛事失败")
		return
	}
	eventID, _ := result.LastInsertId()
	teamAResult, err := transaction.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "A 队", "#ef8354")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	teamA, _ := teamAResult.LastInsertId()
	teamBResult, err := transaction.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "B 队", "#3578e5")
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
	actor, eventID, access := a.managerEvent(c)
	if !access {
		return
	}
	var req eventSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "赛事设置格式不正确")
		return
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE event_id=? AND status='finished'`, eventID).Scan(&count); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if count > 0 {
		fail(c, 400, "已有比分后不能修改赛事设置")
		return
	}
	rules := defaultRules()
	if req.Rules != nil {
		rules = *req.Rules
	}
	if err := rules.validate(); err != nil {
		fail(c, 400, err.Error())
		return
	}
	for _, group := range rules.AceSpecials {
		for _, player := range group.PlayerIDs {
			var exists int
			if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=?`, eventID, player).Scan(&exists); err != nil || exists == 0 {
				fail(c, 400, "特殊候选必须是参赛选手")
				return
			}
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	defer tx.Rollback()
	raw, _ := json.Marshal(rules)
	if _, err := tx.Exec(`UPDATE events SET rules_json=?,round1_no_repeat=?,round1_all_first=?,kof_initial_a=NULL,kof_initial_b=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(raw), boolInt(rules.NoRepeat), boolInt(rules.AllFirst), eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if req.Name != "" || req.EventTime != "" {
		eventTime, err := normalizeEventTime(req.EventTime)
		if err != nil || strings.TrimSpace(req.Name) == "" || req.Round1Target < 1 || req.Round2Target < 1 || req.Round1Target > 99 || req.Round2Target > 99 {
			fail(c, 400, "赛事名称、开始时间或抢分目标无效")
			return
		}
		if _, err := tx.Exec(`UPDATE events SET name=?,description=?,event_time=?,round1_target=?,round2_target=? WHERE id=?`, strings.TrimSpace(req.Name), req.Description, eventTime, req.Round1Target, req.Round2Target, eventID); err != nil {
			fail(c, 500, err.Error())
			return
		}
		if _, err := tx.Exec(`UPDATE rounds SET target_score=CASE stage WHEN 'round1' THEN ? WHEN 'round2' THEN ? ELSE 1 END WHERE event_id=?`, req.Round1Target, req.Round2Target, eventID); err != nil {
			fail(c, 500, err.Error())
			return
		}
	}
	// 尚未录分的抽签随设置一起失效，返回排阵阶段重新确认。
	if _, err := tx.Exec(`DELETE FROM player_selections WHERE event_id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`DELETE FROM matches WHERE event_id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`DELETE FROM lineup_entries WHERE event_id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE rounds SET status='pending' WHERE event_id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE events SET status='draft' WHERE id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, err.Error())
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
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id IN (`+strings.Join(placeholders, ",")+`) AND role<>'superadmin' AND status='active' AND is_deleted=0`, args...).Scan(&count); err != nil || count != len(ids) {
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
