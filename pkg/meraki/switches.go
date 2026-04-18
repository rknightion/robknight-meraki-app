package meraki

import (
	"context"
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
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	return v
}

// GetOrganizationSwitchPortStatuses paginates through every switch in the org
// and returns each with its embedded port list. The handler in
// pkg/plugin/query/switches.go flattens the nested ports into one row per
// (switch, port).
func (c *Client) GetOrganizationSwitchPortStatuses(ctx context.Context, orgID string, opts SwitchPortStatusOptions, ttl time.Duration) ([]SwitchWithPorts, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/switch/ports/statuses/bySwitch", Message: "missing organization id"}}
	}
	var out []SwitchWithPorts
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/switch/ports/statuses/bySwitch",
		orgID, opts.values(), ttl, &out)
	return out, err
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
