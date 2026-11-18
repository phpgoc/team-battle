package app

import (
	"bytes"
	"fmt"
	"testing"
)

func TestMemoryBroadcastKeepsLatestAndReleasesSubscribers(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	first, guest, leaveGuest, err := f.app.subscribeTournament(id)
	if err != nil {
		t.Fatal(err)
	}
	second, manager, leaveManager, err := f.app.subscribeTournament(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.payload, second.payload) {
		t.Fatal("同一赛事应共享公开快照")
	}
	path := fmt.Sprintf("/api/tournaments/%d/draw-settings", id)
	for _, seconds := range []int{7, 8, 9} {
		f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": seconds, "ace_draw_seconds": 5}, 200)
	}
	latest, _ := f.app.publicTournamentPayload(id)
	for _, channel := range []<-chan streamSnapshot{guest, manager} {
		select {
		case update := <-channel:
			if !bytes.Equal(update.payload, latest) {
				t.Fatal("慢连接未合并为最新快照")
			}
		default:
			t.Fatal("保存后没有立即广播")
		}
		select {
		case <-channel:
			t.Fatal("仍缓存了多余的旧消息")
		default:
		}
	}
	// 失败写入和没有公开变化的重复设置不应推送。
	f.request(t, 3, "PATCH", path, map[string]any{"random_draw_seconds": 10, "ace_draw_seconds": 5}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 9, "ace_draw_seconds": 5}, 200)
	select {
	case <-guest:
		t.Fatal("没有公开变化仍触发推送")
	default:
	}
	leaveGuest()
	if len(f.app.broadcast.tournaments) != 1 {
		t.Fatal("仍有连接时过早释放快照")
	}
	leaveManager()
	if len(f.app.broadcast.tournaments) != 0 {
		t.Fatal("最后一个连接离开后未释放内存")
	}
}

func TestBroadcastRefreshesSharedPlayerProfiles(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	_, updates, leave, err := f.app.subscribeTournament(id)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	f.request(t, 1, "PATCH", "/api/users/3", map[string]any{"display_name": "改名选手", "role": "user", "status": "active"}, 200)
	select {
	case next := <-updates:
		if !bytes.Contains(next.payload, []byte("改名选手")) {
			t.Fatal("选手资料修改未同步公开快照")
		}
	default:
		t.Fatal("选手资料修改没有广播")
	}
}
