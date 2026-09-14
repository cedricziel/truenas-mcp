package oauth

import (
	"sync"
	"time"
)

// ReplayCache enforces single-use of an identifier -- an authorization
// code's, specifically -- for a bounded time.
//
// It is process-local, in-memory state, swept lazily on each Claim rather
// than by a background goroutine, so nothing needs to be shut down when the
// server stops. It does not protect against replay across multiple
// replicas of the server; see design.md's Risks / Trade-offs.
type ReplayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time // id -> claimed-until

	now func() time.Time // overridden in tests
}

// NewReplayCache returns an empty cache.
func NewReplayCache() *ReplayCache {
	return &ReplayCache{seen: map[string]time.Time{}, now: time.Now}
}

// Claim reports whether id has not been claimed before, and if so marks it
// claimed until ttl passes. A code redeemed twice within its own validity
// window is rejected on the second attempt; one redeemed after ttl has
// already elapsed was going to be rejected as expired by DecodeCode anyway,
// so losing the claim record across a restart is not a security regression.
func (c *ReplayCache) Claim(id string, ttl time.Duration) bool {
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.sweepLocked(now)

	if until, claimed := c.seen[id]; claimed && now.Before(until) {
		return false
	}
	c.seen[id] = now.Add(ttl)
	return true
}

func (c *ReplayCache) sweepLocked(now time.Time) {
	for id, until := range c.seen {
		if !now.Before(until) {
			delete(c.seen, id)
		}
	}
}
