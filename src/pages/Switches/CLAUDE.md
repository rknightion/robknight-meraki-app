# Switches area (`src/pages/Switches/`)

MS-family scene pages. Fleet Overview → per-switch detail (Overview / Ports / Alerts tabs) → per-port detail.

## Files

```
switchesPage.ts          SceneAppPage — mounts switchesScene + per-serial drilldown to switchDetailPage
switchesScene.ts         Fleet Overview scene (KPI row + ports-by-speed + usage history + fleet PoE +
                         clients-per-switch + inventory table)
switchDetailPage.ts      Per-switch tabbed SceneAppPage — Overview / Ports / Alerts tabs
switchOverviewScene.ts   Overview tab body (KPIs + PoE + VLAN donut + stack + L3 + DHCP-seen)
switchPortsScene.ts      Ports tab body (port map + neighbours + MAC table + ports/:portId drilldown)
switchAlertsScene.ts     Alerts tab body — reuses KindAlerts with serials filter (v0.8)
portDetailPage.ts        Per-port SceneAppPage
portDetailScene.ts       Per-port detail body (KPI row + neighbour + packet counters + config + errors)
panels.ts                All per-area panel factories (inventory, port map, KPIs, VLAN donut, v0.8 panels)
panels.test.ts           Jest smoke tests asserting pluginId + title per factory
links.ts                 urlForSwitch(serial), urlForPort(serial, portId)
variables.ts             Area-specific variable factories (re-exports orgOnlyVariables for convenience)
```

## URL cascade

```
/switches                                (fleet)
  → /switches/:serial/overview           (Overview tab — default)
  → /switches/:serial/ports              (Ports tab)
     → /switches/:serial/ports/:portId   (per-port detail)
  → /switches/:serial/alerts             (Alerts tab — v0.8)
```

Drilldown URLs carry `var-org` so `$org` hydrates on every tab/port without a re-pick.

## Query-kind map

- **Fleet**: `DeviceAvailabilityCounts`, `SwitchPortsOverview`, `SwitchPortsOverviewBySpeed`, `SwitchPortsUsageHistory`, `SwitchFleetPowerHistory` (v0.8), `SwitchPortsClientsOverview` (v0.8), `Devices` (for inventory)
- **Per-switch Overview**: `DeviceAvailabilities`, `Devices`, `SwitchPortsOverview`, `SwitchPoe`, `SwitchVlansSummary`, `NetworkSwitchStacks` (v0.8), `SwitchRoutingInterfaces` (v0.8), `NetworkDhcpServersSeen` (v0.8)
- **Per-switch Ports**: `SwitchPorts`, `SwitchNeighborsTopology` (v0.8), `SwitchMacTable`
- **Per-switch Alerts**: `Alerts` (v0.8 — scoped via `serials: [serial]`)
- **Per-port detail**: `SwitchPorts` (filtered client-side), `SwitchNeighborsTopology` (filtered client-side), `SwitchPortPacketCounters`, `SwitchPortConfig`

## v0.8 invariants

**Widened frames** — the `switch_ports` frame now carries live STP state per port (`stpState`, comma-joined from `spanningTree.statuses`), the bound port profile (`activeProfile`), bi-directional traffic rates (`trafficKbps` + `trafficKbpsSent/Recv`), errors/warnings (comma-joined), secure-port auth state (`secureAuth`), and `isUplink`. Port map hides the less-critical ones (`trafficKbpsSent/Recv`, `usageKbSent/Recv`, `secureAuth`, `isUplink`) by default via the `organize` transform — operators can restore them in the panel editor.

**`switch_port_config` frame** — widened to expose the full port-config shape: RSTP, STP guard, UDLD, link negotiation, isolation, storm control, DAI trust, port schedule, adaptive-policy group, access policy (type + number), MAC allow-list (comma-joined), sticky MAC allow-list + limit. 24 fields total. The detail panel renames fields to friendly display names via `overrideDisplayName` (NOT via `renameByName` — rename transforms change the field's name for subsequent overrides, display-name overrides only change the header).

**Always-visible, empty-state rendering** — the Overview tab's Stack / L3 / DHCP-seen panels always render. L2 switches, standalone (non-stacked), and rogue-free networks get `noValue` text ("Not an L3 switch", "Not a stack member", "No DHCPv4 offers observed..."), NOT hidden panels. Rationale: memory `feedback_optional_feature_fallback` — users don't get a "click to enable" empty state, they get the current state. For the L3 endpoint specifically, the Meraki API 404s on L2 models; the wrapper `GetDeviceSwitchRoutingInterfaces` catches `NotFoundError` and returns `nil` so the empty-frame path fires cleanly.

**`KindNetworkDhcpServersSeen` serial-to-network resolution** — the endpoint is network-scoped but the per-switch Overview page only knows the switch's serial. The handler resolves networkId per serial by reading the cached org-level `statuses/bySwitch` feed (30s TTL — the same cache that backs the port map). When `q.NetworkIDs` is explicitly set, it short-circuits the lookup. When only `q.Serials` is set, it fans out to each distinct network. Empty inputs return a 412-style error.

**`KindSwitchRoutingInterfaces` stack dispatch** — the handler looks up the serial's network (via the cached statuses feed), then lists stacks in that network (5m cache), and routes:
- serial belongs to a stack → `GET /networks/{net}/switch/stacks/{stack}/routing/interfaces`
- standalone → `GET /devices/{serial}/switch/routing/interfaces`

The `source` column on the emitted frame reflects which path was used: `"device"` or `"stack:<stackId>"`.

**Neighbors data is org-wide** — `KindSwitchNeighborsTopology` pulls the entire `organizations/{org}/switch/ports/topology/discovery/byDevice` feed in one paginated call and filters client-side. One API call feeds both the per-switch Ports tab (serials: [serial]) and the per-port detail (further filtered by portId). Do NOT switch this to per-device `/devices/{serial}/lldpCdp` — we'd pay N calls instead of 1 for the same data.

**Alerts tab reuses the existing Alerts handler** — no new backend path. Wire the query with `serials: [serial]`, `metrics: ['']` (severity sentinel for "all"), `alertStatus: 'all'` to see active + resolved + dismissed rows colour-coded by status.

## Conventions

- Every panel factory in `panels.ts` uses the local `oneQuery()` helper (NOT the one in `scene-helpers/panels.ts`) for the Wave-3 decoupling reasons documented at the top of the file. Both helpers do the same thing; keep them in sync when templates expand.
- Column-hiding uses the `organize` transform's `excludeByName` map. Keep `renameByName` empty on panels that rely on `matchFieldsWithName('<original>')` overrides — `organize`'s rename DOES change the field name for subsequent matchers (confirmed via the `sensorFloorPlanHeatmap` pattern in `src/scene-helpers/panels.ts`).
- Drill-in links on tables use `${__value.raw:percentencode}` so URL-safe port IDs work (Meraki returns strings like `"1"` today but the contract allows slashes).
- Per-port client-side filtering (in `portDetailKpiStats`, `portDetailNeighborPanel`) uses `filterByValue` with `id: 'equal'` — safe because we're filtering on a known unique port ID, not the `organize` `filterByValue` + `reduce` combination flagged in §G.20.

## Gotchas

- The fleet inventory table's `serial` column drills to `switches/:serial` (no `/overview` suffix) because Scenes 7 defaults to the first tab. The detail page's `routePath: '<serial>/*'` catches the bare URL and the Overview tab's `routePath: 'overview'` matches by default.
- `switchPortMap` explicitly hides `stackId` via the organize transform — Meraki returns an empty string for standalone switches and the empty-cell `noValue` would leak an error message into every row. The backend still emits the column for fleet-wide panels to group by stack.
- `fleetPoeHistoryTimeseries` doesn't pass an `interval` — Meraki auto-buckets based on the requested window (20m/4h/1d). Setting it to anything else is HTTP 400. The `SwitchFleetPowerHistory` handler respects this.
- The DHCP-seen endpoint's `device.interface` field is the switchport that observed the offer; we format it as `"sw-name / port 5"` so the `Seen by` column stays scannable.
