package plugin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/query"
)

// bundledFolderUID is duplicated here (and in pkg/plugin/alerts/registry.go)
// because the alerts package declares it unexported. Keeping a named const
// here makes the filter intent explicit in the status handler — the alerts
// package is the source of truth for the value itself.
const bundledFolderUID = "meraki-bundled-folder"

// pluginID is the app plugin's manifest id. Duplicated from plugin.json / main.go
// because backend code doesn't have easy access to the manifest at request
// time. Keep in sync with the id field in src/plugin.json.
const pluginID = "robknight-meraki-app"

// pluginPathPrefix is the full Grafana route prefix for this plugin's app
// shell. Threaded into `query.Options.PluginPathPrefix` so handlers that emit
// `drilldownUrl` columns can compose full URLs like
// `/a/<plugin>/access-points/<serial>`.
var pluginPathPrefix = "/a/" + pluginID

// handlePing is a lightweight liveness probe useful during development.
func (a *App) handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":    "ok",
		"configured": a.Configured(),
	})
}

// handleQuery receives a MerakiQuery batch from the nested datasource and returns
// Grafana-wire-format data frames. Requires that the app be configured with an API key.
func (a *App) handleQuery(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.Configured() {
		http.Error(w, "Meraki API key not set", http.StatusPreconditionFailed)
		return
	}
	var body query.QueryRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := query.Handle(req.Context(), a.client, &body, query.Options{
		LabelMode:        string(a.settings.LabelMode),
		PluginPathPrefix: pluginPathPrefix,
	})
	if err != nil {
		a.logger.Warn("query dispatch failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleMetricFind runs a single query for variable hydration (e.g. listing orgs/networks).
func (a *App) handleMetricFind(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.Configured() {
		http.Error(w, "Meraki API key not set", http.StatusPreconditionFailed)
		return
	}
	var body query.MetricFindRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := query.HandleMetricFind(req.Context(), a.client, &body)
	if err != nil {
		a.logger.Warn("metricFind dispatch failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// registerRoutes wires HTTP resource endpoints.
func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ping", a.handlePing)
	mux.HandleFunc("/query", a.handleQuery)
	mux.HandleFunc("/metricFind", a.handleMetricFind)
	mux.HandleFunc("/alerts/templates", a.handleAlertsTemplates)
	mux.HandleFunc("/alerts/status", a.handleAlertsStatus)
	mux.HandleFunc("/alerts/reconcile", a.handleAlertsReconcile)
	mux.HandleFunc("/alerts/uninstall-all", a.handleAlertsUninstallAll)
}

// --- /alerts/* handlers ----------------------------------------------------
//
// The handlers below compose the API surface the Phase 4 config-UI calls:
//   GET  /alerts/templates       — static registry for picker rendering
//   GET  /alerts/status          — live state of managed rules + last
//                                  reconcile telemetry
//   POST /alerts/reconcile       — apply a DesiredState (create/update/delete)
//   POST /alerts/uninstall-all   — empty-DesiredState shortcut: delete every
//                                  rule the plugin manages, no Meraki calls
//
// Configured() gating:
//   - templates + status + uninstall-all do NOT require an API key — users
//     should see what's available and be able to clean up even with an
//     unconfigured or expired key.
//   - reconcile DOES require Configured() because resolveOrgs() fans out to
//     the Meraki API when no OrgOverride is supplied.
//
// Persistence: reconcile writes a summary + timestamp to the alertsStore
// (see pkg/plugin/alerts_store.go for the rationale — dataPath persistence
// avoids an extra HTTP round-trip to Grafana on every reconcile).

// alertThresholdSchemaDTO is the shape emitted over the wire. Default is
// rendered to a `json.RawMessage` (via json.Marshal) so strings, numbers,
// lists, and booleans all survive a round-trip through the frontend without
// needing a tagged-union type on the TypeScript side.
type alertThresholdSchemaDTO struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Default any      `json:"default,omitempty"`
	Label   string   `json:"label,omitempty"`
	Help    string   `json:"help,omitempty"`
	Options []string `json:"options,omitempty"`
}

type alertTemplateDTO struct {
	ID          string                    `json:"id"`
	GroupID     string                    `json:"groupId"`
	DisplayName string                    `json:"displayName"`
	Severity    string                    `json:"severity"`
	Thresholds  []alertThresholdSchemaDTO `json:"thresholds"`
}

type alertGroupDTO struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName"`
	Templates   []alertTemplateDTO `json:"templates"`
}

type alertsTemplatesResponse struct {
	Groups []alertGroupDTO `json:"groups"`
}

// handleAlertsTemplates answers GET /alerts/templates with the in-process
// registry. No Configured() gate — the registry is static YAML embedded at
// build time and should be visible before the user has supplied an API key.
func (a *App) handleAlertsTemplates(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.alertsRegistry == nil {
		http.Error(w, "alerts registry not loaded", http.StatusServiceUnavailable)
		return
	}
	groups := a.alertsRegistry.Groups()
	out := alertsTemplatesResponse{Groups: make([]alertGroupDTO, 0, len(groups))}
	for _, g := range groups {
		dto := alertGroupDTO{
			ID:          g.ID,
			DisplayName: g.DisplayName,
			Templates:   make([]alertTemplateDTO, 0, len(g.Templates)),
		}
		for _, t := range g.Templates {
			tdto := alertTemplateDTO{
				ID:          t.ID,
				GroupID:     t.GroupID,
				DisplayName: t.DisplayName,
				Severity:    t.Severity,
				Thresholds:  make([]alertThresholdSchemaDTO, 0, len(t.Thresholds)),
			}
			for _, th := range t.Thresholds {
				tdto.Thresholds = append(tdto.Thresholds, alertThresholdSchemaDTO{
					Key: th.Key, Type: th.Type, Default: th.Default,
					Label: th.Label, Help: th.Help, Options: th.Options,
				})
			}
			dto.Templates = append(dto.Templates, tdto)
		}
		out.Groups = append(out.Groups, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// alertsInstalledRuleDTO is a one-rule-per-object view of the managed rules
// currently live in Grafana. GroupID + TemplateID + OrgID are parsed out of
// the UID (format: `meraki-<group>-<template>-<org>` — see registry.go).
type alertsInstalledRuleDTO struct {
	GroupID    string `json:"groupId"`
	TemplateID string `json:"templateId"`
	OrgID      string `json:"orgId"`
	UID        string `json:"uid"`
	Enabled    bool   `json:"enabled"`
}

type alertsStatusResponse struct {
	Installed            []alertsInstalledRuleDTO `json:"installed"`
	LastReconciledAt     *time.Time               `json:"lastReconciledAt,omitempty"`
	LastReconcileSummary *AlertsReconcileSummary  `json:"lastReconcileSummary,omitempty"`
	GrafanaReady         bool                     `json:"grafanaReady"`
}

// handleAlertsStatus answers GET /alerts/status with the live managed-rule
// set and the last-reconcile telemetry. Does NOT require Configured() — the
// bundle state should be visible before the user has a working Meraki key so
// they can see what's installed before finishing setup.
//
// grafanaReady = (err == nil) from ListAlertRules. A 401/403/500 from
// Grafana's provisioning API surfaces here as a single boolean that the
// frontend can turn into an actionable banner.
func (a *App) handleAlertsStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := backend.GrafanaConfigFromContext(req.Context())
	resp := alertsStatusResponse{Installed: []alertsInstalledRuleDTO{}}

	if a.alertsStore != nil {
		st := a.alertsStore.Get()
		if !st.LastReconciledAt.IsZero() {
			t := st.LastReconciledAt
			resp.LastReconciledAt = &t
			summary := st.LastReconcileSummary
			resp.LastReconcileSummary = &summary
		}
	}

	if a.newGrafanaAPI == nil {
		// Without a factory we can't probe Grafana at all. Surface as
		// "not ready" but still return 200 so the UI can render the
		// persisted summary.
		writeJSON(w, http.StatusOK, resp)
		return
	}
	api, err := a.newGrafanaAPI(cfg)
	if err != nil {
		a.logger.Debug("alerts status: grafana client unavailable", "err", err)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	rules, err := api.ListAlertRules(req.Context(), bundledFolderUID)
	if err != nil {
		a.logger.Debug("alerts status: list rules failed", "err", err)
		// Fall through with grafanaReady=false but don't 500 — the user
		// may want to read last-reconcile telemetry even when Grafana's
		// provisioning API is transiently unhappy.
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.GrafanaReady = true
	for _, r := range rules {
		if !strings.HasPrefix(r.UID, "meraki-") {
			continue
		}
		if r.Labels["managed_by"] != "meraki-plugin" {
			continue
		}
		groupID, templateID, orgID := parseRuleUID(r.UID)
		resp.Installed = append(resp.Installed, alertsInstalledRuleDTO{
			GroupID:    groupID,
			TemplateID: templateID,
			OrgID:      orgID,
			UID:        r.UID,
			Enabled:    !r.IsPaused,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseRuleUID splits a `meraki-<group>-<template>-<org>` UID back into its
// parts. Group and template IDs can themselves contain hyphens (e.g.
// `device-offline`), so we walk from BOTH ends: the last hyphen separates
// the org, and the first separates the group. Anything between is the
// template ID.
//
// Returns empty strings when the UID doesn't match the expected shape —
// status handler uses this for display only, so a malformed UID shows as a
// blank row rather than failing the whole call.
func parseRuleUID(uid string) (group, template, org string) {
	if !strings.HasPrefix(uid, "meraki-") {
		return "", "", ""
	}
	rest := strings.TrimPrefix(uid, "meraki-")
	// Find the first hyphen -> separates group from (template-org).
	firstIdx := strings.Index(rest, "-")
	if firstIdx < 0 {
		return "", "", ""
	}
	group = rest[:firstIdx]
	remainder := rest[firstIdx+1:]
	// Last hyphen separates template from org.
	lastIdx := strings.LastIndex(remainder, "-")
	if lastIdx < 0 {
		return group, remainder, ""
	}
	template = remainder[:lastIdx]
	org = remainder[lastIdx+1:]
	return group, template, org
}

// desiredStateDTO is the wire shape of a reconcile body. It mirrors
// alerts.DesiredState but JSON-tagged so the frontend can POST a natural
// camel-cased payload. Unmarshalling here rather than in the alerts package
// keeps the reconciler pure-Go.
type desiredStateDTO struct {
	Groups      map[string]groupStateDTO              `json:"groups"`
	Thresholds  map[string]map[string]map[string]any  `json:"thresholds,omitempty"`
	OrgOverride []string                              `json:"orgOverride,omitempty"`
}

type groupStateDTO struct {
	Installed    bool            `json:"installed"`
	RulesEnabled map[string]bool `json:"rulesEnabled"`
}

func (d desiredStateDTO) toInternal() alerts.DesiredState {
	out := alerts.DesiredState{
		Thresholds:  d.Thresholds,
		OrgOverride: d.OrgOverride,
	}
	if len(d.Groups) > 0 {
		out.Groups = make(map[string]alerts.GroupState, len(d.Groups))
		for k, v := range d.Groups {
			out.Groups[k] = alerts.GroupState{Installed: v.Installed, RulesEnabled: v.RulesEnabled}
		}
	}
	return out
}

// handleAlertsReconcile POSTs a DesiredState, runs the reconciler, persists
// the summary, and returns the ReconcileResult. Requires Configured() so
// resolveOrgs() can fan out to the Meraki API — callers that want to pin an
// explicit org list can supply orgOverride to bypass the Meraki call, but
// we still gate on Configured() to keep the surface simple and because
// production workflows always go through the real Meraki client.
func (a *App) handleAlertsReconcile(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.Configured() {
		http.Error(w, "Meraki API key not set", http.StatusPreconditionFailed)
		return
	}
	if a.alertsRegistry == nil {
		http.Error(w, "alerts registry not loaded", http.StatusServiceUnavailable)
		return
	}
	if a.newGrafanaAPI == nil {
		http.Error(w, "grafana client factory unavailable", http.StatusServiceUnavailable)
		return
	}

	var body desiredStateDTO
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := backend.GrafanaConfigFromContext(req.Context())
	api, err := a.newGrafanaAPI(cfg)
	if err != nil {
		http.Error(w, "grafana client: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	m := a.merakiForAlerts()
	result, rerr := alerts.Reconcile(req.Context(), api, m, a.alertsRegistry, body.toInternal())
	a.persistReconcileSummary(result)
	if rerr != nil {
		// Top-level reconcile errors (folder ensure / org lookup) still
		// deserve a 500 — they mean the whole run couldn't even start.
		a.logger.Warn("alerts reconcile failed", "err", rerr)
		http.Error(w, rerr.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAlertsUninstallAll is a reconcile with Groups={} — the diff
// algorithm turns that into "every managed rule is now a delete". Does NOT
// require Configured() because the delete path never calls Meraki; the
// reconciler only hits the Grafana provisioning API once the org list is
// resolved, and an empty OrgOverride with an empty Groups map yields an
// empty desired set without needing the Meraki client at all.
//
// Subtle: resolveOrgs() errors if MerakiAPI is nil AND OrgOverride is empty.
// To keep the uninstall path independent of Meraki we pass OrgOverride=[""]
// (a single empty-string org ID) — which never matches any live rule's org
// suffix and therefore produces no desired rules. The full list of existing
// rules is still discovered via ListAlertRules and deleted normally.
func (a *App) handleAlertsUninstallAll(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.alertsRegistry == nil {
		http.Error(w, "alerts registry not loaded", http.StatusServiceUnavailable)
		return
	}
	if a.newGrafanaAPI == nil {
		http.Error(w, "grafana client factory unavailable", http.StatusServiceUnavailable)
		return
	}

	cfg := backend.GrafanaConfigFromContext(req.Context())
	api, err := a.newGrafanaAPI(cfg)
	if err != nil {
		http.Error(w, "grafana client: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Empty groups map -> every currently-managed rule is classed as DELETE.
	// OrgOverride must be non-empty so resolveOrgs() doesn't attempt the
	// Meraki fallback in the Configured()==false case; any placeholder works
	// because no desired rules will be rendered.
	desired := alerts.DesiredState{
		Groups:      map[string]alerts.GroupState{},
		OrgOverride: []string{"uninstall-placeholder"},
	}

	result, rerr := alerts.Reconcile(req.Context(), api, nil, a.alertsRegistry, desired)
	a.persistReconcileSummary(result)
	if rerr != nil {
		a.logger.Warn("alerts uninstall-all failed", "err", rerr)
		http.Error(w, rerr.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// persistReconcileSummary writes the counters + timestamp to the alertsStore
// in a best-effort manner. Failures are logged but do NOT fail the caller —
// the reconcile succeeded from the user's perspective, and the subsequent
// /alerts/status call can still read the in-memory summary even if the disk
// write didn't stick.
func (a *App) persistReconcileSummary(result alerts.ReconcileResult) {
	if a.alertsStore == nil {
		return
	}
	finished := result.FinishedAt
	if finished.IsZero() {
		finished = time.Now()
	}
	summary := AlertsReconcileSummary{
		Created: len(result.Created),
		Updated: len(result.Updated),
		Deleted: len(result.Deleted),
		Failed:  len(result.Failed),
	}
	if err := a.alertsStore.Set(AlertsState{
		LastReconciledAt:     finished,
		LastReconcileSummary: summary,
	}); err != nil {
		a.logger.Warn("alerts store write failed", "err", err)
	}
}
