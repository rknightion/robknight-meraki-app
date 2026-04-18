# Changelog

## 0.5.0 (Unreleased)

V0.5 panels + pages roadmap — todos.txt §4.4. Additive scene work with no
new backend transport; new query-kind handlers feed new panels and five new
top-level pages.

### Added

- **Appliances (MX) panels — v0.5 §4.4.3-1c.**
  - WAN loss / latency **distribution histograms** on the Uplinks tab
    (reshape of the existing `deviceUplinksLossLatency` feed via the
    Grafana `histogram` transform — no new backend kind).
  - MX uplink **failover event timeline** + detail table backed by a new
    `applianceFailoverEvents` query kind that filters the existing
    `/networks/{id}/events` feed to the canonical uplink-change event
    types (`uplink_change`, `cellular_up`, `cellular_down`, `failover`,
    `wan_failover`). No new Meraki endpoint — pure Go filter on top of
    the existing events wrapper; 30 s TTL.
  - MX **traffic shaping** snapshot panel on the Firewall tab (new
    `applianceTrafficShaping` query kind) covering default-rules, global
    bandwidth caps, default uplink, load balancing, active-active AutoVPN,
    immediate failover, and traffic-preference counts. Backed by
    `/networks/{id}/appliance/trafficShaping` +
    `/networks/{id}/appliance/trafficShaping/uplinkSelection` with a 5 m
    TTL.
- **§4.4.3-1b — MS (switches) panels.** Four new query kinds with
  server-side aggregation:
  - `switchPoe` — per-port PoE draw flattened from the org-level
    statuses/bySwitch feed (TTL 30 s, shared cache with `switchPorts`).
  - `switchStp` — bridge-priority + `rstpEnabled` per network, expanded to
    one row per switch/stack (TTL 1 m, `GET /networks/{id}/switch/stp`).
  - `switchMacTable` — per-switch client list with IP, VLAN, port,
    last-seen, and kbytes sent/recv (TTL 30 s, default 24-hour span,
    `GET /devices/{serial}/clients`).
  - `switchVlansSummary` — port-count per (serial, VLAN) aggregated from
    the config-feed bySwitch endpoint (TTL 5 m); voice VLANs emitted as
    synthetic `voice:<n>` rows.
  Five new scene panels wired onto the switch detail pages:
  - Switch Overview → PoE draw stat + VLAN distribution donut.
  - Ports tab → MAC address table + STP topology table.
  - Port detail → port-error snapshot (reshape of existing
    `switchPortPacketCounters` via a `desc` regex filter — no new kind).

### Changed

- **UX change — Appliances VPN tab.** The VPN peer-matrix table has been
  **REPLACED** with a source × peer reachability **heatmap** (new
  `applianceVpnHeatmap` query kind). The previous matrix became hard to
  read on meshes with more than a handful of peers; the heatmap grid
  lets operators eyeball AutoVPN health at a glance. The aggregated
  `vpnPeerStatsTable` is retained on the same tab for per-pair detail.
  `applianceVpnStatuses` stays on the wire contract for the peer-status
  table (kept as a separate kind so existing tests + the flattened-row
  consumers can still bind to the old shape; the matrix panel factory
  is no longer wired into the scene).

- **Query kind `configurationChangesAnnotation`** — reshapes the existing
  configurationChanges feed into a four-column annotation frame (time,
  title, text, tags) for scene `AnnotationDataLayer` overlays. Reuses the
  same Meraki endpoint + 5 m TTL as `configurationChanges`; cache is
  shared so enabling annotations on a timeseries panel does not add a
  Meraki round-trip (v0.5 §4.4.2).
- **Query kind `alertsMttrSummary`** — aggregates resolvedAt - startedAt
  across assurance alerts into a single-row wide frame with
  `mttrMeanSeconds`, `mttrP50Seconds`, `mttrP95Seconds`, `resolvedCount`,
  `openCount`. Server-side aggregation matches §G.20 — no client-side
  filterByValue + reduce. 1 m TTL (v0.5 §4.4.2).
- **Access Points: per-SSID client count, radio status, band split,
  failed connections, latency stats** (v0.5 §4.4.3-1a). Four new query
  kinds (`wirelessClientCountHistory`, `wirelessFailedConnections`,
  `wirelessLatencyStats`, `deviceRadioStatus`) feed five new panels on
  the top-level Access Points page. `deviceRadioStatus` uses
  `GET /organizations/{id}/wireless/ssids/statuses/byDevice` as the
  org-wide proxy because Meraki's v1 OpenAPI spec does not expose a
  `wireless/devices/radioSettings/bySsid` endpoint. TTLs: 1 m (client
  count), 5 m (failed connections + latency), 15 m (radio status).
  `KnownEndpointRanges` clamps the three timeseries endpoints to 7 d
  with a 5-min resolution floor.

## 0.4.0 (Unreleased)

API optimisation wave — closes out todos.txt §7. Spec-compliant User-Agent,
two new query kinds for change-log / flap-history panels, an Audit Log scene,
and four request-hygiene upgrades that cut Meraki round-trips under
multi-panel dashboards and multi-replica Grafana.

### Added

- **Audit Log scene** (new top-level nav entry) — tabbed page under
  `/audit-log` with a change-volume timeline and a full change-log table
  sourced from `GET /organizations/{orgId}/configurationChanges`. Scene
  variables `$org`, `$network`, `$admin` filter the feed server-side.
- **Query kind `configurationChanges`** — handler emits a 9-column table
  frame (ts, adminName, adminEmail, adminId, page, label, networkId,
  oldValue, newValue). TTL 5 m; pagination via Link header.
- **Query kind `deviceAvailabilityChanges`** — handler emits a 10-column
  table frame with computed oldStatus/newStatus from the details envelope
  and a `drilldownUrl` column per §1.12. Additive to
  `deviceAvailabilities`; TTL 30 s. Surfaced on the Org detail Devices tab
  as a companion table to the current-state inventory.
- **Alerts drilldownUrl column** — `handleAlerts` now emits `drilldownUrl`
  (computed from `Device.ProductType`). The top-level Alerts page and the
  Home "Recent alerts" tile consume it via
  `${__data.fields.drilldownUrl}`, so a table spanning MR/MS/MX/MV/MG/MT
  routes each row to the right detail page.
- **Opt-in per-IP rate limiter** (`enableIPLimiter` on the app config) —
  100 rps / 200 burst, keyed on `"ip"`, acquired before the per-org
  bucket. Off by default; useful only for multi-tenant deployments.
- **Config field `enableIPLimiter`** on the Configuration page (full
  variant).

### Changed

- **Sensor label mode → Device label mode.** The plugin-level `labelMode`
  setting is now generalised across every device family. `handleDeviceUplinksLossLatency`
  (MX) and the camera analytics handlers (`handleCameraAnalyticsOverview`,
  `handleCameraAnalyticsZoneHistory`) now honour the setting alongside the
  existing sensor / wireless handlers — `serial` mode skips the `/devices`
  round-trip those handlers used to issue unconditionally. The wire key
  (`jsonData.labelMode`) and enum values (`"serial"` / `"name"`) are
  unchanged, so persisted settings load without migration. TypeScript type
  renamed `SensorLabelMode` → `DeviceLabelMode`.
- **User-Agent** bumped from `Grafana-Meraki-App` (non-compliant — hyphens)
  to `GrafanaMerakiPlugin/<version> robknight`, matching Meraki's
  [User-Agent specification](https://developer.cisco.com/meraki/api-v1/user-agents-overview/).
  Traffic from this plugin now attributes correctly in the Meraki API
  Analytics dashboard. Version lives in a new `pkg/meraki/version.go` —
  bump alongside `package.json` + CHANGELOG on every release.
- **`TTLCache`** rewritten with per-org partitions (512 entries each, up
  from a shared 2048 global pool), TTL jitter (±10 % by default so
  replicas don't expire in lock-step), stale-while-revalidate (per-entry
  stale grace — `Client` auto-derives as TTL/2 capped at 30 m, matching
  the §7.4-C proposals), and negative-404 caching (60 s default;
  401/403/5xx/412 deliberately NOT cached).
- **`Client.Get` / `Client.GetAll`** wrapped in `singleflight.Group` keyed
  on the cache key — N concurrent panels requesting the same endpoint
  collapse to 1 HTTP round-trip. Stale cache hits serve the stale value
  immediately while an async refresh runs on a detached
  context.Background with a 30 s / 60 s ceiling.

### Internal

- `golang.org/x/sync` promoted from indirect to direct dep.
- New files: `pkg/meraki/version.go`,
  `pkg/meraki/configuration_changes.go`,
  `pkg/plugin/query/configuration_changes.go`,
  `pkg/plugin/query/availabilities_change_history.go`,
  `pkg/meraki/client_test.go` (singleflight / SWR / negative-404 / UA
  tests), `src/pages/AuditLog/*` (6 files).
- Cache tests expanded: 5 new cases covering SWR, negative cache, org
  partitioning, TTL jitter, backward-compat shim.
- Two new `KnownEndpointRanges` entries (`configurationChanges` 365 d,
  `devices/availabilities/changeHistory` 31 d).
- Jest suites: 12 (up from 11). Go test packages: 3 (unchanged), 55 total
  tests (up from ~42).

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

- **Plugin IDs:** `robknight-meraki-app` (app) and `robknight-meraki-datasource` (nested DS).
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
- **Nested data source** (`robknight-meraki-datasource`) — frontend-only, proxies all queries
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
