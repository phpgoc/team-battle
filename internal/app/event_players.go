package app

import (
	"github.com/gin-gonic/gin"
	"sort"
)

// 分组抽签前可以补充选手并调整固定队伍，保存不执行抽签。
func (a *App) addEventPlayers(c *gin.Context) {
	actor, eventID, access := a.managerEvent(c)
	if !access {
		return
	}
	var req createEventRequest
	// 阵容请求无需名称，单独绑定以免触发创建赛事的必填校验。
	var body struct {
		PlayerIDs        []int64           `json:"player_ids"`
		FixedAssignments map[string]string `json:"fixed_assignments"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.PlayerIDs) > 12 {
		fail(c, 400, "参赛列表无效，最多12人")
		return
	}
	req.PlayerIDs = body.PlayerIDs
	req.FixedAssignments = body.FixedAssignments
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if err := requireEditableSettings(tx, eventID); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if grouped, err := eventHasGrouping(tx, eventID); err != nil || grouped {
		fail(c, 400, "分组抽签后不能修改参赛阵容")
		return
	}
	for _, table := range []string{"kof_reservations", "matches", "captain_votes", "lineup_entries", "player_selections", "event_players"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE event_id=?`, eventID); err != nil {
			fail(c, 500, err.Error())
			return
		}
	}
	if err := saveSetupRoster(tx, eventID, req.PlayerIDs, req.FixedAssignments); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE events SET status='draft',kof_initial_a=NULL,kof_initial_b=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE rounds SET status='pending' WHERE event_id=?`, eventID); err != nil {
		fail(c, 500, err.Error())
		return
	}
	var raw string
	if err := tx.QueryRow(`SELECT rules_json FROM events WHERE id=?`, eventID).Scan(&raw); err != nil {
		fail(c, 500, err.Error())
		return
	}
	rules, err := decodeRules(raw)
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	selected := map[int64]bool{}
	for _, id := range req.PlayerIDs {
		selected[id] = true
	}
	groups := []AceSpecial{}
	for _, group := range rules.AceSpecials {
		ids := []int64{}
		for _, id := range group.PlayerIDs {
			if selected[id] {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			group.PlayerIDs = ids
			groups = append(groups, group)
		}
	}
	rules.AceSpecials = groups
	if err := saveRules(tx, eventID, rules); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "event_roster_updated", body)
	ok(c, gin.H{"ok": true})
}

func (a *App) eligibleEventUsers(c *gin.Context) {
	_, _, access := a.managerEvent(c)
	if !access {
		return
	}
	users, err := a.activePlayers()
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"users": users})
}

func (a *App) activePlayers() ([]User, error) {
	directory, err := a.userDirectory()
	if err != nil {
		return nil, err
	}
	users := []User{}
	for _, u := range directory {
		if isNonSuperAdmin(u.Role) && u.Status == "active" && !u.IsDeleted {
			users = append(users, u)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users, nil
}
