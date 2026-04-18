# Bundled alert rules (`pkg/plugin/alerts/`)

Registry + renderer for the alert-rule templates the plugin provisions into
Grafana on behalf of the operator. See `todos.txt` §4.5 for the full phase
roadmap; this package is the Phase 1 foundation.

## Files

```
registry.go        Group, Template, ThresholdSchema, Registry; LoadRegistry + Render
grafana_rule.go    AlertRule / AlertQuery / RelativeTimeRange / Folder (Grafana wire shape)
registry_test.go   LoadRegistry + duplicate-detection + unknown-kind tests
templates_test.go  Golden-file test runner (one fixture per template × org)
templates/         //go:embed source tree, one YAML per template, grouped by folder
testdata/          Golden fixtures — one `<group>-<id>-<orgID>.golden.json` per template
```

## UID scheme — stable across plugin rename

```
meraki-<group>-<template>-<orgID>
```

Computed by `Template.Render()`. This is the primary key the reconciler uses
when GETting/PUTting rules against Grafana's provisioning API, so **the
format is a hard contract**. Do not change it without a migration path.
The plugin ID is deliberately absent so the UIDs survive rename from
`rknightion-*` to `robknight-*` (or any future rename).

## Label schema (every rendered rule carries these)

| Label           | Value                                          |
|-----------------|------------------------------------------------|
| `severity`      | `info` / `warning` / `critical`                |
| `meraki_group`  | Group ID (`availability`, `wan`, ...)          |
| `meraki_product`| Optional — product family (mx/ms/mr/mt/…)      |
| `meraki_org`    | Org ID                                         |
| `meraki_rule`   | Template ID slug                               |
| `managed_by`    | Always `meraki-plugin` (reconciler sentinel)   |

`managed_by=meraki-plugin` is the reconciler's filter: anything with this
label is in-scope for reconciliation, anything without it is user-owned
and left alone. See Phase 2 (`reconciler.go`, arriving in §4.5.4).

## Template YAML shape

```yaml
kind: alert_rule_template
id: <slug>
group: <group-slug>
display_name: <human readable>
severity: info | warning | critical
thresholds:
  - key: <slug>
    type: int | float | string | duration | list
    default: <value>
    label: <UI label>
    help:  <tooltip>
    options: [a, b, c]   # required when type=list and overrides are allowed
rule:
  # Text/template body — see rendering rules below.
```

### Rendering rules

`Template.Render(orgID, overrides)` is the single entry point and is pure.

1. Defaults from `thresholds` merge with `overrides` (overrides win).
2. The `rule:` subtree is re-emitted as YAML text.
3. That text is run through `text/template` with **`<% %>` delimiters**
   (not Go's default `{{ }}`) so the source YAML is valid YAML before
   substitution. The template context exposes `.OrgID` and
   `.Thresholds.<key>`.
4. For list thresholds, use the helper `<% yamlList .Thresholds.foo %>`
   to emit a JSON-compatible flow sequence. Plain `<% .Thresholds.foo %>`
   interpolates `%v` which is wrong for lists.
5. The rendered YAML is decoded → JSON-marshalled → decoded into
   `AlertRule`. FolderUID, RuleGroup, UID and default states
   (`NoDataState=NoData`, `ExecErrState=Error`) are backfilled by
   Render() — do NOT set them in the YAML.

Unknown YAML keys are rejected (`KnownFields(true)`). Missing template keys
are a hard error (`missingkey=error`).

## Golden-fixture workflow

Every template gets one fixture at `testdata/<group>-<id>-987654.golden.json`.

```bash
# First time you add a template, or after an intentional rendering change:
go test ./pkg/plugin/alerts/... -run TestGolden -update
git diff pkg/plugin/alerts/testdata/
# Inspect. If the diff is what you expected, commit the fixture.

# Normal test runs (CI + local):
go test ./pkg/plugin/alerts/...
```

A fixture diff is a canary: any accidental change to rendering shows up here
before it reaches Grafana.

## Adding a new template

1. Pick the correct group directory under `templates/`. Create it if needed.
2. Write `<id>.yaml`. Start by copying an existing template in the same
   group — the structure (thresholds + rule tree) is load-bearing.
3. Ensure every string containing `<% %>` is quoted; list thresholds must
   use `<% yamlList .Thresholds.<key> %>` unquoted.
4. Run `go test ./pkg/plugin/alerts/... -update` to create the golden
   fixture; inspect and commit.
5. If the template introduces a new query kind, add it to
   `pkg/plugin/query/dispatch.go` AND `src/datasource/types.ts` first
   (see `pkg/plugin/query/CLAUDE.md`).
6. Update the frontend registry mirror (Phase 3, `src/pages/Alerts/`) so
   operators can actually surface it in the UI.

## Phase pointers (future files)

- **Phase 2 — `reconciler.go`** (this package): GETs existing rules by
  `managed_by=meraki-plugin` label, diffs against desired state, issues
  create/update/delete calls through a `GrafanaClient` interface defined
  here so we avoid a circular import.
- **Phase 3 — resource endpoints + frontend**: `pkg/plugin/resources.go`
  grows `/alerts/bundled/*` routes that the Alerts scene page consumes.

## Reconcile-summary persistence (§4.5.5)

The `/alerts/reconcile` and `/alerts/uninstall-all` resource endpoints
persist their `ReconcileResult` summary — specifically `{created, updated,
deleted, failed}` counts + an ISO timestamp — to a plugin-local JSON file
so `/alerts/status` can surface the last-run state after a plugin restart.

**Why a file and not a `PUT /api/plugins/<id>/settings` round-trip?** Both
approaches were considered. The Grafana API path survives process restarts
just as well but requires:

1. An extra HTTP call on every reconcile (which can already issue hundreds
   of PUTs to Grafana).
2. The plugin to hold its AppURL + client secret at reconcile time — doable
   via `backend.GrafanaConfigFromContext(ctx)` but makes every test that
   exercises the reconcile handler need a cfg fixture.
3. Tolerance for a race where Grafana overwrites jsonData between the
   reconcile returning and the persistence PUT landing.

The file path (`$GF_PATHS_DATA/plugins/<pluginID>/alerts-state.json`)
avoids all three. It's a single best-effort atomic rename per reconcile,
works in tests with zero plumbing, and keeps the persisted surface narrow
(just the summary — NOT the full desired-state). User toggles + threshold
overrides still live in jsonData; they're authored by the AppConfig UI and
propagated via the normal Grafana settings-save flow.

See `pkg/plugin/alerts_store.go` for the implementation.

## E2E mock (§4.5.8)

`e2e_mock.go` in this package provides `InMemoryGrafana`, a drop-in
`GrafanaAPI` implementation that captures every CRUD call into in-memory
maps. It is wired in from `pkg/plugin/app.go` when the plugin process is
launched with `E2E_MOCK_GRAFANA=1` — at that point the App swaps BOTH
surfaces:

- `newGrafanaAPI` → a shared `*InMemoryGrafana` (same instance for every
  request, so a Playwright session sees one coherent Grafana).
- `newGrafanaProber` → `e2eReadyProber` (always-ready).
- `alertsMerakiOverride` → a two-org static `MerakiAPI` (ids `111111`,
  `222222`). This lets Reconcile fan out to real template rendering
  without a live Meraki API key.
- `Configured()` → returns true so `/alerts/reconcile` passes its
  precondition gate.

**Scope:** only the `/alerts/*` handler surface is affected. `/query` and
`/metricFind` still dereference `a.client` directly and will 412 when no
key is configured, which is fine — Playwright alerts spec doesn't touch
those endpoints.

**Activation:** opt-in per run. `.config/docker-compose-base.yaml` does
NOT set the flag by default; a developer running the alerts spec exports
`E2E_MOCK_GRAFANA=1` before `docker compose up` (see
`.config/AGENTS/e2e-testing.md` for the full procedure). Setting it by
default would mask real-Grafana regressions on the dev lab.

**Non-hermetic alternative:** render + banner tests in `tests/alerts.spec.ts`
use Playwright `page.route()` to intercept `/resources/alerts/{templates,
status}` client-side, which doesn't need the env var. That's the cheaper
path when the test only needs to assert a specific response shape; use the
Go stub when you need to exercise the real reconciler diff algorithm.
