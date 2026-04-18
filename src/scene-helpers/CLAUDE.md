# Scene helpers (`src/scene-helpers/`)

Shared building blocks for every scene page. Keep additions here so scene files stay tiny and declarative.

## Files

```
datasource.ts        MERAKI_DS_UID + MERAKI_DS_REF — the DS reference passed to SceneQueryRunner
variables.ts         Factories: orgVariable, networkVariable, sensorMetricVariable, etc.
panels.ts            Factories: makeStatPanel, organizationsTable, deviceStatusStat, sensorTimeseries, ...
links.ts             urlFor<Thing>() helpers used by drilldown DataLink URLs
ConfigGuard.tsx      <ConfigGuard/> + configGuardFlexItem() — "not configured" banner
sensorMetrics.ts     SENSOR_METRIC_BY_ID, ALL_SENSOR_METRICS, SensorMetricMeta (unit/decimals/thresholds)
```

## MERAKI_DS_UID

`'robknight-meraki-ds'` — **duplicated** in `provisioning/datasources/meraki.yaml`. Keep aligned. If you ever rename, update both plus any tests that assert the UID.

## ConfigGuard (P.5 polish)

Every scene should start with `configGuardFlexItem()` as its first child. It fetches `/resources/ping` and renders an `Alert` with a `LinkButton` to `prefixRoute(ROUTES.Configuration)` when the API key isn't set — so users see a friendly prompt instead of a silent 412.

## Panel conventions

- All query runners go through factories in `panels.ts` that pull `MERAKI_DS_REF` from `datasource.ts`. Don't inline `{ type: 'datasource', uid: '...' }` in scenes.
- Server-side KPI aggregation > client-side `filterByValue + reduce` chains. Transforms silently fall back to the wrong reducer in some Grafana versions — that's why `sensorAlertSummary` + `alertsOverview` exist as dedicated kinds that return one wide frame with per-KPI fields (todos.txt §G.20).
- `hideColumns()` uses the `organize` transform to drop low-value columns (mac, lat, lng, raw) without dropping them from the underlying frame.
