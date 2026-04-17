package meraki

import (
	"strings"
	"testing"
	"time"
)

// fixedNow returns a closure that always reports t — useful for deterministic freshness tests.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestResolveHappyPathNoResolution(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	// Put `to` safely before the freshness cutoff so it is not adjusted.
	to := now.Add(-2 * FreshnessFloor)
	from := to.Add(-6 * time.Hour)

	s := EndpointTimeRange{MaxTimespan: 24 * time.Hour}
	w, err := s.Resolve(from, to, 0, fixedNow(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.T0.Equal(from) {
		t.Fatalf("T0=%v, want %v", w.T0, from)
	}
	if !w.T1.Equal(to) {
		t.Fatalf("T1=%v, want %v", w.T1, to)
	}
	if w.Timespan != 6*time.Hour {
		t.Fatalf("Timespan=%v, want 6h", w.Timespan)
	}
	if w.Resolution != 0 {
		t.Fatalf("Resolution=%v, want 0 (no allowed resolutions)", w.Resolution)
	}
	if w.Truncated {
		t.Fatal("Truncated=true; want false")
	}
	if len(w.Annotations) != 0 {
		t.Fatalf("Annotations=%v, want none", w.Annotations)
	}
}

func TestResolveHappyPathWithResolution(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	to := now.Add(-2 * FreshnessFloor)
	from := to.Add(-6 * time.Hour)

	s := EndpointTimeRange{
		MaxTimespan: 24 * time.Hour,
		AllowedResolutions: []time.Duration{
			60 * time.Second,
			5 * time.Minute,
			15 * time.Minute,
			1 * time.Hour,
		},
	}

	// maxDataPoints=100 → desired = 6h / 100 = 216s → should quantize up to 5m.
	w, err := s.Resolve(from, to, 100, fixedNow(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Resolution != 5*time.Minute {
		t.Fatalf("Resolution=%v, want 5m", w.Resolution)
	}
}

func TestResolveTruncation(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	to := now.Add(-2 * FreshnessFloor)
	from := to.Add(-48 * time.Hour) // 48h window

	s := EndpointTimeRange{MaxTimespan: 24 * time.Hour}
	w, err := s.Resolve(from, to, 0, fixedNow(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.Truncated {
		t.Fatal("Truncated=false; want true")
	}
	if w.Timespan != 24*time.Hour {
		t.Fatalf("Timespan=%v, want 24h", w.Timespan)
	}
	expectedT0 := to.Add(-24 * time.Hour)
	if !w.T0.Equal(expectedT0) {
		t.Fatalf("T0=%v, want %v", w.T0, expectedT0)
	}
	found := false
	for _, a := range w.Annotations {
		if strings.Contains(a, "truncated") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a 'truncated' annotation, got %v", w.Annotations)
	}
}

func TestResolveFreshnessFloor(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	from := now.Add(-1 * time.Hour)
	to := now // to == now triggers the freshness floor adjustment

	s := EndpointTimeRange{MaxTimespan: 24 * time.Hour}
	w, err := s.Resolve(from, to, 0, fixedNow(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedT1 := now.Add(-FreshnessFloor)
	if !w.T1.Equal(expectedT1) {
		t.Fatalf("T1=%v, want now-%v (%v)", w.T1, FreshnessFloor, expectedT1)
	}
	if w.Timespan <= 0 {
		t.Fatalf("Timespan=%v, want > 0", w.Timespan)
	}
}

func TestResolveFreshnessCollapseSafeguard(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	// `from` is only 10s before `to`, and `to == now`. Freshness cutoff = now-60s, which
	// pushes `to` to now-60s. That is earlier than `from`, triggering the safeguard that
	// pulls `from` back to `to - 2*FreshnessFloor`.
	to := now
	from := now.Add(-10 * time.Second)

	s := EndpointTimeRange{MaxTimespan: 24 * time.Hour}
	w, err := s.Resolve(from, to, 0, fixedNow(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedT1 := now.Add(-FreshnessFloor)
	if !w.T1.Equal(expectedT1) {
		t.Fatalf("T1=%v, want %v", w.T1, expectedT1)
	}
	// from should have been pulled backward to T1 - 2*FreshnessFloor.
	expectedT0 := expectedT1.Add(-2 * FreshnessFloor)
	if !w.T0.Equal(expectedT0) {
		t.Fatalf("T0=%v, want %v (from adjusted back)", w.T0, expectedT0)
	}
	if w.Timespan <= 0 {
		t.Fatalf("Timespan=%v, want > 0 after collapse safeguard", w.Timespan)
	}
}

func TestResolveZeroFromError(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	s := EndpointTimeRange{MaxTimespan: 24 * time.Hour}
	_, err := s.Resolve(time.Time{}, now.Add(-2*FreshnessFloor), 0, fixedNow(now))
	if err == nil {
		t.Fatal("expected error for zero `from` time")
	}
}

func TestResolveFromGreaterOrEqualToError(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	s := EndpointTimeRange{MaxTimespan: 24 * time.Hour}

	// from > to
	to := now.Add(-1 * time.Hour)
	from := now
	if _, err := s.Resolve(from, to, 0, fixedNow(now)); err == nil {
		t.Fatal("expected error when from > to")
	}

	// from == to
	equal := now.Add(-2 * time.Hour)
	if _, err := s.Resolve(equal, equal, 0, fixedNow(now)); err == nil {
		t.Fatal("expected error when from == to")
	}
}

func TestQuantizeUp(t *testing.T) {
	t.Parallel()

	allowed := []time.Duration{
		60 * time.Second,
		5 * time.Minute,
		15 * time.Minute,
		1 * time.Hour,
	}

	cases := []struct {
		name    string
		desired time.Duration
		allowed []time.Duration
		want    time.Duration
	}{
		{"empty allowed", time.Second, nil, 0},
		{"empty allowed zero slice", time.Second, []time.Duration{}, 0},
		{"desired zero returns smallest", 0, allowed, 60 * time.Second},
		{"desired negative returns smallest", -time.Second, allowed, 60 * time.Second},
		{"desired larger than largest returns largest", 48 * time.Hour, allowed, 1 * time.Hour},
		{"desired exact match returns same value", 5 * time.Minute, allowed, 5 * time.Minute},
		{"desired between quantizes up", 4 * time.Minute, allowed, 5 * time.Minute},
		{"desired just above bucket", 5*time.Minute + time.Second, allowed, 15 * time.Minute},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := quantizeUp(tc.desired, tc.allowed)
			if got != tc.want {
				t.Fatalf("quantizeUp(%v, %v) = %v, want %v",
					tc.desired, tc.allowed, got, tc.want)
			}
		})
	}
}

func TestKnownEndpointRangesPresence(t *testing.T) {
	t.Parallel()

	// Smoke test — ensure the authoritative map exists and has at least one entry.
	if len(KnownEndpointRanges) == 0 {
		t.Fatal("KnownEndpointRanges should not be empty")
	}
	// Pick a known key and sanity-check its MaxTimespan is set.
	if v, ok := KnownEndpointRanges["organizations/{organizationId}/apiRequests"]; !ok || v.MaxTimespan == 0 {
		t.Fatal("expected organizations/{organizationId}/apiRequests to be populated")
	}
}
