# Query dispatcher (`pkg/plugin/query/`)

Every `POST /resources/query` call ends up here. Each `MerakiQuery.Kind` is dispatched to a handler that returns `[]*data.Frame`.

## Files

```
dispatch.go                 QueryKind constants, MerakiQuery/TimeRange/Options, Handle, runOne,
                            handlerFn type, handlers map, error-frame wrapper
dispatch_test.go            Smoke tests per kind via httptest.NewServer stubbing api.meraki.com
metricfind.go               Variable hydration (Organizations, Networks, Sensors metric list, ...)
device_names.go             resolveDeviceNames(ctx, client, orgID, productTypes...) — shared helper
organizations.go            handleOrganizations
networks.go                 handleNetworks
devices.go                  handleDevices
device_status_overview.go   handleDeviceStatusOverview
availabilities.go           handleDeviceAvailabilities
sensor_readings.go          handleSensorReadingsLatest + handleSensorReadingsHistory
sensor_summary.go           handleSensorAlertSummary (wide frame per todos.txt §G.20)
wireless.go                 handleWireless{ChannelUtil,Usage,NetworkSsids,ApClients}
alerts.go                   handleAlerts + handleAlertsOverview
switches.go                 handleSwitch{Ports,PortConfig,PortPacketCounters}
```

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

- `alerts` → `q.Metrics[0]` is the severity filter.
- `switchPortPacketCounters` → `q.Metrics[0]` is the port ID.

Kept this way to avoid churn on the wire contract. If a third kind needs a scalar, consider adding a dedicated field instead.

## DisplayName templating (todos.txt §G.17)

`FieldConfig.DisplayNameFromDS` is a **pre-formatted final string** — Grafana does NOT template-substitute it. Use `FieldConfig.DisplayName` with `${__field.labels.<name>}` when you need interpolation, or bake the final string at emit time. Symptom of getting this wrong: the legend renders as literal `{{serial}}`.

## Column naming

- Prefer Meraki-native JSON field names (`serial`, `name`, `model`, `networkId`, `orgId`).
- For historical timeseries, the time column is `ts` (matches Meraki's response). Labels live on the value field via `data.Field.Labels`.
