package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOpenParticipationLocksOwnProfile(t *testing.T) {
	for _, stage := range []string{statusDraft, statusCaptainVote, statusLineup, statusRound1, statusRound2, statusAce, statusFinalConfirm} {
		t.Run(stage, func(t *testing.T) {
			f := newWorkflowFixture(t)
			id := f.create(t, "captain")
			if _, err := f.app.db.Exec(`UPDATE tournaments SET status=? WHERE id=?`, stage, id); err != nil {
				t.Fatal(err)
			}
			for _, change := range []map[string]any{{"username": "locked"}, {"display_name": "locked"}, {"display_name": "locked", "new_password": "newpass", "new_password_confirm": "newpass"}} {
				f.request(t, 3, "PATCH", "/api/auth/profile", change, 403)
			}
			f.accountForm(t, 3, "POST", "/api/auth/avatar", nil, true, 403)
			f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE id=3 AND username='user3' AND display_name='选手3' AND password_hash='unused' AND profile_changed_at IS NULL`)
			var me struct {
				Blocked bool `json:"profile_edit_blocked"`
			}
			if err := json.Unmarshal(f.request(t, 3, "GET", "/api/auth/me", nil, 200), &me); err != nil || !me.Blocked {
				t.Fatal("本人资料未标记参赛锁定", err)
			}
			f.request(t, 3, "PATCH", "/api/auth/profile", map[string]any{"new_password": "newpass", "new_password_confirm": "newpass"}, 200)
			f.request(t, 1, "PATCH", "/api/users/3", map[string]any{"display_name": "管理员代改"}, 200)
		})
	}
}

func TestSetupRosterLocksUntilAllParticipationsClose(t *testing.T) {
	f := newWorkflowFixture(t)
	first := createSetupTournament(t, f, 6, "random", "random", nil)
	second := createSetupTournament(t, f, 6, "random", "random", nil)
	f.request(t, 3, "PATCH", "/api/auth/profile", map[string]any{"display_name": "blocked"}, 403)
	if _, err := f.app.db.Exec(`UPDATE tournaments SET status='completed' WHERE id=?`, first); err != nil {
		t.Fatal(err)
	}
	f.request(t, 3, "PATCH", "/api/auth/profile", map[string]any{"display_name": "blocked"}, 403)
	if _, err := f.app.db.Exec(`UPDATE tournaments SET status='cancelled' WHERE id=?`, second); err != nil {
		t.Fatal(err)
	}
	f.request(t, 3, "PATCH", "/api/auth/profile", map[string]any{"display_name": "可修改"}, 200)
	f.request(t, 3, "PATCH", "/api/auth/profile", map[string]any{"username": "仍有冷却"}, 400)
}

func TestSuperAdminHasNoProfileCooldownOrParticipationLock(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	if _, err := f.app.db.Exec(`INSERT INTO tournament_setup_roster(tournament_id,user_id,fixed_team,position) VALUES(?,1,'R',99)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`UPDATE users SET profile_changed_at=? WHERE id=1`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	f.app.invalidateCategoryDirectory()
	f.request(t, 1, "PATCH", "/api/auth/profile", map[string]any{"display_name": "超管改名"}, 200)
	f.request(t, 1, "PATCH", "/api/users/1", map[string]any{"username": "admin-renamed"}, 200)
	f.accountForm(t, 1, "POST", "/api/auth/avatar", nil, true, 200)
	f.request(t, 1, "PATCH", "/api/auth/profile", map[string]any{"display_name": "再次改名"}, 200)
}
