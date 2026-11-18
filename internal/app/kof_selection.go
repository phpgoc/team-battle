package app

import (
	"database/sql"
	"errors"

	"github.com/gin-gonic/gin"
)

// 没有队长时只能本人报名；有队长时由队长派人，管理员可以覆盖。
func (e *matchEngine) authorizeKOFSelection(team, player int64, manager bool) error {
	ids, err := e.players(team)
	if err != nil {
		return err
	}
	if !containsID(ids, player) {
		return errors.New("请选择本队可出场的选手")
	}
	if manager {
		return nil
	}
	if !containsID(ids, e.actorID) {
		return errors.New("只能操作本队出场位置")
	}
	var captain int64
	err = e.db.QueryRow(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND is_captain=1 AND active=1 LIMIT 1`, e.eventID, team).Scan(&captain)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if captain == 0 {
		if player != e.actorID {
			return errors.New("没有队长时只能为自己报名")
		}
	} else if captain != e.actorID {
		return errors.New("本队有队长，请由队长派人")
	}
	return nil
}

func (e *matchEngine) reserveKOF(matchID, team, player int64, manager bool) error {
	stage, err := e.status()
	if err != nil {
		return err
	}
	if stage != stageRound2 {
		return errors.New("当前不是KOF阶段")
	}
	m, err := e.match(matchID)
	if err != nil || m.stage != stageRound2 {
		return errors.New("KOF对局不存在")
	}
	var latest int64
	if err := e.db.QueryRow(`SELECT id FROM matches WHERE round_id=? ORDER BY seq DESC LIMIT 1`, m.roundID).Scan(&latest); err != nil {
		return err
	}
	if latest != m.id {
		return errors.New("比赛已推进，请刷新后重新报名")
	}
	if m.status == statusFinished {
		_, loser := m.loser()
		if team == 0 {
			team = loser
		}
		if team != loser {
			return errors.New("只有败方队伍需要派下一名选手")
		}
	} else if team == 0 {
		if err := e.db.QueryRow(`SELECT team_id FROM event_players WHERE event_id=? AND user_id=? AND active=1`, e.eventID, e.actorID).Scan(&team); err != nil {
			return errors.New("请选择队伍")
		}
	}
	if team != m.teamA && team != m.teamB {
		return errors.New("队伍不存在")
	}
	if err := e.authorizeKOFSelection(team, player, manager); err != nil {
		return err
	}
	if m.status == statusPending && (player == m.playerA || player == m.playerB) {
		return errors.New("正在参赛的选手不能报名下一位置")
	}
	lost, err := e.lost(m.roundID, team, player)
	if err != nil {
		return err
	}
	if lost {
		return errors.New("已在KOF输过的选手不能报名")
	}
	if m.status == statusFinished {
		if err := e.chooseKOF(m, player); err != nil {
			return err
		}
		return e.log("kof_player_selected", map[string]any{"match_id": matchID, "player_id": player})
	}
	var selected int64
	err = e.db.QueryRow(`SELECT player_id FROM kof_reservations WHERE match_id=? AND team_id=?`, m.id, team).Scan(&selected)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if selected != 0 && !manager {
		return errors.New("下一位置已经有人报名或已由队长安排")
	}
	if _, err := e.db.Exec(`INSERT INTO kof_reservations(event_id,match_id,team_id,player_id) VALUES(?,?,?,?) ON CONFLICT(match_id,team_id) DO UPDATE SET player_id=excluded.player_id`, e.eventID, m.id, team, player); err != nil {
		return err
	}
	return e.log("kof_next_reserved", map[string]any{"match_id": matchID, "team_id": team, "player_id": player})
}

// 候选身份仅本人可见；本队和管理员只获得占位状态，预报名不进入公开资料或 SSE。
func (a *App) kofOptions(c *gin.Context) {
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
	engine := &matchEngine{db: a.db, eventID: id, actorID: actor.ID}
	stage, err := engine.status()
	if err != nil {
		fail(c, 404, "赛事不存在")
		return
	}
	manager := a.canManageEvent(actor.ID, actor.Role, id)
	var membership int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=? AND active=1`, id, actor.ID).Scan(&membership); err != nil {
		fail(c, 500, "读取队伍失败")
		return
	}
	if !manager && membership == 0 {
		fail(c, 403, "无权查看报名")
		return
	}
	c.Header("Cache-Control", "no-store")
	choices := []map[string]any{}
	if stage != stageRound2 {
		ok(c, gin.H{"teams": choices})
		return
	}
	var matchID int64
	err = a.db.QueryRow(`SELECT id FROM matches WHERE event_id=? AND round_no=2 ORDER BY seq DESC LIMIT 1`, id).Scan(&matchID)
	if err != nil && err != sql.ErrNoRows {
		fail(c, 500, "读取对局失败")
		return
	}
	var current engineMatch
	if matchID != 0 {
		current, err = engine.match(matchID)
		if err != nil {
			fail(c, 500, "读取对局失败")
			return
		}
	}
	teams, err := engine.teams()
	if err != nil {
		fail(c, 500, "读取队伍失败")
		return
	}
	for _, team := range teams {
		ids, err := engine.players(team)
		if err != nil {
			fail(c, 500, "读取选手失败")
			return
		}
		if !manager && !containsID(ids, actor.ID) {
			continue
		}
		var captain int64
		err = a.db.QueryRow(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND is_captain=1 AND active=1 LIMIT 1`, id, team).Scan(&captain)
		if err != nil && err != sql.ErrNoRows {
			fail(c, 500, "读取队长失败")
			return
		}
		selected := int64(0)
		if matchID == 0 {
			err = a.db.QueryRow(`SELECT player_id FROM player_selections WHERE event_id=? AND stage='round2' AND team_id=?`, id, team).Scan(&selected)
		} else {
			err = a.db.QueryRow(`SELECT player_id FROM kof_reservations WHERE match_id=? AND team_id=?`, matchID, team).Scan(&selected)
		}
		if err != nil && err != sql.ErrNoRows {
			fail(c, 500, "读取报名失败")
			return
		}
		candidates := []int64{}
		for _, player := range ids {
			if current.status == statusFinished {
				_, loser := current.loser()
				if team != loser {
					break
				}
			}
			if current.status == statusPending && (player == current.playerA || player == current.playerB) {
				continue
			}
			if matchID != 0 {
				lost, err := engine.lost(current.roundID, team, player)
				if err != nil {
					fail(c, 500, "读取选手战绩失败")
					return
				}
				if lost {
					continue
				}
			}
			candidates = append(candidates, player)
		}
		occupied := selected != 0
		if selected != actor.ID {
			selected = 0
		}
		choices = append(choices, map[string]any{"team_id": team, "captain_id": captain, "player_id": selected, "occupied": occupied, "candidate_ids": candidates})
	}
	ok(c, gin.H{"teams": choices, "match_id": matchID})
}
