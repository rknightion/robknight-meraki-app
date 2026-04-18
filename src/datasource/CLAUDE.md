# Nested data source (`src/datasource/`)

Plugin id `robknight-meraki-datasource`. **Frontend-only — no Go backend.** Every `query()` and `metricFindQuery()` POSTs to the app plugin's resource endpoints at `/api/plugins/robknight-meraki-app/resources/{query,metricFind}`.

## Why this shape

One API key (held by the app), one `meraki.Client` (per-org rate limiter + TTL cache), one set of resource endpoints. The DS is a thin `DataSourceApi` adapter so Meraki queries can be used in any dashboard alongside Prometheus/Loki/Tempo, while credentials and rate limiting stay centralized in the app.

## Files

```
plugin.json         id: robknight-meraki-datasource, backend: false, metrics: true, alerting: true
module.ts           Exports plugin class — registers MerakiDataSource, ConfigEditor, QueryEditor
datasource.ts       MerakiDataSource extends DataSourceApi — query/metricFindQuery/testDatasource
QueryEditor.tsx     Kind picker (Combobox<QueryKind>) + per-kind fields (org/networks/serials/metrics)
ConfigEditor.tsx    Shows a message pointing users at the app config page (no DS-level config)
types.ts            QueryKind enum, MerakiQuery interface, MerakiDSOptions, MerakiMetricFindValue
img/logo.svg        Synced from src/img/logo.svg — 72×72 generic network icon
```

## Locked-in: the DS has NO backend

- DO NOT add `"backend": true` to `plugin.json` — Grafana would try to launch a non-existent `gpx_robknight-meraki-datasource` binary.
- `ConfigEditor.tsx` has no API-key field. All credentials live on the app.
- `MerakiDSOptions` is deliberately a marker interface (`_placeholder?: never`).

## Template interpolation

`applyTemplateVariables()` expands `$org`, `$networks`, `$serials` before POSTing. Multi-value variables go through `tpl.replace(v, scopedVars, 'csv')` then `splitMulti()` to yield a string array.

## Variable hydration

`metricFindQuery()` POSTs a single `MerakiQuery` to `/resources/metricFind`. Handlers in `pkg/plugin/query/metricfind.go` return `[{text, value}]` tuples.

Convenience shortcuts in `datasource.ts`:
- `listOrganizations()` → `{kind: Organizations}`
- `listNetworks(orgId)` → `{kind: Networks, orgId}`

Scenes usually use `metricFindQuery` via `QueryVariable` factories in `src/scene-helpers/variables.ts`.

## testDatasource

Calls `GET /api/plugins/robknight-meraki-app/health` (the app's `CheckHealth`). The app validates by hitting `GET /organizations` with a 15s timeout. Green when the API key is accepted.

## Query-kind contract

`QueryKind` enum values are the wire discriminator. They **must match** `pkg/plugin/query/dispatch.go` `KindXxx` constants exactly. See the 7-step recipe in `pkg/plugin/query/CLAUDE.md`.
