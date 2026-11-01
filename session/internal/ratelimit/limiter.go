// Package ratelimit provides a bounded fixed-window request limiter. It is
// intentionally simple: no shared state, no background goroutines.
package ratelimit

import (
	"sync"
	"time"
)

const defaultMaxKeys = 10000

// Limiter allows at most limit requests per key in each window.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time
	buckets map[string]window
}

type window struct {
	start time.Time
	count int
}

// New returns a limiter. A non-positive window defaults to one minute.
func New(limit int, span time.Duration) *Limiter {
	if span <= 0 {
		span = time.Minute
	}
	return &Limiter{
		limit:   limit,
		window:  span,
		maxKeys: defaultMaxKeys,
		now:     time.Now,
		buckets: make(map[string]window),
	}
}

// Allow records a request for key. When the request is denied it also returns
// how long the caller should wait before retrying.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.limit <= 0 {
		return false, l.window
	}

	entry, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.pruneLocked(now)
			if len(l.buckets) >= l.maxKeys {
				return false, l.window
			}
		}
		entry = window{start: now}
	} else if now.Sub(entry.start) >= l.window {
		entry = window{start: now}
	}

	if entry.count >= l.limit {
		l.buckets[key] = entry
		retry := entry.start.Add(l.window).Sub(now)
		if retry < 0 {
			retry = 0
		}
		return false, retry
	}
	entry.count++
	l.buckets[key] = entry
	return true, 0
}

func (l *Limiter) pruneLocked(now time.Time) {
	for key, entry := range l.buckets {
		if now.Sub(entry.start) >= l.window {
			delete(l.buckets, key)
		}
	}
}
