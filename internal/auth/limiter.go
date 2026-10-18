package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LoginLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	maximum int
	now     func() time.Time
	attempt map[string][]time.Time
}

func NewLoginLimiter(maximum int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{maximum: maximum, window: window, now: time.Now, attempt: make(map[string][]time.Time)}
}

func (l *LoginLimiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	previous := l.attempt[key]
	kept := previous[:0]
	for _, stamp := range previous {
		if stamp.After(cutoff) {
			kept = append(kept, stamp)
		}
	}
	if len(kept) >= l.maximum {
		l.attempt[key] = kept
		return false
	}
	l.attempt[key] = append(kept, now)
	if len(l.attempt) > 10000 {
		for bucket, stamps := range l.attempt {
			if len(stamps) == 0 || !stamps[len(stamps)-1].After(cutoff) {
				delete(l.attempt, bucket)
			}
		}
	}
	return true
}

func remoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(request.RemoteAddr)
}
