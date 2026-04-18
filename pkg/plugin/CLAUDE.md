# App plugin backend (`pkg/plugin/`)

The Grafana app plugin backend. Owns the shared `meraki.Client`, exposes resource endpoints, implements `CheckHealth`.

## Files

```
app.go              App struct (instance), Settings (merged jsonData + secureJsonData), LabelMode,
                    NewApp factory, Dispose, Client(), Configured(), CheckHealth, buildClient
resources.go        HTTP routes: /ping, /query, /metricFind; registerRoutes(mux)
resources_test.go   Unit tests for the HTTP handlers
query/              Per-kind query dispatcher — see pkg/plugin/query/CLAUDE.md
```

## Instance lifecycle

Grafana calls `NewApp(ctx, AppInstanceSettings)` per plugin instance. `NewApp`:

1. Unmarshals `JSONData` into `appJSONData` and reads `merakiApiKey` from `DecryptedSecureJSONData`.
2. If an API key is present, builds a shared `meraki.Client` with `RateLimiter{10 rps, burst 20, jitter 10%}` and a 2048-entry `TTLCache`.
3. Registers `/ping`, `/query`, `/metricFind` on a `ServeMux` wrapped by `httpadapter.New`.

`Dispose()` is a no-op — the client has no long-lived resources to close.

## Settings

```go
type Settings struct {
    BaseURL        string     // optional regional override (default https://api.meraki.com/api/v1)
    SharedFraction float64    // 0<x≤1; 1/N for N replicas
    APIKey         string     // from secureJsonData.merakiApiKey
    IsApiKeySet    bool
    LabelMode      LabelMode  // "serial" (default) | "name"
}
```

`LabelMode` is threaded through `query.Options` so every per-device timeseries handler — `handleSensorReadingsHistory`, `handleWirelessChannelUtil`, `handleDeviceUplinksLossLatency`, `handleCameraAnalyticsOverview`, `handleCameraAnalyticsZoneHistory` — can switch legend labels between raw serial and human-friendly device name. In `serial` mode the handlers skip the `/devices` lookup entirely.

## Resource endpoints

| Path          | Method | Behaviour                                                              |
|---------------|--------|------------------------------------------------------------------------|
| `/ping`       | GET    | Liveness + `{configured: bool}` — used by `<ConfigGuard>` on scenes   |
| `/query`      | POST   | Dispatches a `QueryRequest` → `[]data.Frame` (JSON-serialized)         |
| `/metricFind` | POST   | Single-query variable hydration → `[]{text, value}`                   |

`/query` and `/metricFind` both return **412 Precondition Failed** when the API key isn't set. `<ConfigGuard>` on the frontend surfaces this as a friendly banner (todos.txt §G.10).

## CheckHealth

Calls `GET /organizations` with a 15s timeout and converts typed errors (`IsUnauthorized`, `IsRateLimit`) into friendly messages. Returned to the config form's "Test connection" button.

## Conventions

- Unknown query kinds become errors in `runOne()` that get attached as frame notices — they do NOT 500 the whole request.
- When adding a route, keep it thin: parse JSON, short-circuit on `!Configured()`, call the domain package, marshal response. Domain logic lives in `query/` and `meraki/`.
