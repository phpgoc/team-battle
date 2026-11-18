package app

import (
	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
)

// 过去赛事与实时赛事共用对局表，一次事务导入后立即封存。
func (a *App) archiveEvent(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil || actor.Role != roleSuperAdmin {
		fail(c, 403, "只有超级管理员可以录入过去赛事")
		return
	}
	var req archiveEventRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, 400, "请输入赛事名称")
		return
	}
	eventTime, err := normalizeEventTime(req.EventTime)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	if len(req.PlayerIDs) < 6 || len(req.PlayerIDs) > 12 || len(req.Matches) == 0 {
		fail(c, 400, "过去赛事需要6到12名选手及完整对局结果")
		return
	}
	if valid, err := backgroundExists(a.db, req.BackgroundID); err != nil || !valid {
		fail(c, 400, "背景不存在")
		return
	}
	scores := [3][2]int{}
	grouped := [3][]archivedMatch{}
	for _, m := range req.Matches {
		if m.Round < 1 || m.Round > 3 || (m.WhoWin != "a" && m.WhoWin != "b") {
			fail(c, 400, "对局轮次或胜者无效")
			return
		}
		grouped[m.Round-1] = append(grouped[m.Round-1], m)
		side := 0
		if m.WhoWin == "b" {
			side = 1
		}
		scores[m.Round-1][side]++
	}
	for i := 0; i < 2; i++ {
		if scores[i][0] == scores[i][1] {
			fail(c, 400, "前两轮都需要完整比分和明确胜者")
			return
		}
	}
	winnerA := scores[0][0] > scores[0][1]
	winnerB := scores[1][0] > scores[1][1]
	if (winnerA != winnerB && len(grouped[2]) != 1) || (winnerA == winnerB && len(grouped[2]) != 0) {
		fail(c, 400, "前两轮平局时需要一场大将战，否则不应录入大将战")
		return
	}
	aceSource := strings.TrimSpace(req.AceSource)
	if len(grouped[2]) == 1 {
		if aceSource == "" {
			aceSource = "random"
		}
		if !validAceSource(aceSource) {
			fail(c, 400, "大将战抽选来源无效")
			return
		}
	}
	for i, matches := range grouped {
		target := max(scores[i][0], scores[i][1])
		current := [2]int{}
		for seq, m := range matches {
			side := 0
			if m.WhoWin == "b" {
				side = 1
			}
			current[side]++
			if current[side] >= target && seq != len(matches)-1 {
				fail(c, 400, "阶段结束后不应继续录入对局")
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
	r, err := tx.Exec(`INSERT INTO events(name,description,event_time,status,round1_target,round2_target,manager_id,created_by,background_id) VALUES(?,?,?,'completed',?,?,?,?,?)`, strings.TrimSpace(req.Name), req.Description, eventTime, max(scores[0][0], scores[0][1]), max(scores[1][0], scores[1][1]), actor.ID, actor.ID, nullableInt64(req.BackgroundID))
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	id, _ := r.LastInsertId()
	teams := []int64{}
	for _, name := range []string{"A 队", "B 队"} {
		result, err := tx.Exec(`INSERT INTO teams(event_id,name) VALUES(?,?)`, id, name)
		if err != nil {
			fail(c, 500, err.Error())
			return
		}
		team, _ := result.LastInsertId()
		teams = append(teams, team)
	}
	if err := saveSetupRoster(tx, id, req.PlayerIDs, req.FixedAssignments); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if err := assignPlayers(tx, id, teams[0], teams[1], req.PlayerIDs, req.FixedAssignments, a.rng); err != nil {
		fail(c, 400, err.Error())
		return
	}
	for i, stage := range []string{stageRound1, stageRound2, stageAce} {
		winner := int64(0)
		if scores[i][0] > scores[i][1] {
			winner = teams[0]
		}
		if scores[i][1] > scores[i][0] {
			winner = teams[1]
		}
		target := max(1, max(scores[i][0], scores[i][1]))
		status := statusFinished
		if len(grouped[i]) == 0 {
			status = statusPending
		}
		result, err := tx.Exec(`INSERT INTO rounds(event_id,stage,target_score,score_a,score_b,winner_team_id,status) VALUES(?,?,?,?,?,?,?)`, id, stage, target, scores[i][0], scores[i][1], nullableInt64(winner), status)
		if err != nil {
			fail(c, 500, err.Error())
			return
		}
		round, _ := result.LastInsertId()
		for seq, m := range grouped[i] {
			for side, player := range []int64{m.AID, m.BID} {
				var exists int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND user_id=?`, id, teams[side], player).Scan(&exists); err != nil || exists != 1 {
					fail(c, 400, "对局选手必须属于对应队伍")
					return
				}
			}
			matchWinner := teams[0]
			if m.WhoWin == "b" {
				matchWinner = teams[1]
			}
			if _, err := tx.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,winner_team_id,status,created_by,finished_at) VALUES(?,?,?,?,?,?,?,?,'finished',?,CURRENT_TIMESTAMP)`, id, round, seq+1, teams[0], teams[1], m.AID, m.BID, matchWinner, actor.ID); err != nil {
				fail(c, 500, err.Error())
				return
			}
		}
	}
	engine := &matchEngine{db: tx, eventID: id, actorID: actor.ID, rng: a.rng}
	if err := engine.log("historical_event_created", map[string]any{"matches": len(req.Matches)}); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if len(grouped[2]) == 1 {
		if err := engine.log("ace_drawn", map[string]any{"source": aceSource, "rules": []string{aceSource, aceSource}}); err != nil {
			fail(c, 500, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(c, 500, err.Error())
		return
	}
	a.invalidateHistoryCache()
	ok(c, gin.H{"id": id})
}

func validAceSource(source string) bool {
	if source == "random" || source == "most_wins" || source == "most_losses" {
		return true
	}
	if !strings.HasPrefix(source, "special:") {
		return false
	}
	value, err := strconv.Atoi(strings.TrimPrefix(source, "special:"))
	return err == nil && value >= 0
}
