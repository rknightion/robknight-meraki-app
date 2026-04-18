# Cisco Meraki Grafana Plugin

App plugin with a nested data source that turns Cisco Meraki Dashboard API responses into Grafana data frames. Every scene page is `@grafana/scenes` — **no provisioned static JSON dashboards.**

**Before making changes:** read `.config/AGENTS/instructions.md` (Grafana-plugin-tools guardrails) and `todos.txt` (full architecture handoff, locked-in decisions, phase roadmap, gotchas).

## Architecture at a glance

```
App plugin (robknight-meraki-app)             Nested DS (robknight-meraki-datasource)
  ├─ Go backend (gpx_meraki)                    ├─ Frontend only — NO backend binary
  ├─ Owns meraki.Client (rate limiter + cache)  ├─ DataSourceApi.query() posts to app's
  ├─ API key in secureJsonData.merakiApiKey     │   /resources/{query,metricFind}
  ├─ Resource endpoints: /ping, /query,         └─ Variable hydration via same path
  │   /metricFind
  └─ CheckHealth → GET /organizations
```

Single API key, one Go binary, one rate limiter + cache shared across every panel and dashboard. Same pattern as Grafana's Synthetic Monitoring app.

## Commands

```bash
npm install                     # JS deps (Node 22+)
npm run dev                     # webpack watch
npm run typecheck               # tsc --noEmit (authoritative; IDE noise is scaffold LSP)
npm run lint                    # ESLint (0 errors required)
npm run test:ci                 # Jest
npm run build                   # webpack production → dist/module.js + dist/datasource/module.js
npm run e2e                     # Playwright (needs `npm run server` running)
npm run server                  # docker compose up --build (Grafana at :3000, admin/admin)

mage -v                         # 6 platform binaries → dist/gpx_meraki_*
mage test                       # go test via SDK helper
go test ./pkg/...               # direct Go unit tests
go vet ./pkg/...
```

## Locked-in decisions (do not revisit without strong reason)

- **Plugin IDs:** `robknight-meraki-app` + `robknight-meraki-datasource`. Renamed from the original `rknightion-*` namespace at the signed-release pass (todos.txt Q.7 — shipped).
- **Go module:** `github.com/robknight/grafana-meraki-plugin`. Independent from plugin ID; can stay as-is after a plugin rename.
- **No Prometheus dependency, no exporter scraping.** `/Users/rob/repos/meraki-dashboard-exporter` is **read-only reference** for endpoint shapes and panel layouts only.
- **Scenes everywhere.** Every page lives under `src/pages/<Area>/`. DO NOT add JSON dashboards under `provisioning/dashboards/`.
- **Rate limit:** per-org token bucket (10 req/s, burst 20, ±10% jitter). `sharedFraction` (0<x≤1) lets operators with N replicas set `1/N`. Distributed limiter is intentionally deferred — rely on 429 + `Retry-After` as cross-replica coordinator.
- **Cache:** in-memory TTL LRU, per plugin instance. No Redis, no cross-replica cache.
- **Query-kind contract is shared between frontend and backend** — see `src/CLAUDE.md` and `pkg/plugin/query/CLAUDE.md`.

## §1.13 Alert-bundle UID + label invariants (v0.6)

The v0.6 alert bundle (§4.5) installs Grafana-managed alert rules into the folder `Meraki (bundled)`. UID scheme, label schema, and deletion safety are load-bearing — changes break idempotent reconcile.

- **UID**: `meraki-<groupId>-<templateId>-<orgId>`. Stable across plugin rename (Q.7) — no plugin-ID substring.
- **Delete gate**: reconciler deletes only rules matching BOTH the `meraki-` UID prefix AND label `managed_by=meraki-plugin`. Prevents clobbering user-authored rules that share the prefix.
- **Template delimiters**: `<% ... %>` in template YAMLs (NOT `{{ }}` — avoids collision with YAML flow-mapping syntax).
- **Full invariants**: see `pkg/plugin/alerts/CLAUDE.md`.

## Repo layout

```
src/                TypeScript + React + @grafana/scenes frontend
pkg/                Go backend: meraki client + plugin app + query dispatcher
provisioning/       Auto-provisioned nested DS + app enable
tests/              Playwright e2e
.config/            Grafana scaffold — DO NOT EDIT (three documented exceptions in todos.txt §2.5)
todos.txt           Full handoff doc: architecture, phase plan, gotchas, acceptance criteria
```

Sub-directory CLAUDE.md files exist for the most common edit surfaces — `src/`, `src/pages/`, `src/datasource/`, `src/scene-helpers/`, `pkg/`, `pkg/meraki/`, `pkg/plugin/`, `pkg/plugin/query/`.

## Critical gotchas

- `.config/` is scaffold-managed. `npx @grafana/create-plugin@latest update` may revert three files with local edits: `bundler/copyFiles.ts`, `docker-compose-base.yaml`, `supervisord/supervisord.conf`. Re-check after any scaffold upgrade (todos.txt §G.14).
- **Config save MUST use `window.location.reload()`** — `locationService.reload()` leaves `plugin.meta` stale and does not re-instantiate the backend with the new secrets (todos.txt §G.15).
- The nested DS has **no backend**. DO NOT set `"backend": true` in `src/datasource/plugin.json`.
- `MERAKI_DS_UID` (`robknight-meraki-ds`) is duplicated in `provisioning/datasources/meraki.yaml` and `src/scene-helpers/datasource.ts` — keep aligned.
- `plugin.json` changes require a **Grafana server restart** before they take effect.

## Reference docs

- Plugin-tools: https://grafana.com/developers/plugin-tools/llms.txt (append `.md` to any page URL for markdown)
- `@grafana/ui` components: https://developers.grafana.com/ui/latest/index.html
- Meraki API docs: `npx ctx7@latest docs /openapi/api_meraki_api_v1_openapispec "<question>"`
- @grafana/scenes: `npx ctx7@latest docs /grafana/scenes "<question>"`

Training data for the Grafana API may be out of date — fetch current docs via `ctx7` or grafana.com directly.
