package main

import (
	"errors"
	"github.com/gin-gonic/gin"
)

func preMatch(status string) bool {
	return status == statusDraft || status == statusCaptainVote || status == statusLineup
}
func (a *App) castCaptainVote(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, 401, "未登录")
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, 400, "赛事编号无效")
		return
	}
	var req struct {
		TargetID int64 `json:"target_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "请选择队长候选人")
		return
	}
	err = a.runMatchMutation(id, user.ID, func(e *matchEngine) error {
		status, err := e.status()
		if err != nil {
			return err
		}
		var team int64
		if err := e.db.QueryRow(`SELECT team_id FROM event_players WHERE event_id=? AND user_id=? AND active=1`, id, user.ID).Scan(&team); err != nil {
			return errors.New("你不是参赛选手")
		}
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		if !containsID(ids, req.TargetID) {
			return errors.New("只能选本队选手")
		}
		var captains int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND team_id=? AND is_captain=1 AND active=1`, id, team).Scan(&captains); err != nil {
			return err
		}
		if captains != 0 {
			return errors.New("本队已有队长，不能继续投票")
		}
		if _, err := e.db.Exec(`INSERT INTO captain_votes(event_id,team_id,voter_id,target_id) VALUES(?,?,?,?) ON CONFLICT(event_id,voter_id) DO UPDATE SET target_id=excluded.target_id`, id, team, user.ID, req.TargetID); err != nil {
			return err
		}
		if err := e.log("captain_vote_cast", req); err != nil {
			return err
		}
		// 达到本队人数的一半（奇数向上取整）立即确定队长。
		var votes int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM captain_votes WHERE event_id=? AND team_id=? AND target_id=?`, id, team, req.TargetID).Scan(&votes); err != nil {
			return err
		}
		threshold := (len(ids) + 1) / 2
		if votes < threshold {
			return nil
		}
		if _, err := e.db.Exec(`UPDATE event_players SET is_captain=CASE WHEN team_id=? AND user_id=? THEN 1 ELSE is_captain END WHERE event_id=? AND team_id=?`, team, req.TargetID, id, team); err != nil {
			return err
		}
		if err := e.log("captain_auto_confirmed", map[string]any{"team_id": team, "player_id": req.TargetID, "votes": votes, "threshold": threshold}); err != nil {
			return err
		}
		if preMatch(status) {
			var captainCount int
			if err := e.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND is_captain=1 AND active=1`, id).Scan(&captainCount); err != nil {
				return err
			}
			if captainCount == 2 {
				return e.setStage(statusLineup)
			}
		}
		return nil
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) confirmCaptains(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	var req struct {
		A int64 `json:"team_a_player_id"`
		B int64 `json:"team_b_player_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "请选择两队队长")
		return
	}
	err := a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		s, err := e.status()
		if err != nil {
			return err
		}
		teams, err := e.teams()
		if err != nil {
			return err
		}
		for i, player := range []int64{req.A, req.B} {
			ids, err := e.players(teams[i])
			if err != nil {
				return err
			}
			if player != 0 && !containsID(ids, player) {
				return errors.New("队长必须属于对应队伍")
			}
		}
		if _, err := e.db.Exec(`UPDATE event_players SET is_captain=CASE WHEN user_id IN (?,?) THEN 1 ELSE 0 END WHERE event_id=?`, req.A, req.B, id); err != nil {
			return err
		}
		if preMatch(s) {
			if err := e.setStage(statusLineup); err != nil {
				return err
			}
		}
		return e.log("captains_confirmed", req)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) generateLineup(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	err := a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		if err := e.fillLineups(true); err != nil {
			return err
		}
		return e.log("lineup_generated", nil)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) submitLineup(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil {
		fail(c, 401, "未登录")
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, 400, "赛事编号无效")
		return
	}
	var req lineupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "排阵格式不正确")
		return
	}
	err = a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		s, err := e.status()
		if err != nil {
			return err
		}
		if !preMatch(s) {
			return errors.New("当前阶段不能排阵")
		}
		if !a.canManageEvent(actor.ID, actor.Role, id) {
			var captain int
			if err := e.db.QueryRow(`SELECT is_captain FROM event_players WHERE event_id=? AND team_id=? AND user_id=?`, id, req.TeamID, actor.ID).Scan(&captain); err != nil || captain != 1 {
				return errors.New("只有本队队长或赛事管理员可以排阵")
			}
		}
		rules, err := e.rules()
		if err != nil {
			return err
		}
		if rules.Round1Mode == "random" && len(req.PlayerIDs) > 0 {
			return errors.New("该赛事首轮为全随机")
		}
		ids, err := e.players(req.TeamID)
		if err != nil || len(ids) == 0 {
			return errors.New("队伍不存在或没有选手")
		}
		var total int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=?`, id).Scan(&total); err != nil {
			return err
		}
		if len(req.PlayerIDs) > min(len(ids), total/2) {
			return errors.New("手动排阵人数超过限制")
		}
		seen := map[int64]bool{}
		for _, player := range req.PlayerIDs {
			if !containsID(ids, player) || seen[player] {
				return errors.New("排阵选手无效或重复")
			}
			seen[player] = true
		}
		if _, err := e.db.Exec(`DELETE FROM lineup_entries WHERE event_id=? AND team_id=?`, id, req.TeamID); err != nil {
			return err
		}
		source := "captain"
		if a.canManageEvent(actor.ID, actor.Role, id) {
			source = "manager"
		}
		for i, player := range req.PlayerIDs {
			if _, err := e.db.Exec(`INSERT INTO lineup_entries(event_id,team_id,position,player_id,source) VALUES(?,?,?,?,?)`, id, req.TeamID, i+1, player, source); err != nil {
				return err
			}
		}
		return e.log("lineup_submitted", req)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
