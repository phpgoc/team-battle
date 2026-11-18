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
	public := fmt.Sprintf("/api/public/events/%d", id)
	private := fmt.Sprintf("/api/events/%d/lineup", id)
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
	f.request(t, 3, "POST", fmt.Sprintf("/api/events/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{5, 3}}, 200)
	for _, user := range []int{0, 1, 3, 6} {
		data := read(f.request(t, user, "GET", public, nil, 200))
		if len(data.Lineups) != 0 || !data.Teams[0].Submitted || data.Teams[1].Submitted {
			t.Fatalf("单方排阵必须隐藏，仅公开提交状态: %+v", data)
		}
	}
	payload, err := f.app.publicEventPayload(id)
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
	f.request(t, 6, "POST", fmt.Sprintf("/api/events/%d/lineup/submit", id), map[string]any{"team_id": teams[1], "player_ids": []int{}}, 200)
	data := read(f.request(t, 0, "GET", public, nil, 200))
	if len(data.Lineups) != 6 || !data.Teams[0].Submitted || !data.Teams[1].Submitted {
		t.Fatalf("双方提交后未公开排阵: %+v", data)
	}
	// 兼容旧数据库中已有排阵但没有提交标记的赛事。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET lineup_a_submitted=0,lineup_b_submitted=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 6, "POST", fmt.Sprintf("/api/events/%d/lineup/submit", id), map[string]any{"team_id": teams[1], "player_ids": []int{8, 6}}, 200)
	if len(read(f.request(t, 0, "GET", public, nil, 200)).Lineups) != 6 {
		t.Fatal("旧排阵未被识别为已提交")
	}
}
