// Package ratelimit implements rate limiting adapters for VibeWarden.
package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// limiterEntry holds a token bucket limiter together with the last time it was accessed.
// It is used by MemoryStore to track per-key state and to evict stale entries.
type limiterEntry struct {
	limiter  *rate.Limiter
	mu       sync.Mutex
	lastSeen time.Time
}

// touch updates the lastSeen timestamp to now, under the entry's own mutex.
func (e *limiterEntry) touch() {
	e.mu.Lock()
	e.lastSeen = time.Now()
	e.mu.Unlock()
}

// seenBefore returns the time at which the entry was last accessed.
func (e *limiterEntry) seenBefore() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastSeen
}

// MemoryStore implements ports.RateLimiter using an in-memory token bucket per key.
// It is safe for concurrent use. A background goroutine periodically removes entries
// that have not been accessed within entryTTL. Call Close to stop the goroutine.
type MemoryStore struct {
	limiters sync.Map // map[string]*limiterEntry

	rule     ports.RateLimitRule
	entryTTL time.Duration

	done    chan struct{}
	once    sync.Once
	cleanWG sync.WaitGroup
}

// NewMemoryStore creates a new MemoryStore configured with the supplied rule.
//
//   - cleanupInterval: how often the background GC goroutine runs.
//   - entryTTL: how long an entry may be unused before it is evicted.
//
// The caller must call Close when the store is no longer needed to stop the background goroutine.
func NewMemoryStore(rule ports.RateLimitRule, cleanupInterval, entryTTL time.Duration) *MemoryStore {
	s := &MemoryStore{
		rule:     rule,
		entryTTL: entryTTL,
		done:     make(chan struct{}),
	}

	s.cleanWG.Add(1)
	go s.runCleanup(cleanupInterval)

	return s
}

// Allow implements ports.RateLimiter.
// It returns a RateLimitResult indicating whether the request identified by key is
// permitted. The key is typically a client IP address or an authenticated user ID.
func (s *MemoryStore) Allow(ctx context.Context, key string) ports.RateLimitResult {
	entry := s.getOrCreate(key)
	entry.touch()

	r := s.rule
	rsv := entry.limiter.Reserve()

	remaining := 0
	if tokens := entry.limiter.Tokens(); tokens >= 0 {
		remaining = int(math.Floor(tokens))
	}

	retryAfter := time.Duration(0)
	allowed := rsv.OK() && rsv.Delay() == 0

	if !allowed {
		// Cancel the reservation so we do not consume a token for a rejected request.
		rsv.Cancel()
		retryAfter = rsv.Delay()
		if retryAfter < 0 {
			retryAfter = 0
		}
		remaining = 0
	}

	return ports.RateLimitResult{
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		Limit:      r.RequestsPerSecond,
		Burst:      r.Burst,
	}
}

// Close stops the background GC goroutine. It is safe to call Close more than once.
// Implements ports.RateLimiter.
func (s *MemoryStore) Close() error {
	s.once.Do(func() {
		close(s.done)
	})
	s.cleanWG.Wait()
	return nil
}

// getOrCreate returns the limiterEntry for key, creating it if it does not yet exist.
func (s *MemoryStore) getOrCreate(key string) *limiterEntry {
	if v, ok := s.limiters.Load(key); ok {
		return v.(*limiterEntry)
	}

	r := s.rule
	lim := rate.NewLimiter(rate.Limit(r.RequestsPerSecond), r.Burst)

	entry := &limiterEntry{
		limiter:  lim,
		lastSeen: time.Now(),
	}

	// LoadOrStore handles the race: if another goroutine inserted between our Load and
	// Store calls, we use the entry that won.
	actual, _ := s.limiters.LoadOrStore(key, entry)
	return actual.(*limiterEntry)
}

// runCleanup runs the background goroutine that evicts stale entries.
func (s *MemoryStore) runCleanup(interval time.Duration) {
	defer s.cleanWG.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.evict()
		case <-s.done:
			return
		}
	}
}

// evict removes all entries that have not been accessed within entryTTL.
func (s *MemoryStore) evict() {
	cutoff := time.Now().Add(-s.entryTTL)
	s.limiters.Range(func(k, v any) bool {
		entry := v.(*limiterEntry)
		if entry.seenBefore().Before(cutoff) {
			s.limiters.Delete(k)
		}
		return true
	})
}
