package meraki

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Insights endpoints — licensing, API usage, and org-level client/usage summaries.
// Path and response shapes verified via ctx7 against the canonical Meraki v1
// OpenAPI spec on 2026-04-17.

// --- Licensing -------------------------------------------------------------

// LicensesOverview is the response shape of
// `GET /organizations/{organizationId}/licenses/overview`. The endpoint returns
// one of two disjoint payload shapes depending on which licensing model the
// org is on:
//
//   - **Co-termination** orgs: `{status, expirationDate, licensedDeviceCounts}`
//     where `licensedDeviceCounts` is a map of device model or SKU → count.
//     Note: `expirationDate` is a **human-readable string** like
//     `"Mar 13, 2027 UTC"` — NOT RFC3339 like everywhere else in the API.
//     See UnmarshalJSON below for the tolerant parser.
//   - **Per-device** orgs: `{states: {active, expired, expiring, recentlyQueued,
//     unused, unusedActive}}` with per-bucket `{count}` entries.
//
// We decode the union (each field is optional) and let the handler branch on
// `IsCoterm()` to pick which shape to report.
type LicensesOverview struct {
	// Co-term fields.
	Status               string           `json:"status,omitempty"`
	ExpirationDate       *time.Time       `json:"-"`
	LicensedDeviceCounts map[string]int64 `json:"licensedDeviceCounts,omitempty"`

	// Per-device fields.
	States *LicensesStates `json:"states,omitempty"`
}

// UnmarshalJSON handles both RFC3339 and Meraki's co-term-only
// `"Mar 13, 2027 UTC"` string shape for `expirationDate`. Older per-device
// orgs (and newer subscription fallback paths) emit RFC3339; co-term orgs
// emit the human-readable form. When neither format parses we leave the
// pointer nil rather than erroring — downstream panels already tolerate a
// nil expiration by rendering `—`.
func (o *LicensesOverview) UnmarshalJSON(data []byte) error {
	type alias LicensesOverview
	aux := struct {
		ExpirationDate *string `json:"expirationDate,omitempty"`
		*alias
	}{alias: (*alias)(o)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.ExpirationDate == nil || *aux.ExpirationDate == "" {
		return nil
	}
	s := *aux.ExpirationDate
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		t = t.UTC()
		o.ExpirationDate = &t
		return nil
	}
	// Meraki co-term shape: "Mar 13, 2027 UTC". Strip the trailing " UTC"
	// and parse with Go's reference layout.
	trimmed := strings.TrimSpace(strings.TrimSuffix(s, " UTC"))
	for _, layout := range []string{"Jan 2, 2006", "2 Jan 2006", "2006-01-02"} {
		if t, err := time.Parse(layout, trimmed); err == nil {
			t = t.UTC()
			o.ExpirationDate = &t
			return nil
		}
	}
	return nil
}

// LicensesStates is the per-device bucket breakdown. Each bucket carries a
// single `count` field in the wire payload; we flatten to a count-only struct
// because nothing else is surfaced at this level.
type LicensesStates struct {
	Active          LicensesStateBucket `json:"active"`
	Expired         LicensesStateBucket `json:"expired"`
	Expiring        LicensesStateBucket `json:"expiring"`
	RecentlyQueued  LicensesStateBucket `json:"recentlyQueued"`
	Unused          LicensesStateBucket `json:"unused"`
	UnusedActive    LicensesStateBucket `json:"unusedActive"`
}

// LicensesStateBucket is `{count: N}` — one entry of LicensesStates.
type LicensesStateBucket struct {
	Count int64 `json:"count"`
}

// IsCoterm reports whether this overview response carries the co-termination
// shape (has `status` or non-empty `licensedDeviceCounts`). Per-device orgs
// return a `states` object instead.
func (o LicensesOverview) IsCoterm() bool {
	return len(o.LicensedDeviceCounts) > 0 || o.Status != ""
}

// OrganizationLicense is one row from `GET /organizations/{organizationId}/licenses`.
// This endpoint is only valid for per-device licensing orgs (co-term orgs
// return the overview only). The `License` short-name is already taken by the
// licensing-model struct embedded on Organization; we use the full name here
// so callers can tell the two apart at a glance.
type OrganizationLicense struct {
	ID                        string            `json:"id"`
	LicenseType               string            `json:"licenseType,omitempty"`
	State                     string            `json:"state,omitempty"`
	DeviceSerial              string            `json:"deviceSerial,omitempty"`
	NetworkID                 string            `json:"networkId,omitempty"`
	SeatCount                 int64             `json:"seatCount,omitempty"`
	ActivationDate            *time.Time        `json:"activationDate,omitempty"`
	ExpirationDate            *time.Time        `json:"expirationDate,omitempty"`
	HeadLicenseID             string            `json:"headLicenseId,omitempty"`
	ClaimDate                 *time.Time        `json:"claimDate,omitempty"`
	OrderNumber               string            `json:"orderNumber,omitempty"`
	PermanentlyQueuedLicenses []json.RawMessage `json:"permanentlyQueuedLicenses,omitempty"`
}

// LicenseListOptions filters `GET /organizations/{organizationId}/licenses`.
// Each field is optional and only emitted when non-zero.
type LicenseListOptions struct {
	State        string // active|expired|expiring|recentlyQueued|unused|unusedActive
	DeviceSerial string
	NetworkID    string
	PerPage      int // 3-1000, default 1000
}

func (o LicenseListOptions) values() url.Values {
	v := url.Values{}
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	if per < 3 {
		per = 3
	}
	if per > 1000 {
		per = 1000
	}
	v.Set("perPage", strconv.Itoa(per))
	if o.State != "" {
		v.Set("state", o.State)
	}
	if o.DeviceSerial != "" {
		v.Set("deviceSerial", o.DeviceSerial)
	}
	if o.NetworkID != "" {
		v.Set("networkId", o.NetworkID)
	}
	return v
}

// GetOrganizationLicensesOverview fetches the org-level licensing summary.
// Not paginated. Returns both co-term and per-device union fields — callers
// inspect `IsCoterm()` to know which to read.
func (c *Client) GetOrganizationLicensesOverview(ctx context.Context, orgID string, ttl time.Duration) (*LicensesOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/licenses/overview", Message: "missing organization id"}}
	}
	var out LicensesOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/licenses/overview",
		orgID, nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrganizationLicenses lists per-device licenses for an org. Link-paginated.
// Only valid for per-device licensing orgs; co-term orgs return 400 which the
// caller should pre-empt by probing the overview first.
func (c *Client) GetOrganizationLicenses(ctx context.Context, orgID string, opts LicenseListOptions, ttl time.Duration) ([]OrganizationLicense, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/licenses", Message: "missing organization id"}}
	}
	var out []OrganizationLicense
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/licenses",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// --- API requests ----------------------------------------------------------

// ApiRequestsOverview is the response shape of
// `GET /organizations/{organizationId}/apiRequests/overview`. The server
// returns a single object with a map of HTTP response code → count aggregated
// over the requested timespan (max 31 days).
type ApiRequestsOverview struct {
	ResponseCodeCounts map[string]int64 `json:"responseCodeCounts"`
}

// ApiRequestCodeCount is one element of an interval's `counts` array —
// `{code: 200, count: 100}`.
type ApiRequestCodeCount struct {
	Code  int   `json:"code"`
	Count int64 `json:"count"`
}

// ApiRequestsByIntervalEntry is one element of the
// `/apiRequests/overview/responseCodes/byInterval` response — one bucket with
// a start/end timestamp and the per-code counts seen during that interval.
type ApiRequestsByIntervalEntry struct {
	StartTs time.Time             `json:"startTs"`
	EndTs   time.Time             `json:"endTs"`
	Counts  []ApiRequestCodeCount `json:"counts"`
}

// ApiRequestsByIntervalOptions filters the byInterval call. Interval is the
// bucket resolution (seconds; allowed values in KnownEndpointRanges are 120,
// 3600, 14400, 21600). Versions/OperationIDs/SourceIPs/AdminIDs are optional
// server-side filters.
type ApiRequestsByIntervalOptions struct {
	Window       *TimeRangeWindow
	Interval     time.Duration
	Versions     []string
	OperationIDs []string
	SourceIPs    []string
	AdminIDs     []string
	UserAgent    string
	PerPage      int
}

func (o ApiRequestsByIntervalOptions) values() url.Values {
	v := url.Values{}
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	v.Set("perPage", strconv.Itoa(per))
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	// Interval: prefer explicit override, fall back to window resolution.
	iv := o.Interval
	if iv <= 0 && o.Window != nil {
		iv = o.Window.Resolution
	}
	if iv > 0 {
		v.Set("interval", strconv.Itoa(int(iv.Seconds())))
	}
	for _, ver := range o.Versions {
		v.Add("version", ver)
	}
	for _, id := range o.OperationIDs {
		v.Add("operationIds[]", id)
	}
	for _, ip := range o.SourceIPs {
		v.Add("sourceIps[]", ip)
	}
	for _, a := range o.AdminIDs {
		v.Add("adminIds[]", a)
	}
	if o.UserAgent != "" {
		v.Set("userAgent", o.UserAgent)
	}
	return v
}

// GetOrganizationApiRequestsOverview fetches the aggregate response-code tally
// for the org over the given timespan (clamped to 31 days by the caller). Not
// paginated.
func (c *Client) GetOrganizationApiRequestsOverview(ctx context.Context, orgID string, timespan time.Duration, ttl time.Duration) (*ApiRequestsOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/apiRequests/overview", Message: "missing organization id"}}
	}
	v := url.Values{}
	if timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(timespan.Seconds())))
	}
	var out ApiRequestsOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/apiRequests/overview",
		orgID, v, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrganizationApiRequestsByInterval fetches the response-code timeseries.
// Link-paginated. The caller must pre-resolve the interval (via
// KnownEndpointRanges) so the request matches the spec's allowed values.
func (c *Client) GetOrganizationApiRequestsByInterval(ctx context.Context, orgID string, opts ApiRequestsByIntervalOptions, ttl time.Duration) ([]ApiRequestsByIntervalEntry, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/apiRequests/overview/responseCodes/byInterval", Message: "missing organization id"}}
	}
	var out []ApiRequestsByIntervalEntry
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/apiRequests/overview/responseCodes/byInterval",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// --- Clients overview ------------------------------------------------------

// ClientsOverview is the response shape of
// `GET /organizations/{organizationId}/clients/overview`. Usage values are in
// kb (kilobytes) per Meraki's spec.
type ClientsOverview struct {
	Counts ClientsOverviewCounts `json:"counts"`
	Usage  ClientsOverviewUsage  `json:"usage"`
}

// ClientsOverviewCounts has the total client count.
type ClientsOverviewCounts struct {
	Total int64 `json:"total"`
}

// ClientsOverviewUsage is the overall and average usage breakdown.
//
// Shape variance: when the request is sent with a `resolution` parameter,
// Meraki returns `average` as a **scalar float** instead of the documented
// `{total, downstream, upstream}` object. Confirmed against api.meraki.com
// 2026-04-19 for org 1019781 with `resolution=86400` — the response is
// `"average": 5428.95`. The custom UnmarshalJSON below treats a scalar as
// the `total` field and zero-fills the other bands so downstream KPI tiles
// still render (total is what `clientsOverviewKpiRow` reads).
type ClientsOverviewUsage struct {
	Overall ClientsOverviewUsageBand `json:"overall"`
	Average ClientsOverviewUsageBand `json:"-"`
}

// UnmarshalJSON accepts both the object form `{total, downstream, upstream}`
// and the scalar-float variant that Meraki returns when `resolution` is set.
// Neither form is a hard error — if decode fails entirely we leave Average
// zero-valued so panels display `0` rather than blanking the whole frame.
func (u *ClientsOverviewUsage) UnmarshalJSON(data []byte) error {
	var aux struct {
		Overall ClientsOverviewUsageBand `json:"overall"`
		Average json.RawMessage          `json:"average"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	u.Overall = aux.Overall
	if len(aux.Average) == 0 || string(aux.Average) == "null" {
		return nil
	}
	// Prefer the documented object shape.
	var band ClientsOverviewUsageBand
	if err := json.Unmarshal(aux.Average, &band); err == nil {
		u.Average = band
		return nil
	}
	// Fallback: scalar float (the `resolution`-present variant).
	var scalar float64
	if err := json.Unmarshal(aux.Average, &scalar); err == nil {
		u.Average = ClientsOverviewUsageBand{Total: scalar}
		return nil
	}
	return nil
}

// ClientsOverviewUsageBand is one (total, downstream, upstream) triple.
type ClientsOverviewUsageBand struct {
	Total      float64 `json:"total"`
	Downstream float64 `json:"downstream"`
	Upstream   float64 `json:"upstream"`
}

// ClientsOverviewOptions filters the clients/overview call. Resolution must be
// one of [7200, 86400, 604800, 2629746] per spec — the caller resolves against
// KnownEndpointRanges before calling.
type ClientsOverviewOptions struct {
	Window     *TimeRangeWindow
	Resolution time.Duration
}

func (o ClientsOverviewOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	res := o.Resolution
	if res <= 0 && o.Window != nil {
		res = o.Window.Resolution
	}
	if res > 0 {
		v.Set("resolution", strconv.Itoa(int(res.Seconds())))
	}
	return v
}

// GetOrganizationClientsOverview fetches the org-wide client count + usage
// summary. Not paginated.
func (c *Client) GetOrganizationClientsOverview(ctx context.Context, orgID string, opts ClientsOverviewOptions, ttl time.Duration) (*ClientsOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/clients/overview", Message: "missing organization id"}}
	}
	var out ClientsOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/clients/overview",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Top-N summaries -------------------------------------------------------

// TopOptions is the shared option shape for every `/summary/top/*` endpoint.
// timespan is clamped to 8h-186d by the server; quantity clamps to 1-50
// (default 10).
type TopOptions struct {
	Window   *TimeRangeWindow
	Timespan time.Duration
	Quantity int
}

// values emits timespan=<seconds> (preferred) or t0/t1 (when a Window is
// supplied instead) and quantity when > 0. Quantity is clamped to 1-50.
func (o TopOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
	}
	q := o.Quantity
	if q > 0 {
		if q > 50 {
			q = 50
		}
		if q < 1 {
			q = 1
		}
		v.Set("quantity", strconv.Itoa(q))
	}
	return v
}

// TopNetworkRef is the abbreviated network reference embedded on most top-N
// responses.
type TopNetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// TopUsage is the `{sent, recv, total}` triple used by top clients/devices.
// Units are MB per spec.
type TopUsage struct {
	Sent  float64 `json:"sent"`
	Recv  float64 `json:"recv"`
	Total float64 `json:"total"`
}

// TopClient is one entry in `/summary/top/clients/byUsage`.
type TopClient struct {
	Name    string        `json:"name,omitempty"`
	ID      string        `json:"id,omitempty"`
	MAC     string        `json:"mac,omitempty"`
	Network TopNetworkRef `json:"network"`
	Usage   TopUsage      `json:"usage"`
}

// TopDevice is one entry in `/summary/top/devices/byUsage`.
type TopDevice struct {
	Name        string        `json:"name,omitempty"`
	Serial      string        `json:"serial"`
	MAC         string        `json:"mac,omitempty"`
	Model       string        `json:"model,omitempty"`
	Network     TopNetworkRef `json:"network"`
	ProductType string        `json:"productType,omitempty"`
	Usage       TopUsage      `json:"usage"`
}

// TopDeviceModelUsage is the `{total, average, downstream, upstream}` triple
// carried by each top-devices-by-model row.
type TopDeviceModelUsage struct {
	Total      float64 `json:"total"`
	Average    float64 `json:"average"`
	Downstream float64 `json:"downstream"`
	Upstream   float64 `json:"upstream"`
}

// TopDeviceModel is one entry in `/summary/top/devices/models/byUsage`.
type TopDeviceModel struct {
	Model string              `json:"model"`
	Count int64               `json:"count"`
	Usage TopDeviceModelUsage `json:"usage"`
}

// TopSsidClients is `{counts: {total: N}}` — the client-count breakdown on
// each top-ssid row.
type TopSsidClients struct {
	Counts TopSsidCountsInner `json:"counts"`
}

// TopSsidCountsInner is `{total: N}`.
type TopSsidCountsInner struct {
	Total int64 `json:"total"`
}

// TopSsidUsage is `{total, downstream, upstream, percentage}` — the usage
// breakdown on each top-ssid row.
type TopSsidUsage struct {
	Total      float64 `json:"total"`
	Downstream float64 `json:"downstream"`
	Upstream   float64 `json:"upstream"`
	Percentage float64 `json:"percentage"`
}

// TopSsid is one entry in `/summary/top/ssids/byUsage`.
type TopSsid struct {
	Name    string         `json:"name"`
	Clients TopSsidClients `json:"clients"`
	Usage   TopSsidUsage   `json:"usage"`
}

// TopSwitchEnergyUsage is `{total: <joules>}` per the spec. The handler
// converts to kWh on emission (divide by 3,600,000).
type TopSwitchEnergyUsage struct {
	Total float64 `json:"total"`
}

// TopSwitchEnergy is one entry in `/summary/top/switches/byEnergyUsage`.
type TopSwitchEnergy struct {
	Name    string               `json:"name,omitempty"`
	Serial  string               `json:"serial"`
	Model   string               `json:"model,omitempty"`
	Network TopNetworkRef        `json:"network"`
	Usage   TopSwitchEnergyUsage `json:"usage"`
}

// TopNetworkStatusStatuses is the WAN/overall status triple on top-networks-
// by-status rows.
type TopNetworkStatusStatuses struct {
	OverallStatus string `json:"overallStatus,omitempty"`
	Wan1Status    string `json:"wan1Status,omitempty"`
	Wan2Status    string `json:"wan2Status,omitempty"`
}

// TopNetworkStatusClients is `{counts: {total: N}}` — matches TopSsidClients.
type TopNetworkStatusClients struct {
	Counts TopSsidCountsInner `json:"counts"`
}

// TopNetworkStatusUsage is the usage triple on top-networks rows.
type TopNetworkStatusUsage struct {
	Total      float64 `json:"total"`
	Downstream float64 `json:"downstream"`
	Upstream   float64 `json:"upstream"`
}

// TopNetworkStatus is one entry in `/summary/top/networks/byStatus`.
// Link-paginated.
type TopNetworkStatus struct {
	NetworkID    string                   `json:"networkId"`
	NetworkName  string                   `json:"networkName,omitempty"`
	ProductTypes []string                 `json:"productTypes,omitempty"`
	Statuses     TopNetworkStatusStatuses `json:"statuses"`
	Clients      TopNetworkStatusClients  `json:"clients"`
	Usage        TopNetworkStatusUsage    `json:"usage"`
	Devices      []json.RawMessage        `json:"devices,omitempty"`
}

// GetOrganizationTopClientsByUsage lists the top-N clients by usage.
func (c *Client) GetOrganizationTopClientsByUsage(ctx context.Context, orgID string, opts TopOptions, ttl time.Duration) ([]TopClient, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/clients/byUsage", Message: "missing organization id"}}
	}
	var out []TopClient
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/clients/byUsage",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationTopDevicesByUsage lists the top-N devices by usage.
func (c *Client) GetOrganizationTopDevicesByUsage(ctx context.Context, orgID string, opts TopOptions, ttl time.Duration) ([]TopDevice, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/devices/byUsage", Message: "missing organization id"}}
	}
	var out []TopDevice
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/devices/byUsage",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationTopDeviceModelsByUsage lists the top-N device models by usage.
func (c *Client) GetOrganizationTopDeviceModelsByUsage(ctx context.Context, orgID string, opts TopOptions, ttl time.Duration) ([]TopDeviceModel, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/devices/models/byUsage", Message: "missing organization id"}}
	}
	var out []TopDeviceModel
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/devices/models/byUsage",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationTopSsidsByUsage lists the top-N SSIDs by usage.
func (c *Client) GetOrganizationTopSsidsByUsage(ctx context.Context, orgID string, opts TopOptions, ttl time.Duration) ([]TopSsid, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/ssids/byUsage", Message: "missing organization id"}}
	}
	var out []TopSsid
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/ssids/byUsage",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationTopSwitchesByEnergyUsage lists the top-N switches by energy
// usage (in joules).
func (c *Client) GetOrganizationTopSwitchesByEnergyUsage(ctx context.Context, orgID string, opts TopOptions, ttl time.Duration) ([]TopSwitchEnergy, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/switches/byEnergyUsage", Message: "missing organization id"}}
	}
	var out []TopSwitchEnergy
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/switches/byEnergyUsage",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationTopNetworksByStatus lists the top-N networks by status.
// Link-paginated (perPage 3-5000).
func (c *Client) GetOrganizationTopNetworksByStatus(ctx context.Context, orgID string, opts TopOptions, ttl time.Duration) ([]TopNetworkStatus, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/networks/byStatus", Message: "missing organization id"}}
	}
	var out []TopNetworkStatus
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/networks/byStatus",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
