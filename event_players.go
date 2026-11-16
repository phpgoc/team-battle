package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *App) addEventPlayers(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var req addEventPlayersRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.PlayerIDs) == 0 {
		fail(c, http.StatusBadRequest, "请选择要加入赛事的账号")
		return
	}
	teams, err := a.teamIDs(eventID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	teamID := req.TeamID
	if teamID != 0 && teamID != teams[0] && teamID != teams[1] {
		fail(c, http.StatusBadRequest, "队伍不存在")
		return
	}
	for _, playerID := range req.PlayerIDs {
		var role string
		if err := a.db.QueryRow(`SELECT role FROM users WHERE id=? AND status='active'`, playerID).Scan(&role); err != nil || !isNonSuperAdmin(role) {
			fail(c, http.StatusBadRequest, "参赛账号无效")
			return
		}
		var exists int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=?`, eventID, playerID).Scan(&exists); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if exists > 0 {
			continue
		}
		if teamID == 0 {
			var countA, countB int
			if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=?`, eventID, teams[0]).Scan(&countA); err != nil {
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
			if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=?`, eventID, teams[1]).Scan(&countB); err != nil {
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
			teamID = teams[0]
			if countB < countA {
				teamID = teams[1]
			}
		}
		if _, err := a.db.Exec(`INSERT INTO event_players(event_id,user_id,team_id) VALUES(?,?,?)`, eventID, playerID, teamID); err != nil {
			fail(c, http.StatusBadRequest, "加入参赛选手失败")
			return
		}
	}
	a.logAction(eventID, actor.ID, "event_players_added", req)
	ok(c, gin.H{"ok": true})
}
