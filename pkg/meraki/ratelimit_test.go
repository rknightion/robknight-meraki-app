package meraki

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a minimal manually-advanced clock for rate-limit tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeSleep records the total duration requested and, using the supplied fakeClock,
// advances virtual time instead of actually sleeping. This guarantees deterministic
// Acquire behaviour when the rate limiter needs to block.
func fakeSleep(clock *fakeClock, total *int64) func(ctx context.Context, d time.Duration) error {
	return func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d <= 0 {
			return nil
		}
		atomic.AddInt64(total, int64(d))
		clock.Advance(d)
		return nil
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimiterConfig{RequestsPerSecond: 0})

	start := time.Now()
	wait, err := rl.Acquire(context.Background(), "orgA")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if wait != 0 {
		t.Fatalf("wait=%v, want 0", wait)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Acquire blocked %v with limiter disabled", elapsed)
	}
}

func TestRateLimiterBurstThenBlocks(t *testing.T) {
	t.Parallel()

	clock := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var sleptNanos int64
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 1,
		Burst:             3,
		JitterRatio:       0, // disable jitter for deterministic wait
		Clock:             clock.Now,
		Sleep:             fakeSleep(clock, &sleptNanos),
	})

	// First 3 calls must return immediately — burst covers them.
	for i := 0; i < 3; i++ {
		wait, err := rl.Acquire(context.Background(), "orgA")
		if err != nil {
			t.Fatalf("call %d: unexpected error %v", i, err)
		}
		if wait != 0 {
			t.Fatalf("call %d: wait=%v, want 0", i, wait)
		}
	}

	// 4th call empties the bucket; with rate=1/s we expect ~1s of wait.
	wait, err := rl.Acquire(context.Background(), "orgA")
	if err != nil {
		t.Fatalf("4th Acquire error: %v", err)
	}
	// Allow slack for discretization (tokens=0 after burst → wait ≈ 1s).
	if wait < 900*time.Millisecond || wait > 1100*time.Millisecond {
		t.Fatalf("4th wait=%v, want ~1s", wait)
	}
	if atomic.LoadInt64(&sleptNanos) != int64(wait) {
		t.Fatalf("sleep hook saw %v total, Acquire reported %v", time.Duration(sleptNanos), wait)
	}
}

func TestRateLimiterJitterDeterministicZero(t *testing.T) {
	t.Parallel()

	clock := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var sleptNanos int64
	// Rand returning 0.5 means (0.5*2 - 1) = 0 → factor = 1.0 → no jitter applied.
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 1,
		Burst:             1,
		JitterRatio:       0.1,
		Clock:             clock.Now,
		Sleep:             fakeSleep(clock, &sleptNanos),
		Rand:              func() float64 { return 0.5 },
	})

	// Drain the single-token burst.
	if _, err := rl.Acquire(context.Background(), "orgA"); err != nil {
		t.Fatalf("priming Acquire error: %v", err)
	}

	// Next call must wait exactly 1s (1 token / 1 rps = 1s) — no jitter.
	wait, err := rl.Acquire(context.Background(), "orgA")
	if err != nil {
		t.Fatalf("Acquire error: %v", err)
	}
	if wait != time.Second {
		t.Fatalf("wait=%v, want exactly 1s (jitter should cancel out)", wait)
	}
}

func TestRateLimiterPerOrgIsolation(t *testing.T) {
	t.Parallel()

	clock := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var sleptNanos int64
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 1,
		Burst:             2,
		JitterRatio:       0,
		Clock:             clock.Now,
		Sleep:             fakeSleep(clock, &sleptNanos),
	})

	// Exhaust orgA's burst (2 free tokens).
	for i := 0; i < 2; i++ {
		wait, err := rl.Acquire(context.Background(), "orgA")
		if err != nil {
			t.Fatalf("orgA prime %d: %v", i, err)
		}
		if wait != 0 {
			t.Fatalf("orgA prime %d wait=%v, want 0", i, wait)
		}
	}

	// orgB is untouched → should fire instantly.
	wait, err := rl.Acquire(context.Background(), "orgB")
	if err != nil {
		t.Fatalf("orgB Acquire error: %v", err)
	}
	if wait != 0 {
		t.Fatalf("orgB wait=%v, expected 0 because orgB bucket is full", wait)
	}
}

func TestRateLimiterParallelSameOrg(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 50
		burst      = 5
		rate       = 50.0 // high enough to make the test fast but still exercise blocking
	)

	clock := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var sleptNanos int64
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: rate,
		Burst:             burst,
		JitterRatio:       0,
		Clock:             clock.Now,
		Sleep:             fakeSleep(clock, &sleptNanos),
	})

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rl.Acquire(context.Background(), "orgShared"); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("goroutine returned %v", err)
		}
	}

	// All goroutines eventually succeed. Total virtual sleep should be at least
	// (N - burst) / rate.
	want := time.Duration(float64(goroutines-burst) / rate * float64(time.Second))
	got := time.Duration(atomic.LoadInt64(&sleptNanos))
	// Because jitter is 0 and all goroutines race, the aggregate sleep equals the sum
	// of each goroutine's wait; it is at least the analytic minimum.
	if got < want {
		t.Fatalf("total sleep %v < expected minimum %v", got, want)
	}
}
