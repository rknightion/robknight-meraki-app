package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

var (
	_ backend.CallResourceHandler   = (*App)(nil)
	_ instancemgmt.InstanceDisposer = (*App)(nil)
	_ backend.CheckHealthHandler    = (*App)(nil)
)

// App is the Meraki app plugin backend. It owns a shared meraki.Client scoped to the plugin
// instance — rate limiter and cache are shared across all requests to this plugin, including
// the nested datasource.
type App struct {
	backend.CallResourceHandler
	settings Settings
	client   *meraki.Client
	logger   log.Logger
}

// LabelMode controls how per-device series are labeled on timeseries panels
// across every Meraki device family the plugin supports (sensors, access
// points, switches, appliances, cameras, cellular gateways).
// Must match src/types.ts DeviceLabelMode.
type LabelMode string

const (
	LabelModeSerial LabelMode = "serial"
	LabelModeName   LabelMode = "name"
)

// Settings is the merged non-secret + secret configuration for a plugin instance.
type Settings struct {
	BaseURL         string
	SharedFraction  float64
	APIKey          string
	IsApiKeySet     bool
	LabelMode       LabelMode
	// EnableIPLimiter turns on the 100 rps per-source-IP token bucket. Useful for
	// multi-tenant deployments where many orgs egress via a single Grafana IP —
	// Meraki's per-IP cap is independent of the per-org cap (todos.txt §7.4-G).
	EnableIPLimiter bool
}

// appJSONData mirrors src/types.ts AppJsonData.
type appJSONData struct {
	BaseURL         string    `json:"baseUrl,omitempty"`
	SharedFraction  float64   `json:"sharedFraction,omitempty"`
	IsAPIKeySet     bool      `json:"isApiKeySet,omitempty"`
	LabelMode       LabelMode `json:"labelMode,omitempty"`
	EnableIPLimiter bool      `json:"enableIPLimiter,omitempty"`
}

// NewApp is the factory invoked by Grafana for each plugin instance.
func NewApp(_ context.Context, s backend.AppInstanceSettings) (instancemgmt.Instance, error) {
	settings, err := loadSettings(s)
	if err != nil {
		return nil, fmt.Errorf("meraki: load settings: %w", err)
	}
	logger := log.DefaultLogger.With("plugin", "rknightion-meraki-app")

	var client *meraki.Client
	if settings.APIKey != "" {
		client, err = buildClient(settings, logger)
		if err != nil {
			return nil, fmt.Errorf("meraki: build client: %w", err)
		}
	}

	app := &App{
		settings: settings,
		client:   client,
		logger:   logger,
	}
	mux := http.NewServeMux()
	app.registerRoutes(mux)
	app.CallResourceHandler = httpadapter.New(mux)
	return app, nil
}

func loadSettings(s backend.AppInstanceSettings) (Settings, error) {
	settings := Settings{}
	if len(s.JSONData) > 0 {
		var jd appJSONData
		if err := json.Unmarshal(s.JSONData, &jd); err != nil {
			return settings, err
		}
		settings.BaseURL = jd.BaseURL
		settings.SharedFraction = jd.SharedFraction
		settings.IsApiKeySet = jd.IsAPIKeySet
		settings.LabelMode = jd.LabelMode
		settings.EnableIPLimiter = jd.EnableIPLimiter
	}
	if settings.LabelMode != LabelModeName {
		// Default to serial — matches the current shipped behaviour and keeps
		// the legend short for users who haven't touched the setting.
		settings.LabelMode = LabelModeSerial
	}
	if v, ok := s.DecryptedSecureJSONData["merakiApiKey"]; ok {
		settings.APIKey = v
	}
	return settings, nil
}

func buildClient(s Settings, logger log.Logger) (*meraki.Client, error) {
	fraction := s.SharedFraction
	if fraction <= 0 {
		fraction = 1.0
	}
	rl := meraki.NewRateLimiter(meraki.RateLimiterConfig{
		RequestsPerSecond: 10,
		Burst:             20,
		SharedFraction:    fraction,
		JitterRatio:       0.1,
	})
	var ipLimiter *meraki.RateLimiter
	if s.EnableIPLimiter {
		// Meraki's per-source-IP cap is 100 rps; burst 200 matches the 2x ratio used for
		// the per-org limiter so short spikes don't get spuriously throttled. SharedFraction
		// matches the org limiter so replica-aware operators get consistent headroom.
		ipLimiter = meraki.NewRateLimiter(meraki.RateLimiterConfig{
			RequestsPerSecond: 100,
			Burst:             200,
			SharedFraction:    fraction,
			JitterRatio:       0.1,
		})
	}
	// Per-org cache: 512 entries per org (up from the old 2048 global pool). JitterRatio
	// 0.1 desynchronises expirations across replicas; NotFoundTTL 60s negative-caches 404s
	// so a broken endpoint doesn't round-trip on every panel refresh.
	cache, err := meraki.NewTTLCacheWithConfig(meraki.TTLCacheConfig{
		PerOrgSize:  512,
		JitterRatio: 0.1,
		NotFoundTTL: 60 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return meraki.NewClient(meraki.ClientConfig{
		APIKey:        s.APIKey,
		BaseURL:       s.BaseURL,
		UserAgent:     meraki.BuildUserAgent(),
		RateLimiter:   rl,
		IPRateLimiter: ipLimiter,
		Cache:         cache,
		Logger:        logger,
	})
}

// Dispose is called by Grafana when plugin settings change and a new instance replaces this one.
func (a *App) Dispose() {}

// Client exposes the underlying Meraki client so the nested datasource (and resource routes)
// can share a single rate limiter and cache.
func (a *App) Client() *meraki.Client {
	return a.client
}

// Configured reports whether an API key is present. Resource handlers that require it should
// short-circuit with a 412 when this returns false.
func (a *App) Configured() bool {
	return a.client != nil
}

// CheckHealth validates the configured API key by calling GET /organizations with a 15s
// timeout. Returns OK with the org count on success, or an error message on failure.
func (a *App) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if a.client == nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Meraki API key not set. Configure it on the plugin settings page.",
		}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	orgs, err := a.client.GetOrganizations(ctx, 0)
	if err != nil {
		a.logger.Warn("meraki health check failed", "err", err)
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: friendlyError(err),
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: fmt.Sprintf("Connected to Meraki; %d organization%s visible.", len(orgs), pluralSuffix(len(orgs))),
	}, nil
}

func friendlyError(err error) string {
	switch {
	case meraki.IsUnauthorized(err):
		return "The Meraki API key was rejected (HTTP 401/403). Double-check the key has Dashboard API access."
	case meraki.IsRateLimit(err):
		return "The Meraki API rejected the request with a rate-limit error. Try again shortly or reduce the shared fraction."
	default:
		return "Failed to contact Meraki: " + err.Error()
	}
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
