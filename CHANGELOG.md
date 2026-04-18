# Changelog

## 0.3.0 (Unreleased)

Closes the remaining device families and adds organization-level lifecycle
tooling. Bundles `todos.txt` phases 8–11.

### Added

- **Appliances (MX)** scene with fleet KPI row, uplink overview stats,
  uplink status table (colour-coded by status), and MX inventory. Per-MX
  drilldown is a tabbed page with Overview / Uplinks / VPN / Firewall. The
  Uplinks tab renders native timeseries of uplink loss % and latency ms
  sourced from `/organizations/{orgId}/devices/uplinksLossAndLatency` and
  clamps to the endpoint's 5-minute cap. The VPN tab shows a peer matrix
  combining `/appliance/vpn/statuses` (reachability, bytes sent/received)
  and `/appliance/vpn/stats` (latency, jitter, loss, MOS). The Firewall tab
  surfaces port-forwarding rules + appliance settings for a selected
  network.
- **Cameras (MV)** scene with inventory, onboarding status table, and
  per-camera drilldown (Overview / Analytics / Zones). Analytics charts
  entrances over time per zone, a live occupancy snapshot, and optional
  per-zone history; object type toggle (person / vehicle) via scene
  variable. The `$zone` variable hydrates from a new
  `cameraAnalyticsZones` metricFind path.
- **Cellular Gateways (MG)** scene with inventory, fleet signal bar chart,
  and per-gateway drilldown (Overview / Uplink / Port Forwarding). RSRP
  and RSRQ strings are parsed to float64 dBm so gauge thresholds colour
  signal strength (red ≤ -110, amber ≤ -100, green > -100). Port
  forwarding rules and connectivity monitoring destinations render on
  their respective tabs.
- **Insights** nav entry — tabbed `SceneAppPage` with Licensing / API Usage
  / Clients:
  - Licensing tab detects co-termination vs per-device licensing,
    surfaces four KPI tiles (Active, Expiring ≤ 30 d, Expired, Total),
    a co-term expiration stat, and a licenses table with a state filter
    variable and days-until-expiry threshold colouring (orange ≤ 30 d,
    red ≤ 7 d).
  - API Usage tab shows response-code-bucket KPIs (2xx / 4xx / 429 / 5xx)
    and a stacked bar chart by interval with per-class colour overrides.
  - Clients tab surfaces total client count + up/downstream usage KPIs
    and five top-N tables backed by `/summary/top/*` endpoints (clients,
    devices, SSIDs, device models, switches by energy).
- **Events** scene with product-type and event-type filters, an hourly
  timeline bar chart binned by event category, and an events table whose
  device-serial column deep-links to the correct per-family page.
- **Cross-family drilldown** — device rows now route to the product-family
  page that owns them (wireless → Access Points, switch → Switches,
  appliance → Appliances, camera → Cameras, cellularGateway → Cellular
  Gateways, sensor → Sensors). Handlers that emit device rows
  (`devices`, `deviceAvailabilities`, `cameraOnboarding`, `mgUplinks`,
  `topDevices`, `topSwitchesByEnergy`, `networkEvents`) now include a
  backend-computed `drilldownUrl` column; the shared
  `orgDevicesTable` consumes it automatically.
- **Meraki API wrappers** for 27 new endpoints across `pkg/meraki/{appliance,insights,camera,cellular,events}.go`,
  plus a `ParseSignalDb` helper for cellular signal strings.
- **Shared variable factories** promoted to `src/scene-helpers/variables.ts`:
  `deviceVariable({name,label,productType})` and
  `networkVariableForProductTypes(productTypes)` replace the per-area
  copies that AccessPoints and Switches carried locally.

### Changed

- **`Options.PluginPathPrefix`** is now threaded through `Handle → runOne →
  handler` so handlers can compose cross-family drilldown URLs without
  hard-coding the plugin ID (the `robknight-*` rename is still planned for
  first signed release).
- **`KnownEndpointRanges`** grew entries for appliance VPN stats (31 d),
  clients overview (31 d + 4 allowed resolutions), apiRequests byInterval
  (31 d + 4 resolutions), top-N usage endpoints (186 d), and camera
  analytics overview (7 d).
- **Plugin `Organization.Licensing` nested type** renamed from `License`
  to `OrgLicensingRef` to avoid collision with the new `License` record
  type used by the insights API.

### Internal

- 28 new QueryKind enum entries + 28 new handler wirings in
  `pkg/plugin/query/dispatch.go`.
- Backend tests: 23 new httptest-driven cases across
  `appliance_test.go`, `insights_test.go`, `camera_test.go`,
  `cellular_test.go`, `events_test.go`, `device_urls_test.go`.
- Frontend tests: Jest coverage for new panel factories, variables,
  and the `deviceDrilldownUrl` link helper (4 new suites, ~20 new tests).
- Playwright smoke tests for the five new pages in
  `tests/appNavigation.spec.ts`.
- `pkg/plugin/query/device_urls.go` — shared `deviceDrilldownURL(prefix,
  productType, serial)` helper + `productTypeRoute` mapping.

## 0.2.0 (Unreleased)

Adds three new scene areas and finishes the v0.1 polish punchlist.

### Added

- **Access Points (MR)** scene with organization + network selectors, AP
  availability KPI row, channel-utilisation timeseries, SSID usage, and a
  clickable inventory table. Per-AP drilldown is a tabbed page with
  Overview / Clients / RF tabs; the RF tab auto-hides bands the AP doesn't
  report.
- **Switches (MS)** scene with inventory table and per-switch drilldown
  (Overview / Ports). The port map colours cells by link speed (red →
  orange → yellow → green) and drills into a per-port detail page with
  snapshot packet counters + port config.
- **Alerts (Assurance)** scene with a severity filter, hourly timeline bar
  chart, and sortable alert table. A top-5 "Recent alerts" tile on the
  Home page cross-links to the full Alerts page with the current org
  pre-selected.
- **Organization detail tabs** — the org drilldown is now a tabbed page
  with Overview / Devices / Alerts tabs; the Alerts tab is scoped to the
  current org.
- **Region URL presets** in the configuration form — Global/US, Canada,
  China, India, US Federal, with a "Custom…" escape hatch for air-gapped
  deployments.
- **"Plugin not configured" banner** on every scene page, linking to the
  in-app configuration page at `/a/<plugin>/configuration`.
- **Meraki API wrappers** for `wireless/devices/channelUtilization/history`,
  `wireless/usageHistory`, `wireless/ssids`, device clients, organization
  device availabilities, assurance alerts (+ overview by type), and switch
  port statuses / config / packet counters.
- **Generic networking plugin logo** (hand-authored SVG, trademark-safe)
  replacing the scaffolded Grafana observability placeholder.

### Changed

- **MaxDataPoints** now threads into sensor-history resolution. Wide panels
  get coarser buckets; narrow panels get finer ones. No more 400s from the
  Meraki API when a panel asks for more samples than the endpoint accepts.
- **QueryEditor** migrated from the deprecated `Select`/`Select isMulti` to
  `Combobox`/`MultiCombobox` from `@grafana/ui`.
- **Sensor label mode** (serial vs. name) added as a plugin-level setting;
  timeseries frames now bake the final display name into
  `DisplayNameFromDS`. Handler signature gained an `Options` parameter
  (`handler(ctx, client, q, tr, opts)`).

### Internal

- Jest unit tests for `variables.ts`, `panels.ts`, and `sensorMetrics.ts`
  (previously `test:ci` reported "No tests found").
- Go test for sensor-history `MaxDataPoints` quantization.
- Go tests for the new wireless, alerts, switch, and availability handlers
  (httptest stubs, including Meraki Link-header pagination and switch
  stack grouping).
- `go mod tidy` dropped the unused `golang.org/x/time` dependency.

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
