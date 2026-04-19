# Frontend (`src/`)

TypeScript + React + `@grafana/scenes`. Two plugins live here:

- **App plugin** (`module.tsx`, `plugin.json` id `robknight-meraki-app`) — the top-level entry. Mounts a `SceneApp`. Current pages (source of truth: `src/components/App/App.tsx` + `plugin.json` includes): Home, Organizations, Appliances, AccessPoints, Switches, Cameras, CellularGateways, Sensors, Insights, Events, Alerts, Traffic, Topology, AuditLog, Clients, Firmware, Configuration.
- **Nested data source** (`datasource/` id `robknight-meraki-datasource`) — frontend-only. Proxies every `query()` and `metricFindQuery()` to the app's resource endpoints via `getBackendSrv().fetch`.

## Layout

```
module.tsx                            App entry — returns <App/> wrapped in PluginPropsContext
plugin.json                           App manifest (id, includes, backend: true, executable: gpx_meraki)
constants.ts                          PLUGIN_ID, ROUTES enum, DEFAULT_MERAKI_BASE_URL, MERAKI_REGIONS
types.ts                              AppJsonData, AppSecureJsonData, RecordingsConfig, DeviceLabelMode,
                                      MerakiProductType, SensorMetric
components/
  App/App.tsx                         SceneApp factory + page list (source of truth)
  AppConfig/AppConfig.tsx             Config form (API key, region, base URL, shared fraction, label mode,
                                      recordings target DS + per-group toggles)
  AppConfig/AlertRulesPanel.tsx       Operator UI for the v0.6 alert bundle
  AppConfig/RecordingsPanel.tsx       Operator UI for the v0.7 recording bundle
  AppConfig/RuleBundlePanel/          Shared group/threshold primitives used by both panels above
  AppConfig/use{Alerts,Recordings}{Status,Templates}.ts  Data hooks wrapping the resource endpoints
  testIds.ts                          data-testid constants for Playwright
pages/<Area>/                         Scene pages (one directory per Area) — see src/pages/CLAUDE.md
scene-helpers/                        Shared variables, panels, links, config/traffic/family gates,
                                      sensor-metric metadata, recording-rule trend/fallback helpers
datasource/                           Nested DS — see src/datasource/CLAUDE.md
utils/
  utils.plugin.ts                     PluginPropsContext (gives scenes access to AppRootProps); also
                                      exports usePluginMeta() used by family-gated layouts
  utils.routing.ts                    prefixRoute(ROUTES.Foo) — returns `/a/<pluginId>/foo`
img/                                  logo.svg + screenshots/
```

## Query-kind contract

**Frontend** `src/datasource/types.ts` defines the `QueryKind` enum and `MerakiQuery` interface. **Backend** `pkg/plugin/query/dispatch.go` defines matching string constants and the `MerakiQuery` Go struct. **When adding a new kind, update both.** See `pkg/plugin/query/CLAUDE.md` for the 7-step recipe.

## Scene patterns

- Reuse variable factories from `scene-helpers/variables.ts` — don't define `orgVariable`/`networkVariable` inline in a scene.
- Reuse panel factories from `scene-helpers/panels.ts` — it already owns the Meraki-DS wiring via `MERAKI_DS_REF`.
- Every scene page should start with `configGuardFlexItem()` so users who haven't configured the plugin see a friendly banner instead of failed queries.
- URL-sync: `routePath: 'path/*'` (trailing `*`) for any page with drilldowns or tabs — deep links must be bookmarkable.

## Conventions

- New dependencies: prefer `@grafana/*` first. `package.json` currently pulls `@grafana/scenes ^7`, `@grafana/ui/data/runtime/schema/i18n 12.4.2`, `react 18`, `rxjs 7.8`.
- **Combobox over Select.** `Select` is deprecated in `@grafana/ui`; `QueryEditor.tsx` uses `Combobox<QueryKind>` + `MultiCombobox<string>` — follow the same pattern for any new picker.
- **AppConfig is NOT wrapped in `<form onSubmit>`.** Use `type="button"` + `onClick` on the Save action. Reasons in todos.txt §G.16.
- Config save path uses `window.location.reload()` (see root `CLAUDE.md` — this is load-bearing, not cosmetic).

## Testing

- Jest: `src/scene-helpers/*.test.ts` + `src/components/AppConfig/AppConfig.test.tsx`. Run `npm run test:ci` (4 suites, 15 tests at last count).
- Playwright: `tests/` — `appConfig.spec.ts` + `appNavigation.spec.ts`. Follow `.config/AGENTS/e2e-testing.md` when adding new specs.

## Gotchas specific to frontend

- IDE TypeScript may report spurious JSX errors — `npm run typecheck` is authoritative (todos.txt §G.12).
- `FieldConfig.DisplayNameFromDS` is a final formatted string; Grafana does NOT template-substitute it. Use `DisplayName` + `${__field.labels.<name>}` when you want interpolation (todos.txt §G.17).
- Timeseries frames must be **one-frame-per-series with `data.Labels` on the value field**. Long-format frames with `(ts, serial, metric, value)` columns render as empty charts — a client-side `partitionByValues` transform does NOT fix this; the backend must emit per-series frames (todos.txt §G.18).
