package main

import (
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
)

func (a *App) runMatchMutation(eventID, actorID int64, work func(*matchEngine) error) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	engine := &matchEngine{db: tx, eventID: eventID, actorID: actorID, rng: a.rng}
	s, err := engine.status()
	if err != nil {
		return err
	}
	if s == statusCompleted || s == statusCancelled {
		return errors.New("已结束赛事不能编辑")
	}
	if err := work(engine); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *App) startEvent(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	if err := a.runMatchMutation(id, actor.ID, func(e *matchEngine) error { return e.start() }); err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) submitResult(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	matchID, err := parseID(c.Param("matchID"))
	if err != nil {
		fail(c, 400, "对局编号无效")
		return
	}
	var req resultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "请选择胜者")
		return
	}
	err = a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		m, err := e.match(matchID)
		if err != nil {
			return errors.New("对局不存在")
		}
		if err := e.record(m, req.WinnerTeamID, req.Note); err != nil {
			return err
		}
		// 兼容管理员同时提交下一位选手，队长也可以随后单独选择。
		if req.NextPlayerID != 0 && m.stage == stageRound2 {
			m, err = e.match(matchID)
			if err != nil {
				return err
			}
			r, err := e.round(stageRound2)
			if err != nil {
				return err
			}
			if r.status != statusFinished && m.next == 0 {
				_, team := m.loser()
				lost, err := e.lost(m.roundID, team, req.NextPlayerID)
				if err != nil {
					return err
				}
				if lost {
					return errors.New("请选择本队尚未输过的选手")
				}
				return e.chooseKOF(m, req.NextPlayerID)
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
func (a *App) correctResult(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	matchID, err := parseID(c.Param("matchID"))
	if err != nil {
		fail(c, 400, "对局编号无效")
		return
	}
	var req resultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "请选择胜者")
		return
	}
	err = a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		m, err := e.match(matchID)
		if err != nil {
			return errors.New("对局不存在")
		}
		return e.correct(m, req.WinnerTeamID, req.Note)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) selectKOFPlayer(c *gin.Context) {
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
	var req struct {
		MatchID  int64 `json:"match_id"`
		PlayerID int64 `json:"player_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.PlayerID <= 0 {
		fail(c, 400, "请选择出场选手")
		return
	}
	err = a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		status, err := e.status()
		if err != nil {
			return err
		}
		if status != stageRound2 {
			return errors.New("当前不是KOF换人阶段")
		}
		m, err := e.match(req.MatchID)
		if err != nil || m.stage != stageRound2 || m.status != statusFinished {
			return errors.New("KOF对局尚未录入结果")
		}
		_, team := m.loser()
		if !a.canManageEvent(actor.ID, actor.Role, id) {
			var captain int
			if err := e.db.QueryRow(`SELECT is_captain FROM event_players WHERE event_id=? AND team_id=? AND user_id=?`, id, team, actor.ID).Scan(&captain); err != nil || captain != 1 {
				return errors.New("只有败方队长或赛事管理员可以换人")
			}
		}
		random, err := e.kofRandom(m.roundID, team)
		if err != nil {
			return err
		}
		if random {
			return errors.New("全员已输过，系统将随机选择")
		}
		lost, err := e.lost(m.roundID, team, req.PlayerID)
		if err != nil {
			return err
		}
		if lost {
			return errors.New("请选择本队尚未输过的选手")
		}
		if err := e.chooseKOF(m, req.PlayerID); err != nil {
			return err
		}
		return e.log("kof_player_selected", req)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) confirmAce(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	var req aceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "请选择双方大将")
		return
	}
	err := a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		s, err := e.status()
		if err != nil {
			return err
		}
		if s != stageAce {
			return errors.New("当前不是大将战阶段")
		}
		teams, err := e.teams()
		if err != nil {
			return err
		}
		idsA, err := e.players(teams[0])
		if err != nil {
			return err
		}
		idsB, err := e.players(teams[1])
		if err != nil {
			return err
		}
		if !containsID(idsA, req.PlayerAID) || !containsID(idsB, req.PlayerBID) {
			return errors.New("大将必须属于对应队伍")
		}
		r, err := e.round(stageAce)
		if err != nil {
			return err
		}
		var matchID int64
		err = e.db.QueryRow(`SELECT id FROM matches WHERE round_id=? AND status='pending'`, r.id).Scan(&matchID)
		if err == sql.ErrNoRows {
			if err := e.insertMatch(stageAce, req.PlayerAID, req.PlayerBID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if _, err := e.db.Exec(`UPDATE matches SET player_a_id=?,player_b_id=? WHERE id=?`, req.PlayerAID, req.PlayerBID, matchID); err != nil {
				return err
			}
		}
		return e.log("ace_players_overridden", req)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
func (a *App) cancelEvent(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	err := a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		if err := e.setStage(statusCancelled); err != nil {
			return err
		}
		return e.log("event_cancelled", nil)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
