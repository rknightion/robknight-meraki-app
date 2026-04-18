package meraki

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Phase 10 (MG cellular gateways) endpoint wrappers. Paths, pagination modes,
// and response shapes verified via ctx7 against the canonical
// /openapi/api_meraki_api_v1_openapispec dataset on 2026-04-17.
//
// Coverage:
//   - /organizations/{organizationId}/cellularGateway/uplink/statuses (paged)
//   - /devices/{serial}/cellularGateway/portForwardingRules (single GET, envelope unwrap)
//   - /devices/{serial}/cellularGateway/lan (single GET)
//   - /networks/{networkId}/cellularGateway/connectivityMonitoringDestinations (single GET, envelope unwrap)

// MgSignalStat is the nested RSRP/RSRQ block for MG uplinks. Per the v1 spec,
// values are STRINGS with units attached (e.g. "-87 dBm"). The ParseSignalDb
// helper extracts the numeric dBm for the handler's numeric column.
type MgSignalStat struct {
	RSRP string `json:"rsrp,omitempty"`
	RSRQ string `json:"rsrq,omitempty"`
}

// MgUplink is one uplink interface on an MG gateway. Unlike the MX feed, MG
// uplinks are always cellular so the provider/signalType/signalStat fields
// are always meaningful.
type MgUplink struct {
	Interface      string       `json:"interface,omitempty"`
	Status         string       `json:"status,omitempty"`
	ICCID          string       `json:"iccid,omitempty"`
	APN            string       `json:"apn,omitempty"`
	Provider       string       `json:"provider,omitempty"`
	PublicIP       string       `json:"publicIp,omitempty"`
	Model          string       `json:"model,omitempty"`
	SignalType     string       `json:"signalType,omitempty"`
	ConnectionType string       `json:"connectionType,omitempty"`
	DNS1           string       `json:"dns1,omitempty"`
	DNS2           string       `json:"dns2,omitempty"`
	SignalStat     MgSignalStat `json:"signalStat"`
}

// MgUplinkStatus is one row from
// `GET /organizations/{orgId}/cellularGateway/uplink/statuses`. Each entry is
// one MG appliance with its nested uplink list.
type MgUplinkStatus struct {
	Serial         string     `json:"serial"`
	Model          string     `json:"model,omitempty"`
	NetworkID      string     `json:"networkId,omitempty"`
	LastReportedAt *time.Time `json:"lastReportedAt,omitempty"`
	Uplinks        []MgUplink `json:"uplinks"`
}

// MgUplinkOptions filters the paginated cellularGateway/uplink/statuses feed.
// perPage is 3-1000 (default 1000). ICCIDs[] is accepted by the endpoint for
// SIM-level filtering.
type MgUplinkOptions struct {
	NetworkIDs []string
	Serials    []string
	ICCIDs     []string
	PerPage    int
}

func (o MgUplinkOptions) values() url.Values {
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
	v := url.Values{"perPage": []string{strconv.Itoa(per)}}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, i := range o.ICCIDs {
		v.Add("iccids[]", i)
	}
	return v
}

// GetOrganizationCellularGatewayUplinkStatuses returns every MG in the org
// with its uplink state. Paginated via Link header — c.GetAll follows.
func (c *Client) GetOrganizationCellularGatewayUplinkStatuses(ctx context.Context, orgID string, opts MgUplinkOptions, ttl time.Duration) ([]MgUplinkStatus, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/cellularGateway/uplink/statuses", Message: "missing organization id"}}
	}
	var out []MgUplinkStatus
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/cellularGateway/uplink/statuses",
		orgID, opts.values(), ttl, &out)
	return out, err
}

// MgPortForwardingRule mirrors one rule in the response of
// `GET /devices/{serial}/cellularGateway/portForwardingRules`. Meraki wraps
// the array in a `{"rules":[...]}` envelope we strip in the wrapper.
type MgPortForwardingRule struct {
	Name       string   `json:"name,omitempty"`
	Protocol   string   `json:"protocol,omitempty"`
	PublicPort string   `json:"publicPort,omitempty"`
	LocalPort  string   `json:"localPort,omitempty"`
	LanIP      string   `json:"lanIp,omitempty"`
	AllowedIPs []string `json:"allowedIps,omitempty"`
	Access     string   `json:"access,omitempty"`
}

type mgPortForwardingEnvelope struct {
	Rules []MgPortForwardingRule `json:"rules"`
}

// GetDeviceCellularGatewayPortForwardingRules unwraps the `{rules:[...]}`
// envelope. Not paginated.
func (c *Client) GetDeviceCellularGatewayPortForwardingRules(ctx context.Context, serial string, ttl time.Duration) ([]MgPortForwardingRule, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/cellularGateway/portForwardingRules", Message: "missing serial"}}
	}
	var env mgPortForwardingEnvelope
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/cellularGateway/portForwardingRules",
		"", nil, ttl, &env); err != nil {
		return nil, err
	}
	return env.Rules, nil
}

// MgFixedIPAssignment is one entry in the LAN config's fixedIpAssignments
// array — a static IP reservation by MAC.
type MgFixedIPAssignment struct {
	MAC  string `json:"mac"`
	Name string `json:"name,omitempty"`
	IP   string `json:"ip"`
}

// MgReservedIPRange is one entry in the LAN config's reservedIpRanges
// array — a contiguous IP range reserved for static assignment.
type MgReservedIPRange struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Comment string `json:"comment,omitempty"`
}

// MgLanConfig is the response shape of
// `GET /devices/{serial}/cellularGateway/lan`. Not paginated.
type MgLanConfig struct {
	DeviceLanIP        string                `json:"deviceLanIp,omitempty"`
	FixedIPAssignments []MgFixedIPAssignment `json:"fixedIpAssignments,omitempty"`
	ReservedIPRanges   []MgReservedIPRange   `json:"reservedIpRanges,omitempty"`
}

// GetDeviceCellularGatewayLan returns the LAN config for one MG. Not paginated.
func (c *Client) GetDeviceCellularGatewayLan(ctx context.Context, serial string, ttl time.Duration) (*MgLanConfig, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/cellularGateway/lan", Message: "missing serial"}}
	}
	var out MgLanConfig
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/cellularGateway/lan",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MgConnectivityDestination is one entry in the response of
// `GET /networks/{networkId}/cellularGateway/connectivityMonitoringDestinations`.
// Meraki wraps the array in a `{"destinations":[...]}` envelope we strip in
// the wrapper.
type MgConnectivityDestination struct {
	IP          string `json:"ip"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

type mgConnectivityEnvelope struct {
	Destinations []MgConnectivityDestination `json:"destinations"`
}

// GetNetworkCellularGatewayConnectivityMonitoringDestinations unwraps the
// `{destinations:[...]}` envelope. Not paginated.
func (c *Client) GetNetworkCellularGatewayConnectivityMonitoringDestinations(ctx context.Context, networkID string, ttl time.Duration) ([]MgConnectivityDestination, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/cellularGateway/connectivityMonitoringDestinations", Message: "missing network id"}}
	}
	var env mgConnectivityEnvelope
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/cellularGateway/connectivityMonitoringDestinations",
		"", nil, ttl, &env); err != nil {
		return nil, err
	}
	return env.Destinations, nil
}

// ParseSignalDb extracts the numeric dBm value from strings like
// `"-85 dBm"`, `"−85 dBm"` (unicode minus), `"-85"`, or `""`. Returns
// (0, false) on unparseable input. Exported so the cellular-gateway handler
// (and, in the future, the MX cellular uplink handler) can share one parser.
//
// The two minus signs come from Meraki's own JSON formatting: most responses
// use ASCII hyphen-minus (U+002D) but some older endpoints use the unicode
// minus sign (U+2212). Both are decoded to the standard arithmetic form
// before strconv sees the value.
func ParseSignalDb(s string) (float64, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, false
	}
	// Replace unicode minus (U+2212) with ASCII hyphen-minus so strconv
	// accepts the value. The substitution is safe even when the string uses
	// the ASCII form — ReplaceAll is a no-op in that case.
	trimmed = strings.ReplaceAll(trimmed, "\u2212", "-")
	// Split once on whitespace to isolate the numeric prefix from the unit
	// suffix ("dBm", "dB", etc). When there's no whitespace the whole string
	// is expected to be a bare number.
	if idx := strings.IndexAny(trimmed, " \t"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	if trimmed == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
