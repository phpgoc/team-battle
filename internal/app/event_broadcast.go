package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type streamSnapshot struct {
	payload []byte
	status  string
	grouped bool
}

type eventSubscribers struct {
	latest  streamSnapshot
	clients map[chan streamSnapshot]struct{}
}

// 订阅者和最新公开快照只存在内存中，最后一个连接离开时释放。
type eventBroadcast struct {
	mu     sync.Mutex
	events map[int64]*eventSubscribers
}

func (a *App) streamSnapshot(eventID int64) (streamSnapshot, error) {
	view, err := a.buildEvent(eventID, false)
	if err != nil {
		return streamSnapshot{}, err
	}
	payload, err := json.Marshal(view)
	if err != nil {
		return streamSnapshot{}, err
	}
	var assigned int
	if err := a.db.QueryRow(`SELECT COUNT(DISTINCT team_id) FROM event_players WHERE event_id=? AND active=1`, eventID).Scan(&assigned); err != nil {
		return streamSnapshot{}, err
	}
	return streamSnapshot{payload: payload, status: view["status"].(string), grouped: assigned == 2}, nil
}

func (a *App) subscribeEvent(eventID int64) (streamSnapshot, <-chan streamSnapshot, func(), error) {
	// 与写事务使用同一把锁，避免在初始快照和登记订阅之间漏掉更新。
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	hub := &a.broadcast
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.events == nil {
		hub.events = make(map[int64]*eventSubscribers)
	}
	entry := hub.events[eventID]
	if entry == nil {
		snapshot, err := a.streamSnapshot(eventID)
		if err != nil {
			return streamSnapshot{}, nil, nil, err
		}
		entry = &eventSubscribers{latest: snapshot, clients: make(map[chan streamSnapshot]struct{})}
		hub.events[eventID] = entry
	}
	client := make(chan streamSnapshot, 1)
	entry.clients[client] = struct{}{}
	unsubscribe := func() {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		delete(entry.clients, client)
		if len(entry.clients) == 0 {
			delete(hub.events, eventID)
		}
	}
	return entry.latest, client, unsubscribe, nil
}

func (a *App) publishEvents(eventID int64) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	hub := &a.broadcast
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for id, entry := range hub.events {
		if eventID != 0 && eventID != id {
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
	if strings.HasPrefix(path, "/api/events/") {
		if id, err := parseID(c.Param("id")); err == nil && id > 0 {
			a.publishEvents(id)
		}
	} else if strings.HasPrefix(path, "/api/users") || strings.HasPrefix(path, "/api/auth/profile") || strings.HasPrefix(path, "/api/auth/avatar") || strings.HasPrefix(path, "/api/site-settings/") {
		a.publishEvents(0)
	}
}
