package meraki

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// Clients endpoints — Page A of v0.5 §4.4.4.
//
// Endpoint URLs and parameters captured from ctx7 against the canonical Meraki
// v1 OpenAPI spec on 2026-04-18:
//
//   GET /organizations/{organizationId}/clients/overview
//     Existing aggregate (totals + usage). Wrapper already lives in
//     insights.go (GetOrganizationClientsOverview).
//
//   GET /organizations/{organizationId}/clients/search?mac=...
//     REQUIRED: mac (full or partial). Pagination via Link header (perPage 3-5).
//     Returns ClientSearchResult with the canonical client identity (id, mac,
//     description) plus an array of `records[]` describing every network the
//     client has been seen on.
//
//   GET /networks/{networkId}/clients
//     Per-network client listing. Defaults to a 1-day timespan (max 31 days).
//     Pagination via Link header (perPage 3-5000). Filters: ip, ip6, mac, os,
//     vlan, statuses (Online|Offline), recentDeviceConnections (Wired|Wireless).
//
//   GET /networks/{networkId}/wireless/clients/{clientId}/latencyHistory
//     Per-client wireless latency history (Plan said /networks/{id}/clients/...
//     but the actual API is wireless-scoped). Lookback up to 791 days; only
//     resolution allowed is 86400s. Returns one row per bucket with average
//     latency split into traffic categories (background / bestEffort / video
//     / voice).

// --- Search ----------------------------------------------------------------

// ClientSearchRecord is one network sighting on /clients/search. The endpoint
// returns a flat client identity plus an array of these records — one per
// network the client has been observed on.
type ClientSearchRecord struct {
	Network              ClientNetworkRef `json:"network"`
	ClientID             string           `json:"clientId,omitempty"`
	IP                   string           `json:"ip,omitempty"`
	IP6                  string           `json:"ip6,omitempty"`
	IP6Local             string           `json:"ip6Local,omitempty"`
	User                 string           `json:"user,omitempty"`
	FirstSeen            *time.Time       `json:"firstSeen,omitempty"`
	LastSeen             *time.Time       `json:"lastSeen,omitempty"`
	OS                   string           `json:"os,omitempty"`
	SSID                 string           `json:"ssid,omitempty"`
	VLAN                 json.Number      `json:"vlan,omitempty"`
	Switchport           string           `json:"switchport,omitempty"`
	Status               string           `json:"status,omitempty"`
	UsageSentKb          float64          `json:"-"`
	UsageRecvKb          float64          `json:"-"`
	UsageRaw             json.RawMessage  `json:"usage,omitempty"`
	RecentDeviceMAC      string           `json:"recentDeviceMac,omitempty"`
	RecentDeviceName     string           `json:"recentDeviceName,omitempty"`
	RecentDeviceSerial   string           `json:"recentDeviceSerial,omitempty"`
	SmInstalled          bool             `json:"smInstalled,omitempty"`
	GroupPolicy8021x     string           `json:"groupPolicy8021x,omitempty"`
	AdaptivePolicyGroup  string           `json:"adaptivePolicyGroup,omitempty"`
	DeviceTypePrediction string           `json:"deviceTypePrediction,omitempty"`
	Manufacturer         string           `json:"manufacturer,omitempty"`
	Description          string           `json:"description,omitempty"`
}

// ClientNetworkRef is the abbreviated network reference embedded on most
// /clients responses.
type ClientNetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ClientSearchResult is the response shape of /clients/search.
type ClientSearchResult struct {
	ClientID    string               `json:"clientId,omitempty"`
	MAC         string               `json:"mac,omitempty"`
	Description string               `json:"description,omitempty"`
	Manufacturer string              `json:"manufacturer,omitempty"`
	OS          string               `json:"os,omitempty"`
	User        string               `json:"user,omitempty"`
	Records     []ClientSearchRecord `json:"records,omitempty"`
}

// ClientSearchOptions filters the org-wide /clients/search call. mac is
// REQUIRED — Meraki returns 400 without it. We expose it as a typed option
// rather than a positional arg so callers stay symmetric with the other
// option structs.
type ClientSearchOptions struct {
	MAC     string
	PerPage int
}

func (o ClientSearchOptions) values() url.Values {
	v := url.Values{}
	if o.MAC != "" {
		v.Set("mac", o.MAC)
	}
	per := o.PerPage
	if per <= 0 {
		per = 5
	}
	if per < 3 {
		per = 3
	}
	if per > 5 {
		per = 5
	}
	v.Set("perPage", strconv.Itoa(per))
	return v
}

// SearchOrganizationClient fetches the org-wide /clients/search payload for a
// given MAC. Single-result endpoint — when Meraki has no record of the MAC the
// response body is empty (handler treats that as "not found"). Pagination via
// Link header (the records array is small enough that a single page typically
// suffices).
func (c *Client) SearchOrganizationClient(ctx context.Context, orgID string, opts ClientSearchOptions, ttl time.Duration) (*ClientSearchResult, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/clients/search", Message: "missing organization id"}}
	}
	if opts.MAC == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/clients/search", Message: "mac is required"}}
	}
	var out ClientSearchResult
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/clients/search",
		orgID, opts.values(), ttl, &out); err != nil {
		// Meraki returns 404 when the MAC is unknown — surface as a typed
		// not-found so the handler can emit a zero-row notice frame instead of
		// a generic error.
		return nil, err
	}
	return &out, nil
}

// --- Per-network clients list ---------------------------------------------

// NetworkClient is one row from `GET /networks/{networkId}/clients`. We keep
// fields the Page A panels actually consume; the upstream payload has more
// (group policy, named VLAN, applied policies, etc.) that we can add later.
type NetworkClient struct {
	ID                  string          `json:"id,omitempty"`
	MAC                 string          `json:"mac"`
	Description         string          `json:"description,omitempty"`
	IP                  string          `json:"ip,omitempty"`
	IP6                 string          `json:"ip6,omitempty"`
	IP6Local            string          `json:"ip6Local,omitempty"`
	User                string          `json:"user,omitempty"`
	FirstSeen           *time.Time      `json:"firstSeen,omitempty"`
	LastSeen            *time.Time      `json:"lastSeen,omitempty"`
	Manufacturer        string          `json:"manufacturer,omitempty"`
	OS                  string          `json:"os,omitempty"`
	DeviceTypePrediction string         `json:"deviceTypePrediction,omitempty"`
	RecentDeviceSerial  string          `json:"recentDeviceSerial,omitempty"`
	RecentDeviceName    string          `json:"recentDeviceName,omitempty"`
	RecentDeviceMAC     string          `json:"recentDeviceMac,omitempty"`
	RecentDeviceConnection string       `json:"recentDeviceConnection,omitempty"`
	SSID                string          `json:"ssid,omitempty"`
	VLAN                json.Number     `json:"vlan,omitempty"`
	Switchport          string          `json:"switchport,omitempty"`
	Status              string          `json:"status,omitempty"`
	UsageSentKb         float64         `json:"-"`
	UsageRecvKb         float64         `json:"-"`
	UsageRaw            json.RawMessage `json:"usage,omitempty"`
	Notes               string          `json:"notes,omitempty"`
}

// NetworkClientsOptions filters /networks/{id}/clients. PerPage is clamped to
// 3-5000 by the server; we default to 1000 to match the rest of the client.
type NetworkClientsOptions struct {
	Timespan time.Duration
	T0       *time.Time
	PerPage  int

	// Optional server-side filters.
	Statuses                string
	IP                      string
	MAC                     string
	OS                      string
	VLAN                    string
	NamedVLAN               string
	Description             string
	RecentDeviceConnections string
}

func (o NetworkClientsOptions) values() url.Values {
	v := url.Values{}
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	if per < 3 {
		per = 3
	}
	if per > 5000 {
		per = 5000
	}
	v.Set("perPage", strconv.Itoa(per))
	if o.T0 != nil {
		v.Set("t0", o.T0.UTC().Format(time.RFC3339))
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
	}
	if o.Statuses != "" {
		v.Set("statuses", o.Statuses)
	}
	if o.IP != "" {
		v.Set("ip", o.IP)
	}
	if o.MAC != "" {
		v.Set("mac", o.MAC)
	}
	if o.OS != "" {
		v.Set("os", o.OS)
	}
	if o.VLAN != "" {
		v.Set("vlan", o.VLAN)
	}
	if o.NamedVLAN != "" {
		v.Set("namedVlan", o.NamedVLAN)
	}
	if o.Description != "" {
		v.Set("description", o.Description)
	}
	if o.RecentDeviceConnections != "" {
		v.Set("recentDeviceConnections", o.RecentDeviceConnections)
	}
	return v
}

// GetNetworkClients lists clients seen on a network within the timespan.
// Link-paginated. Handler is responsible for adding the network ID column to
// the emitted frame so multi-network panels can disambiguate rows.
func (c *Client) GetNetworkClients(ctx context.Context, networkID string, opts NetworkClientsOptions, ttl time.Duration) ([]NetworkClient, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/clients", Message: "missing network id"}}
	}
	var out []NetworkClient
	_, err := c.GetAll(ctx,
		"networks/"+url.PathEscape(networkID)+"/clients",
		networkID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ClientUsageBand is the {sent, recv, total} triple the /clients endpoints
// emit on the optional `usage` field. Units are kb. Exposed so callers that
// want to inspect the raw payload can decode it without re-defining the shape.
type ClientUsageBand struct {
	Sent  float64 `json:"sent"`
	Recv  float64 `json:"recv"`
	Total float64 `json:"total"`
}

// DecodeClientUsage parses the `usage` raw JSON into a typed band. Returns the
// zero value if the raw is nil/empty so call sites don't need a nil check.
func DecodeClientUsage(raw json.RawMessage) ClientUsageBand {
	if len(raw) == 0 {
		return ClientUsageBand{}
	}
	var u ClientUsageBand
	_ = json.Unmarshal(raw, &u)
	return u
}

// --- Per-client wireless latency history -----------------------------------

// ClientLatencyHistoryEntry is one bucket from
// /networks/{networkId}/wireless/clients/{clientId}/latencyHistory. Each bucket
// carries the average latency in milliseconds across four traffic categories
// (background, bestEffort, video, voice). Categories with no traffic in the
// bucket emit zero — the handler treats zero as a sample rather than a gap so
// the timeseries panel renders a continuous line.
type ClientLatencyHistoryEntry struct {
	StartTs            time.Time `json:"startTs"`
	EndTs              time.Time `json:"endTs"`
	AvgLatencyMs       float64   `json:"avgLatencyMs"`
	BackgroundAvgMs    float64   `json:"backgroundAvgLatencyMs,omitempty"`
	BestEffortAvgMs    float64   `json:"bestEffortAvgLatencyMs,omitempty"`
	VideoAvgMs         float64   `json:"videoAvgLatencyMs,omitempty"`
	VoiceAvgMs         float64   `json:"voiceAvgLatencyMs,omitempty"`
}

// ClientLatencyHistoryOptions filters the per-client latency call.
// resolution is fixed at 86400 by the spec (the only allowed value); we
// emit it for completeness.
type ClientLatencyHistoryOptions struct {
	Window     *TimeRangeWindow
	Timespan   time.Duration
	Resolution time.Duration
}

func (o ClientLatencyHistoryOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
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

// GetNetworkWirelessClientLatencyHistory fetches the per-client latency
// history bucket list. Not paginated.
func (c *Client) GetNetworkWirelessClientLatencyHistory(ctx context.Context, networkID, clientID string, opts ClientLatencyHistoryOptions, ttl time.Duration) ([]ClientLatencyHistoryEntry, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/clients/{clientId}/latencyHistory", Message: "missing network id"}}
	}
	if clientID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/clients/{clientId}/latencyHistory", Message: "missing client id"}}
	}
	var out []ClientLatencyHistoryEntry
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/wireless/clients/"+url.PathEscape(clientID)+"/latencyHistory",
		networkID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}
