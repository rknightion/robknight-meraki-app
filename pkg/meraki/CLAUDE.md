# Meraki API client (`pkg/meraki/`)

Low-level HTTP client, rate limiter, cache, time-range logic, and endpoint wrappers. Owned by the app plugin and shared across every query and every dashboard panel.

## Files

```
client.go               Client + ClientConfig; Do (single request), Get (cached), GetAll (Link pagination)
ratelimit.go            RateLimiter — per-org token bucket, 10 rps default, burst 20, ±10% jitter
cache.go                TTLCache — hashicorp/golang-lru/v2 with TTL; CacheKey = sha256(orgID + path + sorted params)
timerange.go            EndpointTimeRange, KnownEndpointRanges, Resolve, quantizeUp, FreshnessFloor (60s)
pagination.go           nextLink() — parses Link: <...>; rel="next" header
errors.go               Typed errors: UnauthorizedError, NotFoundError, RateLimitError, ServerError, PartialSuccessError
warmer.go               Opt-in background cache pre-warmer. Loops on jittered intervals pulling a
                        configurable catalog of endpoints, so user-facing panels hit warm cache
                        instead of blocking on a fresh fetch. Disabled by default.
version.go              `UserAgent()` + version constant baked in at build time.
export_test.go          Exposes internals for tests (TTLCache.clock override, etc).
administered.go         GetAdministered (current API key identity — used by CheckHealth)
organizations.go        GetOrganizations, GetOrganization
networks.go             GetOrganizationNetworks
devices.go              GetOrganizationDevices*, availabilities, memory/CPU history
sensor.go               MT sensor readings (latest + history) + floor plan
wireless.go             MR endpoints: channel util, usage, SSIDs, per-AP client counts, packet loss,
                        ethernet statuses, CPU load, client-count/latency/failed-connection history,
                        radio status
switches.go             MS endpoints: port statuses, config, packet counters, ports-overview,
                        ports-usage, PoE/STP/MAC/VLAN summaries
appliance.go            MX endpoints: uplink statuses + usage, VPN statuses/stats/heatmap, port
                        forwarding, settings, traffic shaping, failover events
camera.go               MV endpoints: onboarding, boundary areas/lines, detections history,
                        retention profiles
cellular.go             MG endpoints: uplinks, LAN, port forwarding, connectivity
alerts.go               Assurance alerts (org list + overview-by-type + byNetwork + historical + MTTR)
clients.go              /organizations/{id}/clients list + /search + session history
configuration_changes.go /organizations/{id}/configurationChanges (annotation source)
events.go               /networks/{id}/events (+ timeline aggregator)
firmware.go             Firmware upgrades (history + scheduled + pending) + device EoX
insights.go             Licensing overview/list, API usage (overview + byInterval), top-N (clients,
                        devices, models, SSIDs, switches-by-energy, networks-by-status)
topology.go             Per-network LLDP/CDP node graph + org-geo centroid aggregation
traffic.go              L7 application traffic (per network + top-N app/category) + analysis-mode
*_test.go               Unit tests per file
```

## Cache layering: TTL + SWR + singleflight + warmer

The cache is more than a TTL LRU — the stack now includes:

1. **TTL LRU** (`cache.go`) — hard expiry, per-key.
2. **Stale-while-revalidate** — past TTL, the first reader gets stale data immediately and the cache kicks off an async refresh. Subsequent readers during the refresh share the in-flight result.
3. **Singleflight coalescing** — concurrent misses for the same key fan in to one backend call.
4. **Optional Warmer** (`warmer.go`) — background goroutine pre-refreshing the navigation spine (`organizations`, per-org `organizationNetworks`). Additive: it refreshes entries BEFORE expiry so even the *first* request after dashboard load is a cache hit. Scope is deliberately narrow — device/alert/sensor kinds rely on SWR+singleflight because pre-warming them would multiply goroutine count without a clear payoff. `RefreshOnce` is exposed so tests can drive a pass without sleep flakes.

## Rate limiting (per-org)

```go
RateLimiterConfig{
  RequestsPerSecond: 10,   // Meraki default per-org
  Burst:             20,
  SharedFraction:    1.0,  // 1/N for N-replica deployments
  JitterRatio:       0.1,  // ±10% to desync replicas
}
```

Key: `orgID`. Meraki rate limits per-organization, not per-key (todos.txt §G.1). Distributed limiter intentionally deferred — Meraki's 429 + `Retry-After` coordinates naturally across replicas.

## Cache TTLs

Per query kind (set by the handler, not the client). Keep these consistent with what's hard-coded in `pkg/plugin/query/*.go`:

| Kind                          | TTL    |
|-------------------------------|--------|
| Organizations                 | 1h     |
| Networks                      | 15m    |
| Devices                       | 5m    |
| DeviceStatusOverview          | 1m     |
| SensorReadingsLatest          | 30s    |
| SensorReadingsHistory         | 1m     |
| Wireless usage / channel util | 1m     |
| NetworkSsids                  | 5m     |
| ApClients                     | 1m     |
| DeviceAvailabilities          | 1m     |
| Alerts                        | 30s    |

## Time-range handling (`timerange.go`)

`EndpointTimeRange` has `MaxTimespan` and `AllowedResolutions`. `Resolve(from, to, maxDataPoints, allowed)` applies:

1. **Freshness floor** (60s) — subtract from `now` before computing `t1`. Meraki timestamps lag 30-120s; removing this empties the tail of timeseries (todos.txt §G.4).
2. **Timespan clamp** to `MaxTimespan` (emits an `Annotation` if truncated).
3. **Resolution quantization** UP to the nearest allowed bucket (`quantizeUp`).

`KnownEndpointRanges` currently covers sensor history (730d), apiRequests (31d), wireless usage history (31d), channel util history (31d), uplinksLossAndLatency (**5 minutes** — passing longer is a 400).

**Add a KnownEndpointRanges entry whenever a new timeseries endpoint is implemented.**

## Adding a new endpoint wrapper

1. Add a typed response struct (match Meraki's JSON field names exactly).
2. Add a `GetXxx(ctx, ...)` method that calls `c.Get()` (single) or `c.GetAll()` (paginated) with an appropriate TTL.
3. If paginated via `startingAfter` instead of Link header, loop in the wrapper (or extend `pagination.go`) — todos.txt §G.3.
4. If timeseries, add a `KnownEndpointRanges` entry in `timerange.go`.
5. Add unit tests for endpoint-specific surprises (pagination, partial-success, 429 handling).

## Assurance alerts — 2026-04 schema shift

As of 2026-04 (verified against api.meraki.com for org `1019781` on 2026-04-19) the assurance alerts endpoint returns `device: null` at the top level on every response. Device context lives in `scope.devices[]` instead. The wire struct keeps the top-level `Device` field for back-compat but callers MUST go through `AssuranceAlert.PrimaryDevice()`:

- Prefers `Device` when populated (old-shape responses).
- Falls back to `Scope.Devices[0]` (new-shape responses).
- Backfills `ProductType` from the top-level `DeviceType` abbreviation (`MS`/`MR`/`MX`/`MG`/`MV`/`MT`) via `productTypeFromAbbrev` when the scope entry omits it — the drilldown URL helpers need the full `switch`/`wireless`/etc. string.
- Returns nil for genuinely network-scoped alerts (e.g. Meraki cloud-connectivity events).

Handlers emitting a device column from alerts MUST call `PrimaryDevice()` — reading `a.Device` directly yields empty columns on every current payload.

## Partial success (`PartialSuccessError`)

Some Meraki endpoints return HTTP 200 with `{"items":[...], "errors":[...]}`. `client.go` detects this and surfaces as `PartialSuccessError` with both `Errors` and the raw body. Handlers that want partial data need to inspect the error type and extract `.items` (todos.txt §G.2).

## Pagination quirk

`GetAll()` follows `Link: <...>; rel="next"`. Newer endpoints (assurance alerts, some org listings) also use `startingAfter`. Assurance alerts actually emit Link headers correctly — we verified this when implementing Phase 6 — so `GetAll()` works for them. If a future endpoint uses `startingAfter` only, extend `pagination.go` or loop manually in the wrapper.

## Snapshot vs timeseries

- **Native timeseries** (history endpoints): sensor readings history, wireless usage history, channel util history, uplinksLossAndLatency.
- **Snapshot** (point-in-time): deviceStatusOverview, deviceStatuses, switch port statuses, VPN statuses. **Do NOT synthesize timeseries from snapshots.** A future opt-in ring buffer for deviceStatuses state transitions is deferred to v0.3+.
