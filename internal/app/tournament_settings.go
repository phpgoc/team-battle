package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 正式开始即锁定设置，不能通过删除或尚未录入比分绕过。
func requireEditableSettings(db sqlRunner, tournamentID int64) error {
	var stage string
	var scored int
	if err := db.QueryRow(`SELECT status,(SELECT COUNT(*) FROM matches WHERE tournament_id=tournaments.id AND status='finished') FROM tournaments WHERE id=?`, tournamentID).Scan(&stage, &scored); err != nil {
		return err
	}
	if !preMatch(stage) || scored != 0 {
		return errors.New("赛事已正式开始，不能修改赛事设置或参赛阵容")
	}
	return nil
}

var errTournamentBasicInfoForbidden = errors.New("只有超级管理员可以修改赛事基本信息")
var errTournamentBasicInfoLocked = errors.New("分组抽签开始后不能修改赛事基本信息")

// tournamentHasGrouping 表示赛事是否已经完成过一次选手分队抽签。
// 分队完成后，第一轮和KOF的规则必须保持不变；大将战规则仍可在首场开赛前调整。
func tournamentHasGrouping(db sqlRunner, tournamentID int64) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND active=1`, tournamentID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func tournamentSetupDrawStarted(db sqlRunner, tournamentID int64) (bool, error) {
	var started bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM tournament_players WHERE tournament_id=? AND active=1) OR EXISTS(SELECT 1 FROM tournament_actions WHERE tournament_id=? AND action='tournament_setup_drawn')`, tournamentID, tournamentID).Scan(&started)
	return started, err
}

func roundSetupChanged(before, after TournamentRules) bool {
	return before.Round1Mode != after.Round1Mode ||
		before.KOFStarter != after.KOFStarter ||
		before.NoRepeat != after.NoRepeat ||
		before.KOFManualRevival != after.KOFManualRevival ||
		before.KOFRevivalNoRepeat != after.KOFRevivalNoRepeat ||
		!reflect.DeepEqual(before.ExtraInfo[stageRound1], after.ExtraInfo[stageRound1]) ||
		!reflect.DeepEqual(before.ExtraInfo[stageRound2], after.ExtraInfo[stageRound2])
}

func changedTournamentRuleFields(before, after TournamentRules) map[string]any {
	changed := map[string]any{}
	checks := []struct {
		name   string
		before any
		after  any
	}{
		{"round1_mode", before.Round1Mode, after.Round1Mode},
		{"kof_starter", before.KOFStarter, after.KOFStarter},
		{"no_repeat", before.NoRepeat, after.NoRepeat},
		{"kof_manual_revival", before.KOFManualRevival, after.KOFManualRevival},
		{"kof_revival_no_repeat", before.KOFRevivalNoRepeat, after.KOFRevivalNoRepeat},
		{"random_draw_seconds", before.RandomDrawSeconds, after.RandomDrawSeconds},
		{"ace_draw_seconds", before.AceDrawSeconds, after.AceDrawSeconds},
		{"ace_most_wins", before.AceMostWins, after.AceMostWins},
		{"ace_most_losses", before.AceMostLosses, after.AceMostLosses},
		{"ace_specials", before.AceSpecials, after.AceSpecials},
		{"extra_info", before.ExtraInfo, after.ExtraInfo},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.before, check.after) {
			changed[check.name] = map[string]any{"from": check.before, "to": check.after}
		}
	}
	return changed
}

func (a *App) updateTournamentSettings(c *gin.Context) {
	actor, tournamentID, access := a.managerTournament(c)
	if !access {
		return
	}
	var req tournamentSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "赛事设置格式不正确")
		return
	}
	// 从最早修改处重新推进时，任何更晚的对局都将失效。
	if req.Correction != nil && len(req.Corrections) != 0 {
		fail(c, 400, "请勿同时提交两种比分修正格式")
		return
	}
	if len(req.Corrections) == 1 {
		req.Correction = &req.Corrections[0]
		req.Corrections = nil
	}
	err := a.runMatchMutation(tournamentID, actor.ID, func(e *matchEngine) error {
		tx := e.db.(*sql.Tx)
		changedFields := map[string]any{}
		if req.AcePlayers != nil && (req.Correction != nil || len(req.Corrections) > 0 || req.AcePlayers.MatchID <= 0) {
			return errors.New("请先保存比分修正，再调整当前大将对阵")
		}
		beforeRules, err := e.rules()
		if err != nil {
			return err
		}
		// 设置与比分纠错共用事务，非法修正不能留下部分设置变更。
		var currentBackground sql.NullInt64
		if err := tx.QueryRow(`SELECT background_id FROM tournaments WHERE id=?`, tournamentID).Scan(&currentBackground); err != nil {
			return err
		}
		currentBackgroundID := int64(0)
		if currentBackground.Valid {
			currentBackgroundID = currentBackground.Int64
		}
		if req.BackgroundID != nil && *req.BackgroundID != currentBackgroundID {
			valid, err := backgroundExists(tx, *req.BackgroundID)
			if err != nil || *req.BackgroundID < 0 || !valid {
				return errors.New("背景不存在")
			}
			if _, err := tx.Exec(`UPDATE tournaments SET background_id=? WHERE id=?`, nullableInt64(*req.BackgroundID), tournamentID); err != nil {
				return err
			}
			changedFields["background_id"] = map[string]any{"from": currentBackgroundID, "to": *req.BackgroundID}
		}
		if err := requireEditableSettings(e.db, tournamentID); err != nil {
			if req.Name != nil || req.StartTime != nil || req.Description != nil || req.ManagerID != nil || req.PlayerIDs != nil || hasTournamentRulesPatch(req.Rules) || req.Round1Target != nil || req.Round2Target != nil {
				return err
			}
			rules := beforeRules
			if req.RandomDrawSeconds != nil {
				rules.RandomDrawSeconds = *req.RandomDrawSeconds
			}
			if req.AceDrawSeconds != nil {
				rules.AceDrawSeconds = *req.AceDrawSeconds
			}
			if err := rules.validateStored(); err != nil {
				return err
			}
			for key, value := range changedTournamentRuleFields(beforeRules, rules) {
				changedFields[key] = value
			}
			if !reflect.DeepEqual(beforeRules, rules) {
				if err := saveRules(tx, tournamentID, rules); err != nil {
					return err
				}
			}
			if len(req.Corrections) > 1 {
				if err := e.correctBatch(req.Corrections); err != nil {
					return err
				}
			} else if req.Correction != nil {
				if err := e.correct(*req.Correction); err != nil {
					return err
				}
			}
			if req.AcePlayers != nil {
				if err := e.overrideAce(aceRequest{PlayerAID: req.AcePlayers.PlayerAID, PlayerBID: req.AcePlayers.PlayerBID}, req.AcePlayers.MatchID); err != nil {
					return err
				}
				changedFields["ace_players"] = map[string]int64{"player_a_id": req.AcePlayers.PlayerAID, "player_b_id": req.AcePlayers.PlayerBID}
			}
			if len(req.Corrections) > 0 || req.Correction != nil {
				changedFields["corrections"] = true
			}
			if len(changedFields) == 0 {
				return nil
			}
			return e.log("tournament_settings_updated", changedFields)
		}
		if req.AcePlayers != nil {
			return errors.New("当前不是大将战阶段")
		}
		if req.Correction != nil || len(req.Corrections) > 0 {
			return errors.New("只能修正已录入对局的合法结果")
		}
		var beforeName, beforeDescription, beforeStartTime string
		var beforeManagerID int64
		var beforeRound1Target, beforeRound2Target int
		if err := tx.QueryRow(`SELECT name,description,start_time,manager_id,round1_target,round2_target FROM tournaments WHERE id=?`, tournamentID).Scan(&beforeName, &beforeDescription, &beforeStartTime, &beforeManagerID, &beforeRound1Target, &beforeRound2Target); err != nil {
			return err
		}
		grouped, err := tournamentHasGrouping(tx, tournamentID)
		if err != nil {
			return err
		}
		basicInfoChanged := req.ManagerID != nil && *req.ManagerID != beforeManagerID ||
			req.Name != nil && strings.TrimSpace(*req.Name) != beforeName ||
			req.Description != nil && *req.Description != beforeDescription
		if req.StartTime != nil {
			startTime, err := normalizeStartTime(*req.StartTime)
			if err != nil {
				return errors.New("比赛开始时间无效")
			}
			basicInfoChanged = basicInfoChanged || startTime != beforeStartTime
		}
		if basicInfoChanged {
			if actor.Role != roleSuperAdmin {
				return errTournamentBasicInfoForbidden
			}
			drawStarted, err := tournamentSetupDrawStarted(tx, tournamentID)
			if err != nil {
				return err
			}
			if drawStarted {
				return errTournamentBasicInfoLocked
			}
		}
		if req.ManagerID != nil && *req.ManagerID != beforeManagerID {
			manager := *req.ManagerID
			if err := validateTournamentManager(tx, manager); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE tournaments SET manager_id=? WHERE id=?`, manager, tournamentID); err != nil {
				return err
			}
			changedFields["manager_id"] = map[string]int64{"from": beforeManagerID, "to": manager}
		}
		rules := beforeRules
		if hasTournamentRulesPatch(req.Rules) {
			rules, err = mergeTournamentRules(beforeRules, req.Rules)
			if err != nil {
				return errors.New("赛事规则格式不正确")
			}
			// 抽签样式来自创建赛事时的网站配置快照。
			rules.DrawAnimation = beforeRules.DrawAnimation
		}
		if req.RandomDrawSeconds != nil {
			rules.RandomDrawSeconds = *req.RandomDrawSeconds
		}
		if req.AceDrawSeconds != nil {
			rules.AceDrawSeconds = *req.AceDrawSeconds
		}
		if err := normalizeTournamentExtraInfo(tx, &rules, &beforeRules); err != nil {
			return err
		}
		if err := rules.validate(); err != nil {
			return err
		}
		round1Target, round2Target := beforeRound1Target, beforeRound2Target
		if req.Round1Target != nil {
			round1Target = *req.Round1Target
		}
		if req.Round2Target != nil {
			round2Target = *req.Round2Target
		}
		round1TargetChanged := round1Target != beforeRound1Target
		round2TargetChanged := round2Target != beforeRound2Target
		ruleFields := changedTournamentRuleFields(beforeRules, rules)
		if grouped && ((round1TargetChanged || round2TargetChanged) || roundSetupChanged(beforeRules, rules)) {
			return errors.New("分组抽签后不能修改第一轮和KOF设置")
		}
		if round1Target < 1 || round2Target < 1 || round1Target > 99 || round2Target > 99 {
			return errors.New("抢分目标必须在1到99之间")
		}
		if round1TargetChanged {
			changedFields["round1_target"] = map[string]int{"from": beforeRound1Target, "to": round1Target}
		}
		if round2TargetChanged {
			changedFields["round2_target"] = map[string]int{"from": beforeRound2Target, "to": round2Target}
		}
		if len(ruleFields) > 0 {
			changedFields["rules"] = ruleFields
		}
		rosterChanged := false
		if req.PlayerIDs != nil {
			changed := false
			var rosterChanges map[string]any
			if grouped {
				changed, err = tournamentRosterChanged(tx, tournamentID, *req.PlayerIDs, req.FixedAssignments)
			} else {
				rosterChanges, err = tournamentSetupRosterChanges(tx, tournamentID, *req.PlayerIDs, req.FixedAssignments)
				changed = len(rosterChanges) > 0
			}
			if err != nil {
				return err
			}
			rosterChanged = changed
			if grouped && changed {
				return errors.New("分组抽签后不能修改参赛阵容")
			}
			if changed {
				if !grouped {
					changedFields["roster"] = rosterChanges
				}
				if err := saveSetupRoster(tx, tournamentID, *req.PlayerIDs, req.FixedAssignments); err != nil {
					return err
				}
				for _, table := range []string{"captain_votes", "tournament_players"} {
					if _, err := tx.Exec(`DELETE FROM `+table+` WHERE tournament_id=?`, tournamentID); err != nil {
						return err
					}
				}
			}
		}
		for _, group := range rules.AceSpecials {
			for _, player := range group.allPlayerIDs() {
				var exists int
				if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=? AND active=1)+(SELECT COUNT(*) FROM tournament_setup_roster WHERE tournament_id=? AND user_id=?)`, tournamentID, player, tournamentID, player).Scan(&exists); err != nil {
					return err
				}
				if exists == 0 {
					return errors.New("特殊候选必须是参赛选手")
				}
			}
		}
		updates := []string{}
		args := []any{}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return errors.New("赛事名称不能为空")
			}
			if name != beforeName {
				updates = append(updates, "name=?")
				args = append(args, name)
				changedFields["name"] = map[string]string{"from": beforeName, "to": name}
			}
		}
		if req.Description != nil && *req.Description != beforeDescription {
			updates = append(updates, "description=?")
			args = append(args, *req.Description)
			changedFields["description"] = map[string]string{"from": beforeDescription, "to": *req.Description}
		}
		if req.StartTime != nil {
			startTime, err := normalizeStartTime(*req.StartTime)
			if err != nil {
				return errors.New("比赛开始时间无效")
			}
			if startTime != beforeStartTime {
				updates = append(updates, "start_time=?")
				args = append(args, startTime)
				changedFields["start_time"] = map[string]string{"from": beforeStartTime, "to": startTime}
			}
		}
		if round1TargetChanged {
			updates = append(updates, "round1_target=?")
			args = append(args, round1Target)
			if _, err := tx.Exec(`UPDATE rounds SET target_score=? WHERE tournament_id=? AND stage='round1'`, round1Target, tournamentID); err != nil {
				return err
			}
		}
		if round2TargetChanged {
			updates = append(updates, "round2_target=?")
			args = append(args, round2Target)
			if _, err := tx.Exec(`UPDATE rounds SET target_score=? WHERE tournament_id=? AND stage='round2'`, round2Target, tournamentID); err != nil {
				return err
			}
		}
		if len(updates) > 0 {
			updates = append(updates, "updated_at=CURRENT_TIMESTAMP")
			args = append(args, tournamentID)
			if _, err := tx.Exec(`UPDATE tournaments SET `+strings.Join(updates, ",")+` WHERE id=?`, args...); err != nil {
				return err
			}
		}
		if len(ruleFields) > 0 {
			if err := saveRules(tx, tournamentID, rules); err != nil {
				return err
			}
		}
		// 仅修改大将战规则、赛事资料或管理员时保留当前的队长和排阵阶段。
		// 未分队或重新分队时仍清空依赖旧阵容的流程数据。
		resetSetup := !grouped && (rosterChanged || round1TargetChanged || round2TargetChanged || roundSetupChanged(beforeRules, rules))
		if resetSetup {
			if _, err := tx.Exec(`UPDATE tournaments SET lineup_a_submitted=0,lineup_b_submitted=0 WHERE id=?`, tournamentID); err != nil {
				return err
			}
			for _, table := range []string{"kof_reservations", "player_selections", "matches", "lineup_entries"} {
				if _, err := tx.Exec(`DELETE FROM `+table+` WHERE tournament_id=?`, tournamentID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`UPDATE rounds SET status='pending' WHERE tournament_id=?`, tournamentID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE tournaments SET status='draft',round1_no_repeat=?,kof_initial_a=NULL,kof_initial_b=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, boolInt(rules.NoRepeat), tournamentID); err != nil {
				return err
			}
		}
		if len(changedFields) == 0 {
			return nil
		}
		return e.log("tournament_settings_updated", changedFields)
	})
	if err != nil {
		status := 400
		if errors.Is(err, errTournamentBasicInfoForbidden) {
			status = 403
		}
		fail(c, status, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

func hasTournamentRulesPatch(patch json.RawMessage) bool {
	return len(patch) > 0 && strings.TrimSpace(string(patch)) != "null"
}

func mergeTournamentRules(current TournamentRules, patch json.RawMessage) (TournamentRules, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patch, &fields); err != nil || fields == nil {
		return TournamentRules{}, errors.New("规则补丁必须是对象")
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return TournamentRules{}, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(currentJSON, &merged); err != nil {
		return TournamentRules{}, err
	}
	for key, value := range fields {
		merged[key] = value
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return TournamentRules{}, err
	}
	var rules TournamentRules
	if err := json.Unmarshal(mergedJSON, &rules); err != nil {
		return TournamentRules{}, err
	}
	return rules, nil
}

func tournamentSetupRosterChanges(db sqlRunner, tournamentID int64, ids []int64, fixed map[string]string) (map[string]any, error) {
	rows, err := db.Query(`SELECT user_id,fixed_team FROM tournament_setup_roster WHERE tournament_id=? ORDER BY position,user_id`, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	current := make([]setupRosterEntry, 0, len(ids))
	currentByID := make(map[int64]setupRosterEntry)
	for rows.Next() {
		var entry setupRosterEntry
		if err := rows.Scan(&entry.UserID, &entry.FixedTeam); err != nil {
			return nil, err
		}
		entry.Position = len(current)
		current = append(current, entry)
		currentByID[entry.UserID] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	requested := make(map[int64]bool, len(ids))
	added := []int64{}
	assignments := []map[string]any{}
	orderChanged := false
	for position, userID := range ids {
		requested[userID] = true
		previous, exists := currentByID[userID]
		if !exists {
			added = append(added, userID)
			orderChanged = false
			continue
		}
		beforeTeam := normalizedFixedTeam(previous.FixedTeam)
		afterTeam := normalizedFixedTeam(fixed[strconv.FormatInt(userID, 10)])
		if beforeTeam != afterTeam {
			assignments = append(assignments, map[string]any{"player_id": userID, "from": beforeTeam, "to": afterTeam})
		}
		if previous.Position != position {
			orderChanged = true
		}
	}
	removed := []int64{}
	for _, entry := range current {
		if !requested[entry.UserID] {
			removed = append(removed, entry.UserID)
		}
	}
	changes := map[string]any{}
	if len(added) > 0 {
		changes["added"] = added
	}
	if len(removed) > 0 {
		changes["removed"] = removed
	}
	if len(assignments) > 0 {
		changes["fixed_teams"] = assignments
	}
	if orderChanged {
		beforeOrder := make([]int64, len(current))
		for i, entry := range current {
			beforeOrder[i] = entry.UserID
		}
		changes["order"] = map[string]any{"from": beforeOrder, "to": append([]int64(nil), ids...)}
	}
	return changes, nil
}

// 固定分队与当前阵容一致时保留队长及投票；主动随机分队则重新确认。
func tournamentRosterChanged(db sqlRunner, tournamentID int64, ids []int64, fixed map[string]string) (bool, error) {
	if len(ids) > 12 {
		return false, errors.New("参赛列表最多12人")
	}
	rows, err := db.Query(`SELECT ep.user_id,CASE WHEN ep.team_id=(?*2-1) THEN 'A' ELSE 'B' END FROM tournament_players ep WHERE ep.tournament_id=? AND ep.active=1`, tournamentID, tournamentID)
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
