package app

// 赛事创建、参赛分配和首页历史缓存。

import (
	"database/sql"
	"encoding/json"
	"errors"
	mathrand "math/rand"
	"net/http"
	"sort"
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
	items, err := a.publicEventSummaries(false)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取赛事失败")
		return
	}
	ok(c, gin.H{"events": items})
}

func (a *App) publicHistory(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	playerID := int64(0)
	if raw := strings.TrimSpace(c.Query("player_id")); raw != "" {
		var err error
		playerID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || playerID < 0 {
			fail(c, http.StatusBadRequest, "选手编号无效")
			return
		}
	}
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
	participants := map[int64]bool{}
	for _, item := range items {
		for _, id := range item["player_ids"].([]int64) {
			participants[id] = true
		}
		entered := playerID == 0
		if playerID != 0 {
			for _, id := range item["player_ids"].([]int64) {
				if id == playerID {
					entered = true
					break
				}
			}
		}
		if !entered {
			continue
		}
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
	directory, err := a.userDirectory()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取选手失败")
		return
	}
	// 筛选候选来自完整历史，使用内存目录补充公开姓名，不暴露登录名。
	playerIDs := make([]int64, 0, len(participants))
	for id := range participants {
		playerIDs = append(playerIDs, id)
	}
	sort.Slice(playerIDs, func(i, j int) bool { return playerIDs[i] < playerIDs[j] })
	players := make([]gin.H, 0, len(playerIDs))
	for _, id := range playerIDs {
		if user, exists := directory[id]; exists {
			players = append(players, gin.H{"id": id, "display_name": user.DisplayName, "category_name": user.CategoryName})
		}
	}
	ok(c, gin.H{"events": filtered[start:end], "players": players, "page": page, "page_size": pageSize, "total": total})
}

func (a *App) historyItems() ([]map[string]any, error) {
	a.history.mu.RLock()
	if !a.history.loadedAt.IsZero() && time.Since(a.history.loadedAt) < 30*time.Second {
		items := append([]map[string]any(nil), a.history.items...)
		a.history.mu.RUnlock()
		return items, nil
	}
	a.history.mu.RUnlock()
	items, err := a.publicEventSummaries(true)
	if err != nil {
		return nil, err
	}
	// 批量读取历史参赛ID并随摘要缓存，筛选时无需逐场查询或关联账号表。
	rows, err := a.db.Query(`SELECT p.event_id,p.user_id FROM event_players p JOIN events e ON e.id=p.event_id WHERE e.status='completed' AND p.active=1 ORDER BY p.event_id,p.user_id`)
	if err != nil {
		return nil, err
	}
	participants := map[int64][]int64{}
	for rows.Next() {
		var eventID, userID int64
		if err := rows.Scan(&eventID, &userID); err != nil {
			rows.Close()
			return nil, err
		}
		participants[eventID] = append(participants[eventID], userID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, item := range items {
		ids := participants[item["id"].(int64)]
		if ids == nil {
			ids = []int64{}
		}
		item["player_ids"] = ids
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
	if valid, err := backgroundExists(a.db, req.BackgroundID); err != nil || !valid {
		fail(c, http.StatusBadRequest, "背景不存在")
		return
	}
	for _, group := range rules.AceSpecials {
		for _, id := range group.allPlayerIDs() {
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
	result, err := transaction.Exec(`INSERT INTO events(name, description, event_time, status, round1_target, round2_target, manager_id, created_by, rules_json, background_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.EventTime, statusDraft, req.Round1Target, req.Round2Target, req.ManagerID, actor.ID, string(rulesJSON), nullableInt64(req.BackgroundID))
	if err != nil {
		fail(c, http.StatusBadRequest, "创建赛事失败")
		return
	}
	eventID, _ := result.LastInsertId()
	_, err = transaction.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "A 队", "#ef8354")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = transaction.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, "B 队", "#3578e5")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if len(req.PlayerIDs) > 0 {
		if err := saveSetupRoster(transaction, eventID, req.PlayerIDs, req.FixedAssignments); err != nil {
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
