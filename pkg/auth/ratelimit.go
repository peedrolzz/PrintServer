package auth

import (
	"net/http"
	"sync"
	"time"
)

type Limiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string][]time.Time
}

func NewLimiter(window time.Duration, max int) *Limiter {
	return &Limiter{window: window, max: max, hits: make(map[string][]time.Time)}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	in := l.hits[key]
	var kept []time.Time
	for _, t := range in {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	l.hits[key] = kept
	return true
}

func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	var count int
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			count++
		}
	}
	return count >= l.max
}

func (l *Limiter) Record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	var kept []time.Time
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	l.hits[key] = kept
}

func HTTPKey(r *http.Request) string {
	host, _ := SplitFirst(r.RemoteAddr, ":")
	if host == "" {
		host = r.RemoteAddr
	}
	return host
}
