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
- Nested data source (`rknightion-meraki-datasource`) so Meraki queries can also appear in your
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

Outputs land in `dist/`. Copy the contents to `<grafana>/data/plugins/rknightion-meraki-app/`
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
allow_loading_unsigned_plugins = rknightion-meraki-app,rknightion-meraki-datasource
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
`GrafanaMerakiPlugin/<version> rknightion`. Your organization admins can see how much
API traffic this plugin generates from the Meraki dashboard under **Organization →
API & Webhooks → API requests** — filter the `userAgent` column for
`GrafanaMerakiPlugin` to attribute requests back to this integration.
See Cisco's [User-Agent guide](https://developer.cisco.com/meraki/api-v1/user-agents-overview/).

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
