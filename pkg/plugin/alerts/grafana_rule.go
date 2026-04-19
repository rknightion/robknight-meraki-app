// Package alerts contains the bundled alert-rule registry and the reconciler
// that provisions those rules into Grafana via the Grafana provisioning HTTP
// API.
//
// This file defines the Grafana-side AlertRule wire shape. It deliberately
// mirrors the POST /api/v1/provisioning/alert-rules request body documented
// in Grafana's alerting HTTP API, and is the canonical type shared between
// the registry (which produces AlertRules) and the reconciler / GrafanaClient
// (which ship them to Grafana).
//
// The same endpoint and struct are used for Grafana-managed RECORDING rules
// by the sibling `pkg/plugin/recordings` package — recording rules omit
// Condition/NoDataState/ExecErrState and set Record instead (see §4.6). The
// three alert-only fields carry `omitempty` so recording-rule payloads don't
// emit them; the alerts renderer always sets them for alert rules so the
// serialisation for alert payloads is unchanged.
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
	Condition    string            `json:"condition,omitempty"`
	Data         []AlertQuery      `json:"data"`
	NoDataState  string            `json:"noDataState,omitempty"`
	ExecErrState string            `json:"execErrState,omitempty"`
	For          string            `json:"for"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Labels       map[string]string `json:"labels"`
	FolderUID    string            `json:"folderUID"`
	RuleGroup    string            `json:"ruleGroup"`
	// Record, when non-nil, marks this as a Grafana-managed recording rule.
	// See `pkg/plugin/recordings` for the registry that populates it.
	// Recording rules must NOT carry Condition, NoDataState, or ExecErrState
	// — Grafana rejects those fields on recording-rule submissions.
	Record *RecordBlock `json:"record,omitempty"`
	// OrgID is filled in by Grafana on GET; we leave it zero on POST so
	// Grafana uses the calling token's org. Keep `omitempty` so the emitted
	// JSON fixtures don't drift when the field isn't set.
	OrgID    int64 `json:"orgID,omitempty"`
	IsPaused bool  `json:"isPaused,omitempty"`
}

// RecordBlock is the Grafana-managed recording-rule descriptor attached to
// an AlertRule (via AlertRule.Record) for rules of kind "recording". The
// JSON field names match Grafana's provisioning API exactly — the
// underscored `target_datasource_uid` is Grafana's spelling, NOT a
// snake_case typo.
type RecordBlock struct {
	// Metric is the Prometheus metric name the rule will emit. Must match
	// `^[a-zA-Z_:][a-zA-Z0-9_:]*$` per Prometheus, and the recordings
	// package additionally constrains this to `^meraki_[a-z][a-z0-9_]*$`.
	Metric string `json:"metric"`
	// From is the refId of the AlertQuery in Data[] whose result becomes
	// the emitted samples.
	From string `json:"from"`
	// TargetDatasourceUID is the UID of the Prometheus-compatible data
	// source Grafana remote-writes samples into. Backfilled at Render time
	// from the operator's selection in plugin jsonData. If empty, Grafana
	// falls back to `[recording_rules].default_datasource_uid` in
	// grafana.ini — but the recordings reconciler refuses to submit rules
	// with an empty target UID to keep installs self-contained.
	TargetDatasourceUID string `json:"target_datasource_uid,omitempty"`
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
