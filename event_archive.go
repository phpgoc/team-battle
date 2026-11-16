package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// archiveEvent 将已经结束的线下赛事写入历史记录，不开放实时操作。
func (a *App) archiveEvent(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil || actor.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以录入历史赛事")
		return
	}
	var req archiveEventRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.EventTime) == "" {
		fail(c, http.StatusBadRequest, "赛事名和比赛开始时间不能为空")
		return
	}
	if len(req.PlayerIDs) != 0 && (len(req.PlayerIDs) < 6 || len(req.PlayerIDs) > 12) {
		fail(c, http.StatusBadRequest, "历史赛事选手人数必须为6到12人")
		return
	}
	if req.Round1ScoreA < 0 || req.Round1ScoreB < 0 || req.Round2ScoreA < 0 || req.Round2ScoreB < 0 {
		fail(c, http.StatusBadRequest, "比分不能为负数")
		return
	}
	if req.Winner != "A" && req.Winner != "B" {
		fail(c, http.StatusBadRequest, "请选择最终获胜队伍")
		return
	}
	if strings.TrimSpace(req.TeamAName) == "" {
		req.TeamAName = "赤队"
	}
	if strings.TrimSpace(req.TeamBName) == "" {
		req.TeamBName = "蓝队"
	}

	tx, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO events(name,description,event_time,status,round1_target,round2_target,manager_id,created_by) VALUES(?,?,?,?,?,?,?,?)`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.EventTime, statusCompleted, 1, 1, actor.ID, actor.ID)
	if err != nil {
		fail(c, http.StatusBadRequest, "历史赛事创建失败")
		return
	}
	eventID, err := result.LastInsertId()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	aResult, err := tx.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, req.TeamAName, "#ef8354")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	teamA, _ := aResult.LastInsertId()
	bResult, err := tx.Exec(`INSERT INTO teams(event_id,name,color) VALUES(?,?,?)`, eventID, req.TeamBName, "#3578e5")
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	teamB, _ := bResult.LastInsertId()
	if len(req.PlayerIDs) > 0 {
		if err := assignPlayers(tx, eventID, teamA, teamB, req.PlayerIDs, nil, a.rng); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, round := range []struct {
		stage  string
		a, b   int
		winner int64
	}{
		{stageRound1, req.Round1ScoreA, req.Round1ScoreB, 0},
		{stageRound2, req.Round2ScoreA, req.Round2ScoreB, 0},
		{stageAce, 0, 0, 0},
	} {
		winner := round.winner
		if round.stage != stageAce && round.a > round.b {
			winner = teamA
		} else if round.stage != stageAce && round.b > round.a {
			winner = teamB
		}
		if _, err := tx.Exec(`INSERT INTO rounds(event_id,stage,target_score,score_a,score_b,winner_team_id,status) VALUES(?,?,?,?,?,?,?)`, eventID, round.stage, 1, round.a, round.b, nullableInt64(winner), statusFinished); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := tx.Exec(`UPDATE events SET updated_at=CURRENT_TIMESTAMP WHERE id=?`, eventID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "historical_event_created", gin.H{"winner": req.Winner})
	a.invalidateHistoryCache()
	ok(c, gin.H{"id": eventID})
}
