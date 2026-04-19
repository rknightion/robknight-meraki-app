# Bundled recording rules (`pkg/plugin/recordings/`)

Registry + renderer + reconciler for Grafana-managed **recording** rules the
plugin provisions into Grafana on behalf of the operator. Companion to the
v0.6 alert bundle (`pkg/plugin/alerts/`) — the two reuse the shared
`AlertRule` wire shape (same provisioning endpoint) but install into
separate folders and label-gate their reconciles with distinct
`meraki_kind` labels.

See `todos.txt §4.6` for the full phase plan and §1.14 for the invariants.

## Why recording rules at all

Two use cases, one mechanism (see plan file in
`~/.claude/plans/we-need-to-investigate-crystalline-aho.md`):

1. **Trend history for snapshot-only endpoints.** Meraki endpoints like
   `deviceStatusOverview`, `applianceUplinkStatuses`, `mgUplinks` return
   only "now". Recording them every N minutes into Prometheus gives
   long-term history backed by the operator's own TSDB. Supersedes the
   §4.2 ring-buffer proposal.
2. **Meraki API rate-limit relief for high-traffic endpoints that already
   return history.** `SwitchPortsOverview`, `UplinksLossAndLatency`, etc.
   accept timespans and return timeseries. Recording centralises the
   Meraki fetch on a single interval-driven rule so every dashboard view
   reads from Prometheus instead.

## Files

```
registry.go              LoadRegistry + LoadRegistryFS, Template, Render,
                         threshold plumbing (mirrors alerts/registry.go with
                         recording-rule tweaks: `recording_rule_template`
                         kind, `meraki-rec-` UID prefix, `Record` block
                         injected, for="0s" forced, no condition / state
                         defaults).
registry_test.go         LoadRegistry + duplicate-detection + kind rejection.
templates_test.go        (§4.6.2) Golden-file runner — one fixture per
                         template × org.
reconciler.go            (§4.6.3) Idempotent diff/apply over the shared
                         GrafanaAPI interface.
templates/               //go:embed source tree, one YAML per template,
                         grouped by folder.
testdata/                (§4.6.2) Golden fixtures, one JSON per
                         (template, org).
```

## UID scheme — disjoint from alerts

```
meraki-rec-<group>-<template>-<orgID>
```

The `-rec-` infix prevents collision with alert UIDs
(`meraki-<group>-<template>-<orgID>`). Both bundles share the
`managed_by=meraki-plugin` label, so the recording reconciler MUST filter
by the additional `meraki_kind=recording` label when deciding which rules
are in scope for its delete gate. Symmetrically, the alerts reconciler's
filter must remain tight enough not to match `meraki_kind=recording` rules.

## Label schema (every rendered rule carries these)

| Label           | Value                                           |
|-----------------|-------------------------------------------------|
| `meraki_kind`   | Always `recording` (reconciler kind filter)     |
| `meraki_group`  | Group ID (`availability`, `wan`, `wireless`, …) |
| `meraki_org`    | Org ID                                          |
| `meraki_rule`   | Template ID slug                                |
| `managed_by`    | Always `meraki-plugin` (shared with alerts)     |

Optional labels added per template where meaningful (e.g. `meraki_product`
for product-scoped rules). Unlike alerts, recording rules do NOT carry a
`severity` label — recording rules don't fire.

## Folder

```
Meraki (bundled recordings)   uid = meraki-bundled-rec-folder
```

Distinct from the alert bundle folder (`Meraki (bundled)` /
`meraki-bundled-folder`) so operators can find recording rules at
`/alerting/recording-rules` and alerts at `/alerting/list`.

## Metric-name contract

Every recording rule emits one metric. Metric names MUST match
`^meraki_[a-z][a-z0-9_]*$` (a valid Prometheus name, prefixed with
`meraki_`, snake_case). `LoadRegistry` enforces this at template-load
time — bad names are a startup error, not a reconcile-time surprise.

The canonical list of metric names also lives at
`src/scene-helpers/recording-metrics.ts` so frontend panels can import the
same string literal both for the recording-rule payload and for their
fallback-vs-recorded PromQL selection.

## Template YAML shape

```yaml
kind: recording_rule_template
id: <slug>
group: <group-slug>
display_name: <human readable>
thresholds:           # optional; same schema as alerts
  - key: <slug>
    type: int | float | string | duration | list
    default: <value>
    label: <UI label>
    help:  <tooltip>
    options: [a, b, c]   # required when type=list and overrides are allowed
rule:
  title:  "<% .OrgID %>-scoped title"
  data:   [...]          # exactly like alerts
  record:
    metric: "meraki_<group>_<name>"
    from:   "A"          # refId within data[]
  labels:
    managed_by: meraki-plugin
    meraki_kind: recording
    meraki_group: <group>
    meraki_org: "<% .OrgID %>"
    meraki_rule: <id>
  annotations: { … }     # optional
```

### Rendering rules (delegated to `Template.Render()`)

`Template.Render(orgID, overrides, targetDsUID)` is the single entry point
and is pure.

1. Defaults from `thresholds` merge with `overrides` (overrides win).
2. The `rule:` subtree is re-emitted as YAML text.
3. Text/template runs with **`<% %>` delimiters** (same as alerts) so YAML
   remains valid before substitution. Context: `.OrgID`, `.Thresholds.<key>`.
4. The rendered YAML is decoded → JSON-marshalled → decoded into
   `alerts.AlertRule`. Backfilled by Render():
   - `FolderUID = bundledRecordingsFolderUID`
   - `RuleGroup = t.GroupID`
   - `UID = meraki-rec-<group>-<tpl>-<org>`
   - `For = "0s"` (Grafana requirement for recording rules)
   - `Record.TargetDatasourceUID = targetDsUID` (from caller)
5. Render **does NOT** set `NoDataState`, `ExecErrState`, or `Condition` —
   Grafana rejects these on recording rules. The `omitempty` tags on
   `AlertRule` ensure they drop out of the JSON payload when unset.

Missing template keys are a hard error (`missingkey=error`). Unknown YAML
keys are rejected (`KnownFields(true)`).

## Reconciler idempotency contract (§4.6.3)

`reconciler.go` GETs every rule in the recordings folder, filters to
`managed_by=meraki-plugin` AND `meraki_kind=recording`, and computes a
`ruleSignature` for both existing and desired rules. Two rules with the
same signature are skipped (no PUT).

The signature covers fields the plugin owns — title, data, labels,
annotations, for, AND the **Record block**. It ignores Grafana-added
fields (updated, provenance, version, is_paused). Operators may pause a
rule in the UI — reconciler leaves `is_paused` alone across a no-op
reconcile. An explicit toggle-off followed by reconcile still deletes the
rule (that's the point of the kind filter).

Rules that match the `managed_by` label but lack `meraki_kind=recording`
are NEVER touched by this reconciler — they belong to the alerts bundle or
a user. Symmetrically, the alerts reconciler must not delete rules with
`meraki_kind=recording`.

## Target datasource

`Render()` takes a `targetDsUID string` argument. When empty, Render
returns an error rather than emitting a Record block without
`target_datasource_uid`. The resource handler
(`POST /recordings/reconcile`) is responsible for reading
`jsonData.recordings.targetDatasourceUid`, returning 412 Precondition
Failed if it's empty, and passing it through to Render.

## Adding a new template

1. Pick the correct group directory under `templates/`. Create it if needed.
2. Write `<id>.yaml` — start by copying an existing template in the same
   group.
3. Name the metric `meraki_<group>_<name>` (snake_case, matches the regex).
4. Export the metric name as a const in
   `src/scene-helpers/recording-metrics.ts` so the panel fallback helper
   references the same literal.
5. Run `go test ./pkg/plugin/recordings/... -update` (§4.6.2) to create
   the golden fixture; inspect and commit.
6. Add a panel that calls `trendQuery(...)` with the new metric +
   existing Meraki query-kind fallback, so users without the feature
   still see live data.
