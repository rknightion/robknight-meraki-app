# Go backend (`pkg/`)

Module: `github.com/robknight/grafana-meraki-plugin`. Go 1.25+ (go.mod declares 1.25.7; 1.26 works). Built via `mage` → 6 platform binaries `dist/gpx_meraki_*`.

## Layout

```
main.go                          app.Manage("robknight-meraki-app", NewApp, ManageOpts{})
meraki/                          Meraki API client — see pkg/meraki/CLAUDE.md
  client.go                      HTTP, auth, 429+5xx retry, Link pagination
  ratelimit.go                   Per-org token bucket (testable Clock/Sleep/Rand hooks)
  cache.go                       TTL LRU (hashicorp/golang-lru/v2 underneath)
  timerange.go                   EndpointTimeRange, KnownEndpointRanges, Resolve/quantizeUp/FreshnessFloor
  pagination.go                  Link rel=next parser
  errors.go                      UnauthorizedError, NotFoundError, RateLimitError, ServerError, PartialSuccessError
  warmer.go                      Opt-in background cache pre-warmer (loops on jittered intervals)
  version.go                     Plugin version baked in at build — reported in the Meraki User-Agent
  <endpoint>.go                  Per-endpoint-family wrappers: administered, alerts, appliance, camera,
                                 cellular, clients, configuration_changes, devices, events, firmware,
                                 insights, networks, organizations, sensor, switches, topology, traffic,
                                 wireless (17 in total — see pkg/meraki/ directory)
plugin/                          Plugin app — see pkg/plugin/CLAUDE.md
  app.go                         App, Settings, LabelMode, NewApp factory, CheckHealth
  resources.go                   HTTP mux: /ping, /query, /metricFind, /alerts/*, /recordings/*
  grafanaclient.go               Thin HTTP wrapper around local Grafana API (provisioning CRUD +
                                 folder bootstrap) — uses the externalServiceAccounts-issued token
  meraki_adapter.go              Narrow shim: *meraki.Client → alerts.MerakiAPI (org list only)
  alerts_store.go                On-disk persistence of last-reconcile summary (alerts bundle)
  recordings_store.go            On-disk persistence of last-reconcile summary (recordings bundle)
  query/                         Query dispatcher (90+ kinds) — see pkg/plugin/query/CLAUDE.md
  alerts/                        Alert-rule bundle — see pkg/plugin/alerts/CLAUDE.md
  recordings/                    Recording-rule bundle — see pkg/plugin/recordings/CLAUDE.md
```

## Go module path vs plugin ID

They're independent. The Go module is `github.com/robknight/...` (namespaced to Rob's GitHub account) and the plugin IDs are `robknight-*` (namespaced to the Grafana org). The module path retains `grafana-meraki-plugin` as its repo-name segment from before the folder rename — that's fine because the backend is built as a binary and isn't consumed as a library via `go get`.

## Dependencies

- `github.com/grafana/grafana-plugin-sdk-go` — framework
- `github.com/hashicorp/golang-lru/v2` — cache
- `golang.org/x/time/rate` — companion for the token-bucket in ratelimit.go

New deps need a clear justification in the PR description.

## Build + test

```bash
mage -v                         # buildAll: 6 platforms
mage test                       # go test via SDK helper
go test ./pkg/...               # direct
go vet ./pkg/...
```

CI installs mage via `magefile/mage-action`. Locally: `go install github.com/magefile/mage@latest` (todos.txt §G.13).
