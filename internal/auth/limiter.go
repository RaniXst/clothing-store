package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LoginLimiter ограничивает число запросов входа для ключа в скользящем окне
// и хранит счётчики в памяти процесса.
type LoginLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	maximum int
	now     func() time.Time
	attempt map[string][]time.Time
}

// NewLoginLimiter создаёт ограничитель с максимумом попыток за заданный промежуток времени.
func NewLoginLimiter(maximum int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{maximum: maximum, window: window, now: time.Now, attempt: make(map[string][]time.Time)}
}

// Allow удаляет старые отметки и сообщает, разрешена ли новая попытка для ключа.
// Скользящее окно не позволяет получить новую порцию попыток на границе фиксированного интервала.
// Счётчик в памяти подходит для одного процесса и сбрасывается при перезапуске.
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

// remoteIP извлекает адрес клиента из HTTP-запроса для ограничения попыток входа.
func remoteIP(request *http.Request) string {
	// Порт клиента меняется между подключениями, поэтому ключом служит адрес. Заголовки прокси
	// не используются: без списка доверенных прокси клиент мог бы подставить в них другой адрес.
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(request.RemoteAddr)
}
