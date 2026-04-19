# Cisco Meraki — Grafana App Plugin

An open-source Grafana app plugin that turns your Cisco Meraki estate into an observability-ready
experience. The plugin calls the Cisco Meraki Dashboard API directly, transforms the JSON into
Grafana data frames, and renders curated `@grafana/scenes` pages in-app — one install, one API
key, no extra exporter to deploy.

**v0.1 MVP coverage:**

- Organization inventory
- Device status overview (online / alerting / offline / dormant)
- MT environmental sensor readings — temperature, humidity, door, water, CO₂, PM2.5, TVOC, noise,
  battery, and indoor air quality — with native timeseries history
- Nested data source (`robknight-meraki-datasource`) so Meraki queries can also appear in your
  own dashboards alongside Prometheus, Loki, Tempo, etc.

Roadmap phases v0.2+ add MR wireless APs, MS switches, MX security appliances, MV cameras,
MG cellular gateways, alerts, licensing, and API usage dashboards.

## How it works

```
Grafana UI (scenes)               Grafana backend
     │                                 │
     │  metricFindQuery                │
     ├──────────────────▶              │
     │   DataSourceApi.query()         │
     │   (frontend-only DS)            │
     │                                 │
     │  POST /api/plugins/…/resources  │  gpx_meraki (Go)
     │        /{query, metricFind}     │
     ├────────────────────────────────▶│
     │                                 │  Meraki client (per-org token
     │                                 │  bucket, TTL LRU cache, 429
     │                                 │  + 5xx retry, Link/startingAfter
     │                                 │  pagination, time-range clamp)
     │                                 │           │
     │                                 │           ▼
     │                                 │     api.meraki.com/api/v1
     │   data.Frame JSON               │
     │ ◀───────────────────────────────┤
     ▼
  PanelBuilders.*
```

The data source has **no backend of its own** — every query is proxied through the Cisco Meraki
app plugin's resource endpoints. This keeps the API key server-side, centralizes rate limiting,
and lets the in-memory cache be shared across every scene and every dashboard panel.

## Installation

> The plugin is not yet published to the Grafana Catalog. For now, build from source.

### Prerequisites

- Node.js 22+
- Go 1.25+ (go.mod declares 1.25.7; Go 1.26 also works)
- [mage](https://magefile.org/)
- Docker (only needed for `npm run server` / e2e)

### Build

```bash
npm install           # install JS deps
mage -v               # build Go backend binaries for all platforms
npm run build         # build the frontend bundle
```

Outputs land in `dist/`. Copy the contents to `<grafana>/data/plugins/robknight-meraki-app/`
(or mount them via the provided `docker-compose.yaml`).

### Run locally

```bash
npm run server        # starts a Grafana + plugin container via docker compose
```

Grafana is reachable at <http://localhost:3000> (admin/admin). Navigate to **Apps → Cisco
Meraki** and paste your Meraki API key on the configuration tab. The nested `Cisco Meraki` data
source is provisioned automatically.

### Allow the unsigned plugin in production Grafana

For self-hosted Grafana that is not using the dev docker-compose, add the plugin ID to the
`allow_loading_unsigned_plugins` list in `grafana.ini`:

```ini
[plugins]
allow_loading_unsigned_plugins = robknight-meraki-app,robknight-meraki-datasource
```

## Configuration

Open the Meraki app's settings page and provide:

| Field                | Required | Notes                                                                    |
|----------------------|----------|--------------------------------------------------------------------------|
| API key              | ✅        | Meraki Dashboard API key. Stored encrypted by Grafana.                   |
| Base URL             | —        | Optional regional override, e.g. `https://api.meraki.cn/api/v1`.         |
| Shared fraction      | —        | 0 < x ≤ 1. Fraction of the per-org 10 rps limit this instance may use.  |
| Sensor label mode    | —        | `serial` (default) or `name`.                                            |
| Enable per-IP limit  | —        | Opt-in 100 rps / 200 burst limiter for multi-tenant deployments.         |

The **Test connection** button calls `GET /organizations` — a green result tells you the key is
valid and the plugin is reachable.

Get an API key from your Meraki dashboard → Organization → Settings → Dashboard API access. See
Cisco's guide: <https://developer.cisco.com/meraki/api-v1/authorization/>.

### Monitoring your plugin's API usage

The plugin identifies itself to Meraki with a spec-compliant User-Agent of the form
`GrafanaMerakiPlugin/<version> robknight`. Your organization admins can see how much
API traffic this plugin generates from the Meraki dashboard under **Organization →
API & Webhooks → API requests** — filter the `userAgent` column for
`GrafanaMerakiPlugin` to attribute requests back to this integration.
See Cisco's [User-Agent guide](https://developer.cisco.com/meraki/api-v1/user-agents-overview/).

## Bundled alert rules

The plugin ships a curated set of Grafana-managed alert rules. Enable
them per-group from the Configuration page's "Bundled alert rules"
section. Rules are installed into the "Meraki (bundled)" folder; contact
points and notification policies remain your responsibility.

<!-- TODO(docs): screenshot of Configuration → Bundled alert rules section -->

### What gets installed

| Group        | Rules | What they watch                                               |
|--------------|-------|---------------------------------------------------------------|
| availability | 2     | Any device offline; Meraki assurance critical-severity count  |
| wan          | 3     | Appliance uplink down; uplink loss/latency; VPN peer down     |
| sensors      | 3     | MT readings out of range; binary-state change; battery low    |
| wireless     | 2     | AP channel utilisation high; failed-connection rate high      |
| cameras      | 1     | Camera offline                                                |
| lifecycle    | 3     | License expiring (info/warning/critical at 90/30/7 days)      |

**Total: 13 rule templates across 6 groups**, fanned out per Meraki
organisation your API key has access to.

### Label schema

Every installed rule carries the following labels — route them in your
notification policies:

- `severity` (`info` | `warning` | `critical`)
- `meraki_group` (`availability` | `wan` | `wireless` | `sensors` | `cameras` | `lifecycle`)
- `meraki_product` (`appliance` | `switch` | `wireless` | `camera` | `sensor` | empty)
- `meraki_org` (Meraki organisation ID)
- `meraki_rule` (stable template slug)
- `managed_by: meraki-plugin`

Example Grafana notification-policy matcher for critical Meraki alerts:

```yaml
matchers:
  - managed_by = meraki-plugin
  - severity = critical
```

### Source of truth

The plugin UI is authoritative. Thresholds edited directly in Grafana's
Alerting UI will be reverted on the next Reconcile. The Configuration
page surfaces a drift banner when it detects this mismatch.

### Install / uninstall

- **Install a group**: toggle it on in Configuration → Bundled alert
  rules, edit thresholds as desired, click Reconcile.
- **Uninstall everything**: click Uninstall all. Only rules matching
  both `uid` prefix `meraki-` AND label `managed_by=meraki-plugin`
  are removed — user-authored rules are never touched.
- **Per-rule uninstall**: untick the rule, click Reconcile.

### Feature-toggle prerequisite

The install UX calls Grafana's alert provisioning API via a plugin
service account. This requires the `externalServiceAccounts` feature
toggle. It is enabled by default on Grafana Cloud. On self-hosted
Grafana 12.x you may need to enable it in `grafana.ini` or via
`GF_FEATURE_TOGGLES_ENABLE=externalServiceAccounts`. The Configuration
page surfaces a warning banner when the toggle is missing.

## Bundled recording rules

The plugin also ships a curated set of Grafana-managed **recording
rules** that poll Meraki on a schedule via the nested data source and
remote-write the samples into a Prometheus-compatible data source of
your choice. Recording rules are opt-in — nothing is written until you
pick a target data source and toggle at least one group on.

Two motivations, one mechanism:

1. **Trend history for snapshot-only endpoints.** Many Meraki v1
   endpoints (device status overview, appliance uplink statuses,
   cellular signal, alerts overview) return only "now". Recording them
   every N minutes into Prometheus gives you long-term history backed
   by the TSDB you already operate.
2. **Meraki API rate-limit relief for high-traffic endpoints that
   already return history.** `GetOrganizationSwitchPortsOverview`,
   `GetOrganizationDevicesUplinksLossAndLatency`, and friends accept a
   timespan and return a real timeseries — but every dashboard view
   hits Meraki directly. A recording rule fetches once per interval;
   every subsequent dashboard read is served from Prometheus.

This is a distinct feature from Grafana Enterprise's
[Recorded Queries](https://grafana.com/docs/grafana/latest/administration/recorded-queries/),
which stores query results in Grafana's internal database. The plugin
uses [Grafana-managed recording rules](https://grafana.com/docs/grafana/latest/alerting/alerting-rules/create-grafana-managed-rule/#recording-rules)
instead.

### How to enable

1. Open **Apps → Cisco Meraki → Configuration** and scroll to the
   **Bundled recording rules** section.
2. **Pick a target data source** in the DataSourcePicker at the top.
   Only Prometheus-family data sources are listed (Prometheus, Grafana
   Amazon Prometheus, Mimir, Cortex). The Reconcile button stays
   disabled until this is set.
3. Toggle on the groups you want to record, tune any thresholds, and
   click **Reconcile**. Rules install into the `Meraki (bundled
   recordings)` folder with UID prefix `meraki-rec-`.
4. Visit **Alerting → Recording rules** in Grafana to confirm the
   rules, or navigate to `/alerting/recording-rules` directly.

### Panel fallback behaviour

Panels that consume recorded metrics go through the
`trendQuery(...)` helper in `src/scene-helpers/trend-query.ts`. When
the feature is off (or no target data source is set), the helper falls
back to the same direct Meraki query the panel used before v0.7 — so
operators who never enable recordings see the same panels they see
today, just without extended retention. No empty states.

### What gets installed

| Group        | Templates | Metric prefix                                              |
|--------------|-----------|------------------------------------------------------------|
| availability | 1         | `meraki_device_status_count`                               |
| wan          | 4         | `meraki_appliance_uplink_*`, `meraki_wan_uplink_*`         |
| wireless     | 4         | `meraki_ap_client_count`, `meraki_wireless_*`              |
| cellular     | 1         | `meraki_mg_rsrp_dbm` (+ rsrq, sinr)                        |
| switches     | 1         | `meraki_switch_ports_count`                                |
| alerts       | 3         | `meraki_alerts_by_*_count`, `meraki_alerts_history_count`  |

**Total: 14 templates across 6 groups**, fanned out per Meraki
organisation your API key has access to. Metric names are constrained
to `^meraki_[a-z][a-z0-9_]*$` and validated at template-load time.

### Install / uninstall

- **Install a group**: toggle it on, tune thresholds, click Reconcile.
- **Uninstall everything**: click Uninstall all. Only rules matching
  BOTH `uid` prefix `meraki-rec-` AND label
  `managed_by=meraki-plugin` AND label `meraki_kind=recording` are
  removed — user-authored rules are never touched.
- **Per-rule uninstall**: untick the rule, click Reconcile.

### Feature-toggle prerequisite

The install UX uses the same `externalServiceAccounts` Grafana feature
toggle as the alert bundle — enabled by default on Grafana Cloud, and
settable via `grafana.ini` or
`GF_FEATURE_TOGGLES_ENABLE=externalServiceAccounts` on self-hosted
Grafana 12.x. The Configuration page surfaces a warning banner when
the toggle is missing.

## Repository layout

```
src/                         # TypeScript + React + @grafana/scenes frontend
├── components/              # App shell, config form
├── pages/                   # Scene pages (Home, Organizations, Sensors)
├── scene-helpers/           # Shared variables, panels, links
├── datasource/              # Nested Meraki data source (frontend-only)
└── plugin.json              # App manifest
pkg/
├── meraki/                  # Meraki API client: ratelimit, cache, timerange, endpoints
└── plugin/
    ├── app.go               # App instance + CheckHealth
    ├── resources.go         # POST /query, /metricFind, /ping
    └── query/               # Per-kind query → data.Frame handlers
provisioning/
└── datasources/meraki.yaml  # Auto-provisioned nested DS
```

## Development

| Command                   | Purpose                                     |
|---------------------------|---------------------------------------------|
| `npm run dev`             | webpack watch mode                          |
| `npm run typecheck`       | `tsc --noEmit`                              |
| `npm run lint`            | ESLint over frontend                        |
| `npm run test:ci`         | Jest unit tests                             |
| `npm run e2e`             | Playwright end-to-end (requires `server`)   |
| `mage -v`                 | Build every platform backend binary         |
| `mage test`               | `go test ./...` via SDK helper              |
| `go test ./pkg/...`       | Go unit tests directly                      |
| `go vet ./pkg/...`        | Go static analysis                          |

## Contributing

Contributions are welcome. File an issue describing the Meraki product family or scene flow you
want to add and we'll discuss the slice. Every PR should pass `npm run lint`, `npm run typecheck`,
`npm run test:ci`, and `go test ./pkg/...`.

## License

Apache-2.0. See [LICENSE](./LICENSE).

## Acknowledgements

Built on top of `@grafana/create-plugin` and `@grafana/scenes`. API usage patterns are informed
by the excellent open-source
[meraki-dashboard-exporter](https://github.com/rknightion/meraki-dashboard-exporter) (Prometheus).
