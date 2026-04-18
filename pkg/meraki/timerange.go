package meraki

import (
	"fmt"
	"time"
)

// AllowedResolutions lists the seconds-values a particular endpoint accepts for its `resolution`
// parameter. Meraki rejects arbitrary values with 400s, so we quantize upward to the nearest
// allowed bucket.
type EndpointTimeRange struct {
	// MaxTimespan caps (t1 - t0). A zero value means the endpoint does not accept timespan.
	MaxTimespan time.Duration
	// AllowedResolutions must be sorted ascending. An empty slice means the endpoint has no
	// resolution parameter.
	AllowedResolutions []time.Duration
}

// FreshnessFloor is subtracted from "now" before computing t1. Meraki's "latest" samples lag
// 30-120s; using Grafana's exact `now` often returns empty tails.
const FreshnessFloor = 60 * time.Second

// TimeRangeWindow describes a normalized Meraki time range for a single endpoint query.
type TimeRangeWindow struct {
	T0, T1      time.Time
	Timespan    time.Duration
	Resolution  time.Duration
	Truncated   bool // true if the incoming window was clamped to MaxTimespan
	Annotations []string
}

// ResolveTimeRange clamps, quantizes, and applies the freshness floor given Grafana's
// from/to and the endpoint's limits. maxDataPoints mirrors the value Grafana passes on the
// query (typically the width of the panel in pixels); it may be 0 which falls back to the
// smallest allowed resolution.
func (s EndpointTimeRange) Resolve(from, to time.Time, maxDataPoints int64, now func() time.Time) (TimeRangeWindow, error) {
	if now == nil {
		now = time.Now
	}
	if to.IsZero() {
		to = now()
	}
	if from.IsZero() {
		return TimeRangeWindow{}, fmt.Errorf("missing from time")
	}
	if !from.Before(to) {
		return TimeRangeWindow{}, fmt.Errorf("from >= to")
	}

	// Apply freshness floor: if `to` is within the last FreshnessFloor, pull it back.
	cutoff := now().Add(-FreshnessFloor)
	if to.After(cutoff) {
		to = cutoff
	}
	if !from.Before(to) {
		// Freshness floor collapsed the window; expand it back to 2× the floor so we
		// still return something usable.
		from = to.Add(-2 * FreshnessFloor)
	}

	w := TimeRangeWindow{T0: from, T1: to, Timespan: to.Sub(from)}

	if s.MaxTimespan > 0 && w.Timespan > s.MaxTimespan {
		w.T0 = w.T1.Add(-s.MaxTimespan)
		w.Timespan = s.MaxTimespan
		w.Truncated = true
		w.Annotations = append(w.Annotations, fmt.Sprintf(
			"window truncated to endpoint max timespan of %s", s.MaxTimespan))
	}

	if len(s.AllowedResolutions) > 0 {
		var desired time.Duration
		if maxDataPoints > 0 {
			desired = w.Timespan / time.Duration(maxDataPoints)
		}
		w.Resolution = quantizeUp(desired, s.AllowedResolutions)
	}

	return w, nil
}

// quantizeUp returns the smallest allowed resolution >= desired. If desired is <= 0 or
// smaller than the smallest allowed, returns the smallest. If desired exceeds every allowed
// bucket, returns the largest.
func quantizeUp(desired time.Duration, allowed []time.Duration) time.Duration {
	if len(allowed) == 0 {
		return 0
	}
	if desired <= 0 {
		return allowed[0]
	}
	for _, a := range allowed {
		if a >= desired {
			return a
		}
	}
	return allowed[len(allowed)-1]
}

// KnownEndpointRanges is the authoritative per-endpoint rate-limit table for the v0.1 scope.
// Extended in later phases as more endpoints are wired up.
//
// Keys are the logical endpoint path used in pkg/meraki (the path after /api/v1). Values
// reflect Meraki's published limits as of the Dashboard API v1.
var KnownEndpointRanges = map[string]EndpointTimeRange{
	"organizations/{organizationId}/sensor/readings/history": {
		MaxTimespan: 730 * 24 * time.Hour, // 2 years
		AllowedResolutions: []time.Duration{
			60 * time.Second,
			5 * time.Minute,
			15 * time.Minute,
			1 * time.Hour,
			6 * time.Hour,
			24 * time.Hour,
		},
	},
	"organizations/{organizationId}/apiRequests": {
		MaxTimespan: 31 * 24 * time.Hour,
	},
	"organizations/{organizationId}/apiRequests/overview": {
		MaxTimespan: 31 * 24 * time.Hour,
	},
	"networks/{networkId}/wireless/usageHistory": {
		MaxTimespan: 31 * 24 * time.Hour,
		AllowedResolutions: []time.Duration{
			5 * time.Minute,
			10 * time.Minute,
			15 * time.Minute,
			30 * time.Minute,
			1 * time.Hour,
			2 * time.Hour,
			4 * time.Hour,
			24 * time.Hour,
		},
	},
	"organizations/{organizationId}/wireless/devices/channelUtilization/history": {
		MaxTimespan: 31 * 24 * time.Hour,
		AllowedResolutions: []time.Duration{
			10 * time.Minute,
			20 * time.Minute,
			1 * time.Hour,
			1 * time.Hour,
		},
	},
	"organizations/{organizationId}/devices/uplinksLossAndLatency": {
		MaxTimespan: 5 * time.Minute,
	},
	// Phase 8 — appliance VPN stats aggregates latency/jitter/loss per peer
	// over the window; no resolution parameter.
	"organizations/{organizationId}/appliance/vpn/stats": {
		MaxTimespan: 31 * 24 * time.Hour,
	},
	// Phase 9 — organization client overview.
	"organizations/{organizationId}/clients/overview": {
		MaxTimespan: 31 * 24 * time.Hour,
		AllowedResolutions: []time.Duration{
			2 * time.Hour,
			24 * time.Hour,
			7 * 24 * time.Hour,
			30 * 24 * time.Hour, // 2629746s = ~30.44 days (monthly bucket)
		},
	},
	// Phase 9 — API requests byInterval chart.
	"organizations/{organizationId}/apiRequests/overview/responseCodes/byInterval": {
		MaxTimespan: 31 * 24 * time.Hour,
		AllowedResolutions: []time.Duration{
			2 * time.Minute,
			1 * time.Hour,
			4 * time.Hour,
			6 * time.Hour,
		},
	},
	// Phase 9 — top-N usage summaries.
	"organizations/{organizationId}/summary/top/clients/byUsage": {
		MaxTimespan: 186 * 24 * time.Hour,
	},
	"organizations/{organizationId}/summary/top/devices/byUsage": {
		MaxTimespan: 186 * 24 * time.Hour,
	},
	"organizations/{organizationId}/summary/top/devices/models/byUsage": {
		MaxTimespan: 186 * 24 * time.Hour,
	},
	"organizations/{organizationId}/summary/top/ssids/byUsage": {
		MaxTimespan: 186 * 24 * time.Hour,
	},
	// Top switches by energy requires a 25-minute floor per spec; Resolve()
	// doesn't enforce a floor, handlers should use a 24h default when the
	// user-picked window is shorter.
	"organizations/{organizationId}/summary/top/switches/byEnergyUsage": {
		MaxTimespan: 186 * 24 * time.Hour,
	},
	// Phase 12 (§7.3-C) — configurationChanges: Meraki's documented lookback is 365 days.
	"organizations/{organizationId}/configurationChanges": {
		MaxTimespan: 365 * 24 * time.Hour,
	},
	// Phase 12 (§7.3-D) — device availability change history: Meraki's documented max is
	// 31 days; default 1 day when unset. Handlers compute t0/t1 from the panel range and
	// Resolve() clamps to MaxTimespan.
	"organizations/{organizationId}/devices/availabilities/changeHistory": {
		MaxTimespan: 31 * 24 * time.Hour,
	},
}
