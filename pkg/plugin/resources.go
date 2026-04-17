package plugin

import (
	"encoding/json"
	"net/http"

	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/query"
)

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
	resp, err := query.Handle(req.Context(), a.client, &body)
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
}
