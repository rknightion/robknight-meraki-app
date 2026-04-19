package meraki

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// Warmer keeps the navigation-spine cache entries — `organizations` and
// per-org `organizationNetworks` — fresh in the background so first paint
// on a cold page never pays the Meraki round-trip.
//
// The cache layer already implements stale-while-revalidate (SWR) and
// singleflight coalescing; the warmer is additive on top of those:
//
//   - SWR returns stale data immediately past TTL while triggering an
//     async refresh, so the SECOND request after expiry is fast.
//   - The warmer refreshes the entry BEFORE expiry so the FIRST request
//     after the dashboard loads is also a cache hit.
//
// Scope is intentionally narrow. Devices (5m TTL), alerts (30s) and
// sensor readings (30s) are page-specific and the existing SWR +
// singleflight already cover them; pre-warming them would multiply
// goroutine count without a clear cold-start payoff.
//
// Concurrency model: a single goroutine runs the warm loop. RefreshOnce
// is the unit of work and is exposed publicly so tests can drive a pass
// directly without time.Sleep flake.
type Warmer struct {
	client *Client
	logger log.Logger
	cfg    WarmerConfig

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

// WarmerConfig parameterises Warmer. Zero values fall back to defaults
// chosen to match the cache TTLs in pkg/meraki/CLAUDE.md.
type WarmerConfig struct {
	// OrgsTTL is the TTL passed to GetOrganizations. Default: 1h.
	OrgsTTL time.Duration
	// NetworksTTL is the TTL passed to GetOrganizationNetworks. Default: 15m.
	NetworksTTL time.Duration
	// Interval is how often the warm loop runs. Default: min(OrgsTTL,
	// NetworksTTL) / 2 — half-TTL guarantees the cache is refreshed before
	// even SWR kicks in. Capped to a minimum of 30s so pathological
	// configs (zero/tiny TTLs) don't melt the rate limiter.
	Interval time.Duration
	// Sleep is overridable for tests. Production passes nil → time.Sleep
	// gated on a context-aware select. Tests can substitute a fake that
	// returns immediately and signals an external counter.
	Sleep func(ctx context.Context, d time.Duration)
}

const (
	defaultWarmerOrgsTTL     = time.Hour
	defaultWarmerNetworksTTL = 15 * time.Minute
	minWarmerInterval        = 30 * time.Second
)

// NewWarmer constructs a Warmer. It does NOT start the loop — call Start
// after registering it with the App so Dispose() can call Stop().
func NewWarmer(client *Client, cfg WarmerConfig, logger log.Logger) *Warmer {
	if cfg.OrgsTTL <= 0 {
		cfg.OrgsTTL = defaultWarmerOrgsTTL
	}
	if cfg.NetworksTTL <= 0 {
		cfg.NetworksTTL = defaultWarmerNetworksTTL
	}
	if cfg.Interval <= 0 {
		cfg.Interval = min(cfg.OrgsTTL, cfg.NetworksTTL) / 2
	}
	if cfg.Interval < minWarmerInterval {
		cfg.Interval = minWarmerInterval
	}
	if cfg.Sleep == nil {
		cfg.Sleep = ctxSleep
	}
	if logger == nil {
		logger = log.DefaultLogger
	}
	return &Warmer{
		client: client,
		logger: logger,
		cfg:    cfg,
		done:   make(chan struct{}),
	}
}

// Start launches the warm loop. Subsequent calls are no-ops; the lifetime
// of the loop is bound to the context passed to the first call. Cancel
// either via Stop or by cancelling the parent context.
func (w *Warmer) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		w.cancel = cancel
		go w.loop(loopCtx)
	})
}

// Stop signals shutdown and waits for the loop to exit. Safe to call
// before Start (no-op) and idempotent across multiple calls.
func (w *Warmer) Stop() {
	w.stopOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
			<-w.done
		} else {
			close(w.done)
		}
	})
}

// RefreshOnce runs a single warm pass: fetch organizations, then per-org
// networks. Errors per call are logged but do not abort the pass —
// transient failures on one org should not starve siblings. Returns the
// first error encountered so callers (mostly tests) can assert.
func (w *Warmer) RefreshOnce(ctx context.Context) error {
	orgs, err := w.client.GetOrganizations(ctx, w.cfg.OrgsTTL)
	if err != nil {
		w.logger.Warn("warmer: GetOrganizations failed", "err", err)
		return err
	}
	var firstErr error
	for _, org := range orgs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, e := w.client.GetOrganizationNetworks(ctx, org.ID, nil, w.cfg.NetworksTTL); e != nil {
			w.logger.Warn("warmer: GetOrganizationNetworks failed", "orgId", org.ID, "err", e)
			if firstErr == nil {
				firstErr = e
			}
		}
	}
	return firstErr
}

// loop is the long-running body of the warmer goroutine. The first pass
// fires immediately so a freshly-restarted plugin warms its cache before
// the operator clicks any page.
func (w *Warmer) loop(ctx context.Context) {
	defer close(w.done)
	for {
		_ = w.RefreshOnce(ctx)
		w.cfg.Sleep(ctx, w.cfg.Interval)
		if ctx.Err() != nil {
			return
		}
	}
}

// ctxSleep blocks for d, returning early if ctx is cancelled. Used as the
// default Sleep when WarmerConfig.Sleep is nil.
func ctxSleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
