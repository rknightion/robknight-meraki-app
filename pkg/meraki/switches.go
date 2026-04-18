package meraki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// SwitchNetworkRef is the abbreviated network summary that the org-level
// switch ports endpoint embeds on every entry.
type SwitchNetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// SwitchWithPorts mirrors one entry in the response of
// `GET /organizations/{organizationId}/switch/ports/statuses/bySwitch`.
// Each element represents one switch device. Stack members are returned as
// separate entries; the `switchStackId` field (per the v1 OpenAPI spec)
// carries the stack id when the device belongs to a stack.
type SwitchWithPorts struct {
	Serial       string             `json:"serial"`
	Name         string             `json:"name,omitempty"`
	Model        string             `json:"model,omitempty"`
	MAC          string             `json:"mac,omitempty"`
	Network      SwitchNetworkRef   `json:"network"`
	StackID      string             `json:"switchStackId,omitempty"`
	Ports        []SwitchPortStatus `json:"ports"`
}

// SwitchPortStatus is one entry in the `ports` array embedded per switch by
// the org-level `statuses/bySwitch` endpoint. The shape combines port config
// (enabled, vlan) with live status (status, duplex, speed) and link-diagnostic
// fields (errors, warnings, clientCount, powerUsageInWatts).
type SwitchPortStatus struct {
	PortID            string   `json:"portId"`
	Enabled           bool     `json:"enabled"`
	Status            string   `json:"status,omitempty"`
	Errors            []string `json:"errors,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	// Speed is surfaced as a human-readable string like "10 Gbps" in some
	// responses and a numeric Mbps value in others (the v1 spec is string).
	// We keep a string copy and derive Mbps in the handler via parseSpeedMbps
	// so callers get a stable numeric column.
	Speed             string   `json:"speed,omitempty"`
	Duplex            string   `json:"duplex,omitempty"`
	ClientCount       int64    `json:"clientCount,omitempty"`
	PowerUsageInWatts float64  `json:"powerUsageInWatts,omitempty"`
	TrafficInKbps     *SwitchPortTrafficKbps `json:"trafficInKbps,omitempty"`
	UsageInKb         *SwitchPortUsageKb     `json:"usageInKb,omitempty"`
	// Config fields that the bySwitch endpoint echoes.
	Vlan         int    `json:"vlan,omitempty"`
	VoiceVlan    int    `json:"voiceVlan,omitempty"`
	AllowedVlans string `json:"allowedVlans,omitempty"`
	IsUplink     bool   `json:"isUplink,omitempty"`
}

// SwitchPortTrafficKbps and SwitchPortUsageKb are surfaced by some firmware
// versions of the statuses feed. We keep them so tests and UI can inspect the
// totals when present without hard-depending on their availability.
type SwitchPortTrafficKbps struct {
	Total float64 `json:"total,omitempty"`
	Sent  float64 `json:"sent,omitempty"`
	Recv  float64 `json:"recv,omitempty"`
}

type SwitchPortUsageKb struct {
	Total int64 `json:"total,omitempty"`
	Sent  int64 `json:"sent,omitempty"`
	Recv  int64 `json:"recv,omitempty"`
}

// SwitchPortStatusOptions filters the org-level port-status call. The
// endpoint's perPage cap is 3-20 (default 10) — much lower than most paged
// endpoints. With our MaxPages=100 this still supports ~2000 switches.
type SwitchPortStatusOptions struct {
	NetworkIDs []string
	Serials    []string
}

func (o SwitchPortStatusOptions) values() url.Values {
	// perPage 20 is the endpoint's maximum per the v1 OpenAPI spec.
	v := url.Values{"perPage": []string{"20"}}
	// The `statuses/bySwitch` endpoint requires bracketed array filters
	// (`serials[]=X`, `networkIds[]=Y`). The plain `serials=X` form returns
	// HTTP 400 "'serials' must be an array" — verified against api.meraki.com
	// 2026-04-18. Keep the brackets or the per-switch detail pages go empty.
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	return v
}

// switchPortsBySwitchResponse matches the on-the-wire shape of
// GET /organizations/{organizationId}/switch/ports/statuses/bySwitch, which
// wraps the switches array in an `items` object along with a pagination meta
// block. This is DIFFERENT from most Meraki v1 list endpoints (which return a
// raw JSON array), so we can't use the shared `GetAll` helper — its pagination
// merger assumes each page is an array literal.
type switchPortsBySwitchResponse struct {
	Items []SwitchWithPorts `json:"items"`
}

// GetOrganizationSwitchPortStatuses fetches every switch in the org with its
// embedded port list. The handler in pkg/plugin/query/switches.go flattens the
// nested ports into one row per (switch, port).
//
// Single-page fetch by design: the endpoint caps perPage at 20 but most estates
// have ≤20 switches, and the fleet page uses this same cached entry. If an
// estate grows beyond the cap we'll need startingAfter-based pagination (the
// Link header here only emits rel=first / rel=last, not rel=next, so the
// shared Link-follower in GetAll wouldn't work either way).
func (c *Client) GetOrganizationSwitchPortStatuses(ctx context.Context, orgID string, opts SwitchPortStatusOptions, ttl time.Duration) ([]SwitchWithPorts, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/switch/ports/statuses/bySwitch", Message: "missing organization id"}}
	}
	var wrapper switchPortsBySwitchResponse
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/switch/ports/statuses/bySwitch",
		orgID, opts.values(), ttl, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Items, nil
}

// SwitchPortConfig mirrors one entry of `GET /devices/{serial}/switch/ports`.
// This is the device-local port config feed — longer-lived than the status
// feed, so callers use a 5-minute cache.
type SwitchPortConfig struct {
	PortID                  string   `json:"portId"`
	Name                    string   `json:"name,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
	Enabled                 bool     `json:"enabled"`
	PoeEnabled              bool     `json:"poeEnabled,omitempty"`
	Type                    string   `json:"type,omitempty"`
	Vlan                    int      `json:"vlan,omitempty"`
	VoiceVlan               int      `json:"voiceVlan,omitempty"`
	AllowedVlans            string   `json:"allowedVlans,omitempty"`
	IsolationEnabled        bool     `json:"isolationEnabled,omitempty"`
	RstpEnabled             bool     `json:"rstpEnabled,omitempty"`
	StpGuard                string   `json:"stpGuard,omitempty"`
	LinkNegotiation         string   `json:"linkNegotiation,omitempty"`
	PortScheduleID          string   `json:"portScheduleId,omitempty"`
	Udld                    string   `json:"udld,omitempty"`
	AccessPolicyType        string   `json:"accessPolicyType,omitempty"`
	AccessPolicyNumber      int      `json:"accessPolicyNumber,omitempty"`
	MacAllowList            []string `json:"macAllowList,omitempty"`
	StickyMacAllowList      []string `json:"stickyMacAllowList,omitempty"`
	StickyMacAllowListLimit int      `json:"stickyMacAllowListLimit,omitempty"`
	StormControlEnabled     bool     `json:"stormControlEnabled,omitempty"`
	AdaptivePolicyGroupID   string   `json:"adaptivePolicyGroupId,omitempty"`
	PeerSgtCapable          bool     `json:"peerSgtCapable,omitempty"`
	FlexibleStackingEnabled bool     `json:"flexibleStackingEnabled,omitempty"`
	DaiTrusted              bool     `json:"daiTrusted,omitempty"`
}

// GetDeviceSwitchPorts returns the port config list for one switch. Unlike
// the org-level statuses feed, this endpoint is not paginated.
func (c *Client) GetDeviceSwitchPorts(ctx context.Context, serial string, ttl time.Duration) ([]SwitchPortConfig, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/switch/ports", Message: "missing serial"}}
	}
	var out []SwitchPortConfig
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/switch/ports",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SwitchPortPacketCounter is one entry in the response of
// `GET /devices/{serial}/switch/ports/statuses/packets`. The Meraki endpoint
// returns counters for every port of a switch; each entry carries a `packets`
// array with one bucket per counter category (Total, Broadcast, Multicast,
// CRC align errors, Collisions, etc.).
type SwitchPortPacketCounter struct {
	PortID  string                 `json:"portId"`
	Packets []SwitchPortPacketBucket `json:"packets"`
}

// SwitchPortPacketBucket is one counter category for a port. `desc` is the
// human-readable name (e.g. "Total", "Broadcast"); `total/sent/recv` are
// cumulative counts over the timespan; `ratePerSec` is the derivative.
type SwitchPortPacketBucket struct {
	Desc       string                  `json:"desc"`
	Total      int64                   `json:"total"`
	Sent       int64                   `json:"sent"`
	Recv       int64                   `json:"recv"`
	RatePerSec *SwitchPortPacketRate   `json:"ratePerSec,omitempty"`
}

// SwitchPortPacketRate is the per-second rate block nested inside each
// packet counter bucket.
type SwitchPortPacketRate struct {
	Total float64 `json:"total"`
	Sent  float64 `json:"sent"`
	Recv  float64 `json:"recv"`
}

// GetDeviceSwitchPortPacketCounters returns packet counters for every port on
// a switch. Meraki only exposes a device-level endpoint (not per-port), so
// callers that need a single port must filter client-side on `PortID`.
// When timespan > 0 it is passed as the `timespan` query param (in seconds);
// the API snaps that to the nearest preset window (5m / 15m / 1h / 1d).
func (c *Client) GetDeviceSwitchPortPacketCounters(ctx context.Context, serial string, timespan time.Duration, ttl time.Duration) ([]SwitchPortPacketCounter, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/switch/ports/statuses/packets", Message: "missing serial"}}
	}
	params := url.Values{}
	if timespan > 0 {
		params.Set("timespan", strconv.Itoa(int(timespan.Seconds())))
	}
	var out []SwitchPortPacketCounter
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/switch/ports/statuses/packets",
		"", params, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §3.1 — Switch ports overview by speed + usage history
// ---------------------------------------------------------------------------

// SwitchPortsOverviewBySpeed is a flattened speed bucket from
// GET /organizations/{organizationId}/switch/ports/overview.
//
// Response shape (verified 2026-04-18):
//
//	{"counts":{"total":N,"byStatus":{"active":{"total":N,"byMediaAndLinkSpeed":{"rj45":{"10":N,"100":N,"1000":N,...,"total":N},"sfp":{...}}},"inactive":{"total":N,"byMedia":{"rj45":{"total":N},"sfp":{"total":N}}}}}}
//
// We flatten into one row per (media × speed) plus one row per (media, "inactive").
// The handler in pkg/plugin/query may aggregate further.
type SwitchPortsOverviewBySpeed struct {
	// Media is "rj45" or "sfp".
	Media  string
	// Speed is the link speed in Mbps as a string, e.g. "10","100","1000","10000".
	// For inactive buckets where no per-speed breakdown exists, Speed is "inactive".
	Speed  string
	Active int64 // count of active ports at this speed
}

// SwitchPortsOverviewOptions filters the /switch/ports/overview call.
// All fields optional; the endpoint accepts t0/t1 or timespan (12h–186d).
type SwitchPortsOverviewOptions struct {
	NetworkIDs []string
	Serials    []string
	Window     *TimeRangeWindow
}

func (o SwitchPortsOverviewOptions) values() url.Values {
	v := url.Values{}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	return v
}

// switchPortsOverviewResponse is the raw response envelope for
// GET /organizations/{organizationId}/switch/ports/overview.
type switchPortsOverviewResponse struct {
	Counts switchPortsOverviewCounts `json:"counts"`
}

type switchPortsOverviewCounts struct {
	Total    int64                     `json:"total"`
	ByStatus switchPortsOverviewStatus `json:"byStatus"`
}

type switchPortsOverviewStatus struct {
	Active   switchPortsOverviewActive   `json:"active"`
	Inactive switchPortsOverviewInactive `json:"inactive"`
}

type switchPortsOverviewActive struct {
	Total               int64                          `json:"total"`
	ByMediaAndLinkSpeed switchPortsOverviewByMediaSpeed `json:"byMediaAndLinkSpeed"`
}

type switchPortsOverviewByMediaSpeed struct {
	RJ45 map[string]int64 `json:"rj45"` // keys are speed strings + "total"
	SFP  map[string]int64 `json:"sfp"`
}

type switchPortsOverviewInactive struct {
	Total   int64                       `json:"total"`
	ByMedia switchPortsOverviewByMedia  `json:"byMedia"`
}

type switchPortsOverviewByMedia struct {
	RJ45 struct{ Total int64 `json:"total"` } `json:"rj45"`
	SFP  struct{ Total int64 `json:"total"` } `json:"sfp"`
}

// GetOrganizationSwitchPortsOverview returns speed-bucket counts for all switch
// ports in the org. Not paginated — single GET.
func (c *Client) GetOrganizationSwitchPortsOverview(ctx context.Context, orgID string, opts SwitchPortsOverviewOptions, ttl time.Duration) ([]SwitchPortsOverviewBySpeed, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/switch/ports/overview", Message: "missing organization id"}}
	}
	var raw switchPortsOverviewResponse
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/switch/ports/overview",
		orgID, opts.values(), ttl, &raw); err != nil {
		return nil, err
	}

	var out []SwitchPortsOverviewBySpeed
	// Flatten active RJ45 speeds.
	for speed, cnt := range raw.Counts.ByStatus.Active.ByMediaAndLinkSpeed.RJ45 {
		if speed == "total" {
			continue
		}
		out = append(out, SwitchPortsOverviewBySpeed{Media: "rj45", Speed: speed, Active: cnt})
	}
	// Flatten active SFP speeds.
	for speed, cnt := range raw.Counts.ByStatus.Active.ByMediaAndLinkSpeed.SFP {
		if speed == "total" {
			continue
		}
		out = append(out, SwitchPortsOverviewBySpeed{Media: "sfp", Speed: speed, Active: cnt})
	}
	// Inactive buckets (no per-speed breakdown available).
	if raw.Counts.ByStatus.Inactive.ByMedia.RJ45.Total > 0 {
		out = append(out, SwitchPortsOverviewBySpeed{Media: "rj45", Speed: "inactive", Active: 0})
	}
	if raw.Counts.ByStatus.Inactive.ByMedia.SFP.Total > 0 {
		out = append(out, SwitchPortsOverviewBySpeed{Media: "sfp", Speed: "inactive", Active: 0})
	}
	return out, nil
}

// ---------------------------------------------------------------------------

// SwitchPortUsageHistoryPoint is one flattened (device × time interval) row
// from GET /organizations/{organizationId}/switch/ports/usage/history/byDevice/byInterval.
//
// Response shape (verified 2026-04-18):
//
//	{"items":[{"serial":"…","network":{"id":"…","name":"…"},"ports":[{"portId":"…","intervals":[{"startTs":"…","endTs":"…","data":{"usage":{"total":N,"upstream":N,"downstream":N}}}]}]}],"meta":{…}}
//
// We sum port-level usage across all ports on each device per interval so
// callers get a per-device aggregate (total throughput for the switch).
// The Sent/Recv/Total values are in kilobytes per the Meraki spec.
type SwitchPortUsageHistoryPoint struct {
	StartTs   time.Time
	EndTs     time.Time
	Serial    string
	NetworkID string
	Sent      int64 // kilobytes (upstream from the device's perspective)
	Recv      int64 // kilobytes (downstream)
	Total     int64 // kilobytes
}

// SwitchPortsUsageHistoryOptions filters the byDevice/byInterval call.
type SwitchPortsUsageHistoryOptions struct {
	NetworkIDs []string
	Serials    []string
	Window     *TimeRangeWindow
	Interval   time.Duration
}

func (o SwitchPortsUsageHistoryOptions) values() url.Values {
	v := url.Values{"perPage": []string{"1000"}}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	if o.Interval > 0 {
		v.Set("interval", strconv.Itoa(int(o.Interval.Seconds())))
	}
	return v
}

// switchPortsUsageHistoryResponse is the on-wire envelope — wraps items in an
// object with a meta block (same shape as statuses/bySwitch and memory/history).
type switchPortsUsageHistoryResponse struct {
	Items []switchPortsUsageHistoryDevice `json:"items"`
	Meta  struct {
		Counts struct {
			Items struct {
				Remaining int `json:"remaining"`
			} `json:"items"`
		} `json:"counts"`
	} `json:"meta"`
}

type switchPortsUsageHistoryDevice struct {
	Serial  string                   `json:"serial"`
	Network SwitchNetworkRef         `json:"network"`
	Ports   []switchPortUsagePort    `json:"ports"`
}

type switchPortUsagePort struct {
	PortID    string                      `json:"portId"`
	Intervals []switchPortUsageInterval   `json:"intervals"`
}

type switchPortUsageInterval struct {
	StartTs string                   `json:"startTs"`
	EndTs   string                   `json:"endTs"`
	Data    switchPortUsageIntervalData `json:"data"`
}

type switchPortUsageIntervalData struct {
	Usage struct {
		Total      int64 `json:"total"`
		Upstream   int64 `json:"upstream"`
		Downstream int64 `json:"downstream"`
	} `json:"usage"`
}

// ---------------------------------------------------------------------------
// §4.4.3-1b — switchPoe / switchStp / switchMacTable / switchVlansSummary
// ---------------------------------------------------------------------------

// SwitchPortPoeStatus is a per-port PoE draw entry. Sourced by reusing the
// existing org-level `statuses/bySwitch` endpoint (which already carries
// `powerUsageInWatts` per port); we shape it per-port rather than per-switch
// so callers can render a per-port PoE distribution without an expand
// transform.
type SwitchPortPoeStatus struct {
	Serial      string  `json:"serial"`
	SwitchName  string  `json:"switchName,omitempty"`
	NetworkID   string  `json:"networkId,omitempty"`
	NetworkName string  `json:"networkName,omitempty"`
	PortID      string  `json:"portId"`
	Enabled     bool    `json:"enabled"`
	PoeWatts    float64 `json:"poeWatts"`
}

// SwitchStpSettings mirrors `GET /networks/{networkId}/switch/stp`.
// Verified shape per Meraki v1 OpenAPI: {"rstpEnabled": bool,
// "stpBridgePriority": [{"switches": [serial…], "stacks": [stackId…],
// "switchProfiles": [profileId…], "stpPriority": int}]}.
type SwitchStpSettings struct {
	RstpEnabled       bool                    `json:"rstpEnabled"`
	StpBridgePriority []SwitchStpBridgePriority `json:"stpBridgePriority"`
}

type SwitchStpBridgePriority struct {
	Switches       []string `json:"switches,omitempty"`
	Stacks         []string `json:"stacks,omitempty"`
	SwitchProfiles []string `json:"switchProfiles,omitempty"`
	StpPriority    int      `json:"stpPriority"`
}

// GetNetworkSwitchStp returns the STP settings for a switch network.
func (c *Client) GetNetworkSwitchStp(ctx context.Context, networkID string, ttl time.Duration) (*SwitchStpSettings, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/switch/stp", Message: "missing network id"}}
	}
	var out SwitchStpSettings
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/switch/stp",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeviceClient mirrors one entry of `GET /devices/{serial}/clients` — the
// per-device client list. For a switch the `switchport` field is populated;
// for APs it's null (callers here filter for switches, so it's always set).
// Usage is in kilobytes per the Meraki spec.
type DeviceClient struct {
	ID          string  `json:"id,omitempty"`
	MAC         string  `json:"mac"`
	Description string  `json:"description,omitempty"`
	IP          string  `json:"ip,omitempty"`
	IP6         string  `json:"ip6,omitempty"`
	User        string  `json:"user,omitempty"`
	VLAN        int     `json:"vlan,omitempty"`
	SwitchPort  string  `json:"switchport,omitempty"`
	Manufacturer string  `json:"manufacturer,omitempty"`
	OS          string  `json:"os,omitempty"`
	Usage        struct {
		Sent float64 `json:"sent"`
		Recv float64 `json:"recv"`
	} `json:"usage"`
	FirstSeen int64 `json:"firstSeen,omitempty"`
	LastSeen  int64 `json:"lastSeen,omitempty"`
}

// GetDeviceClients fetches the client list for one device. `timespan` is in
// seconds and passed through when > 0 (max 31 days per the Meraki spec). Used
// by the switch MAC-table panel — one row per MAC that was connected to this
// switch in the requested window.
func (c *Client) GetDeviceClients(ctx context.Context, serial string, timespan time.Duration, ttl time.Duration) ([]DeviceClient, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/clients", Message: "missing serial"}}
	}
	params := url.Values{}
	if timespan > 0 {
		params.Set("timespan", strconv.Itoa(int(timespan.Seconds())))
	}
	var out []DeviceClient
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/clients",
		"", params, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SwitchBySwitchPortsOptions filters `/organizations/{orgId}/switch/ports/bySwitch`.
// Used by the VLAN-summary handler (port count per VLAN per switch). Same
// serials/networkIds filter contract as the statuses/bySwitch endpoint.
type SwitchBySwitchPortsOptions struct {
	NetworkIDs []string
	Serials    []string
}

func (o SwitchBySwitchPortsOptions) values() url.Values {
	v := url.Values{"perPage": []string{"50"}}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	return v
}

// SwitchBySwitch is one entry in `GET /organizations/{orgId}/switch/ports/bySwitch`
// — a switch with its list of configured ports (NOT live statuses). Used for
// port-count-per-VLAN-per-switch aggregation.
type SwitchBySwitch struct {
	Serial  string              `json:"serial"`
	Name    string              `json:"name,omitempty"`
	Model   string              `json:"model,omitempty"`
	MAC     string              `json:"mac,omitempty"`
	Network SwitchNetworkRef    `json:"network"`
	Ports   []SwitchBySwitchPort `json:"ports"`
}

// SwitchBySwitchPort is the port-config row embedded on each `SwitchBySwitch`.
type SwitchBySwitchPort struct {
	PortID       string `json:"portId"`
	Name         string `json:"name,omitempty"`
	Enabled      bool   `json:"enabled"`
	Type         string `json:"type,omitempty"`
	Vlan         int    `json:"vlan,omitempty"`
	VoiceVlan    int    `json:"voiceVlan,omitempty"`
	AllowedVlans string `json:"allowedVlans,omitempty"`
}

// GetOrganizationSwitchPortsBySwitch fetches the config-feed variant of the
// bySwitch endpoint (distinct from `/switch/ports/statuses/bySwitch`). The
// response wraps items in an `items` object like the statuses variant.
func (c *Client) GetOrganizationSwitchPortsBySwitch(ctx context.Context, orgID string, opts SwitchBySwitchPortsOptions, ttl time.Duration) ([]SwitchBySwitch, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/switch/ports/bySwitch", Message: "missing organization id"}}
	}
	var wrapper struct {
		Items []SwitchBySwitch `json:"items"`
	}
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/switch/ports/bySwitch",
		orgID, opts.values(), ttl, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Items, nil
}

// GetOrganizationSwitchPortsUsageHistory paginates through the switch ports
// usage history endpoint for an org. It follows Link: rel=next headers.
// Results are aggregated to per-device per-interval (summed across all ports).
func (c *Client) GetOrganizationSwitchPortsUsageHistory(ctx context.Context, orgID string, opts SwitchPortsUsageHistoryOptions, ttl time.Duration) ([]SwitchPortUsageHistoryPoint, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/switch/ports/usage/history/byDevice/byInterval", Message: "missing organization id"}}
	}

	endpoint := "organizations/" + url.PathEscape(orgID) + "/switch/ports/usage/history/byDevice/byInterval"
	params := opts.values()

	// This endpoint wraps items in an envelope (not a raw array) so we can't use
	// GetAll directly. Follow Link: rel=next pages manually.
	var allDevices []switchPortsUsageHistoryDevice
	path := endpoint
	for pageNum := 0; pageNum < MaxPages; pageNum++ {
		var pageParams url.Values
		if pageNum == 0 {
			pageParams = params
		}
		body, hdr, err := c.Do(ctx, "GET", path, orgID, pageParams, nil)
		if err != nil {
			return nil, err
		}

		var pageResp switchPortsUsageHistoryResponse
		if err := json.Unmarshal(body, &pageResp); err != nil {
			return nil, fmt.Errorf("meraki: decode switch ports usage history: %w", err)
		}
		allDevices = append(allDevices, pageResp.Items...)

		next := nextLink(hdr)
		if next == "" {
			break
		}
		path = next
	}

	// Flatten and aggregate per-device per-interval (sum across all ports).
	type devIntervalKey struct {
		Serial  string
		StartTs string
	}
	type devIntervalVal struct {
		EndTs     string
		NetworkID string
		Sent      int64
		Recv      int64
		Total     int64
	}
	aggMap := make(map[devIntervalKey]*devIntervalVal)

	for _, dev := range allDevices {
		for _, port := range dev.Ports {
			for _, iv := range port.Intervals {
				k := devIntervalKey{Serial: dev.Serial, StartTs: iv.StartTs}
				e, ok := aggMap[k]
				if !ok {
					e = &devIntervalVal{EndTs: iv.EndTs, NetworkID: dev.Network.ID}
					aggMap[k] = e
				}
				e.Sent += iv.Data.Usage.Upstream
				e.Recv += iv.Data.Usage.Downstream
				e.Total += iv.Data.Usage.Total
			}
		}
	}

	out := make([]SwitchPortUsageHistoryPoint, 0, len(aggMap))
	for k, v := range aggMap {
		startTs, _ := time.Parse(time.RFC3339, k.StartTs)
		endTs, _ := time.Parse(time.RFC3339, v.EndTs)
		out = append(out, SwitchPortUsageHistoryPoint{
			StartTs:   startTs,
			EndTs:     endTs,
			Serial:    k.Serial,
			NetworkID: v.NetworkID,
			Sent:      v.Sent,
			Recv:      v.Recv,
			Total:     v.Total,
		})
	}
	return out, nil
}
