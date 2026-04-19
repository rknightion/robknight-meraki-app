# Scene pages (`src/pages/`)

Each directory is one Area. `@grafana/scenes` — no static JSON dashboards.

## Current pages

| Area             | Product | Scope                                                                                         |
|------------------|---------|-----------------------------------------------------------------------------------------------|
| Home             | —       | Org KPIs, device-status-by-family stack, org change-feed tile, MTTR summary                   |
| Organizations    | —       | Inventory → tabbed detail page (Overview / Devices / Alerts)                                  |
| Appliances       | MX      | Uplinks overview + VPN heatmap + per-device uplink loss/latency history, traffic shaping      |
| AccessPoints     | MR      | Channel util + SSID usage + per-AP client counts → per-AP drilldown (Overview / RF / Clients) |
| Switches         | MS      | Inventory + PoE/STP/MAC/VLAN summaries → per-switch detail (Overview / Ports, port map)       |
| Cameras          | MV      | Onboarding + boundary (areas/lines) + detections history + retention profiles                 |
| CellularGateways | MG      | Uplinks, LAN, port forwarding, connectivity                                                   |
| Sensors          | MT      | Native timeseries per metric + per-sensor detail + floor plan (auto-hide-on-empty)            |
| Insights         | —       | Licensing, API-usage, top clients/devices/models/SSIDs, top networks by status                |
| Events           | —       | Network events table + hourly timeline                                                        |
| Alerts           | —       | Severity filter + hourly timeline bar chart + sortable alerts table + byNetwork breakdown     |
| Traffic          | —       | L7 application breakdown (analysis-mode gated via `<TrafficGuard>`)                           |
| Topology         | —       | Per-network node graph (LLDP/CDP) + org geo map                                               |
| AuditLog         | —       | Configuration changes feed + annotations overlay                                              |
| Clients          | —       | Org clients list + per-MAC lookup + session history                                           |
| Firmware         | —       | Upgrade history/schedule + pending per-device + device EoX bucket                             |
| Configuration    | —       | In-app scene mirroring `AppConfig`, reachable via `Apps → Cisco Meraki → Configuration`       |

Pages are wired in `src/components/App/App.tsx` and `src/plugin.json` `includes`. Device-family pages (Appliances / AccessPoints / Switches / Cameras / CellularGateways / Sensors) are always present in the nav today — a `showEmptyFamilies=false` auto-hide mode is plumbed on the backend (`KindOrgProductTypes`) and gated per-page via `FamilyGatedLayout` (see `src/scene-helpers/familyGate.tsx`), but the nav itself is not mutated; see the comment on `allPages()` in `App.tsx`.

## Files per Area (convention)

```
<area>Page.ts          SceneAppPage — routePath, title, subtitle, getScene() returning the root scene
<area>Scene.ts         EmbeddedScene factory — variables + layout
panels.ts              Per-area panel factories (reuse helpers from src/scene-helpers/panels.ts)
variables.ts           Per-area scene variables (reuse helpers from src/scene-helpers/variables.ts)
links.ts               urlFor...() helpers for drilldown links (bookmarkable deep-links)
<detail>Page.ts        Drilldown SceneAppPage (tabs live here)
<detail>Scene.ts       Drilldown scene body
```

## Adding a new scene page (6-step recipe)

1. `src/pages/<Area>/<area>Scene.ts` — `EmbeddedScene` factory. First child should be `configGuardFlexItem()` from `scene-helpers/ConfigGuard.tsx`.
2. `src/pages/<Area>/<area>Page.ts` — `SceneAppPage` with `routePath: '<route>/*'` (trailing `*` if there are tabs/drilldowns).
3. Import the page in `src/components/App/App.tsx` and add it to the `pages[]` array passed to `new SceneApp({...})`.
4. Add an entry under `includes` in `src/plugin.json` (type `page`, path `/a/%PLUGIN_ID%/<route>`, `addToNav: true`).
5. Variables: reuse from `scene-helpers/variables.ts`. New factories go there, NOT inline.
6. Panels: reuse from `scene-helpers/panels.ts`. New panels go there, NOT inline.

**Remind the user that `plugin.json` changes require a Grafana restart.**

## URL-sync & drilldowns

- Detail pages use `routePath: 'path/*'` so drilldown routes like `/access-points/<serial>/rf` resolve.
- Drilldown URLs come from per-area `links.ts` (e.g. `urlForAccessPoint(serial)`). Don't hand-build paths in scene files.
- Variable binding: `$org`, `$network`, `$ap`, etc. Scene pages declare the variables they need; panels interpolate via `$org` in `orgId` fields.

## Gotcha — empty-chart trap

When rendering native timeseries, the backend must emit **one frame per series with labels on the value field**. If you see an empty timeseries viz but the data is there in the frame inspector, the frame shape is wrong (long-format instead of labelled per-series). Fix in the Go handler, not with a client-side transform. Canonical shapes: `pkg/plugin/query/sensor_readings.go:handleSensorReadingsHistory` and `pkg/plugin/query/wireless.go:handleWirelessChannelUtil`.

## Alert-bundle UI

Alert-bundle config UI lives in `src/components/AppConfig/AlertRulesPanel.tsx` — see also `pkg/plugin/alerts/CLAUDE.md` for the backend invariants (UID scheme, reconciler idempotency, managed_by delete gate).

## Recording-bundle fallback pattern (v0.7)

Panels that want to benefit from recording-rule trend history without breaking when the operator hasn't enabled it use `trendQueryRunner()` from `src/scene-helpers/trend-query.ts`. The helper reads `jsonData.recordings` at scene-build time (via `readAppJsonData()` in `app-jsondata.ts`) and either (a) emits a PromQL query against the operator's target DS, or (b) falls back to the existing Meraki query-kind. The two branches must produce equivalent frame shapes so the panel renders identically either way. Metric names are centralised in `src/scene-helpers/recording-metrics.ts` — import from there rather than hard-coding literals. See root `CLAUDE.md` §1.14 for the full invariants.
