// Package alerts contains the bundled alert-rule registry and (in Phase 2)
// the reconciler that provisions those rules into Grafana via the Grafana
// provisioning HTTP API.
//
// This file defines the Grafana-side AlertRule wire shape. It deliberately
// mirrors the POST /api/v1/provisioning/alert-rules request body documented
// in Grafana's alerting HTTP API, and is the canonical type shared between
// the registry (which produces AlertRules) and the reconciler / GrafanaClient
// (which ship them to Grafana).
package alerts

// AlertRule is the JSON body accepted/returned by Grafana's alert-rule
// provisioning endpoint (`POST /api/v1/provisioning/alert-rules`).
// Only the fields the plugin actually cares about are modelled here —
// Grafana ignores unknown keys on input and our reconciler is free to
// ignore unknown keys on output. Extend this struct if/when the reconciler
// needs access to more fields.
type AlertRule struct {
	UID          string            `json:"uid"`
	Title        string            `json:"title"`
	Condition    string            `json:"condition"`
	Data         []AlertQuery      `json:"data"`
	NoDataState  string            `json:"noDataState"`
	ExecErrState string            `json:"execErrState"`
	For          string            `json:"for"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Labels       map[string]string `json:"labels"`
	FolderUID    string            `json:"folderUID"`
	RuleGroup    string            `json:"ruleGroup"`
	// OrgID is filled in by Grafana on GET; we leave it zero on POST so
	// Grafana uses the calling token's org. Keep `omitempty` so the emitted
	// JSON fixtures don't drift when the field isn't set.
	OrgID    int64 `json:"orgID,omitempty"`
	IsPaused bool  `json:"isPaused,omitempty"`
}

// AlertQuery is one step in an alert rule's expression chain — either a
// datasource query (DatasourceUID = real DS UID) or an expression
// (DatasourceUID = "__expr__").
type AlertQuery struct {
	RefID             string            `json:"refId"`
	QueryType         string            `json:"queryType,omitempty"`
	DatasourceUID     string            `json:"datasourceUid"`
	Model             map[string]any    `json:"model"`
	RelativeTimeRange RelativeTimeRange `json:"relativeTimeRange"`
}

// RelativeTimeRange is an [From, To) window measured in seconds relative
// to the evaluation time. Grafana's server-side expressions take
// {from:0,to:0} by convention.
type RelativeTimeRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Folder is the minimum shape we need to create / look up the folder
// under which bundled rules live. Grafana's folder API returns many more
// fields; the reconciler only reads UID/Title.
type Folder struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}
