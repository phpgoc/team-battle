package app

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
)

func TestTournamentOperationsAreWrittenToStandardLogger(t *testing.T) {
	fixture := newWorkflowFixture(t)
	previousWriter, previousFlags := log.Writer(), log.Flags()
	var output bytes.Buffer
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	tournamentID := fixture.create(t, "random")
	output.Reset()
	fixture.enterKOF(t, tournamentID)
	kofMatch := fixture.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND round_no=2 AND status='pending'`, tournamentID)
	teams := fixture.teams(t, tournamentID)
	fixture.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/matches/%d/result", tournamentID, kofMatch), map[string]any{"winner_team_id": teams[0]}, 200)

	logged := output.String()
	for _, expected := range []string{"操作记录", "操作者=\"选手2\"", "身份=\"选手\"", "赛事=\"首发验证\"", "录入比赛结果", fmt.Sprintf("赛事ID=%d", tournamentID)} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("标准日志缺少 %q: %s", expected, logged)
		}
	}
	if strings.Contains(logged, "密码") || strings.Contains(logged, "password") {
		t.Fatalf("标准日志包含不应记录的敏感信息: %s", logged)
	}

	output.Reset()
	fixture.request(t, 1, "PATCH", "/api/users/3", map[string]any{
		"display_name": "资料日志验证", "new_password": "never-log-this", "new_password_confirm": "never-log-this",
	}, 200)
	logged = output.String()
	if !strings.Contains(logged, "账号操作") || !strings.Contains(logged, "修改账号资料") || !strings.Contains(logged, "目标=\"资料日志验证\"") {
		t.Fatalf("账号资料操作没有记录操作者及目标: %s", logged)
	}
	if strings.Contains(logged, "never-log-this") {
		t.Fatalf("标准日志泄露了密码: %s", logged)
	}
}

func TestOperationLogsSupportConfiguredLanguages(t *testing.T) {
	fixture := newWorkflowFixture(t)
	tournamentID := fixture.create(t, "random")
	if _, err := fixture.app.db.Exec(`UPDATE tournaments SET manager_id=3 WHERE id=?`, tournamentID); err != nil {
		t.Fatal(err)
	}
	previousWriter, previousFlags := log.Writer(), log.Flags()
	var output bytes.Buffer
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	tests := []struct {
		language        string
		operationPrefix string
		actorRole       string
		operationName   string
		accountPrefix   string
		accountRole     string
		changedField    string
		categoryField   string
		drawSource      string
	}{
		{"", "操作记录", "选手", "录入比赛结果", "账号操作", "超级管理员", "显示名", "分类", "多胜"},
		{"en", "Tournament operation", "Player", "Record match result", "Account operation", "Super administrator", "Display name", "Category", "Most wins"},
		{"ko", "대회 작업", "선수", "경기 결과 기록", "계정 작업", "최고 관리자", "표시 이름", "분류", "다승"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			fixture.app.logLanguage = test.language
			output.Reset()
			fixture.app.logTournamentOperation(tournamentOperation{
				TournamentID: tournamentID,
				ActorID:      2,
				Action:       "match_result_submitted",
				Payload:      `{"winner_team_id":1}`,
			})
			logged := output.String()
			for _, expected := range []string{test.operationPrefix, test.actorRole, test.operationName} {
				if !strings.Contains(logged, expected) {
					t.Fatalf("赛事日志缺少 %q: %s", expected, logged)
				}
			}
			if source := operationDetails("ace_drawn", `{"source":"most_wins"}`, appLogLanguage(test.language)); !strings.Contains(source, test.drawSource) {
				t.Fatalf("大将战来源未翻译为 %q: %s", test.drawSource, source)
			}

			output.Reset()
			fixture.app.logAccountOperation(User{ID: 1, DisplayName: "管理员", Role: roleSuperAdmin}, 2, "选手2", "修改账号资料", []string{"display_name", "password", "category"})
			logged = output.String()
			for _, expected := range []string{test.accountPrefix, test.accountRole, test.changedField, test.categoryField} {
				if !strings.Contains(logged, expected) {
					t.Fatalf("账号日志缺少 %q: %s", expected, logged)
				}
			}
			if test.language != "" && strings.Contains(logged, "分类") {
				t.Fatalf("非中文账号日志包含中文字段: %s", logged)
			}
			if strings.Contains(logged, "never-log-this") {
				t.Fatalf("账号日志泄露了密码值: %s", logged)
			}
		})
	}
}
