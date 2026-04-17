# Changelog

## 0.1.0 (Unreleased)

First public preview. Everything is new.

### Added

- **Plugin IDs:** `rknightion-meraki-app` (app) and `rknightion-meraki-datasource` (nested DS).
  The IDs will migrate to `robknight-*` at first signed release under the Grafana staff org.
- **App plugin shell** (`@grafana/scenes`) with three pages:
  - *Home* — organization count, device status overview, org listing.
  - *Organizations* — full inventory table.
  - *Sensors* — MT temperature, humidity, door, water, CO₂, PM2.5, TVOC, noise, battery,
    and indoor air quality timeseries plus latest-reading tiles.
- **Configuration page** with API key (encrypted via `secureJsonData`), optional regional base
  URL (e.g. `https://api.meraki.cn/api/v1`), and a *shared fraction* knob for HA rate-limit
  coordination.
- **`CheckHealth`** — the app backend calls `GET /organizations` to validate the API key and
  returns the visible org count on success.
- **Meraki API client** (`pkg/meraki`): per-organization token-bucket rate limiter with jitter,
  TTL LRU cache, typed errors for 401/404/429/5xx + partial-success bodies, Link-header and
  `startingAfter` pagination, per-endpoint time-range clamping + resolution quantization, and
  endpoint wrappers for organizations, networks, devices, device status overview, and sensor
  readings (latest + history).
- **Nested data source** (`rknightion-meraki-datasource`) — frontend-only, proxies all queries
  and variable hydration through the app plugin's resource endpoints so the API key never
  reaches the browser. Auto-provisioned via `provisioning/datasources/meraki.yaml`.
- **Query dispatcher** in `pkg/plugin/query` with handlers for `organizations`, `networks`,
  `devices`, `deviceStatusOverview`, `sensorReadingsLatest`, and `sensorReadingsHistory`.
  Handlers return native-timeseries frames for historical endpoints and point-in-time frames
  for snapshot endpoints; per-query errors become frame notices so a single bad query does not
  take down the panel.

### Known gaps (tracked for v0.2+)

- MR wireless APs, MS switches, MX appliances, MV cameras, MG cellular gateways, alerts,
  licensing, API usage, client overview — all planned post-MVP.
- No distributed rate limiter; multi-replica operators should set `sharedFraction = 1/N`.
- No scene-ified drilldown yet (`Organizations → Org detail` is a thin pass-through in MVP).

### Infrastructure

- Vendored `golang.org/x/time/rate` and `github.com/hashicorp/golang-lru/v2`.
- CI (GitHub Actions): typecheck, ESLint, Jest, webpack build, `golangci-lint`, Mage `buildAll`,
  Mage `test`, Playwright end-to-end, `grafana/plugin-validator-cli metadatavalid`.
- Local dev via `npm run server` (Docker).
