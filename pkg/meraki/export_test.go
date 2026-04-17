package meraki

import "time"

// SetClock replaces the cache's clock for deterministic TTL expiry tests.
// This is only available to _test.go files because the symbol is defined here.
func (c *TTLCache) SetClock(fn func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock = fn
}
