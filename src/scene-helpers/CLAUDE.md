# Scene helpers (`src/scene-helpers/`)

Shared building blocks for every scene page. Keep additions here so scene files stay tiny and declarative.

## Files

```
datasource.ts            MERAKI_DS_UID + MERAKI_DS_REF — the DS reference passed to SceneQueryRunner
variables.ts             Factories: orgVariable, networkVariable, sensorMetricVariable, etc.
panels.ts                Factories: makeStatPanel, organizationsTable, deviceStatusStat, sensorTimeseries, ...
links.ts                 urlFor<Thing>() helpers used by drilldown DataLink URLs
ConfigGuard.tsx          <ConfigGuard/> + configGuardFlexItem() — "not configured" banner
sensorMetrics.ts         SENSOR_METRIC_BY_ID, ALL_SENSOR_METRICS, SensorMetricMeta (unit/decimals/thresholds)
app-jsondata.ts          readAppJsonData() — scene-build-time read of plugin jsonData via `config.apps`
orgDeviceFamilies.ts     useOrgDeviceFamilies() hook — powers family-gated page visibility
familyGate.tsx           FamilyGatedLayout / wrapInFamilyGate — replaces a scene body with a banner
                         when the selected org has zero devices of a given product family
TrafficGuard.tsx         <TrafficGuard/> — wraps the Traffic page body; banners networks whose
                         Meraki "Traffic analysis" mode is disabled or basic rather than detailed
recording-metrics.ts     Canonical Prometheus metric-name constants shared with Go recording-rule
                         templates. Import from here; never hard-code `meraki_...` literals.
trend-query.ts           trendQueryRunner(opts) — helper that returns either a PromQL query against
                         the operator's recording-rules target DS OR a Meraki query-kind fallback,
                         based on jsonData.recordings state. See root CLAUDE.md §1.14.
```

## MERAKI_DS_UID

`'robknight-meraki-ds'` — **duplicated** in `provisioning/datasources/meraki.yaml`. Keep aligned. If you ever rename, update both plus any tests that assert the UID.

## ConfigGuard (P.5 polish)

Every scene should start with `configGuardFlexItem()` as its first child. It fetches `/resources/ping` and renders an `Alert` with a `LinkButton` to `prefixRoute(ROUTES.Configuration)` when the API key isn't set — so users see a friendly prompt instead of a silent 412.

## Panel conventions

- All query runners go through factories in `panels.ts` that pull `MERAKI_DS_REF` from `datasource.ts`. Don't inline `{ type: 'datasource', uid: '...' }` in scenes.
- Server-side KPI aggregation > client-side `filterByValue + reduce` chains. Transforms silently fall back to the wrong reducer in some Grafana versions — that's why `sensorAlertSummary` + `alertsOverview` exist as dedicated kinds that return one wide frame with per-KPI fields (todos.txt §G.20).
- `hideColumns()` uses the `organize` transform to drop low-value columns (mac, lat, lng, raw) without dropping them from the underlying frame.

## Recording-rule-aware panels (v0.7)

For trend panels that can benefit from recording-rule history, use `trendQueryRunner()` from `trend-query.ts` instead of a raw `SceneQueryRunner`. The helper encapsulates "recorded PromQL vs Meraki-kind fallback" branching so scenes stay declarative and panels work identically on/off. Pair the call with a metric-name constant from `recording-metrics.ts` — those strings must match the `meraki_<group>_<name>` metrics emitted by templates in `pkg/plugin/recordings/templates/`, and the const sharing is what prevents drift.

## Gate components (`ConfigGuard`, `TrafficGuard`, `FamilyGatedLayout`)

- **`configGuardFlexItem()`** — first child of EVERY scene. Renders a banner when the API key isn't set. Non-negotiable.
- **`<TrafficGuard/>`** — Traffic page only. Banners networks with "Traffic analysis" disabled or set to basic; queries against those networks return empty frames, so the guard provides context so users don't think the plugin is broken.
- **`FamilyGatedLayout`** — device-family pages (Appliances / AP / Switches / Cameras / CellularGateways / Sensors). When `jsonData.showEmptyFamilies=false` AND the selected org has zero devices of the page's product family, it replaces the scene body with a friendly banner while keeping the scene's variable + time-range controls live (so the user can switch org without re-mounting).
