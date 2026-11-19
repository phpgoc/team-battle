package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestManualLineupsRevealAfterBothSubmissions(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	teams := f.teams(t, id)
	public := fmt.Sprintf("/api/public/tournaments/%d", id)
	private := fmt.Sprintf("/api/tournaments/%d/lineup", id)
	read := func(raw []byte) struct {
		Lineups []struct {
			TeamID   int64 `json:"team_id"`
			PlayerID int64 `json:"player_id"`
		} `json:"lineups"`
		Teams []struct {
			Submitted bool `json:"lineup_submitted"`
		} `json:"teams"`
	} {
		t.Helper()
		var data struct {
			Lineups []struct {
				TeamID   int64 `json:"team_id"`
				PlayerID int64 `json:"player_id"`
			} `json:"lineups"`
			Teams []struct {
				Submitted bool `json:"lineup_submitted"`
			} `json:"teams"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	f.request(t, 3, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{5, 3}}, 200)
	f.request(t, 3, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{4, 3}}, 200)
	privateLineup := read(f.request(t, 3, "GET", private, nil, 200)).Lineups
	if len(privateLineup) != 2 || privateLineup[0].PlayerID != 4 {
		t.Fatalf("另一方尚未提交时，先排阵的队伍应能重排: %+v", privateLineup)
	}
	for _, user := range []int{0, 1, 3, 6} {
		data := read(f.request(t, user, "GET", public, nil, 200))
		if len(data.Lineups) != 0 || !data.Teams[0].Submitted || data.Teams[1].Submitted {
			t.Fatalf("单方排阵必须隐藏，仅公开提交状态: %+v", data)
		}
	}
	payload, err := f.app.publicTournamentPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(read(payload).Lineups) != 0 {
		t.Fatal("公开SSE泄露单方排阵")
	}
	if len(read(f.request(t, 3, "GET", private, nil, 200)).Lineups) != 2 {
		t.Fatal("本队队长无法读取已保存排阵")
	}
	if len(read(f.request(t, 6, "GET", private, nil, 200)).Lineups) != 0 {
		t.Fatal("对方队长读取了单方排阵")
	}
	if len(read(f.request(t, 2, "GET", private, nil, 200)).Lineups) != 2 {
		t.Fatal("管理员无法代排")
	}
	f.request(t, 0, "GET", private, nil, 401)
	f.request(t, 4, "GET", private, nil, 403)
	// 全部随机补位同样算提交，不能一直等不到另一方确认。
	f.request(t, 6, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[1], "player_ids": []int{}}, 200)
	data := read(f.request(t, 0, "GET", public, nil, 200))
	if len(data.Lineups) != 6 || !data.Teams[0].Submitted || !data.Teams[1].Submitted {
		t.Fatalf("双方提交后未公开排阵: %+v", data)
	}
	lockedSnapshot, err := f.app.publicTournamentPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		user int
		body map[string]any
	}{
		{3, map[string]any{"team_id": teams[0], "player_ids": []int{3, 4}}},
		{6, map[string]any{"team_id": teams[1], "player_ids": []int{7, 6}}},
		{2, map[string]any{"team_id": teams[0], "player_ids": []int{3, 4}}},
		{2, map[string]any{"team_id": teams[1], "player_ids": []int{7, 6}}},
	} {
		f.request(t, request.user, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), request.body, 400)
	}
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/generate", id), nil, 400)
	for _, user := range []int{2, 3, 6} {
		f.request(t, user, "GET", fmt.Sprintf("/live/%d/lineup", id), nil, 302)
	}
	afterLockedAttempts, err := f.app.publicTournamentPayload(id)
	if err != nil || string(lockedSnapshot) != string(afterLockedAttempts) {
		t.Fatalf("公开后不能重排，拒绝操作不得修改排阵: %s %v", afterLockedAttempts, err)
	}
	// 兼容旧数据库中已有排阵但没有提交标记的赛事。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET lineup_a_submitted=0,lineup_b_submitted=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 6, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[1], "player_ids": []int{8, 6}}, 400)
	if len(read(f.request(t, 0, "GET", public, nil, 200)).Lineups) != 6 {
		t.Fatal("旧排阵未保持已公开状态")
	}
}
