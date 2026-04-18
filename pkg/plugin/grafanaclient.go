package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// alertRule is a Phase 0 placeholder for the typed AlertRule struct that
// lands in §4.5.3 (pkg/plugin/alerts/grafana_rule.go). Using json.RawMessage
// keeps the method signatures stable so callers compile now; the real
// shape (title, orgID, folderUID, data[], noDataState, …) replaces this in
// Phase 1.
type alertRule = json.RawMessage

// AlertRule is the Phase 0 exported alias. It will be replaced by a struct
// type in Phase 1; keep callers referencing this symbol rather than the
// unexported alias so the swap is source-compatible.
type AlertRule = alertRule

// GrafanaClient is a thin HTTP wrapper around the local Grafana API for the
// v0.6 bundled alert-rules feature. It exists so CheckHealth can probe the
// alert provisioning endpoint and, in later phases (§4.5.4), so the
// reconciler can CRUD rules via the externalServiceAccounts-issued token.
//
// Construction reads the plugin app's client secret + AppURL from the
// backend.GrafanaCfg that Grafana passes in through request contexts; we do
// NOT import any external HTTP client here to keep the backend dep tree
// unchanged from §4.5.2 scope.
type GrafanaClient struct {
	baseURL string
	token   string
	hc      *http.Client
}

// GrafanaProber is the narrow interface CheckHealth depends on so tests can
// inject a stub without spinning up an httptest server for every scenario.
// Keep the surface minimal — Probe is the only method CheckHealth needs.
type GrafanaProber interface {
	Probe(ctx context.Context) (ready bool, reason string, err error)
}

// NewGrafanaClient builds a client from the Grafana config Grafana injects
// into every request context. Returns an error if either the AppURL or the
// plugin app client secret is missing — callers should treat that as "alerts
// bundle disabled" rather than fatal (see CheckHealth).
func NewGrafanaClient(cfg *backend.GrafanaCfg) (*GrafanaClient, error) {
	if cfg == nil {
		return nil, errors.New("grafana config is nil")
	}
	appURL, err := cfg.AppURL()
	if err != nil {
		return nil, fmt.Errorf("grafana AppURL: %w", err)
	}
	token, err := cfg.PluginAppClientSecret()
	if err != nil {
		return nil, fmt.Errorf("grafana plugin app client secret: %w", err)
	}
	return &GrafanaClient{
		baseURL: strings.TrimRight(appURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Probe issues a GET against the alert provisioning endpoint with limit=1
// and classifies the outcome for CheckHealth. Contract:
//
//   - (true, "ready", nil)                    — 2xx: externalServiceAccounts on,
//     token valid, alerts bundle ready.
//   - (false, <reason>, nil)                  — 401/403: toggle off or missing
//     permission. Returned as NOT-READY but not as an error so CheckHealth can
//     keep going.
//   - (false, <reason>, nil)                  — other non-2xx: treated as
//     unreachable and surfaced verbatim.
//   - (false, "", err)                        — transport failure (DNS, TLS,
//     timeout). CheckHealth logs this and degrades gracefully.
func (c *GrafanaClient) Probe(ctx context.Context) (bool, string, error) {
	url := c.baseURL + "/api/v1/provisioning/alert-rules?limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return false, "", err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, "ready", nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return false, "unavailable — enable externalServiceAccounts feature toggle or upgrade Grafana build", nil
	default:
		return false, fmt.Sprintf("unreachable — Grafana returned %d", resp.StatusCode), nil
	}
}

// ListAlertRules lists the alert rules in the given folder. Body lands in
// §4.5.4 once the AlertRule struct is defined in pkg/plugin/alerts.
func (c *GrafanaClient) ListAlertRules(folderUID string) ([]AlertRule, error) {
	_ = folderUID
	return nil, errors.New("not implemented — Phase 2")
}

// CreateAlertRule provisions a single alert rule. Body lands in §4.5.4.
func (c *GrafanaClient) CreateAlertRule(r AlertRule) error {
	_ = r
	return errors.New("not implemented — Phase 2")
}

// UpdateAlertRule replaces an existing rule by UID. Body lands in §4.5.4.
func (c *GrafanaClient) UpdateAlertRule(uid string, r AlertRule) error {
	_, _ = uid, r
	return errors.New("not implemented — Phase 2")
}

// DeleteAlertRule removes a rule by UID. Body lands in §4.5.4.
func (c *GrafanaClient) DeleteAlertRule(uid string) error {
	_ = uid
	return errors.New("not implemented — Phase 2")
}

// EnsureFolder creates the target folder if absent (idempotent). Body lands
// in §4.5.4.
func (c *GrafanaClient) EnsureFolder(uid, title string) error {
	_, _ = uid, title
	return errors.New("not implemented — Phase 2")
}
