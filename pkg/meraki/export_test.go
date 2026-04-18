package meraki

import "time"

// SetClock replaces the cache's clock for deterministic TTL expiry tests.
// This is only available to _test.go files because the symbol is defined here.
func (c *TTLCache) SetClock(fn func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.Clock = fn
}

// SetRand replaces the cache's random source for deterministic jitter tests.
func (c *TTLCache) SetRand(fn func() float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.Rand = fn
}

// SwitchPortStatusOptionsValues exposes the private values() builder for tests.
func SwitchPortStatusOptionsValues(o SwitchPortStatusOptions) map[string][]string {
	return o.values()
}
