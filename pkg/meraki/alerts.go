package meraki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Assurance alerts — one-endpoint-per-method wrappers around the
// /organizations/{organizationId}/assurance/alerts family. The older
// /organizations/{organizationId}/alerts path is deprecated; the v1 spec uses
// the assurance namespace. Paths and response shapes verified via ctx7 against
// the canonical OpenAPI spec at /openapi/api_meraki_api_v1_openapispec and the
// Cisco Meraki dev docs (v1-46-0) on 2026-04-17.

// AssuranceAlert is one entry returned by
// GET /organizations/{organizationId}/assurance/alerts. Meraki returns the
// list as a top-level JSON array (Link-header paginated), so the surrounding
// wrapper uses c.GetAll() to collect pages.
//
// The `Scope` payload carries device-type-specific detail the UI renders as
// free-form JSON; we retain it as json.RawMessage to avoid coupling the wire
// struct to every product family.
type AssuranceAlert struct {
	ID             string                  `json:"id"`
	CategoryType   string                  `json:"categoryType,omitempty"`
	AlertType      string                  `json:"type,omitempty"`
	AlertTypeID    string                  `json:"alertTypeId,omitempty"`
	Severity       string                  `json:"severity,omitempty"`
	DismissedAt    *time.Time              `json:"dismissedAt,omitempty"`
	ResolvedAt     *time.Time              `json:"resolvedAt,omitempty"`
	StartedAt      *time.Time              `json:"startedAt,omitempty"`
	OccurredAt     *time.Time              `json:"occurredAt,omitempty"`
	Title          string                  `json:"title,omitempty"`
	Description    string                  `json:"description,omitempty"`
	Network        *AssuranceAlertNetwork  `json:"network,omitempty"`
	Device         *AssuranceAlertDevice   `json:"device,omitempty"`
	DeviceTags     []string                `json:"deviceTags,omitempty"`
	Scope          json.RawMessage         `json:"scope,omitempty"`
}

// AssuranceAlertNetwork is the compact network reference embedded on each
// alert. The `Name` field is populated on the dashboard-friendly responses;
// `ID` is always present.
type AssuranceAlertNetwork struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// AssuranceAlertDevice is the compact device reference embedded on each
// alert. For network-wide alerts that don't tie back to a specific device the
// whole field can be nil.
type AssuranceAlertDevice struct {
	Serial      string `json:"serial"`
	Name        string `json:"name,omitempty"`
	ProductType string `json:"productType,omitempty"`
}

// AssuranceAlertsOverview is the response shape of
// GET /organizations/{organizationId}/assurance/alerts/overview/byType. The
// byType endpoint returns a flat list of {type, count} items. We also keep a
// severity-aware Counts field so the handler can aggregate KPI columns from
// either the per-type response (fallback) or a future `/overview` call that
// exposes the severity breakdown.
//
// ctx7 reports two shapes in the wild:
//
//   - v1-46-0 `/overview`: {"counts":{"total":N,"bySeverity":[{"type":S,"count":N}]}}
//   - v1-46-0 `/overview/byType`: {"items":[{"type":T,"count":N}], "meta":{...}}
//
// We decode the union so the handler can prefer `counts.bySeverity` when the
// server supplies it and fall back to per-type aggregation otherwise.
type AssuranceAlertsOverview struct {
	Counts *AlertsCounts `json:"counts,omitempty"`
	Items  []AlertsTypeCount `json:"items,omitempty"`
}

// AlertsCounts aggregates alerts by severity, matching the `/overview` shape.
type AlertsCounts struct {
	Total      int64              `json:"total"`
	BySeverity []AlertsSeverityCount `json:"bySeverity,omitempty"`
}

// AlertsSeverityCount is one {type: severity, count: N} element of
// counts.bySeverity — where `type` is the severity string ("critical",
// "warning", "informational") rather than the alert type.
type AlertsSeverityCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// AlertsTypeCount is one element of the /overview/byType `items` array:
// {"type":"vlan_mismatch","count":3}.
type AlertsTypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// AlertsOptions is the set of server-side filters we push down to Meraki
// instead of filtering client-side. The zero value means "no filter" — each
// field is only added to the query string when meaningfully set.
//
// Booleans are `*bool` so we can distinguish "unset" from "explicitly false":
// Meraki's defaults are active=true, dismissed=false, resolved=false, and
// overriding those to zero would silently hide active alerts if we naïvely
// emitted `active=false`.
type AlertsOptions struct {
	Severity    string
	NetworkID   string
	Serials     []string
	DeviceTypes []string
	DeviceTags  []string
	TSStart     *time.Time
	TSEnd       *time.Time
	SortOrder   string
	Active      *bool
	Dismissed   *bool
	Resolved    *bool
	PerPage     int
}

// values converts the options struct into url.Values, clamping PerPage to
// the 4-300 range Meraki accepts. Empty strings and nil pointers are omitted
// so we never send `severity=` to the API (which Meraki rejects).
func (o AlertsOptions) values() url.Values {
	v := url.Values{}
	per := o.PerPage
	if per <= 0 {
		per = 300
	}
	if per < 4 {
		per = 4
	}
	if per > 300 {
		per = 300
	}
	v.Set("perPage", strconv.Itoa(per))
	if o.Severity != "" {
		v.Set("severity", o.Severity)
	}
	if o.NetworkID != "" {
		v.Set("networkId", o.NetworkID)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, dt := range o.DeviceTypes {
		v.Add("deviceTypes[]", dt)
	}
	for _, tag := range o.DeviceTags {
		v.Add("deviceTags[]", tag)
	}
	if o.TSStart != nil && !o.TSStart.IsZero() {
		v.Set("tsStart", o.TSStart.UTC().Format(time.RFC3339))
	}
	if o.TSEnd != nil && !o.TSEnd.IsZero() {
		v.Set("tsEnd", o.TSEnd.UTC().Format(time.RFC3339))
	}
	if o.SortOrder != "" {
		v.Set("sortOrder", o.SortOrder)
	}
	if o.Active != nil {
		v.Set("active", strconv.FormatBool(*o.Active))
	}
	if o.Dismissed != nil {
		v.Set("dismissed", strconv.FormatBool(*o.Dismissed))
	}
	if o.Resolved != nil {
		v.Set("resolved", strconv.FormatBool(*o.Resolved))
	}
	return v
}

// overviewValues is the variant of values() used for the /overview/byType
// endpoint, which does NOT accept `perPage` on the body (it's a summary
// endpoint — not paginated). Meraki's spec lists perPage but it's effectively
// ignored; we drop it to keep the URL short and the cache key stable.
func (o AlertsOptions) overviewValues() url.Values {
	v := o.values()
	v.Del("perPage")
	return v
}

// GetOrganizationAssuranceAlerts paginates through every alert matching opts
// for the given org. Pagination follows the Link: rel=next header — clients
// MUST NOT construct startingAfter themselves (per Meraki's docs), which is
// exactly what our shared c.GetAll() does. We pass perPage=300 (the maximum)
// to minimise the page count on busy orgs.
func (c *Client) GetOrganizationAssuranceAlerts(ctx context.Context, orgID string, opts AlertsOptions, ttl time.Duration) ([]AssuranceAlert, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/assurance/alerts", Message: "missing organization id"}}
	}
	var out []AssuranceAlert
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/assurance/alerts",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationAssuranceAlertsOverviewByType returns the summary view used
// for the alerts-by-type bar chart. Not paginated — the server returns a
// single object with `items[]` (per alert-type counts with severity, category,
// etc. on each item).
//
// For KPI tiles (severity rollup) use GetOrganizationAssuranceAlertsOverview
// instead — the byType response has NO `counts.bySeverity` aggregate, and
// summing items[].count over-counts because Meraki emits one item per (type,
// severity) combination across the full retained history.
func (c *Client) GetOrganizationAssuranceAlertsOverviewByType(ctx context.Context, orgID string, opts AlertsOptions, ttl time.Duration) (*AssuranceAlertsOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/assurance/alerts/overview/byType", Message: "missing organization id"}}
	}
	var out AssuranceAlertsOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/assurance/alerts/overview/byType",
		orgID, opts.overviewValues(), ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrganizationAssuranceAlertsOverview returns the severity-rolled summary
// used for the KPI tiles: {counts: {total, bySeverity: [{type, count}, ...]}}.
// This is the correct endpoint for "how many critical / warning /
// informational alerts match the filter"; its sibling /overview/byType does
// NOT emit the `counts` block and silently degrades KPI tiles to total-only.
//
// Not paginated — the server returns a single summary object.
func (c *Client) GetOrganizationAssuranceAlertsOverview(ctx context.Context, orgID string, opts AlertsOptions, ttl time.Duration) (*AssuranceAlertsOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/assurance/alerts/overview", Message: "missing organization id"}}
	}
	var out AssuranceAlertsOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/assurance/alerts/overview",
		orgID, opts.overviewValues(), ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// §3.4 — Alerts overview byNetwork + historical
// ---------------------------------------------------------------------------

// AlertsOverviewByNetwork is one row from
// GET /organizations/{organizationId}/assurance/alerts/overview/byNetwork.
//
// Response shape (verified 2026-04-18):
//
//	{"items":[{"networkId":"N_…","networkName":"…","alertCount":N,"severityCounts":[{"type":"warning","count":N},...]}],"meta":{…}}
type AlertsOverviewByNetwork struct {
	NetworkID     string
	NetworkName   string
	Critical      int64
	Warning       int64
	Informational int64
	Total         int64
}

// alertsOverviewByNetworkResponse is the on-wire envelope.
type alertsOverviewByNetworkResponse struct {
	Items []alertsOverviewByNetworkItem `json:"items"`
}

type alertsOverviewByNetworkItem struct {
	NetworkID      string                `json:"networkId"`
	NetworkName    string                `json:"networkName"`
	AlertCount     int64                 `json:"alertCount"`
	SeverityCounts []AlertsSeverityCount `json:"severityCounts"`
}

// GetOrganizationAssuranceAlertsOverviewByNetwork returns a per-network alert
// summary. Paginated via Link header.
func (c *Client) GetOrganizationAssuranceAlertsOverviewByNetwork(ctx context.Context, orgID string, opts AlertsOptions, ttl time.Duration) ([]AlertsOverviewByNetwork, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/assurance/alerts/overview/byNetwork", Message: "missing organization id"}}
	}
	params := opts.overviewValues()
	params.Set("perPage", "300")

	var allItems []alertsOverviewByNetworkItem
	path := "organizations/" + url.PathEscape(orgID) + "/assurance/alerts/overview/byNetwork"
	for pageNum := 0; pageNum < MaxPages; pageNum++ {
		var pageParams url.Values
		if pageNum == 0 {
			pageParams = params
		}
		body, hdr, err := c.Do(ctx, "GET", path, orgID, pageParams, nil)
		if err != nil {
			return nil, err
		}
		var pageResp alertsOverviewByNetworkResponse
		if err := json.Unmarshal(body, &pageResp); err != nil {
			return nil, fmt.Errorf("meraki: decode alerts overview byNetwork: %w", err)
		}
		allItems = append(allItems, pageResp.Items...)
		next := nextLink(hdr)
		if next == "" {
			break
		}
		path = next
	}

	out := make([]AlertsOverviewByNetwork, 0, len(allItems))
	for _, item := range allItems {
		row := AlertsOverviewByNetwork{
			NetworkID:   item.NetworkID,
			NetworkName: item.NetworkName,
			Total:       item.AlertCount,
		}
		for _, sc := range item.SeverityCounts {
			switch strings.ToLower(sc.Type) {
			case "critical":
				row.Critical += sc.Count
			case "warning":
				row.Warning += sc.Count
			case "informational", "info":
				row.Informational += sc.Count
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// AlertsOverviewHistoricalPoint is one segment from
// GET /organizations/{organizationId}/assurance/alerts/overview/historical.
//
// Response shape (verified 2026-04-18):
//
//	{"items":[{"segmentStart":"2022-04-01T15:00:00Z","totals":{"critical":N,"warning":N,"informational":N},"byAlertType":[...]}],"meta":{…}}
type AlertsOverviewHistoricalPoint struct {
	StartTs    time.Time
	TotalCount int64
	BySeverity map[string]int64
}

// AlertsOverviewHistoricalOptions filters the historical call.
type AlertsOverviewHistoricalOptions struct {
	NetworkID string
	Serial    string
	Window    *TimeRangeWindow
	Segment   time.Duration // maps to segmentDuration query param (seconds)
}

func (o AlertsOverviewHistoricalOptions) values() url.Values {
	v := url.Values{}
	if o.NetworkID != "" {
		v.Set("networkId", o.NetworkID)
	}
	if o.Serial != "" {
		v.Set("serial", o.Serial)
	}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	if o.Segment > 0 {
		v.Set("segmentDuration", strconv.Itoa(int(o.Segment.Seconds())))
	}
	return v
}

// alertsOverviewHistoricalResponse is the on-wire envelope.
type alertsOverviewHistoricalResponse struct {
	Items []alertsOverviewHistoricalItem `json:"items"`
}

type alertsOverviewHistoricalItem struct {
	SegmentStart string                         `json:"segmentStart"`
	Totals       alertsOverviewHistoricalTotals `json:"totals"`
}

type alertsOverviewHistoricalTotals struct {
	Critical      int64 `json:"critical"`
	Warning       int64 `json:"warning"`
	Informational int64 `json:"informational"`
}

// GetOrganizationAssuranceAlertsOverviewHistorical returns time-bucketed
// severity counts. Not paginated per the spec — single GET.
func (c *Client) GetOrganizationAssuranceAlertsOverviewHistorical(ctx context.Context, orgID string, opts AlertsOverviewHistoricalOptions, ttl time.Duration) ([]AlertsOverviewHistoricalPoint, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/assurance/alerts/overview/historical", Message: "missing organization id"}}
	}
	var raw alertsOverviewHistoricalResponse
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/assurance/alerts/overview/historical",
		orgID, opts.values(), ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]AlertsOverviewHistoricalPoint, 0, len(raw.Items))
	for _, item := range raw.Items {
		ts, _ := time.Parse(time.RFC3339, item.SegmentStart)
		totalCount := item.Totals.Critical + item.Totals.Warning + item.Totals.Informational
		out = append(out, AlertsOverviewHistoricalPoint{
			StartTs:    ts,
			TotalCount: totalCount,
			BySeverity: map[string]int64{
				"critical":      item.Totals.Critical,
				"warning":       item.Totals.Warning,
				"informational": item.Totals.Informational,
			},
		})
	}
	return out, nil
}
