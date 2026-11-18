package app

import (
	"bytes"
	"fmt"
	"testing"
)

func TestLineupPagePermissionsAndMode(t *testing.T) {
	f := newWorkflowFixture(t)
	id := createSetupTournament(t, f, 6, "manual", "captain", map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"})
	path := fmt.Sprintf("/live/%d/lineup", id)
	f.request(t, 1, "GET", path, nil, 302)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", id), nil, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/captains/confirm", id), map[string]any{"team_a_player_id": 3, "team_b_player_id": 6}, 200)
	for _, user := range []int{1, 2, 3, 6} {
		f.request(t, user, "GET", path, nil, 200)
	}
	for _, user := range []int{0, 4, 9} {
		f.request(t, user, "GET", path, nil, 403)
	}
	teams := f.teams(t, id)
	f.request(t, 3, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[1], "player_ids": []int{6, 7}}, 400)
	f.request(t, 3, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[0], "player_ids": []int{5, 3}}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/lineup/submit", id), map[string]any{"team_id": teams[1], "player_ids": []int{8, 6}}, 200)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/start", id), nil, 200)
	f.request(t, 3, "GET", path, nil, 302)
	f.request(t, 2, "GET", path, nil, 302)
	randomID := createSetupTournament(t, f, 6, "random", "random", nil)
	f.request(t, 2, "POST", fmt.Sprintf("/api/tournaments/%d/setup-draw", randomID), nil, 200)
	f.request(t, 2, "GET", fmt.Sprintf("/live/%d/lineup", randomID), nil, 302)
	f.request(t, 1, "GET", "/live/999999/lineup", nil, 404)
}

func TestLineupAndKOFAssetsEmbedded(t *testing.T) {
	f := newWorkflowFixture(t)
	index := f.request(t, 0, "GET", "/", nil, 200)
	for _, asset := range []string{"lineup-page.js", "lineup-page.css", "kof-board.js", "kof-board.css"} {
		path := "/assets/" + asset
		if !bytes.Contains(index, []byte(path)) || len(f.request(t, 0, "GET", path, nil, 200)) == 0 {
			t.Fatalf("正式二进制缺少排阵资源: %s", path)
		}
	}
}
