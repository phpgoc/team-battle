package main

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
		if err := requireEditableSettings(e.db, eventID); err != nil {
			return err
		}
		tx := e.db.(*sql.Tx)
		rules, err := e.rules()
		if err != nil {
			return err
		}
		if req.Rules != nil {
			rules = *req.Rules
		}
		if err := rules.validate(); err != nil {
			return err
		}
		if req.PlayerIDs != nil {
			changed, err := eventRosterChanged(tx, eventID, *req.PlayerIDs, req.FixedAssignments)
			if err != nil {
				return err
			}
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
			if err != nil || strings.TrimSpace(req.Name) == "" || req.Round1Target < 1 || req.Round2Target < 1 || req.Round1Target > 99 || req.Round2Target > 99 {
				return errors.New("赛事名称、开始时间或抢分目标无效")
			}
			if _, err := tx.Exec(`UPDATE events SET name=?,description=?,event_time=?,round1_target=?,round2_target=? WHERE id=?`, strings.TrimSpace(req.Name), req.Description, eventTime, req.Round1Target, req.Round2Target, eventID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE rounds SET target_score=CASE stage WHEN 'round1' THEN ? WHEN 'round2' THEN ? ELSE 1 END WHERE event_id=?`, req.Round1Target, req.Round2Target, eventID); err != nil {
				return err
			}
		}
		if err := saveRules(tx, eventID, rules); err != nil {
			return err
		}
		for _, table := range []string{"kof_reservations", "player_selections", "matches", "lineup_entries"} {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE event_id=?`, eventID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE rounds SET status='pending' WHERE event_id=?`, eventID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE events SET status='draft',round1_no_repeat=?,round1_all_first=?,kof_initial_a=NULL,kof_initial_b=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, boolInt(rules.NoRepeat), boolInt(rules.AllFirst), eventID); err != nil {
			return err
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
