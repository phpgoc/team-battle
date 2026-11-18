package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestOngoingSummariesKeepUnconfirmedTournaments(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	for _, status := range []string{statusDraft, statusCaptainVote, statusLineup, statusRound1, statusRound2, statusAce, statusFinalConfirm, statusCompleted, statusCancelled} {
		if _, err := f.app.db.Exec(`UPDATE tournaments SET status=? WHERE id=?`, status, id); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Tournaments []struct {
				ID        int64 `json:"id"`
				HasRoster bool  `json:"has_roster"`
			} `json:"tournaments"`
		}
		if err := json.Unmarshal(f.request(t, 0, "GET", "/api/public/tournaments", nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		want := 1
		if status == statusCompleted || status == statusCancelled {
			want = 0
		}
		if len(result.Tournaments) != want || (want == 1 && (result.Tournaments[0].ID != id || !result.Tournaments[0].HasRoster)) {
			t.Fatalf("未确认赛事应持续留在正在进行分类，status=%s: %+v", status, result)
		}
	}
	// 名单足够时即使尚未分组，也允许直接打开实时页。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET status='draft' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`DELETE FROM tournament_players WHERE tournament_id=?`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 0, "GET", fmt.Sprintf("/live/%d", id), nil, 200)
	f.request(t, 2, "GET", fmt.Sprintf("/live/%d", id), nil, 200)
}
