package meraki

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// RateLimiterConfig configures a per-organization token bucket rate limiter.
//
// The defaults (RequestsPerSecond=10, Burst=20, SharedFraction=1.0, JitterRatio=0.1) reflect
// Meraki's documented cap of 10 requests per second per organization. When multiple Grafana
// replicas share the same API key, set SharedFraction to 1/N so the combined throughput stays
// under the server-side ceiling.
type RateLimiterConfig struct {
	RequestsPerSecond float64
	Burst             int
	SharedFraction    float64
	JitterRatio       float64
	// Clock is an optional override used by tests. Defaults to time.Now.
	Clock func() time.Time
	// Sleep is an optional override used by tests. Defaults to context-aware sleep.
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand is an optional random source for jitter. Defaults to a seeded rand.Rand.
	Rand func() float64
}

// RateLimiter implements a per-orgID token bucket with configurable jitter on wait.
//
// It is safe for concurrent use. An empty orgID falls back to a single shared bucket keyed
// by the literal "global".
type RateLimiter struct {
	cfg RateLimiterConfig

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter constructs a RateLimiter from cfg. Zero / negative values are replaced with
// safe defaults. A RequestsPerSecond of 0 means "disabled" (Acquire returns immediately).
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	if cfg.SharedFraction <= 0 {
		cfg.SharedFraction = 1.0
	}
	if cfg.SharedFraction > 1 {
		cfg.SharedFraction = 1.0
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 20
	}
	if cfg.JitterRatio < 0 {
		cfg.JitterRatio = 0
	}
	if cfg.JitterRatio > 1 {
		cfg.JitterRatio = 1
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepCtx
	}
	if cfg.Rand == nil {
		r := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // jitter, not crypto
		var rmu sync.Mutex
		cfg.Rand = func() float64 {
			rmu.Lock()
			defer rmu.Unlock()
			return r.Float64()
		}
	}
	return &RateLimiter{cfg: cfg, buckets: map[string]*bucket{}}
}

// Acquire blocks until a token is available for orgID (or ctx is cancelled). Returns the total
// time waited. When the limiter is disabled (RequestsPerSecond == 0), returns 0 immediately.
func (r *RateLimiter) Acquire(ctx context.Context, orgID string) (time.Duration, error) {
	rate := r.cfg.RequestsPerSecond * r.cfg.SharedFraction
	if rate <= 0 {
		return 0, nil
	}
	key := orgID
	if key == "" {
		key = "global"
	}
	burst := float64(r.cfg.Burst)

	var total time.Duration
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		r.mu.Lock()
		b, ok := r.buckets[key]
		now := r.cfg.Clock()
		if !ok {
			b = &bucket{tokens: burst, last: now}
			r.buckets[key] = b
		}
		elapsed := now.Sub(b.last).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		b.tokens = min64(burst, b.tokens+elapsed*rate)
		b.last = now

		if b.tokens >= 1 {
			b.tokens--
			r.mu.Unlock()
			return total, nil
		}
		deficit := 1 - b.tokens
		wait := time.Duration((deficit / rate) * float64(time.Second))
		wait = r.applyJitter(wait)
		r.mu.Unlock()

		total += wait
		if err := r.cfg.Sleep(ctx, wait); err != nil {
			return total, err
		}
	}
}

func (r *RateLimiter) applyJitter(d time.Duration) time.Duration {
	if d <= 0 || r.cfg.JitterRatio == 0 {
		return d
	}
	// Jitter the wait by ±JitterRatio of its value. Never returns a negative duration.
	factor := 1.0 + (r.cfg.Rand()*2-1)*r.cfg.JitterRatio
	if factor < 0 {
		factor = 0
	}
	return time.Duration(float64(d) * factor)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
