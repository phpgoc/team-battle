package app

type tournamentOperation struct {
	TournamentID int64
	ActorID      int64
	Action       string
	Payload      string
}

var tournamentOperationNames = map[string][3]string{
	"ace_drawn":                      {"抽取大将", "Draw ace players", "에이스 결정전 추첨"},
	"ace_players_overridden":         {"调整大将选手", "Change ace players", "에이스 선수 변경"},
	"captain_auto_confirmed":         {"投票确定队长", "Confirm captain by vote", "투표로 주장 확정"},
	"captain_vote_cast":              {"投票选队长", "Vote for captain", "주장 투표"},
	"captains_confirmed":             {"确认队长", "Confirm captains", "주장 확정"},
	"draw_settings_updated":          {"修改抽签设置", "Change draw settings", "추첨 설정 변경"},
	"historical_tournament_created":  {"录入历史赛事", "Enter past tournament", "지난 대회 입력"},
	"kof_next_reserved":              {"选择 KOF 下一位", "Select next KOF player", "다음 KOF 선수 선택"},
	"kof_player_selected":            {"选择 KOF 下一位", "Select next KOF player", "다음 KOF 선수 선택"},
	"kof_starter_selected":           {"选择 KOF 首发", "Select KOF starter", "KOF 선발 지정"},
	"lineup_generated":               {"随机安排第一轮阵容", "Randomize round 1 lineup", "1라운드 대진 무작위 배정"},
	"lineup_submitted":               {"提交第一轮阵容", "Submit round 1 lineup", "1라운드 대진 제출"},
	"match_result_corrected":         {"修正比赛结果", "Correct match result", "경기 결과 수정"},
	"match_results_corrected":        {"批量修正比赛结果", "Correct multiple match results", "여러 경기 결과 수정"},
	"match_result_submitted":         {"录入比赛结果", "Record match result", "경기 결과 기록"},
	"round1_started":                 {"开始第一轮比赛", "Start round 1", "1라운드 시작"},
	"team_captain_selected":          {"指定队长", "Appoint captain", "주장 지정"},
	"tournament_background_updated":  {"修改赛事背景", "Change tournament background", "대회 배경 변경"},
	"tournament_cancelled":           {"取消赛事", "Cancel tournament", "대회 취소"},
	"tournament_completed":           {"确认赛事结束", "Confirm tournament complete", "대회 종료 확정"},
	"tournament_created":             {"创建赛事", "Create tournament", "대회 생성"},
	"tournament_deleted":             {"删除赛事", "Delete tournament", "대회 삭제"},
	"tournament_permanently_deleted": {"永久删除赛事", "Permanently delete tournament", "대회 영구 삭제"},
	"tournament_restored":            {"恢复赛事", "Restore tournament", "대회 복원"},
	"tournament_roster_updated":      {"调整参赛名单", "Update tournament roster", "대회 선수 명단 변경"},
	"tournament_settings_updated":    {"修改赛事设置", "Update tournament settings", "대회 설정 변경"},
	"tournament_setup_drawn":         {"完成赛前抽签", "Complete pre-match draws", "경기 전 추첨 완료"},
	"account_created":                {"创建账号", "Create account", "계정 생성"},
	"account_profile_updated":        {"修改账号资料", "Update account profile", "계정 프로필 수정"},
	"account_password_updated":       {"修改账号密码", "Change account password", "계정 비밀번호 변경"},
	"account_deleted":                {"删除账号", "Delete account", "계정 삭제"},
	"login_success":                  {"登录成功", "Sign in", "로그인 성공"},
	"login_failed":                   {"登录失败", "Sign-in failed", "로그인 실패"},
	"logout":                         {"退出登录", "Sign out", "로그아웃"},
	"site_defaults_updated":          {"修改赛事默认设置", "Update tournament defaults", "대회 기본 설정 수정"},
	"site_tournament_name_created":   {"创建常用赛事名", "Create tournament name", "대회 이름 생성"},
	"site_tournament_name_updated":   {"修改常用赛事名", "Update tournament name", "대회 이름 수정"},
	"site_tournament_name_deleted":   {"删除常用赛事名", "Delete tournament name", "대회 이름 삭제"},
	"site_category_created":          {"创建分类", "Create category", "분류 생성"},
	"site_category_updated":          {"修改分类", "Update category", "분류 수정"},
	"site_category_deleted":          {"删除分类", "Delete category", "분류 삭제"},
	"site_extra_info_created":        {"创建额外信息", "Create extra info", "추가 정보 생성"},
	"site_extra_info_updated":        {"修改额外信息", "Update extra info", "추가 정보 수정"},
	"site_extra_info_deleted":        {"删除额外信息", "Delete extra info", "추가 정보 삭제"},
	"site_background_created":        {"创建背景", "Create background", "배경 생성"},
	"site_background_updated":        {"修改背景", "Update background", "배경 수정"},
	"site_background_deleted":        {"删除背景", "Delete background", "배경 삭제"},
	"database_migration_completed":   {"数据库迁移完成", "Database migration complete", "데이터베이스 마이그레이션 완료"},
}

func (a *App) logTournamentOperation(operation tournamentOperation) {
	if a.operationLogger != nil {
		a.operationLogger.captureTournamentOperation(operation)
	}
}

func (a *App) logAccountOperation(actor User, targetID int64, targetName, action string, fields []string) {
	if a.operationLogger != nil {
		a.operationLogger.captureAccountOperation(action)
	}
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

func operationTagLabel(language, tag string) string {
	if names, exists := tournamentOperationNames[tag]; exists {
		return names[appLogLanguage(language)]
	}
	return tag
}
