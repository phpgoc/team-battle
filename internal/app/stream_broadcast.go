package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type streamSnapshot struct {
	payload     []byte
	status      string
	publicReady bool
}

type streamSubscribers struct {
	latest  streamSnapshot
	clients map[chan streamSnapshot]struct{}
}

// 订阅者和最新公开快照只存在内存中，最后一个连接离开时释放。
type streamHub struct {
	mu          sync.Mutex
	tournaments map[int64]*streamSubscribers
}

func (a *App) streamSnapshot(tournamentID int64) (streamSnapshot, error) {
	view, err := a.buildTournament(tournamentID, false)
	if err != nil {
		return streamSnapshot{}, err
	}
	payload, err := json.Marshal(view)
	if err != nil {
		return streamSnapshot{}, err
	}
	return streamSnapshot{payload: payload, status: view["status"].(string), publicReady: view["public_ready"] == true}, nil
}

func (a *App) subscribeTournament(tournamentID int64) (streamSnapshot, <-chan streamSnapshot, func(), error) {
	if a.shutdownStarted.Load() {
		return streamSnapshot{}, nil, nil, errServerStopping
	}
	// 与写事务使用同一把锁，避免在初始快照和登记订阅之间漏掉更新。
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	hub := &a.broadcast
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if a.shutdownStarted.Load() {
		return streamSnapshot{}, nil, nil, errServerStopping
	}
	if hub.tournaments == nil {
		hub.tournaments = make(map[int64]*streamSubscribers)
	}
	entry := hub.tournaments[tournamentID]
	if entry == nil {
		snapshot, err := a.streamSnapshot(tournamentID)
		if err != nil {
			return streamSnapshot{}, nil, nil, err
		}
		entry = &streamSubscribers{latest: snapshot, clients: make(map[chan streamSnapshot]struct{})}
		hub.tournaments[tournamentID] = entry
	}
	client := make(chan streamSnapshot, 1)
	entry.clients[client] = struct{}{}
	unsubscribe := func() {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		delete(entry.clients, client)
		if len(entry.clients) == 0 {
			delete(hub.tournaments, tournamentID)
		}
	}
	return entry.latest, client, unsubscribe, nil
}

func (a *App) publishTournamentUpdates(tournamentID int64) {
	if a.shutdownStarted.Load() {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	hub := &a.broadcast
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if a.shutdownStarted.Load() {
		return
	}
	for id, entry := range hub.tournaments {
		if tournamentID != 0 && tournamentID != id {
			continue
		}
		next, err := a.streamSnapshot(id)
		if err != nil {
			next = streamSnapshot{}
		}
		if bytes.Equal(next.payload, entry.latest.payload) {
			continue
		}
		entry.latest = next
		for client := range entry.clients {
			// 慢连接只保留最新快照，不能阻塞录分或拖住其他观众。
			select {
			case <-client:
			default:
			}
			client <- next
		}
	}
}

// 只有保存成功才广播，私有报名等没有公开变化的操作不会产生消息。
func (a *App) broadcastAfterMutation(c *gin.Context) {
	c.Next()
	if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" || c.Writer.Status() >= 400 {
		return
	}
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/tournaments/") {
		if id, err := parseID(c.Param("id")); err == nil && id > 0 {
			a.publishTournamentUpdates(id)
		}
	} else if strings.HasPrefix(path, "/api/users") || strings.HasPrefix(path, "/api/auth/profile") || strings.HasPrefix(path, "/api/auth/avatar") || strings.HasPrefix(path, "/api/site-settings/") {
		a.publishTournamentUpdates(0)
	}
}
