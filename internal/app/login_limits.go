package app

import (
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	loginAttemptsPerMinute = 20
	loginMaxTrackedIPs     = 4096
	loginMaxConcurrent     = 8
)

type loginLimiter struct {
	mu       sync.Mutex
	window   time.Time
	attempts map[netip.Addr]int
	active   int
}

func (l *loginLimiter) allowAttempt(ip netip.Addr, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	window := now.Truncate(time.Minute)
	if !l.window.Equal(window) {
		l.window = window
		l.attempts = make(map[netip.Addr]int)
	}
	count := l.attempts[ip]
	if count >= loginAttemptsPerMinute || (count == 0 && len(l.attempts) >= loginMaxTrackedIPs) {
		return false
	}
	l.attempts[ip] = count + 1
	return true
}

func (l *loginLimiter) start() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active >= loginMaxConcurrent {
		return false
	}
	l.active++
	return true
}

func (l *loginLimiter) finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
}

func requestPeerIP(req *http.Request) netip.Addr {
	peer, _ := netip.ParseAddrPort(req.RemoteAddr)
	return peer.Addr().Unmap().WithZone("")
}

// 只接受本机反向代理覆盖后的转发头，直接连接使用其对端地址。
func loginClientIP(req *http.Request) netip.Addr {
	peer := requestPeerIP(req)
	if peer.IsLoopback() {
		if client, err := netip.ParseAddr(req.Header.Get("X-Real-IP")); err == nil {
			return client.Unmap().WithZone("")
		}
	}
	return peer
}

func secureSessionRequest(req *http.Request) bool {
	return req.TLS != nil || (requestPeerIP(req).IsLoopback() && req.Header.Get("X-Forwarded-Proto") == "https")
}
