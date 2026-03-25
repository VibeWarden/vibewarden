package ratelimit

import (
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// DefaultCleanupInterval is the default interval between GC runs in MemoryStore.
const DefaultCleanupInterval = time.Minute

// DefaultEntryTTL is the default duration after which an unused entry is evicted.
const DefaultEntryTTL = 10 * time.Minute

// MemoryFactory implements ports.RateLimiterFactory and creates MemoryStore instances.
// All stores share the same cleanupInterval and entryTTL but receive their own rule.
type MemoryFactory struct {
	cleanupInterval time.Duration
	entryTTL        time.Duration
}

// NewMemoryFactory creates a MemoryFactory.
//
//   - cleanupInterval: how often each created MemoryStore runs its GC pass.
//   - entryTTL: how long an entry may be idle before it is evicted.
func NewMemoryFactory(cleanupInterval, entryTTL time.Duration) *MemoryFactory {
	return &MemoryFactory{
		cleanupInterval: cleanupInterval,
		entryTTL:        entryTTL,
	}
}

// NewDefaultMemoryFactory creates a MemoryFactory with production-suitable defaults:
// 1-minute cleanup interval and 10-minute entry TTL.
func NewDefaultMemoryFactory() *MemoryFactory {
	return NewMemoryFactory(DefaultCleanupInterval, DefaultEntryTTL)
}

// NewLimiter implements ports.RateLimiterFactory.
// It returns a new MemoryStore configured with the supplied rule.
func (f *MemoryFactory) NewLimiter(rule ports.RateLimitRule) ports.RateLimiter {
	return NewMemoryStore(rule, f.cleanupInterval, f.entryTTL)
}
