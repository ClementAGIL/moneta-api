// In-memory token buckets per client IP (one server process, so no shared store is needed).
package httpapi

import (
	"sync"
	"time"
)

const (
	loginPerMinute    = 5.0
	loginBurst        = 10.0
	registerPerMinute = 0.1 // one every 10 minutes
	registerBurst     = 3.0
	importPerMinute   = 0.5  // 30 uploads an hour per user…
	importBurst       = 20.0 // …after a first batch of 20
	idleBucketTTL     = time.Hour
)

type bucket struct {
	tokens float64
	last   time.Time
}

type limiter struct {
	mu       sync.Mutex
	perSec   float64
	burst    float64
	buckets  map[string]*bucket
	lastSwep time.Time
}

func newLimiter(perMinute, burst float64) *limiter {
	return &limiter{perSec: perMinute / 60, burst: burst, buckets: map[string]*bucket{}, lastSwep: time.Now()}
}

// allow takes one token from key's bucket, refilled continuously up to burst.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.perSec)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops idle buckets so the map cannot grow forever.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.lastSwep) < idleBucketTTL {
		return
	}
	for key, b := range l.buckets {
		if now.Sub(b.last) > idleBucketTTL {
			delete(l.buckets, key)
		}
	}
	l.lastSwep = now
}
