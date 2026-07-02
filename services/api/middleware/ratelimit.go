package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateLimiter is a small in-memory, per-key fixed-window limiter. It has no
// external dependencies and is meant for low-cardinality, security-sensitive
// endpoints (e.g. login) — not general API throttling.
type rateLimiter struct {
	mu       sync.Mutex
	hits     map[string][]time.Time
	max      int
	window   time.Duration
	lastGC   time.Time
	gcPeriod time.Duration
}

// RateLimit limits each client IP to max requests per window, returning 429 with a
// Retry-After header when exceeded. Apply it to a specific route (chi's r.With),
// not globally.
func RateLimit(max int, window time.Duration) func(http.Handler) http.Handler {
	rl := &rateLimiter{hits: make(map[string][]time.Time), max: max, window: window, gcPeriod: 10 * window}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientIP(r)) {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Occasionally drop stale keys so the map can't grow unbounded.
	if now.Sub(rl.lastGC) > rl.gcPeriod {
		for k, ts := range rl.hits {
			if len(ts) == 0 || ts[len(ts)-1].Before(cutoff) {
				delete(rl.hits, k)
			}
		}
		rl.lastGC = now
	}

	kept := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.max {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now)
	return true
}

// clientIP prefers the IP set by the RealIP middleware (RemoteAddr), stripping any
// port. Falls back to the raw RemoteAddr.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
