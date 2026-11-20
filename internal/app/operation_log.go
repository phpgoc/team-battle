package app

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
)

type tournamentOperation struct {
	TournamentID int64
	ActorID      int64
	Action       string
	Payload      string
}

var tournamentOperationNames = map[string][3]string{
	"ace_drawn":                     {"抽取大将", "Draw ace players", "에이스 결정전 추첨"},
	"ace_players_overridden":        {"调整大将选手", "Change ace players", "에이스 선수 변경"},
	"captain_auto_confirmed":        {"投票确定队长", "Confirm captain by vote", "투표로 주장 확정"},
	"captain_vote_cast":             {"投票选队长", "Vote for captain", "주장 투표"},
	"captains_confirmed":            {"确认队长", "Confirm captains", "주장 확정"},
	"draw_settings_updated":         {"修改抽签设置", "Change draw settings", "추첨 설정 변경"},
	"historical_tournament_created": {"录入历史赛事", "Enter past tournament", "지난 대회 입력"},
	"kof_next_reserved":             {"报名或安排 KOF 下一位", "Sign up or select next KOF player", "다음 KOF 선수 신청 또는 지정"},
	"kof_player_selected":           {"确定 KOF 下一位", "Select next KOF player", "다음 KOF 선수 지정"},
	"kof_starter_selected":          {"选择 KOF 首发", "Select KOF starter", "KOF 선발 지정"},
	"lineup_generated":              {"随机安排第一轮阵容", "Randomize round 1 lineup", "1라운드 대진 무작위 배정"},
	"lineup_submitted":              {"提交第一轮阵容", "Submit round 1 lineup", "1라운드 대진 제출"},
	"match_result_corrected":        {"修正比赛结果", "Correct match result", "경기 결과 수정"},
	"match_results_corrected":       {"批量修正比赛结果", "Correct multiple match results", "여러 경기 결과 수정"},
	"match_result_submitted":        {"录入比赛结果", "Record match result", "경기 결과 기록"},
	"round1_started":                {"开始第一轮比赛", "Start round 1", "1라운드 시작"},
	"team_captain_selected":         {"指定队长", "Appoint captain", "주장 지정"},
	"tournament_background_updated": {"修改赛事背景", "Change tournament background", "대회 배경 변경"},
	"tournament_cancelled":          {"取消赛事", "Cancel tournament", "대회 취소"},
	"tournament_completed":          {"确认赛事结束", "Confirm tournament complete", "대회 종료 확정"},
	"tournament_created":            {"创建赛事", "Create tournament", "대회 생성"},
	"tournament_deleted":            {"删除赛事", "Delete tournament", "대회 삭제"},
	"tournament_restored":           {"恢复赛事", "Restore tournament", "대회 복원"},
	"tournament_roster_updated":     {"调整参赛名单", "Update tournament roster", "대회 선수 명단 변경"},
	"tournament_settings_updated":   {"修改赛事设置", "Update tournament settings", "대회 설정 변경"},
	"tournament_setup_drawn":        {"完成赛前抽签", "Complete pre-match draws", "경기 전 추첨 완료"},
}

func (a *App) logTournamentOperation(operation tournamentOperation) {
	language := appLogLanguage(a.logLanguage)
	actorName, actorRole := localized("System", "System", "시스템", language), localized("system", "system", "시스템", language)
	managerID := int64(0)
	if operation.ActorID != 0 {
		actorName, actorRole = localized("Account unavailable", "Account unavailable", "사용할 수 없는 계정", language), localized("unknown", "unknown", "알 수 없음", language)
		if actor, err := a.cachedUser(operation.ActorID); err == nil {
			actorName, actorRole = actor.DisplayName, accountRoleName(actor.Role, language)
		}
	}
	tournamentName := localized("Tournament unavailable", "Tournament unavailable", "대회를 사용할 수 없음", language)
	if err := a.db.QueryRow(`SELECT name,manager_id FROM tournaments WHERE id=?`, operation.TournamentID).Scan(&tournamentName, &managerID); err != nil {
		tournamentName = localized("Tournament unavailable", "Tournament unavailable", "대회를 사용할 수 없음", language)
	} else if operation.ActorID == managerID && actorRole == localized("Player", "Player", "선수", language) {
		actorRole = localized("Tournament manager", "Tournament manager", "대회 관리자", language)
	}
	actionNames, exists := tournamentOperationNames[operation.Action]
	actionName := operation.Action
	if exists {
		actionName = actionNames[language]
	}
	format := [3]string{
		"操作记录 操作者=%q 账号ID=%d 身份=%q 赛事=%q 赛事ID=%d 操作=%q 详情=%s",
		"Tournament operation actor=%q actor_id=%d role=%q tournament=%q tournament_id=%d action=%q details=%s",
		"대회 작업 작업자=%q 계정ID=%d 역할=%q 대회=%q 대회ID=%d 작업=%q 세부정보=%s",
	}[language]
	log.Printf(format, actorName, operation.ActorID, actorRole, tournamentName, operation.TournamentID, actionName, operationDetails(operation.Action, operation.Payload, language))
}

func appLogLanguage(language string) int {
	switch language {
	case "en":
		return 1
	case "ko":
		return 2
	default:
		return 0
	}
}

func localized(chinese, english, korean string, language int) string {
	return [3]string{chinese, english, korean}[language]
}

func operationLogText(language, chinese, english, korean string) string {
	return localized(chinese, english, korean, appLogLanguage(language))
}

func accountRoleName(role string, language int) string {
	switch role {
	case roleSuperAdmin:
		return localized("超级管理员", "Super administrator", "최고 관리자", language)
	case roleUser:
		return localized("选手", "Player", "선수", language)
	case roleManager:
		return localized("赛事管理员", "Tournament manager", "대회 관리자", language)
	default:
		return role
	}
}

func operationDetails(action, payload string, language int) string {
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &values) != nil || len(values) == 0 {
		return "{}"
	}
	keys := []string{}
	if action == "tournament_settings_updated" || action == "draw_settings_updated" {
		fields := []string{}
		for key, value := range values {
			if key == "description" || key == "name" || key == "expected" || key == "correction" || key == "corrections" || string(value) == "null" || string(value) == `""` {
				continue
			}
			if string(value) == "0" && key != "background_id" {
				continue
			}
			fields = append(fields, operationFieldName(key, language))
		}
		sort.Strings(fields)
		encoded, _ := json.Marshal(map[string][]string{localized("修改项", "Changed fields", "변경 항목", language): fields})
		return string(encoded)
	}
	switch action {
	case "ace_drawn":
		keys = []string{"match_id", "player_a_id", "player_b_id", "source"}
	case "captain_auto_confirmed", "captain_vote_cast", "captains_confirmed", "team_captain_selected":
		keys = []string{"team_id", "target_id", "team_a_player_id", "team_b_player_id", "player_id", "votes", "threshold"}
	case "lineup_submitted":
		keys = []string{"team_id", "player_ids"}
	case "kof_next_reserved", "kof_player_selected", "kof_starter_selected", "match_result_submitted", "match_result_corrected", "ace_players_overridden":
		keys = []string{"match_id", "team_id", "player_id", "player_a_id", "player_b_id", "winner_team_id"}
	case "tournament_deleted", "tournament_restored", "tournament_background_updated":
		keys = []string{"is_deleted", "background_id"}
	case "tournament_settings_updated", "draw_settings_updated":
		for key := range values {
			if key != "description" && key != "name" && key != "expected" && key != "correction" && key != "corrections" {
				keys = append(keys, key)
			}
		}
	case "tournament_roster_updated":
		keys = []string{"player_ids"}
	case "historical_tournament_created":
		keys = []string{"matches"}
	case "tournament_setup_drawn":
		var steps []struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(values["steps"], &steps) == nil {
			counts := map[string]int{}
			for _, step := range steps {
				counts[operationFieldName(step.Kind, language)]++
			}
			encoded, _ := json.Marshal(counts)
			return string(encoded)
		}
	case "match_results_corrected":
		var corrections []json.RawMessage
		if json.Unmarshal(values["corrections"], &corrections) == nil {
			encoded, _ := json.Marshal(map[string]int{localized("修正局数", "Corrected matches", "수정 경기 수", language): len(corrections)})
			return string(encoded)
		}
	}
	filtered := map[string]json.RawMessage{}
	for _, key := range keys {
		if value, exists := values[key]; exists {
			if key == "source" {
				var source string
				if json.Unmarshal(value, &source) == nil {
					value, _ = json.Marshal(operationSourceName(source, language))
				}
			}
			filtered[operationFieldName(key, language)] = value
		}
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func operationFieldName(key string, language int) string {
	names := map[string][3]string{
		"ace_draw_seconds":    {"大将抽选秒数", "Ace draw seconds", "에이스 추첨 시간"},
		"background_id":       {"背景编号", "Background ID", "배경 ID"},
		"category":            {"分类", "Category", "분류"},
		"fixed_assignments":   {"固定队伍", "Fixed teams", "고정 팀"},
		"group":               {"分队", "Team assignment", "팀 배정"},
		"kof_starter":         {"KOF 首发方式", "KOF starter mode", "KOF 선발 방식"},
		"lineup":              {"第一轮排阵", "Round 1 lineup", "1라운드 대진"},
		"manager_id":          {"赛事管理员", "Tournament manager", "대회 관리자"},
		"match_id":            {"对局编号", "Match ID", "경기 ID"},
		"matches":             {"对局数", "Matches", "경기 수"},
		"no_repeat":           {"避免连续出场", "Avoid consecutive appearances", "연속 출전 방지"},
		"player_a_id":         {"A 队选手编号", "Team A player ID", "A팀 선수 ID"},
		"player_b_id":         {"B 队选手编号", "Team B player ID", "B팀 선수 ID"},
		"player_id":           {"选手编号", "Player ID", "선수 ID"},
		"player_ids":          {"选手编号列表", "Player IDs", "선수 ID 목록"},
		"random_draw_seconds": {"常规抽选秒数", "Standard draw seconds", "일반 추첨 시간"},
		"rules":               {"赛事规则", "Tournament rules", "대회 규칙"},
		"round1":              {"第一轮排阵", "Round 1 lineup", "1라운드 대진"},
		"round1_mode":         {"第一轮方式", "Round 1 mode", "1라운드 방식"},
		"round1_target":       {"第一轮抢分目标", "Round 1 score target", "1라운드 목표 승수"},
		"round2":              {"KOF 首发", "KOF starter", "KOF 선발"},
		"round2_target":       {"KOF 抢分目标", "KOF score target", "KOF 목표 승수"},
		"source":              {"抽选来源", "Draw source", "추첨 방식"},
		"start_time":          {"开始时间", "Start time", "시작 시간"},
		"special":             {"大将特殊候选", "Ace special candidates", "에이스 특별 후보"},
		"steps":               {"抽选步骤", "Draw steps", "추첨 단계"},
		"team_id":             {"队伍编号", "Team ID", "팀 ID"},
		"team_a_player_id":    {"A 队队长", "Team A captain", "A팀 주장"},
		"team_b_player_id":    {"B 队队长", "Team B captain", "B팀 주장"},
		"target_id":           {"候选选手编号", "Candidate player ID", "후보 선수 ID"},
		"votes":               {"票数", "Votes", "득표 수"},
		"winner_team_id":      {"胜方队伍编号", "Winning team ID", "승리 팀 ID"},
		"username":            {"登录名", "Login name", "로그인 이름"},
		"display_name":        {"显示名", "Display name", "표시 이름"},
		"role":                {"账号类型", "Account role", "계정 유형"},
		"avatar":              {"头像", "Avatar", "프로필 사진"},
		"password":            {"密码", "Password", "비밀번호"},
	}
	if name, exists := names[key]; exists {
		return name[language]
	}
	return key
}

func operationSourceName(source string, language int) string {
	names := map[string][3]string{
		"most_wins":   {"多胜", "Most wins", "다승"},
		"most_losses": {"多败", "Most losses", "다패"},
		"special":     {"特殊候选", "Special candidates", "특별 후보"},
		"random":      {"全随机", "Fully random", "완전 무작위"},
		"mixed":       {"混合规则", "Mixed rules", "혼합 규칙"},
	}
	if name, exists := names[source]; exists {
		return name[language]
	}
	return source
}

func (a *App) logAccountOperation(actor User, targetID int64, targetName, action string, fields []string) {
	language := appLogLanguage(a.logLanguage)
	localizedFields := make([]string, len(fields))
	for i, field := range fields {
		localizedFields[i] = operationFieldName(field, language)
	}
	sort.Strings(localizedFields)
	role := accountRoleName(actor.Role, language)
	format := [3]string{
		"账号操作 操作者=%q 账号ID=%d 身份=%q 目标=%q 目标ID=%d 操作=%q 修改项=%q",
		"Account operation actor=%q actor_id=%d role=%q target=%q target_id=%d action=%q changed=%q",
		"계정 작업 작업자=%q 계정ID=%d 역할=%q 대상=%q 대상ID=%d 작업=%q 변경 항목=%q",
	}[language]
	log.Printf(format, actor.DisplayName, actor.ID, role, targetName, targetID, accountOperationName(action, language), strings.Join(localizedFields, ","))
}

func accountOperationName(action string, language int) string {
	names := map[string][3]string{
		"创建账号":   {"创建账号", "Create account", "계정 생성"},
		"删除账号":   {"删除账号", "Delete account", "계정 삭제"},
		"修改账号资料": {"修改账号资料", "Update account profile", "계정 프로필 수정"},
		"修改账号密码": {"修改账号密码", "Change account password", "계정 비밀번호 변경"},
		"重置账号密码": {"重置账号密码", "Reset account password", "계정 비밀번호 재설정"},
	}
	if name, exists := names[action]; exists {
		return name[language]
	}
	return action
}
