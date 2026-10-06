package server

import (
	"math"
	"sync"
	"time"
)

// limiter is a small token-bucket rate limiter keyed by string (a user id or a
// client address). It is deliberately simple: one mutex, lazy refill, periodic
// pruning so the map cannot grow without bound.
type limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(rate, burst float64, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: burst, now: now, buckets: map[string]*bucket{}}
}

func (l *limiter) refill(key string) *bucket {
	t := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, at: t}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+t.Sub(b.at).Seconds()*l.rate)
	b.at = t
	if t.Sub(l.lastGC) > 10*time.Minute {
		l.lastGC = t
		for k, o := range l.buckets {
			if t.Sub(o.at) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
	}
	return b
}

// allow consumes one token. When it returns false, retry says how long to wait.
func (l *limiter) allow(key string) (ok bool, retry time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key)
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// peek reports whether a token is available without consuming it.
func (l *limiter) peek(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refill(key).tokens >= 1
}
