package app

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 正式开始即锁定设置，不能通过删除或尚未录入比分绕过。
func requireEditableSettings(db sqlRunner, eventID int64) error {
	var stage string
	var scored int
	if err := db.QueryRow(`SELECT status,(SELECT COUNT(*) FROM matches WHERE event_id=events.id AND status='finished') FROM events WHERE id=?`, eventID).Scan(&stage, &scored); err != nil {
		return err
	}
	if !preMatch(stage) || scored != 0 {
		return errors.New("赛事已正式开始，不能修改赛事设置或参赛阵容")
	}
	return nil
}

// eventHasGrouping 表示赛事是否已经完成过一次选手分队抽签。
// 分队完成后，第一轮和KOF的规则必须保持不变；大将战规则仍可在首场开赛前调整。
func eventHasGrouping(db sqlRunner, eventID int64) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND active=1`, eventID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func roundSetupChanged(before, after EventRules) bool {
	return before.Round1Mode != after.Round1Mode || before.KOFStarter != after.KOFStarter || before.NoRepeat != after.NoRepeat
}

func (a *App) updateEventSettings(c *gin.Context) {
	actor, eventID, access := a.managerEvent(c)
	if !access {
		return
	}
	var req eventSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "赛事设置格式不正确")
		return
	}
	if req.ManagerID != nil && actor.Role != roleSuperAdmin {
		fail(c, 403, "只有超级管理员可以更换赛事管理员")
		return
	}
	err := a.runMatchMutation(eventID, actor.ID, func(e *matchEngine) error {
		tx := e.db.(*sql.Tx)
		beforeRules, err := e.rules()
		if err != nil {
			return err
		}
		// 所有设置共用一个事务，进行中仅接受抽签时长和赛事背景。
		if req.BackgroundID != nil {
			valid, err := backgroundExists(tx, *req.BackgroundID)
			if err != nil || *req.BackgroundID < 0 || !valid {
				return errors.New("背景不存在")
			}
			if _, err := tx.Exec(`UPDATE events SET background_id=? WHERE id=?`, nullableInt64(*req.BackgroundID), eventID); err != nil {
				return err
			}
		}
		if err := requireEditableSettings(e.db, eventID); err != nil {
			if req.Name != "" || req.EventTime != "" || req.Description != "" || req.ManagerID != nil || req.PlayerIDs != nil || req.Rules != nil || req.Round1Target != 0 || req.Round2Target != 0 {
				return err
			}
			rules := beforeRules
			if req.RandomDrawSeconds != nil {
				rules.RandomDrawSeconds = *req.RandomDrawSeconds
			}
			if req.AceDrawSeconds != nil {
				rules.AceDrawSeconds = *req.AceDrawSeconds
			}
			if err := rules.validate(); err != nil {
				return err
			}
			if err := saveRules(tx, eventID, rules); err != nil {
				return err
			}
			return e.log("event_settings_updated", req)
		}
		var beforeRound1Target, beforeRound2Target int
		if err := tx.QueryRow(`SELECT round1_target,round2_target FROM events WHERE id=?`, eventID).Scan(&beforeRound1Target, &beforeRound2Target); err != nil {
			return err
		}
		grouped, err := eventHasGrouping(tx, eventID)
		if err != nil {
			return err
		}
		rules := beforeRules
		if req.Rules != nil {
			rules = *req.Rules
		}
		if req.RandomDrawSeconds != nil {
			rules.RandomDrawSeconds = *req.RandomDrawSeconds
		}
		if req.AceDrawSeconds != nil {
			rules.AceDrawSeconds = *req.AceDrawSeconds
		}
		if err := rules.validate(); err != nil {
			return err
		}
		if grouped && ((req.Round1Target != 0 && req.Round1Target != beforeRound1Target) || (req.Round2Target != 0 && req.Round2Target != beforeRound2Target) || roundSetupChanged(beforeRules, rules)) {
			return errors.New("分组抽签后不能修改第一轮和KOF设置")
		}
		round1Target, round2Target := req.Round1Target, req.Round2Target
		if round1Target == 0 {
			round1Target = beforeRound1Target
		}
		if round2Target == 0 {
			round2Target = beforeRound2Target
		}
		rosterChanged := false
		if req.PlayerIDs != nil {
			changed, err := eventRosterChanged(tx, eventID, *req.PlayerIDs, req.FixedAssignments)
			if err != nil {
				return err
			}
			rosterChanged = changed
			if changed {
				teams, err := e.teams()
				if err != nil {
					return err
				}
				for _, table := range []string{"captain_votes", "event_players"} {
					if _, err := tx.Exec(`DELETE FROM `+table+` WHERE event_id=?`, eventID); err != nil {
						return err
					}
				}
				if len(*req.PlayerIDs) > 0 {
					if err := assignPlayers(tx, eventID, teams[0], teams[1], *req.PlayerIDs, req.FixedAssignments, a.rng); err != nil {
						return err
					}
				}
			}
		}
		for _, group := range rules.AceSpecials {
			for _, player := range group.PlayerIDs {
				var exists int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=? AND active=1`, eventID, player).Scan(&exists); err != nil {
					return err
				}
				if exists == 0 {
					return errors.New("特殊候选必须是参赛选手")
				}
			}
		}
		if req.ManagerID != nil {
			manager := *req.ManagerID
			if manager == 0 {
				manager = actor.ID
			}
			var valid int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id=? AND status='active' AND is_deleted=0`, manager).Scan(&valid); err != nil || valid != 1 {
				return errors.New("赛事管理员账号无效")
			}
			if _, err := tx.Exec(`UPDATE events SET manager_id=? WHERE id=?`, manager, eventID); err != nil {
				return err
			}
		}
		if req.Name != "" || req.EventTime != "" {
			eventTime, err := normalizeEventTime(req.EventTime)
			if err != nil || strings.TrimSpace(req.Name) == "" || round1Target < 1 || round2Target < 1 || round1Target > 99 || round2Target > 99 {
				return errors.New("赛事名称、开始时间或抢分目标无效")
			}
			if _, err := tx.Exec(`UPDATE events SET name=?,description=?,event_time=?,round1_target=?,round2_target=? WHERE id=?`, strings.TrimSpace(req.Name), req.Description, eventTime, round1Target, round2Target, eventID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE rounds SET target_score=CASE stage WHEN 'round1' THEN ? WHEN 'round2' THEN ? ELSE 1 END WHERE event_id=?`, round1Target, round2Target, eventID); err != nil {
				return err
			}
		}
		if err := saveRules(tx, eventID, rules); err != nil {
			return err
		}
		// 仅修改大将战规则、赛事资料或管理员时保留当前的队长和排阵阶段。
		// 未分队或重新分队时仍清空依赖旧阵容的流程数据。
		if !grouped || rosterChanged {
			for _, table := range []string{"kof_reservations", "player_selections", "matches", "lineup_entries"} {
				if _, err := tx.Exec(`DELETE FROM `+table+` WHERE event_id=?`, eventID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`UPDATE rounds SET status='pending' WHERE event_id=?`, eventID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE events SET status='draft',round1_no_repeat=?,kof_initial_a=NULL,kof_initial_b=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, boolInt(rules.NoRepeat), eventID); err != nil {
				return err
			}
		}
		return e.log("event_settings_updated", req)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

// 固定分队与当前阵容一致时保留队长及投票；主动随机分队则重新确认。
func eventRosterChanged(db sqlRunner, eventID int64, ids []int64, fixed map[string]string) (bool, error) {
	if len(ids) > 12 {
		return false, errors.New("参赛列表最多12人")
	}
	rows, err := db.Query(`SELECT ep.user_id,CASE WHEN ep.team_id=(SELECT MIN(id) FROM teams WHERE event_id=?) THEN 'A' ELSE 'B' END FROM event_players ep WHERE ep.event_id=? AND ep.active=1`, eventID, eventID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	current := map[int64]string{}
	for rows.Next() {
		var id int64
		var team string
		if err := rows.Scan(&id, &team); err != nil {
			return false, err
		}
		current[id] = team
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	seen := map[int64]bool{}
	changed := len(ids) != len(current)
	for _, id := range ids {
		if seen[id] {
			return false, errors.New("参赛选手不能重复")
		}
		seen[id] = true
		if current[id] == "" || fixed[strconv.FormatInt(id, 10)] != current[id] {
			changed = true
		}
	}
	return changed, nil
}
