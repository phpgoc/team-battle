package main

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *App) startEvent(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM events WHERE id=?`, eventID).Scan(&status); err != nil {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	if status != statusDraft && status != statusCaptainVote && status != statusLineup {
		fail(c, http.StatusBadRequest, "当前阶段不能开始首轮")
		return
	}
	if err := a.generateLineupForEvent(eventID, actor.ID); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := a.db.Exec(`UPDATE rounds SET status=? WHERE event_id=? AND stage=?`, statusActive, eventID, stageRound1); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusRound1, eventID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.createNextRound1Match(eventID, actor.ID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "round1_started", nil)
	ok(c, gin.H{"ok": true})
}

func (a *App) submitResult(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	matchID, err := parseID(c.Param("matchID"))
	if err != nil {
		fail(c, http.StatusBadRequest, "对局编号无效")
		return
	}
	var req resultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择胜者")
		return
	}
	var matchEvent, roundID, teamA, teamB, playerA, playerB int64
	var stage, matchStatus string
	if err := a.db.QueryRow(`SELECT m.event_id,m.round_id,m.team_a_id,m.team_b_id,m.player_a_id,m.player_b_id,m.status,r.stage FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.id=?`, matchID).Scan(&matchEvent, &roundID, &teamA, &teamB, &playerA, &playerB, &matchStatus, &stage); err != nil || matchEvent != eventID {
		fail(c, http.StatusNotFound, "对局不存在")
		return
	}
	if matchStatus != statusPending {
		fail(c, http.StatusBadRequest, "该对局已经录入结果")
		return
	}
	if req.WinnerTeamID != teamA && req.WinnerTeamID != teamB {
		fail(c, http.StatusBadRequest, "胜者队伍无效")
		return
	}
	if stage == stageRound2 {
		loserTeam := teamB
		if req.WinnerTeamID == teamB {
			loserTeam = teamA
		}
		canRandom, err := a.kofRandomAllowed(eventID, roundID, loserTeam)
		if err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if req.NextPlayerID == 0 && !canRandom {
			fail(c, http.StatusBadRequest, "KOF必须选择败方下一位选手")
			return
		}
		if req.NextPlayerID != 0 {
			var exists int
			if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND user_id=? AND active=1`, eventID, loserTeam, req.NextPlayerID).Scan(&exists); err != nil || exists == 0 {
				fail(c, http.StatusBadRequest, "KOF下一位选手不属于败方队伍")
				return
			}
			if !canRandom {
				var lost int
				if err := a.kofPlayerLost(eventID, roundID, loserTeam, req.NextPlayerID, &lost); err != nil {
					fail(c, http.StatusInternalServerError, err.Error())
					return
				}
				if lost != 0 {
					fail(c, http.StatusBadRequest, "KOF下一位选手必须是尚未输过的选手")
					return
				}
			}
		}
	}
	if _, err := a.db.Exec(`UPDATE matches SET winner_team_id=?, next_player_id=?, status=?, note=?, created_by=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`, req.WinnerTeamID, nullableInt64(req.NextPlayerID), statusFinished, req.Note, actor.ID, matchID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	var scoreA, scoreB, target int
	if err := a.db.QueryRow(`SELECT score_a,score_b,target_score FROM rounds WHERE id=?`, roundID).Scan(&scoreA, &scoreB, &target); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if req.WinnerTeamID == teamA {
		scoreA++
	} else {
		scoreB++
	}
	if _, err := a.db.Exec(`UPDATE rounds SET score_a=?,score_b=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, scoreA, scoreB, roundID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "match_result_submitted", gin.H{"match_id": matchID, "winner_team_id": req.WinnerTeamID, "stage": stage})
	if scoreA >= target || scoreB >= target {
		winner := teamA
		if scoreB >= target {
			winner = teamB
		}
		if _, err := a.db.Exec(`UPDATE rounds SET winner_team_id=?,status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, winner, statusFinished, roundID); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.advanceAfterRound(eventID, stage, winner, actor.ID); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else if stage == stageRound1 {
		if err := a.createNextRound1Match(eventID, actor.ID); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else if stage == stageRound2 {
		if err := a.createNextKOFMatch(eventID, roundID, matchID, actor.ID); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) correctResult(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	matchID, err := parseID(c.Param("matchID"))
	if err != nil {
		fail(c, http.StatusBadRequest, "对局编号无效")
		return
	}
	var req resultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择胜者")
		return
	}
	var matchEvent, roundID, teamA, teamB int64
	var stage, matchStatus, eventStatus string
	if err := a.db.QueryRow(`SELECT m.event_id,m.round_id,m.team_a_id,m.team_b_id,r.stage,m.status,e.status FROM matches m JOIN rounds r ON r.id=m.round_id JOIN events e ON e.id=m.event_id WHERE m.id=?`, matchID).Scan(&matchEvent, &roundID, &teamA, &teamB, &stage, &matchStatus, &eventStatus); err != nil || matchEvent != eventID {
		fail(c, http.StatusNotFound, "对局不存在")
		return
	}
	if eventStatus != statusFinalConfirm || stage != stageRound2 || matchStatus != statusFinished {
		fail(c, http.StatusBadRequest, "当前对局不能修改")
		return
	}
	if req.WinnerTeamID != teamA && req.WinnerTeamID != teamB {
		fail(c, http.StatusBadRequest, "胜者队伍无效")
		return
	}
	if _, err := a.db.Exec(`UPDATE matches SET winner_team_id=?,note=? WHERE id=?`, req.WinnerTeamID, req.Note, matchID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	var scoreA, scoreB, target int
	if err := a.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN winner_team_id=team_a_id THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN winner_team_id=team_b_id THEN 1 ELSE 0 END),0),target_score FROM matches m JOIN rounds r ON r.id=m.round_id WHERE r.id=?`, roundID).Scan(&scoreA, &scoreB, &target); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := a.db.Exec(`UPDATE rounds SET score_a=?,score_b=?,winner_team_id=?,status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, scoreA, scoreB, req.WinnerTeamID, statusFinished, roundID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	var round1Winner int64
	_ = a.db.QueryRow(`SELECT winner_team_id FROM rounds WHERE event_id=? AND stage=?`, eventID, stageRound1).Scan(&round1Winner)
	if req.WinnerTeamID == round1Winner && (scoreA >= target || scoreB >= target) {
		_, _ = a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusFinalConfirm, eventID)
	} else {
		_, _ = a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusAce, eventID)
		_, _ = a.db.Exec(`UPDATE rounds SET status=? WHERE event_id=? AND stage=?`, statusActive, eventID, stageAce)
	}
	a.logAction(eventID, actor.ID, "match_result_corrected", gin.H{"match_id": matchID, "winner_team_id": req.WinnerTeamID})
	ok(c, gin.H{"ok": true})
}

func (a *App) selectKOFPlayer(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var body struct {
		MatchID  int64 `json:"match_id" binding:"required"`
		PlayerID int64 `json:"player_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "请选择选手")
		return
	}
	var matchEvent, teamA, teamB int64
	var winner sql.NullInt64
	if err := a.db.QueryRow(`SELECT event_id,team_a_id,team_b_id,winner_team_id FROM matches WHERE id=?`, body.MatchID).Scan(&matchEvent, &teamA, &teamB, &winner); err != nil || matchEvent != eventID {
		fail(c, http.StatusNotFound, "对局不存在")
		return
	}
	if winner.Valid {
		loserTeam := teamB
		if winner.Int64 == teamB {
			loserTeam = teamA
		}
		var count int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND user_id=?`, eventID, loserTeam, body.PlayerID).Scan(&count); err != nil || count == 0 {
			fail(c, http.StatusBadRequest, "选手不属于败方队伍")
			return
		}
	}
	if _, err := a.db.Exec(`UPDATE matches SET next_player_id=? WHERE id=? AND status=?`, body.PlayerID, body.MatchID, statusFinished); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "kof_player_selected", body)
	ok(c, gin.H{"ok": true})
}

func (a *App) confirmAce(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	var body aceRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "请选择大将战选手")
		return
	}
	var roundID, teamA, teamB int64
	if err := a.db.QueryRow(`SELECT id, (SELECT id FROM teams WHERE event_id=? ORDER BY id LIMIT 1), (SELECT id FROM teams WHERE event_id=? ORDER BY id LIMIT 1 OFFSET 1) FROM rounds WHERE event_id=? AND stage=?`, eventID, eventID, eventID, stageAce).Scan(&roundID, &teamA, &teamB); err != nil {
		fail(c, http.StatusBadRequest, "大将战尚未开放")
		return
	}
	if !a.isEventPlayer(eventID, teamA, body.PlayerAID) || !a.isEventPlayer(eventID, teamB, body.PlayerBID) {
		fail(c, http.StatusBadRequest, "大将战选手不属于对应队伍")
		return
	}
	var exists int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=?`, roundID).Scan(&exists); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if exists == 0 {
		_, err := a.db.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,status,created_by) VALUES(?,?,?,?,?,?,?, ?,?)`, eventID, roundID, 1, teamA, teamB, body.PlayerAID, body.PlayerBID, statusPending, actor.ID)
		if err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) cancelEvent(c *gin.Context) {
	actor, eventID, okAccess := a.managerEvent(c)
	if !okAccess {
		return
	}
	if _, err := a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status NOT IN (?,?)`, statusCancelled, eventID, statusCompleted, statusCancelled); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.logAction(eventID, actor.ID, "event_cancelled", nil)
	ok(c, gin.H{"ok": true})
}

func (a *App) generateLineupForEvent(eventID, actorID int64) error {
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=?`, eventID).Scan(&total); err != nil {
		return err
	}
	limit := total / 2
	teams, err := a.teamIDs(eventID)
	if err != nil {
		return err
	}
	for _, teamID := range teams {
		var teamSize int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=?`, eventID, teamID).Scan(&teamSize); err != nil {
			return err
		}
		if teamSize < limit {
			limit = teamSize
		}
	}
	for _, teamID := range teams {
		rows, err := a.db.Query(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? ORDER BY RANDOM() LIMIT ?`, eventID, teamID, limit)
		if err != nil {
			return err
		}
		playerIDs := []int64{}
		for rows.Next() {
			var playerID int64
			if err := rows.Scan(&playerID); err != nil {
				rows.Close()
				return err
			}
			playerIDs = append(playerIDs, playerID)
		}
		rows.Close()
		for position, playerID := range playerIDs {
			if _, err := a.db.Exec(`INSERT INTO lineup_entries(event_id,team_id,position,player_id,source) VALUES(?,?,?,?,?) ON CONFLICT(event_id,team_id,position) DO UPDATE SET player_id=excluded.player_id,source=excluded.source`, eventID, teamID, position+1, playerID, "random"); err != nil {
				return err
			}
		}
	}
	a.logAction(eventID, actorID, "lineup_generated", gin.H{"manual_slots": limit})
	return nil
}

func (a *App) createNextRound1Match(eventID, actorID int64) error {
	roundID, target, scoreA, scoreB, status, err := a.roundInfo(eventID, stageRound1)
	if err != nil || status != statusActive {
		return err
	}
	var matchCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=?`, roundID).Scan(&matchCount); err != nil {
		return err
	}
	if scoreA >= target || scoreB >= target || matchCount >= target*2-1 {
		return nil
	}
	teams, err := a.teamIDs(eventID)
	if err != nil {
		return err
	}
	previousA, previousB := int64(0), int64(0)
	if matchCount > 0 {
		_ = a.db.QueryRow(`SELECT player_a_id,player_b_id FROM matches WHERE round_id=? ORDER BY seq DESC LIMIT 1`, roundID).Scan(&previousA, &previousB)
	}
	playerA, err := a.nextRound1Player(eventID, teams[0], matchCount+1, previousA)
	if err != nil {
		return err
	}
	playerB, err := a.nextRound1Player(eventID, teams[1], matchCount+1, previousB)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,status,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, eventID, roundID, matchCount+1, teams[0], teams[1], playerA, playerB, statusPending, actorID)
	return err
}

func (a *App) nextRound1Player(eventID, teamID int64, position int, previous int64) (int64, error) {
	var playerID int64
	if err := a.db.QueryRow(`SELECT player_id FROM lineup_entries WHERE event_id=? AND team_id=? AND position=?`, eventID, teamID, position).Scan(&playerID); err == nil {
		return playerID, nil
	}
	rows, err := a.db.Query(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND active=1 AND user_id<>? ORDER BY RANDOM()`, eventID, teamID, previous)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if rows.Next() && rows.Scan(&playerID) == nil {
		return playerID, nil
	}
	if err := a.db.QueryRow(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND active=1 ORDER BY RANDOM() LIMIT 1`, eventID, teamID).Scan(&playerID); err != nil {
		return 0, err
	}
	return playerID, nil
}

func (a *App) createNextKOFMatch(eventID, roundID, previousMatchID, actorID int64) error {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=?`, roundID).Scan(&count); err != nil {
		return err
	}
	var previousWinner, teamA, teamB, playerA, playerB, nextPlayer sql.NullInt64
	if err := a.db.QueryRow(`SELECT winner_team_id,team_a_id,team_b_id,player_a_id,player_b_id,next_player_id FROM matches WHERE id=?`, previousMatchID).Scan(&previousWinner, &teamA, &teamB, &playerA, &playerB, &nextPlayer); err != nil {
		return err
	}
	if !previousWinner.Valid || !teamA.Valid || !teamB.Valid {
		return errors.New("KOF上一场结果不完整")
	}
	currentPlayer := playerA.Int64
	loserTeam := teamB.Int64
	if previousWinner.Int64 == teamB.Int64 {
		currentPlayer = playerB.Int64
		loserTeam = teamA.Int64
	}
	nextPlayerID := int64(0)
	if nextPlayer.Valid {
		nextPlayerID = nextPlayer.Int64
	}
	if nextPlayerID == 0 {
		var err error
		nextPlayerID, err = a.nextKOFPlayer(eventID, roundID, loserTeam, currentPlayer)
		if err != nil {
			return err
		}
	}
	teamAID, teamBID := teamA.Int64, teamB.Int64
	playerAID, playerBID := playerA.Int64, playerB.Int64
	if loserTeam == teamAID {
		playerAID = nextPlayerID
		playerBID = currentPlayer
	} else {
		playerAID = currentPlayer
		playerBID = nextPlayerID
	}
	_, err := a.db.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,status,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, eventID, roundID, count+1, teamAID, teamBID, playerAID, playerBID, statusPending, actorID)
	return err
}

func (a *App) nextKOFPlayer(eventID, roundID, teamID, currentPlayer int64) (int64, error) {
	rows, err := a.db.Query(`SELECT ep.user_id FROM event_players ep WHERE ep.event_id=? AND ep.team_id=? AND ep.active=1 AND ep.user_id<>? AND NOT EXISTS (SELECT 1 FROM matches m WHERE m.round_id=? AND m.status='finished' AND m.winner_team_id<>? AND ((m.team_a_id=? AND m.player_a_id=ep.user_id) OR (m.team_b_id=? AND m.player_b_id=ep.user_id))) ORDER BY RANDOM()`, eventID, teamID, currentPlayer, roundID, teamID, teamID, teamID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var id int64
	if rows.Next() && rows.Scan(&id) == nil {
		return id, nil
	}
	if err := a.db.QueryRow(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND active=1 AND user_id<>? ORDER BY RANDOM() LIMIT 1`, eventID, teamID, currentPlayer).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (a *App) kofRandomAllowed(eventID, roundID, teamID int64) (bool, error) {
	var target, players, lost int
	if err := a.db.QueryRow(`SELECT target_score FROM rounds WHERE id=?`, roundID).Scan(&target); err != nil {
		return false, err
	}
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND active=1`, eventID, teamID).Scan(&players); err != nil {
		return false, err
	}
	if err := a.db.QueryRow(`SELECT COUNT(DISTINCT ep.user_id) FROM event_players ep JOIN matches m ON m.round_id=? AND m.status='finished' AND m.winner_team_id<>? AND ((m.team_a_id=? AND m.player_a_id=ep.user_id) OR (m.team_b_id=? AND m.player_b_id=ep.user_id)) WHERE ep.event_id=? AND ep.team_id=? AND ep.active=1`, roundID, teamID, teamID, teamID, eventID, teamID).Scan(&lost); err != nil {
		return false, err
	}
	return target > players && players > 0 && lost >= players, nil
}

func (a *App) kofPlayerLost(eventID, roundID, teamID, playerID int64, lost *int) error {
	return a.db.QueryRow(`SELECT COUNT(*) FROM matches m WHERE m.round_id=? AND m.status='finished' AND m.winner_team_id<>? AND ((m.team_a_id=? AND m.player_a_id=?) OR (m.team_b_id=? AND m.player_b_id=?))`, roundID, teamID, teamID, playerID, teamID, playerID).Scan(lost)
}

func (a *App) advanceAfterRound(eventID int64, stage string, winner, actorID int64) error {
	if stage == stageRound1 {
		if _, err := a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusRound2, eventID); err != nil {
			return err
		}
		if _, err := a.db.Exec(`UPDATE rounds SET status=? WHERE event_id=? AND stage=?`, statusActive, eventID, stageRound2); err != nil {
			return err
		}
		if err := a.createInitialKOFMatch(eventID, actorID); err != nil {
			return err
		}
		a.logAction(eventID, actorID, "round1_finished", gin.H{"winner_team_id": winner})
		return nil
	}
	if stage == stageRound2 {
		var round1Winner int64
		if err := a.db.QueryRow(`SELECT winner_team_id FROM rounds WHERE event_id=? AND stage=?`, eventID, stageRound1).Scan(&round1Winner); err != nil {
			return err
		}
		if round1Winner == winner {
			if _, err := a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusFinalConfirm, eventID); err != nil {
				return err
			}
			a.logAction(eventID, actorID, "event_ready_for_confirmation", gin.H{"winner_team_id": winner})
			return nil
		}
		if _, err := a.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, statusAce, eventID); err != nil {
			return err
		}
		if _, err := a.db.Exec(`UPDATE rounds SET status=? WHERE event_id=? AND stage=?`, statusActive, eventID, stageAce); err != nil {
			return err
		}
		a.logAction(eventID, actorID, "ace_opened", gin.H{"round1_winner": round1Winner, "round2_winner": winner})
	}
	return nil
}

func (a *App) createInitialKOFMatch(eventID, actorID int64) error {
	roundID, _, _, _, _, err := a.roundInfo(eventID, stageRound2)
	if err != nil {
		return err
	}
	teams, err := a.teamIDs(eventID)
	if err != nil {
		return err
	}
	var initialA, initialB sql.NullInt64
	if err := a.db.QueryRow(`SELECT kof_initial_a,kof_initial_b FROM events WHERE id=?`, eventID).Scan(&initialA, &initialB); err != nil {
		return err
	}
	playerA := int64(0)
	if initialA.Valid {
		playerA = initialA.Int64
	}
	if playerA == 0 {
		playerA, err = a.randomPlayer(eventID, teams[0], 0)
	}
	if err != nil {
		return err
	}
	playerB := int64(0)
	if initialB.Valid {
		playerB = initialB.Int64
	}
	if playerB == 0 {
		playerB, err = a.randomPlayer(eventID, teams[1], 0)
	}
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,status,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, eventID, roundID, 1, teams[0], teams[1], playerA, playerB, statusPending, actorID)
	return err
}

func (a *App) randomPlayer(eventID, teamID, except int64) (int64, error) {
	var id int64
	query := `SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND active=1`
	args := []any{eventID, teamID}
	if except != 0 {
		query += ` AND user_id<>?`
		args = append(args, except)
	}
	query += ` ORDER BY RANDOM() LIMIT 1`
	if err := a.db.QueryRow(query, args...).Scan(&id); err != nil && except != 0 {
		if err := a.db.QueryRow(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND active=1 ORDER BY RANDOM() LIMIT 1`, eventID, teamID).Scan(&id); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	return id, nil
}
