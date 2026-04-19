# Query dispatcher (`pkg/plugin/query/`)

Every `POST /resources/query` call ends up here. Each `MerakiQuery.Kind` is dispatched to a handler that returns `[]*data.Frame`.

## Files

The package has grown to 90+ query kinds across ~40 handler files. **`dispatch.go` is the canonical index** — its `handlers` map is the source of truth for what's wired up. Browse that when you need to find the handler for a given kind; the list below is grouped by product-family for orientation:

```
dispatch.go                 QueryKind constants + Options + handlers map. Canonical kind list.
dispatch_test.go            Per-kind smoke tests via httptest.NewServer stubbing api.meraki.com.
metricfind.go               Variable hydration (Organizations, Networks, Sensors metrics list, ...).
device_names.go             resolveDeviceNames(ctx, client, orgID, productTypes...) — shared helper.

# Org / device / cross-cutting
organizations.go             org_product_types.go        org_health.go         org_change_feed.go
networks.go                  devices.go                  device_status_overview.go
device_status_by_family.go   device_offline_count.go     device_urls.go        devices_memory_test.go
availabilities.go            availabilities_counts.go    availabilities_change_history.go
configuration_changes.go     configuration_changes_annotation.go   mttr.go    insights.go

# Family handlers
sensor_readings.go           sensor_summary.go           sensor_floorplan.go
wireless.go                  wireless_1a.go              # MR
switches.go                  switch_ports_overview.go    # MS
appliance.go                 appliance_mx_panels.go      # MX
camera.go                    cellular.go                 # MV / MG

# Cross-page catalogues
alerts.go                    events.go                   events_timeline.go
clients.go                   firmware.go                 traffic.go            topology.go
```

Every `.go` here has a sibling `_test.go`. When grep doesn't find a handler by name, search the `handlers` map in `dispatch.go` first — it's the authoritative wire-format index.

## Query-kind contract — keep in sync

| Frontend (src/datasource/types.ts) | Backend (dispatch.go)             |
|-------------------------------------|-----------------------------------|
| `QueryKind` enum (string values)    | `KindXxx` typed string constants |
| `MerakiQuery` interface             | `MerakiQuery` struct              |

Wire format is JSON; the discriminator is `kind`. **If you change one side, change the other in the same commit.**

## Adding a new query kind (7-step recipe)

1. Add the kind to BOTH `src/datasource/types.ts` `QueryKind` enum AND `pkg/plugin/query/dispatch.go` `Kind...` constants.
2. Add an endpoint wrapper in `pkg/meraki/` if not already present (see `pkg/meraki/CLAUDE.md`).
3. If the endpoint is timeseries, add an `EndpointTimeRange` entry in `pkg/meraki/timerange.go` `KnownEndpointRanges`.
4. Create `pkg/plugin/query/<kind>.go` with a handler:
   ```go
   func handleXxx(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error)
   ```
5. Register the handler in `dispatch.go` `handlers` map.
6. For variable hydration, extend `metricfind.go`.
7. Extend `src/datasource/QueryEditor.tsx` so users can pick the kind manually in panel queries.

Add a smoke test to `dispatch_test.go` that stubs the Meraki response with `httptest.NewServer` and asserts the emitted frame shape.

## Handler signature — Options is mandatory

```go
type handlerFn func(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error)
```

**Accept `Options` even if you don't use it — `_ Options` is fine.** The dispatcher threads plugin settings (`LabelMode` today, more later) through `Handle → runOne → handler`. Leaving it out breaks the interface (todos.txt §G.19).

## Frame-notice error pattern (todos.txt §1.10)

Per-query errors become `data.Notice{Severity: Error, Text: ...}` attached to the **first frame only** (or a synthesized `<refId>_error` placeholder when the handler returned no frames). A single bad query does NOT kill the whole panel or siblings in the batch. Keep this pattern for every new kind.

## Frame shape — one-frame-per-series for timeseries

Native-timeseries frames **must be one frame per series with `data.Labels` on the value field**. A single long-format frame `(ts, serial, metric, value)` renders as an **empty chart** — Grafana can't infer series grouping from a mixed `(time, string, number)` shape, and a client-side `partitionByValues` transform does NOT fix it.

Canonical shapes to copy from:
- `sensor_readings.go:handleSensorReadingsHistory` — one frame per `(serial, metric)`.
- `wireless.go:handleWirelessChannelUtil` — one frame per `(serial, band)`.

## KPI aggregation: server-side > client-side (todos.txt §G.20)

Client-side `filterByValue + reduce` transform chains silently fall back to the wrong reducer in some Grafana versions. That's why `sensorAlertSummary` + `alertsOverview` exist as dedicated kinds: **one wide frame, one field per KPI**. Every future KPI row (switch port totals, AP availability, licensing) should follow the same pattern.

## Option overloads

A couple of query kinds borrow `q.Metrics[0]` to smuggle a scalar without changing `MerakiQuery`:

- `alerts` → `q.Metrics[0]` is the severity filter (single value; blank = all severities).
- `switchPortPacketCounters` → `q.Metrics[0]` is the port ID.

Kept this way to avoid churn on the wire contract. If a third kind needs a scalar, consider adding a dedicated field instead.

### Alerts status — dedicated field, NOT a positional overload

`MerakiQuery.AlertStatus` (values `"active" | "resolved" | "dismissed" | "all"`) is the first-class filter for the alerts-lifecycle picker and MUST be used by new scenes. The handler still reads `q.Metrics[1]` as a fallback for legacy callers, but new code sets `alertStatus` on the query object. Default when omitted: `"active"`.

**Why a dedicated field:** the old positional encoding (`metrics[0]=severity`, `metrics[1]=status`) broke when the frontend's CSV template interpolation dropped the empty `$severity` placeholder — `"all"` shifted into the severity slot and Meraki HTTP 500'd on `severity=all`. See comment on `alertsStatusSentinel` in `alerts.go` for the full history.

**Time-filter semantics** (load-bearing, re-derive at your peril):
- `active` / `all` → no `tsStart`/`tsEnd` applied. Meraki filters on `alert.startedAt`, so narrowing the window hides long-running active alerts whose `startedAt` predates the picker.
- `resolved` / `dismissed` → picker window applied. Audit views genuinely want "incidents that started in this period".

## Events multi-family fan-out

`handleNetworkEvents` expands `q.ProductTypes` empty (the `$productType=All` picker) into one request per product family the target network actually has — Meraki's `/networks/{id}/events` 400s on multi-family networks without a `productType`. The whitelist of valid families lives in `networkEventsProductTypes` and **excludes `sensor`**: MT is a legitimate productType on networks but the events endpoint rejects it with 400. Per-family fetch failures during fan-out are tolerated (first-error preserved, surfaced as a notice) so one rejected family doesn't zero out the whole panel.

## Switches: org-level vs device-scoped endpoints

`/organizations/{orgId}/switch/ports/statuses/bySwitch` returns a **minimal port shape** — no `clientCount`, `powerUsageInWh`, `usageInKb`, `trafficInKbps`. Use it only for fleet inventory / port-map panels that don't expose those columns.

- **Detail pages (`q.Serials` non-empty)** → `fetchPerSwitchPortStatuses` fans out to `/devices/{serial}/switch/ports/statuses` + `/devices/{serial}/switch/ports` (config for VLANs) merged by port ID.
- **Fleet aggregation (PoE totals, client counts)** → `fetchSwitchesForAggregate` fans out to device-scoped statuses for up to `fleetFanoutCap = 25` switches; larger estates return the minimal shape plus a truncation notice rather than silently under-counting.

## DisplayName templating (todos.txt §G.17)

`FieldConfig.DisplayNameFromDS` is a **pre-formatted final string** — Grafana does NOT template-substitute it. Use `FieldConfig.DisplayName` with `${__field.labels.<name>}` when you need interpolation, or bake the final string at emit time. Symptom of getting this wrong: the legend renders as literal `{{serial}}`.

## Column naming

- Prefer Meraki-native JSON field names (`serial`, `name`, `model`, `networkId`, `orgId`).
- For historical timeseries, the time column is `ts` (matches Meraki's response). Labels live on the value field via `data.Field.Labels`.
