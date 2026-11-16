package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *App) castCaptainVote(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil || user.Role != rolePlayer {
		fail(c, http.StatusForbidden, "只有参赛选手可以投票")
		return
	}
	eventID, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	var body struct {
		TargetID int64 `json:"target_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "请选择队长候选人")
		return
	}
	var teamID, targetTeamID int64
	if err := a.db.QueryRow(`SELECT team_id FROM event_players WHERE event_id=? AND user_id=? AND active=1`, eventID, user.ID).Scan(&teamID); err != nil {
		fail(c, http.StatusForbidden, "你不是该赛事选手")
		return
	}
	if err := a.db.QueryRow(`SELECT team_id FROM event_players WHERE event_id=? AND user_id=? AND active=1`, eventID, body.TargetID).Scan(&targetTeamID); err != nil || targetTeamID != teamID {
		fail(c, http.StatusBadRequest, "只能投票给本队选手")
		return
	}
	if _, err := a.db.Exec(`INSERT INTO captain_votes(event_id,team_id,voter_id,target_id) VALUES(?,?,?,?) ON CONFLICT(event_id,voter_id) DO UPDATE SET target_id=excluded.target_id, created_at=CURRENT_TIMESTAMP`, eventID, teamID, user.ID, body.TargetID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, user.ID, "captain_vote_cast", gin.H{"team_id": teamID, "target_id": body.TargetID})
	ok(c, gin.H{"ok": true})
}

func (a *App) confirmCaptains(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var body struct {
		TeamAPlayerID int64 `json:"team_a_player_id" binding:"required"`
		TeamBPlayerID int64 `json:"team_b_player_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "请选择两队队长")
		return
	}
	transaction, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec(`UPDATE event_players SET is_captain=0 WHERE event_id=?`, eventID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	for _, playerID := range []int64{body.TeamAPlayerID, body.TeamBPlayerID} {
		if _, err := transaction.Exec(`UPDATE event_players SET is_captain=1 WHERE event_id=? AND user_id=?`, eventID, playerID); err != nil {
			fail(c, http.StatusBadRequest, "队长不属于该赛事")
			return
		}
	}
	if _, err := transaction.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusLineup, eventID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := transaction.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "captains_confirmed", body)
	ok(c, gin.H{"ok": true})
}

func (a *App) generateLineup(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	if err := a.generateLineupForEvent(eventID, actor.ID); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) submitLineup(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	eventID, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM events WHERE id=?`, eventID).Scan(&status); err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	if status == statusCompleted || status == statusCancelled || status == statusRound1 || status == statusRound2 || status == statusAce {
		fail(c, http.StatusBadRequest, "当前阶段不能修改排阵")
		return
	}
	managerAccess := (actor.Role == roleSuperAdmin || actor.Role == roleManager) && a.canManageEvent(actor.ID, actor.Role, eventID)
	var body lineupRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "排阵格式不正确")
		return
	}
	var totalPlayers int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=?`, eventID).Scan(&totalPlayers); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	var teamSize int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=?`, eventID, body.TeamID).Scan(&teamSize); err != nil {
		fail(c, http.StatusBadRequest, "队伍不存在")
		return
	}
	if !managerAccess {
		if actor.Role != rolePlayer {
			fail(c, http.StatusForbidden, "无权提交排阵")
			return
		}
		var captain int
		if err := a.db.QueryRow(`SELECT is_captain FROM event_players WHERE event_id=? AND team_id=? AND user_id=? AND active=1`, eventID, body.TeamID, actor.ID).Scan(&captain); err != nil || captain != 1 {
			fail(c, http.StatusForbidden, "只有本队队长可以提交排阵")
			return
		}
	}
	limit := totalPlayers / 2
	if teamSize < limit {
		limit = teamSize
	}
	if len(body.PlayerIDs) > limit {
		fail(c, http.StatusBadRequest, fmt.Sprintf("该队最多排前%d名选手", limit))
		return
	}
	for i, playerID := range body.PlayerIDs {
		var exists int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND user_id=?`, eventID, body.TeamID, playerID).Scan(&exists); err != nil || exists == 0 {
			fail(c, http.StatusBadRequest, "排阵选手不属于该队")
			return
		}
		_, _ = a.db.Exec(`INSERT INTO lineup_entries(event_id,team_id,position,player_id,source) VALUES(?,?,?,?,?) ON CONFLICT(event_id,team_id,position) DO UPDATE SET player_id=excluded.player_id, source=excluded.source`, eventID, body.TeamID, i+1, playerID, "captain")
	}
	a.logAction(eventID, actor.ID, "lineup_submitted", body)
	ok(c, gin.H{"ok": true, "limit": limit})
}
