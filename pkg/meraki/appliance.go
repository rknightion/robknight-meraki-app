package meraki

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Phase 8 (MX security appliances + VPN) endpoint wrappers. Paths, pagination
// modes, and response shapes verified via ctx7 against the canonical
// /openapi/api_meraki_api_v1_openapispec dataset on 2026-04-17.
//
// Coverage:
//   - /organizations/{organizationId}/appliance/uplink/statuses (paged)
//   - /organizations/{organizationId}/appliance/uplinks/statuses/overview (single GET)
//   - /organizations/{organizationId}/appliance/vpn/statuses (paged)
//   - /organizations/{organizationId}/appliance/vpn/stats (paged, windowed)
//   - /organizations/{organizationId}/devices/uplinksLossAndLatency (single GET, 5-min window)
//   - /networks/{networkId}/appliance/firewall/portForwardingRules (single GET, envelope unwrap)
//   - /networks/{networkId}/appliance/settings (single GET)

// ApplianceUplinkEntry is one row from
// `GET /organizations/{orgId}/appliance/uplink/statuses`. Each entry holds the
// appliance identifying fields plus a nested `uplinks` array (wan1/wan2/cellular).
// The handler flattens nested uplinks into one row per (serial, interface).
type ApplianceUplinkEntry struct {
	Serial           string                      `json:"serial"`
	Model            string                      `json:"model,omitempty"`
	NetworkID        string                      `json:"networkId"`
	LastReportedAt   *time.Time                  `json:"lastReportedAt,omitempty"`
	HighAvailability *ApplianceHighAvailability  `json:"highAvailability,omitempty"`
	Uplinks          []ApplianceUplinkInterface  `json:"uplinks"`
}

// ApplianceHighAvailability captures warm-spare state when the MX is paired.
type ApplianceHighAvailability struct {
	Role    string `json:"role,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

// ApplianceUplinkInterface is one WAN interface on an appliance. Cellular
// uplinks carry the provider/signalType/signalStat fields; wired uplinks leave
// them blank. Status values per Meraki docs: "active" | "ready" | "failed" |
// "not connected".
type ApplianceUplinkInterface struct {
	Interface      string                   `json:"interface"`
	Status         string                   `json:"status,omitempty"`
	IP             string                   `json:"ip,omitempty"`
	Gateway        string                   `json:"gateway,omitempty"`
	PublicIP       string                   `json:"publicIp,omitempty"`
	PrimaryDns     string                   `json:"primaryDns,omitempty"`
	SecondaryDns   string                   `json:"secondaryDns,omitempty"`
	IPAssignedBy   string                   `json:"ipAssignedBy,omitempty"`
	ICCID          string                   `json:"iccid,omitempty"`
	Provider       string                   `json:"provider,omitempty"`
	SignalType     string                   `json:"signalType,omitempty"`
	APN            string                   `json:"apn,omitempty"`
	ConnectionType string                   `json:"connectionType,omitempty"`
	SignalStat     *ApplianceUplinkSignalStat `json:"signalStat,omitempty"`
}

// ApplianceUplinkSignalStat is the nested RSRP/RSRQ block for cellular uplinks.
// Both fields are strings on the wire (Meraki reports dBm with a unit suffix
// in some responses) so we keep them as strings rather than forcing a parse.
type ApplianceUplinkSignalStat struct {
	RSRP string `json:"rsrp,omitempty"`
	RSRQ string `json:"rsrq,omitempty"`
}

// ApplianceUplinkOptions filters the paginated appliance/uplink/statuses feed.
// Meraki supports networkIds[], serials[], and iccids[] filters; perPage is
// 3-1000 (default 1000 — what we send).
type ApplianceUplinkOptions struct {
	NetworkIDs []string
	Serials    []string
	ICCIDs     []string
}

func (o ApplianceUplinkOptions) values() url.Values {
	v := url.Values{"perPage": []string{"1000"}}
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

// GetOrganizationApplianceUplinkStatuses returns every appliance in the org
// with its WAN/cellular interface state. Uses Link-header pagination via
// c.GetAll — not startingAfter — per Meraki's published pagination mode for
// this endpoint.
func (c *Client) GetOrganizationApplianceUplinkStatuses(ctx context.Context, orgID string, opts ApplianceUplinkOptions, ttl time.Duration) ([]ApplianceUplinkEntry, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/appliance/uplink/statuses", Message: "missing organization id"}}
	}
	var out []ApplianceUplinkEntry
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/appliance/uplink/statuses",
		orgID, opts.values(), ttl, &out)
	return out, err
}

// ApplianceUplinksOverview is the response shape of
// `GET /organizations/{orgId}/appliance/uplinks/statuses/overview`. Meraki
// returns one counts bucket with the four known statuses; we surface them as-is
// plus a computed total in the handler.
type ApplianceUplinksOverview struct {
	Counts ApplianceUplinksOverviewCounts `json:"counts"`
}

type ApplianceUplinksOverviewCounts struct {
	ByStatus ApplianceUplinksOverviewByStatus `json:"byStatus"`
}

type ApplianceUplinksOverviewByStatus struct {
	Active       int64 `json:"active"`
	Ready        int64 `json:"ready"`
	Failed       int64 `json:"failed"`
	NotConnected int64 `json:"notConnected"`
}

// GetOrganizationApplianceUplinksOverview returns the org-wide status-count
// summary. Not paginated.
func (c *Client) GetOrganizationApplianceUplinksOverview(ctx context.Context, orgID string, ttl time.Duration) (*ApplianceUplinksOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/appliance/uplinks/statuses/overview", Message: "missing organization id"}}
	}
	var out ApplianceUplinksOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/appliance/uplinks/statuses/overview",
		orgID, nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplianceVpnStatus is one row from
// `GET /organizations/{orgId}/appliance/vpn/statuses`. Each entry describes
// one network's AutoVPN posture plus the list of meraki and thirdParty VPN
// peers it is currently configured with.
type ApplianceVpnStatus struct {
	NetworkID          string                         `json:"networkId"`
	NetworkName        string                         `json:"networkName,omitempty"`
	DeviceStatus       string                         `json:"deviceStatus,omitempty"`
	DeviceSerial       string                         `json:"deviceSerial,omitempty"`
	VpnMode            string                         `json:"vpnMode,omitempty"`
	Uplinks            []ApplianceVpnUplink           `json:"uplinks,omitempty"`
	ExportedSubnets    []ApplianceVpnExportedSubnet   `json:"exportedSubnets,omitempty"`
	MerakiVpnPeers     []ApplianceVpnMerakiPeer       `json:"merakiVpnPeers,omitempty"`
	ThirdPartyVpnPeers []ApplianceVpnThirdPartyPeer   `json:"thirdPartyVpnPeers,omitempty"`
}

// ApplianceVpnUplink is one entry of the `uplinks` array — the WAN IP a peer
// would be reached on.
type ApplianceVpnUplink struct {
	Interface string `json:"interface,omitempty"`
	PublicIP  string `json:"publicIp,omitempty"`
}

// ApplianceVpnExportedSubnet is one LAN subnet this network contributes to the
// AutoVPN mesh. `usedInVpn` is Meraki's opt-in flag.
type ApplianceVpnExportedSubnet struct {
	Subnet    string `json:"subnet"`
	UsedInVpn bool   `json:"usedInVpn"`
}

// ApplianceVpnMerakiPeer is one AutoVPN peer (another Meraki network).
type ApplianceVpnMerakiPeer struct {
	NetworkID    string                     `json:"networkId"`
	NetworkName  string                     `json:"networkName,omitempty"`
	Reachability string                     `json:"reachability,omitempty"`
	UsageSummary *ApplianceVpnUsageSummary  `json:"usageSummary,omitempty"`
}

// ApplianceVpnUsageSummary is the cumulative usage counter attached to each
// meraki peer.
type ApplianceVpnUsageSummary struct {
	SentKilobytes     int64 `json:"sentKilobytes"`
	ReceivedKilobytes int64 `json:"receivedKilobytes"`
}

// ApplianceVpnThirdPartyPeer is one non-Meraki IPsec peer. Unlike meraki
// peers, thirdParty peers have no network id — only a name + publicIp.
type ApplianceVpnThirdPartyPeer struct {
	Name         string `json:"name,omitempty"`
	PublicIP     string `json:"publicIp,omitempty"`
	Reachability string `json:"reachability,omitempty"`
}

// ApplianceVpnStatusOptions filters the paged vpn/statuses feed.
type ApplianceVpnStatusOptions struct {
	NetworkIDs []string
}

func (o ApplianceVpnStatusOptions) values() url.Values {
	// perPage 300 is the endpoint maximum per v1 OpenAPI.
	v := url.Values{"perPage": []string{"300"}}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	return v
}

// GetOrganizationApplianceVpnStatuses returns every VPN-enabled network in
// the org with its peer list. Paginated via Link header — c.GetAll follows.
func (c *Client) GetOrganizationApplianceVpnStatuses(ctx context.Context, orgID string, opts ApplianceVpnStatusOptions, ttl time.Duration) ([]ApplianceVpnStatus, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/appliance/vpn/statuses", Message: "missing organization id"}}
	}
	var out []ApplianceVpnStatus
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/appliance/vpn/statuses",
		orgID, opts.values(), ttl, &out)
	return out, err
}

// ApplianceVpnStatsEntry is one row from
// `GET /organizations/{orgId}/appliance/vpn/stats`. Each entry is a single
// network and holds a list of peers, each of which carries four per-uplink-pair
// summary arrays (latency, jitter, loss, mos). The handler merges the four
// summary arrays by (senderUplink, receiverUplink) key so one row represents
// one peer-pair.
type ApplianceVpnStatsEntry struct {
	NetworkID      string                       `json:"networkId"`
	NetworkName    string                       `json:"networkName,omitempty"`
	MerakiVpnPeers []ApplianceVpnStatsMerakiPeer `json:"merakiVpnPeers,omitempty"`
}

// ApplianceVpnStatsMerakiPeer is one peer's aggregated stats within a
// ApplianceVpnStatsEntry. The four *Summaries arrays each key on
// (senderUplink, receiverUplink) so we merge them client-side.
type ApplianceVpnStatsMerakiPeer struct {
	NetworkID               string                            `json:"networkId"`
	NetworkName             string                            `json:"networkName,omitempty"`
	UsageSummary            *ApplianceVpnUsageSummary         `json:"usageSummary,omitempty"`
	JitterSummaries         []ApplianceVpnJitterSummary       `json:"jitterSummaries,omitempty"`
	LatencySummaries        []ApplianceVpnLatencySummary      `json:"latencySummaries,omitempty"`
	LossPercentageSummaries []ApplianceVpnLossSummary         `json:"lossPercentageSummaries,omitempty"`
	MosSummaries            []ApplianceVpnMosSummary          `json:"mosSummaries,omitempty"`
}

// ApplianceVpnJitterSummary is one (senderUplink, receiverUplink) jitter row.
type ApplianceVpnJitterSummary struct {
	SenderUplink   string  `json:"senderUplink"`
	ReceiverUplink string  `json:"receiverUplink"`
	AvgJitter      float64 `json:"avgJitter"`
}

// ApplianceVpnLatencySummary is one (senderUplink, receiverUplink) latency row.
type ApplianceVpnLatencySummary struct {
	SenderUplink   string  `json:"senderUplink"`
	ReceiverUplink string  `json:"receiverUplink"`
	AvgLatencyMs   float64 `json:"avgLatencyMs"`
}

// ApplianceVpnLossSummary is one (senderUplink, receiverUplink) packet-loss
// row. Values are percentages.
type ApplianceVpnLossSummary struct {
	SenderUplink      string  `json:"senderUplink"`
	ReceiverUplink    string  `json:"receiverUplink"`
	AvgLossPercentage float64 `json:"avgLossPercentage"`
}

// ApplianceVpnMosSummary is one (senderUplink, receiverUplink) MOS score row.
type ApplianceVpnMosSummary struct {
	SenderUplink   string  `json:"senderUplink"`
	ReceiverUplink string  `json:"receiverUplink"`
	AvgMos         float64 `json:"avgMos"`
}

// ApplianceVpnStatsOptions filters the paged vpn/stats feed. Window is
// preferred over Timespan when set — identical shape to other timeseries
// option structs. perPage 300 is the endpoint max.
type ApplianceVpnStatsOptions struct {
	NetworkIDs []string
	Window     *TimeRangeWindow
	Timespan   time.Duration
}

func (o ApplianceVpnStatsOptions) values() url.Values {
	v := url.Values{"perPage": []string{"300"}}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
	}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	return v
}

// GetOrganizationApplianceVpnStats returns per-peer-pair latency/jitter/loss/mos
// summaries over the window. Paginated via Link header.
func (c *Client) GetOrganizationApplianceVpnStats(ctx context.Context, orgID string, opts ApplianceVpnStatsOptions, ttl time.Duration) ([]ApplianceVpnStatsEntry, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/appliance/vpn/stats", Message: "missing organization id"}}
	}
	var out []ApplianceVpnStatsEntry
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/appliance/vpn/stats",
		orgID, opts.values(), ttl, &out)
	return out, err
}

// DeviceUplinkLossLatency is one row from
// `GET /organizations/{orgId}/devices/uplinksLossAndLatency`. Each entry is
// one (serial, uplink, ip) combo with a nested timeSeries array.
type DeviceUplinkLossLatency struct {
	Serial     string                          `json:"serial"`
	Uplink     string                          `json:"uplink"`
	IP         string                          `json:"ip"`
	TimeSeries []DeviceUplinkLossLatencyPoint  `json:"timeSeries"`
}

// DeviceUplinkLossLatencyPoint is one interval sample. loss/latency can be
// null when the probe failed — we decode as *float64 so the handler can emit
// gap-preserving nullable series (Grafana renders nil as a gap, not zero).
type DeviceUplinkLossLatencyPoint struct {
	Ts          time.Time `json:"ts"`
	LossPercent *float64  `json:"lossPercent"`
	LatencyMs   *float64  `json:"latencyMs"`
}

// UplinkLossLatencyOptions filters the devices/uplinksLossAndLatency call.
// The Meraki endpoint enforces a MaxTimespan of 5 minutes; longer windows 400.
// Optional Uplink and IP narrow to a single probe. There is no serial filter
// on this endpoint — callers must filter client-side.
type UplinkLossLatencyOptions struct {
	Uplink   string
	IP       string
	Window   *TimeRangeWindow
	Timespan time.Duration
}

func (o UplinkLossLatencyOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
	}
	if o.Uplink != "" {
		v.Set("uplink", o.Uplink)
	}
	if o.IP != "" {
		v.Set("ip", o.IP)
	}
	return v
}

// GetOrganizationDevicesUplinksLossAndLatency returns per-(serial,uplink,ip)
// timeSeries rows. Not paginated — the endpoint returns a single array.
func (c *Client) GetOrganizationDevicesUplinksLossAndLatency(ctx context.Context, orgID string, opts UplinkLossLatencyOptions, ttl time.Duration) ([]DeviceUplinkLossLatency, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/devices/uplinksLossAndLatency", Message: "missing organization id"}}
	}
	var out []DeviceUplinkLossLatency
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/devices/uplinksLossAndLatency",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ApplianceForwardingRule mirrors one rule in the response of
// `GET /networks/{networkId}/appliance/firewall/portForwardingRules`.
// Meraki wraps the array in a `{"rules":[...]}` envelope we strip in the wrapper.
type ApplianceForwardingRule struct {
	Name        string   `json:"name,omitempty"`
	Protocol    string   `json:"protocol,omitempty"`
	PublicPort  string   `json:"publicPort,omitempty"`
	LocalPort   string   `json:"localPort,omitempty"`
	LanIP       string   `json:"lanIp,omitempty"`
	Uplink      string   `json:"uplink,omitempty"`
	AllowedIPs  []string `json:"allowedIps,omitempty"`
}

// applianceForwardingRulesEnvelope is the wire wrapper shape.
type applianceForwardingRulesEnvelope struct {
	Rules []ApplianceForwardingRule `json:"rules"`
}

// GetNetworkAppliancePortForwardingRules unwraps the `{rules:[...]}` envelope
// and returns the rules slice.
func (c *Client) GetNetworkAppliancePortForwardingRules(ctx context.Context, networkID string, ttl time.Duration) ([]ApplianceForwardingRule, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/appliance/firewall/portForwardingRules", Message: "missing network id"}}
	}
	var env applianceForwardingRulesEnvelope
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/appliance/firewall/portForwardingRules",
		"", nil, ttl, &env); err != nil {
		return nil, err
	}
	return env.Rules, nil
}

// ApplianceSettings is the response shape of
// `GET /networks/{networkId}/appliance/settings`.
type ApplianceSettings struct {
	ClientTrackingMethod string                        `json:"clientTrackingMethod,omitempty"`
	DeploymentMode       string                        `json:"deploymentMode,omitempty"`
	DynamicDns           *ApplianceSettingsDynamicDns  `json:"dynamicDns,omitempty"`
}

// ApplianceSettingsDynamicDns is the nested dynamic DNS config block.
type ApplianceSettingsDynamicDns struct {
	Enabled bool   `json:"enabled,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	URL     string `json:"url,omitempty"`
}

// GetNetworkApplianceSettings returns the per-network appliance config snapshot.
func (c *Client) GetNetworkApplianceSettings(ctx context.Context, networkID string, ttl time.Duration) (*ApplianceSettings, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/appliance/settings", Message: "missing network id"}}
	}
	var out ApplianceSettings
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/appliance/settings",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

