package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
)

// GrafanaClient is a thin HTTP wrapper around the local Grafana API for the
// v0.6 bundled alert-rules feature. It exists so CheckHealth can probe the
// alert provisioning endpoint and, starting in §4.5.4, so the reconciler can
// CRUD rules via the externalServiceAccounts-issued token.
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

// do is the common request helper for the alert-rule + folder CRUD methods.
// It sets Authorization/Accept, injects Content-Type + X-Disable-Provenance
// on mutating verbs, and returns the response body (fully consumed) on any
// 2xx. Non-2xx responses return a descriptive error including status + body
// snippet so reconciler failures are diagnosable.
func (c *GrafanaClient) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		// X-Disable-Provenance: true keeps API-provisioned rules editable in
		// the Grafana UI and prevents them being marked as file-provisioned.
		// Required on every mutation so an operator can tweak a bundled rule
		// without losing the ability to re-reconcile it later.
		req.Header.Set("X-Disable-Provenance", "true")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return buf, resp.StatusCode, fmt.Errorf("%s %s: status %d: %s",
			method, path, resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	return buf, resp.StatusCode, nil
}

// ListAlertRules returns every alert rule whose FolderUID matches the given
// folder. Grafana's provisioning GET /alert-rules endpoint returns the full
// set across folders in one shot; the client-side filter keeps the
// reconciler focused on its own folder.
func (c *GrafanaClient) ListAlertRules(ctx context.Context, folderUID string) ([]alerts.AlertRule, error) {
	buf, _, err := c.do(ctx, http.MethodGet, "/api/v1/provisioning/alert-rules", nil)
	if err != nil {
		return nil, err
	}
	var all []alerts.AlertRule
	if err := json.Unmarshal(buf, &all); err != nil {
		return nil, fmt.Errorf("decode alert-rules: %w", err)
	}
	if folderUID == "" {
		return all, nil
	}
	out := make([]alerts.AlertRule, 0, len(all))
	for _, r := range all {
		if r.FolderUID == folderUID {
			out = append(out, r)
		}
	}
	return out, nil
}

// CreateAlertRule provisions a single alert rule via POST. Callers are
// responsible for having called EnsureFolder beforehand.
func (c *GrafanaClient) CreateAlertRule(ctx context.Context, r alerts.AlertRule) error {
	_, _, err := c.do(ctx, http.MethodPost, "/api/v1/provisioning/alert-rules", r)
	return err
}

// UpdateAlertRule replaces an existing rule by UID.
func (c *GrafanaClient) UpdateAlertRule(ctx context.Context, uid string, r alerts.AlertRule) error {
	_, _, err := c.do(ctx, http.MethodPut, "/api/v1/provisioning/alert-rules/"+uid, r)
	return err
}

// DeleteAlertRule removes a rule by UID.
func (c *GrafanaClient) DeleteAlertRule(ctx context.Context, uid string) error {
	_, _, err := c.do(ctx, http.MethodDelete, "/api/v1/provisioning/alert-rules/"+uid, nil)
	return err
}

// EnsureFolder is idempotent: it GETs the target folder first and only POSTs
// on 404. The GET is cheap (one folder) and lets us distinguish "already
// exists" from "couldn't create" without racing on the POST status.
func (c *GrafanaClient) EnsureFolder(ctx context.Context, uid, title string) error {
	// Probe for existence. We intentionally don't parse the body — any 2xx is
	// a green light, 404 means create.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/provisioning/folders/"+uid, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		// Fall through to create.
	default:
		return fmt.Errorf("GET /folders/%s: status %d", uid, resp.StatusCode)
	}

	body := alerts.Folder{UID: uid, Title: title}
	_, _, err = c.do(ctx, http.MethodPost, "/api/v1/provisioning/folders", body)
	return err
}
