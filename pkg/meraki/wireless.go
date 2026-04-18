package meraki

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// WirelessChannelUtilPoint is one interval sample from
// `GET /organizations/{organizationId}/wireless/devices/channelUtilization/history/byDevice/byInterval`.
//
// Meraki returns one object per (device, interval) with a nested `byBand` array — we flatten
// that here so each row is one (serial, band, interval) triple, which is what the downstream
// timeseries frame emitters want anyway.
type WirelessChannelUtilPoint struct {
	Serial             string
	MAC                string
	NetworkID          string
	Band               string // "2.4", "5", "6"
	StartTs            time.Time
	EndTs              time.Time
	Utilization        float64 // total percentage
	WifiUtilization    float64 // wifi percentage
	NonWifiUtilization float64 // non-wifi percentage
}

// rawChannelUtilEntry matches the wire JSON shape so we can flatten it into WirelessChannelUtilPoint.
type rawChannelUtilEntry struct {
	StartTs time.Time                `json:"startTs"`
	EndTs   time.Time                `json:"endTs"`
	Serial  string                   `json:"serial"`
	MAC     string                   `json:"mac"`
	Network struct {
		ID string `json:"id"`
	} `json:"network"`
	ByBand []rawChannelUtilByBand `json:"byBand"`
}

type rawChannelUtilByBand struct {
	Band    string `json:"band"`
	Wifi    struct {
		Percentage float64 `json:"percentage"`
	} `json:"wifi"`
	NonWifi struct {
		Percentage float64 `json:"percentage"`
	} `json:"nonWifi"`
	Total struct {
		Percentage float64 `json:"percentage"`
	} `json:"total"`
}

// WirelessChannelUtilOptions filters the channel-utilization-history call.
// Bands is applied client-side because the Meraki endpoint does not accept a bands filter;
// limiting here keeps the handler's emit loop symmetrical with other per-band panels.
type WirelessChannelUtilOptions struct {
	NetworkIDs []string
	Serials    []string
	Bands      []string
	Window     *TimeRangeWindow
	// Interval, if set, overrides Window.Resolution. Allowed values per the API: 300, 600,
	// 3600, 7200, 14400, 21600 seconds.
	Interval time.Duration
}

func (o WirelessChannelUtilOptions) values() url.Values {
	v := url.Values{"perPage": []string{"1000"}}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
		if o.Interval > 0 {
			v.Set("interval", strconv.Itoa(int(o.Interval.Seconds())))
		} else if o.Window.Resolution > 0 {
			v.Set("interval", strconv.Itoa(int(o.Window.Resolution.Seconds())))
		}
	} else if o.Interval > 0 {
		v.Set("interval", strconv.Itoa(int(o.Interval.Seconds())))
	}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	return v
}

// GetOrganizationWirelessChannelUtilHistory fetches per-AP, per-band channel utilisation
// samples and flattens them into one entry per (serial, band, interval) triple.
//
// Endpoint path: /organizations/{organizationId}/wireless/devices/channelUtilization/history/byDevice/byInterval
// The `KnownEndpointRanges` table keys this as the shorter legacy path
// `organizations/{organizationId}/wireless/devices/channelUtilization/history` for resolver lookup.
func (c *Client) GetOrganizationWirelessChannelUtilHistory(ctx context.Context, orgID string, opts WirelessChannelUtilOptions, ttl time.Duration) ([]WirelessChannelUtilPoint, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/wireless/devices/channelUtilization/history/byDevice/byInterval", Message: "missing organization id"}}
	}
	var raw []rawChannelUtilEntry
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/wireless/devices/channelUtilization/history/byDevice/byInterval",
		orgID, opts.values(), ttl, &raw)
	if err != nil {
		return nil, err
	}

	// Normalise the Bands filter to a set for O(1) membership checks.
	var bandFilter map[string]struct{}
	if len(opts.Bands) > 0 {
		bandFilter = make(map[string]struct{}, len(opts.Bands))
		for _, b := range opts.Bands {
			bandFilter[b] = struct{}{}
		}
	}

	points := make([]WirelessChannelUtilPoint, 0, len(raw))
	for _, e := range raw {
		for _, b := range e.ByBand {
			if bandFilter != nil {
				if _, keep := bandFilter[b.Band]; !keep {
					continue
				}
			}
			points = append(points, WirelessChannelUtilPoint{
				Serial:             e.Serial,
				MAC:                e.MAC,
				NetworkID:          e.Network.ID,
				Band:               b.Band,
				StartTs:            e.StartTs,
				EndTs:              e.EndTs,
				Utilization:        b.Total.Percentage,
				WifiUtilization:    b.Wifi.Percentage,
				NonWifiUtilization: b.NonWifi.Percentage,
			})
		}
	}
	return points, nil
}

// WirelessUsagePoint is one interval sample from `GET /networks/{networkId}/wireless/usageHistory`.
// The API returns sent+received separately (sentKbps, receivedKbps) plus a combined totalKbps;
// we preserve all three so the caller can choose which to plot.
type WirelessUsagePoint struct {
	StartTs        time.Time `json:"startTs"`
	EndTs          time.Time `json:"endTs"`
	TotalKbps      float64   `json:"totalKbps"`
	SentKbps       float64   `json:"sentKbps"`
	ReceivedKbps   float64   `json:"receivedKbps"`
}

// WirelessUsageOptions filters the network wireless usage-history call.
// The endpoint supports an SSID filter (by number) and an optional resolution.
type WirelessUsageOptions struct {
	SSID       string
	Band       string
	Window     *TimeRangeWindow
	Resolution time.Duration
}

func (o WirelessUsageOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
		if o.Resolution > 0 {
			v.Set("resolution", strconv.Itoa(int(o.Resolution.Seconds())))
		} else if o.Window.Resolution > 0 {
			v.Set("resolution", strconv.Itoa(int(o.Window.Resolution.Seconds())))
		}
	} else if o.Resolution > 0 {
		v.Set("resolution", strconv.Itoa(int(o.Resolution.Seconds())))
	}
	if o.SSID != "" {
		v.Set("ssid", o.SSID)
	}
	if o.Band != "" {
		v.Set("band", o.Band)
	}
	return v
}

// GetNetworkWirelessUsageHistory fetches per-interval usage samples for a single network.
// Meraki returns the array un-paginated so we use c.Get rather than c.GetAll.
func (c *Client) GetNetworkWirelessUsageHistory(ctx context.Context, networkID string, opts WirelessUsageOptions, ttl time.Duration) ([]WirelessUsagePoint, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/usageHistory", Message: "missing network id"}}
	}
	var out []WirelessUsagePoint
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/wireless/usageHistory",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// NetworkSsid is one SSID configuration row from `GET /networks/{networkId}/wireless/ssids`.
// We only surface the subset used by the inventory table + config snapshot panel; additional
// fields (RADIUS servers, walled gardens, etc.) can be added as tests exercise them.
type NetworkSsid struct {
	Number         int    `json:"number"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	SplashPage     string `json:"splashPage,omitempty"`
	AuthMode       string `json:"authMode,omitempty"`
	EncryptionMode string `json:"encryptionMode,omitempty"`
	WpaEncryption  string `json:"wpaEncryptionMode,omitempty"`
	Visible        bool   `json:"visible,omitempty"`
	MinBitrate     int    `json:"minBitrate,omitempty"`
	BandSelection  string `json:"bandSelection,omitempty"`
	IpAssignment   string `json:"ipAssignmentMode,omitempty"`
}

// GetNetworkWirelessSsids fetches the SSID configuration snapshot for one network.
// The endpoint is not paginated — it returns all 15 SSIDs (enabled or not) in a single
// response — so we use c.Get.
func (c *Client) GetNetworkWirelessSsids(ctx context.Context, networkID string, ttl time.Duration) ([]NetworkSsid, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/ssids", Message: "missing network id"}}
	}
	var out []NetworkSsid
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/wireless/ssids",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// WirelessClient represents one client entry from `GET /devices/{serial}/clients`.
//
// Note: the Meraki v1 API does NOT expose RSSI, signal strength, or SSID on this endpoint.
// For RF-level metrics the caller needs `/networks/{networkId}/wireless/signalQualityHistory`,
// which is a separate timeseries. What /devices/{serial}/clients gives us is the list of
// currently-associated clients plus identifying metadata and usage counters in KB.
type WirelessClient struct {
	ID             string `json:"id,omitempty"`
	MAC            string `json:"mac"`
	IP             string `json:"ip,omitempty"`
	Description    string `json:"description,omitempty"`
	User           string `json:"user,omitempty"`
	VLAN           string `json:"vlan,omitempty"`
	NamedVLAN      string `json:"namedVlan,omitempty"`
	Switchport     string `json:"switchport,omitempty"`
	AdaptivePolicy string `json:"adaptivePolicyGroup,omitempty"`
	MdnsName       string `json:"mdnsName,omitempty"`
	DhcpHostname   string `json:"dhcpHostname,omitempty"`
	Usage          struct {
		Sent float64 `json:"sent"`
		Recv float64 `json:"recv"`
	} `json:"usage"`
}

// GetDeviceWirelessClients fetches the client list for one device (typically an AP).
// timespan is the lookback window in seconds; the endpoint caps it to 31 days. A zero
// timespan defers to the endpoint's default (1 day).
func (c *Client) GetDeviceWirelessClients(ctx context.Context, serial string, timespan time.Duration, ttl time.Duration) ([]WirelessClient, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/clients", Message: "missing device serial"}}
	}
	v := url.Values{}
	if timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(timespan.Seconds())))
	}
	var out []WirelessClient
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/clients",
		"", v, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — wireless client count history (per-network timeseries)
// ---------------------------------------------------------------------------

// WirelessClientCountPoint is one interval sample from
// GET /networks/{networkId}/wireless/clientCountHistory.
//
// Wire shape (per item):
//
//	{"startTs":"...","endTs":"...","clientCount": N}
//
// The same endpoint optionally filters by SSID; when an SSID filter is
// applied Meraki still returns the same flat shape so we do not model
// per-SSID partitioning at this layer.
type WirelessClientCountPoint struct {
	StartTs     time.Time `json:"startTs"`
	EndTs       time.Time `json:"endTs"`
	ClientCount int64     `json:"clientCount"`
}

// WirelessClientCountOptions filters the client-count-history call.
type WirelessClientCountOptions struct {
	SSID         string
	Band         string
	DeviceSerial string
	Window       *TimeRangeWindow
	Resolution   time.Duration
}

func (o WirelessClientCountOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
		if o.Resolution > 0 {
			v.Set("resolution", strconv.Itoa(int(o.Resolution.Seconds())))
		} else if o.Window.Resolution > 0 {
			v.Set("resolution", strconv.Itoa(int(o.Window.Resolution.Seconds())))
		}
	} else if o.Resolution > 0 {
		v.Set("resolution", strconv.Itoa(int(o.Resolution.Seconds())))
	}
	if o.SSID != "" {
		v.Set("ssid", o.SSID)
	}
	if o.Band != "" {
		v.Set("band", o.Band)
	}
	if o.DeviceSerial != "" {
		v.Set("deviceSerial", o.DeviceSerial)
	}
	return v
}

// GetNetworkWirelessClientCountHistory fetches per-interval client-count
// samples for a single network. Endpoint is not paginated.
func (c *Client) GetNetworkWirelessClientCountHistory(ctx context.Context, networkID string, opts WirelessClientCountOptions, ttl time.Duration) ([]WirelessClientCountPoint, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/clientCountHistory", Message: "missing network id"}}
	}
	var out []WirelessClientCountPoint
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/wireless/clientCountHistory",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — wireless failed connections (per-network event list)
// ---------------------------------------------------------------------------

// WirelessFailedConnection is one failure event row from
// GET /networks/{networkId}/wireless/failedConnections.
//
// Wire shape (per item):
//
//	{"ts":"...", "type":"assoc|auth|dhcp|dns", "serial":"Q2...",
//	 "clientMac":"...", "ssidNumber": N, "failureStep":"..." , "vlan":"...",
//	 "apTag":"...", "band":"2.4|5|6", "channel": N}
type WirelessFailedConnection struct {
	Ts          time.Time `json:"ts"`
	Type        string    `json:"type"`
	Serial      string    `json:"serial"`
	ClientMac   string    `json:"clientMac"`
	SsidNumber  int       `json:"ssidNumber"`
	FailureStep string    `json:"failureStep"`
	Vlan        string    `json:"vlan"`
	ApTag       string    `json:"apTag"`
	Band        string    `json:"band"`
	Channel     int       `json:"channel"`
}

// WirelessFailedConnectionsOptions filters the failed-connections call.
type WirelessFailedConnectionsOptions struct {
	SSID   string
	Band   string
	Serial string
	Window *TimeRangeWindow
}

func (o WirelessFailedConnectionsOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	if o.SSID != "" {
		v.Set("ssid", o.SSID)
	}
	if o.Band != "" {
		v.Set("band", o.Band)
	}
	if o.Serial != "" {
		v.Set("serial", o.Serial)
	}
	return v
}

// GetNetworkWirelessFailedConnections fetches the failed-connection events
// for a single network over the window. Endpoint is not paginated.
func (c *Client) GetNetworkWirelessFailedConnections(ctx context.Context, networkID string, opts WirelessFailedConnectionsOptions, ttl time.Duration) ([]WirelessFailedConnection, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/failedConnections", Message: "missing network id"}}
	}
	var out []WirelessFailedConnection
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/wireless/failedConnections",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — wireless latency history (per-network timeseries)
// ---------------------------------------------------------------------------

// WirelessLatencyPoint is one interval sample from
// GET /networks/{networkId}/wireless/latencyHistory.
//
// Wire shape (per item):
//
//	{"startTs":"...","endTs":"...","avgLatencyMs": F,
//	 "backgroundTrafficMs": F, "bestEffortTrafficMs": F,
//	 "videoTrafficMs": F, "voiceTrafficMs": F}
//
// Any of the per-access-category fields may be absent when the response
// does not include a breakdown (default: aggregate only).
type WirelessLatencyPoint struct {
	StartTs             time.Time `json:"startTs"`
	EndTs               time.Time `json:"endTs"`
	AvgLatencyMs        float64   `json:"avgLatencyMs"`
	BackgroundTrafficMs float64   `json:"backgroundTrafficMs"`
	BestEffortTrafficMs float64   `json:"bestEffortTrafficMs"`
	VideoTrafficMs      float64   `json:"videoTrafficMs"`
	VoiceTrafficMs      float64   `json:"voiceTrafficMs"`
}

// WirelessLatencyOptions filters the latency-history call.
type WirelessLatencyOptions struct {
	SSID           string
	Band           string
	DeviceSerial   string
	AccessCategory string
	Window         *TimeRangeWindow
	Resolution     time.Duration
}

func (o WirelessLatencyOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
		if o.Resolution > 0 {
			v.Set("resolution", strconv.Itoa(int(o.Resolution.Seconds())))
		} else if o.Window.Resolution > 0 {
			v.Set("resolution", strconv.Itoa(int(o.Window.Resolution.Seconds())))
		}
	} else if o.Resolution > 0 {
		v.Set("resolution", strconv.Itoa(int(o.Resolution.Seconds())))
	}
	if o.SSID != "" {
		v.Set("ssid", o.SSID)
	}
	if o.Band != "" {
		v.Set("band", o.Band)
	}
	if o.DeviceSerial != "" {
		v.Set("deviceSerial", o.DeviceSerial)
	}
	if o.AccessCategory != "" {
		v.Set("accessCategory", o.AccessCategory)
	}
	return v
}

// GetNetworkWirelessLatencyHistory fetches per-interval latency samples
// for a single network. Endpoint is not paginated.
func (c *Client) GetNetworkWirelessLatencyHistory(ctx context.Context, networkID string, opts WirelessLatencyOptions, ttl time.Duration) ([]WirelessLatencyPoint, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/wireless/latencyHistory", Message: "missing network id"}}
	}
	var out []WirelessLatencyPoint
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/wireless/latencyHistory",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — wireless SSID statuses by device (org-wide radio status proxy)
// ---------------------------------------------------------------------------

// WirelessSsidStatusByDevice is one BSSID row from
// GET /organizations/{organizationId}/wireless/ssids/statuses/byDevice.
//
// Wire shape (per item):
//
//	{
//	  "network": {"id": "N_..."},
//	  "serial": "Q2...",
//	  "basicServiceSets": [
//	    {"ssid": {"number": N, "name": "..."},
//	     "radio": {"band": "2.4|5|6", "channel": N, "channelWidth": N, "isBroadcasting": bool},
//	     "bssid": "...", "visible": bool, "enabled": bool}
//	  ]
//	}
//
// We use this in lieu of a true org-wide radioSettings/bySsid endpoint (which
// Meraki does not expose). For each device we aggregate which bands are
// currently broadcasting to yield {serial, band2_4, band5, band6, enabled}.
type WirelessSsidStatusByDevice struct {
	Serial    string
	NetworkID string
	// Band24Active is true when at least one BSSID on this device reports a
	// 2.4 GHz radio with isBroadcasting=true (and the SSID itself is enabled).
	Band24Active bool
	Band5Active  bool
	Band6Active  bool
	// AnyEnabled mirrors the plan's "enabled" column — true when any BSSID
	// on the device is enabled.
	AnyEnabled bool
}

// WirelessSsidStatusOptions filters the ssids/statuses/byDevice call.
type WirelessSsidStatusOptions struct {
	NetworkIDs []string
	Serials    []string
}

type rawBasicServiceSet struct {
	Enabled bool `json:"enabled"`
	Radio   struct {
		Band           string `json:"band"`
		IsBroadcasting bool   `json:"isBroadcasting"`
	} `json:"radio"`
}

type rawSsidStatusEntry struct {
	Serial  string `json:"serial"`
	Network struct {
		ID string `json:"id"`
	} `json:"network"`
	BasicServiceSets []rawBasicServiceSet `json:"basicServiceSets"`
}

// GetOrganizationWirelessSsidsStatusesByDevice returns per-device BSSID
// broadcasting snapshot. Paginated via Link header; perPage 500 (max).
func (c *Client) GetOrganizationWirelessSsidsStatusesByDevice(ctx context.Context, orgID string, opts WirelessSsidStatusOptions, ttl time.Duration) ([]WirelessSsidStatusByDevice, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{
			Endpoint: "organizations/{organizationId}/wireless/ssids/statuses/byDevice",
			Message:  "missing organization id",
		}}
	}
	v := url.Values{"perPage": []string{"500"}}
	for _, id := range opts.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range opts.Serials {
		v.Add("serials[]", s)
	}
	var raw []rawSsidStatusEntry
	if _, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/wireless/ssids/statuses/byDevice",
		orgID, v, ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]WirelessSsidStatusByDevice, 0, len(raw))
	for _, e := range raw {
		row := WirelessSsidStatusByDevice{Serial: e.Serial, NetworkID: e.Network.ID}
		for _, b := range e.BasicServiceSets {
			if b.Enabled {
				row.AnyEnabled = true
			}
			if !b.Radio.IsBroadcasting {
				continue
			}
			switch b.Radio.Band {
			case "2.4":
				row.Band24Active = true
			case "5":
				row.Band5Active = true
			case "6":
				row.Band6Active = true
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §2.1 — Org-level AP client counts
// ---------------------------------------------------------------------------

// WirelessApClientCounts holds the per-device online client count from
// GET /organizations/{organizationId}/wireless/clients/overview/byDevice.
//
// Wire shape (per item):
//   {"network":{"id":"N_..."}, "serial":"Q2...", "counts":{"byStatus":{"online":N}}}
type WirelessApClientCounts struct {
	Serial      string
	NetworkID   string
	OnlineCount int64
}

// WirelessApClientCountsOptions filters the byDevice client-count call.
type WirelessApClientCountsOptions struct {
	NetworkIDs []string
	Serials    []string
}

// rawApClientCountEntry matches the wire JSON for a single device item.
type rawApClientCountEntry struct {
	Serial  string `json:"serial"`
	Network struct {
		ID string `json:"id"`
	} `json:"network"`
	Counts struct {
		ByStatus struct {
			Online int64 `json:"online"`
		} `json:"byStatus"`
	} `json:"counts"`
}

// GetOrganizationWirelessClientsOverviewByDevice returns per-AP online client
// counts for the organization. Paginated via Link header; perPage 1000.
func (c *Client) GetOrganizationWirelessClientsOverviewByDevice(ctx context.Context, orgID string, opts WirelessApClientCountsOptions, ttl time.Duration) ([]WirelessApClientCounts, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{
			Endpoint: "organizations/{organizationId}/wireless/clients/overview/byDevice",
			Message:  "missing organization id",
		}}
	}
	v := url.Values{"perPage": []string{"1000"}}
	for _, id := range opts.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range opts.Serials {
		v.Add("serials[]", s)
	}
	var raw []rawApClientCountEntry
	if _, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/wireless/clients/overview/byDevice",
		orgID, v, ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]WirelessApClientCounts, 0, len(raw))
	for _, e := range raw {
		out = append(out, WirelessApClientCounts{
			Serial:      e.Serial,
			NetworkID:   e.Network.ID,
			OnlineCount: e.Counts.ByStatus.Online,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless packet loss, ethernet statuses, CPU load history
// ---------------------------------------------------------------------------

// WirelessPacketLossByNetwork holds aggregate packet-loss metrics per network from
// GET /organizations/{organizationId}/wireless/devices/packetLoss/byNetwork.
//
// Wire shape (per item):
//
//	{
//	  "network": {"id": "N_..."},
//	  "downstream": {"total": N, "lost": N, "lossPercentage": F},
//	  "upstream":   {"total": N, "lost": N, "lossPercentage": F},
//	  "total":      {"total": N, "lost": N, "lossPercentage": F}
//	}
type WirelessPacketLossByNetwork struct {
	NetworkID  string
	Downstream *WirelessPacketLossSample
	Upstream   *WirelessPacketLossSample
	Total      *WirelessPacketLossSample
}

// WirelessPacketLossSample is a single directional packet-loss aggregate.
type WirelessPacketLossSample struct {
	TotalPackets int64
	LostPackets  int64
	LossPercent  float64
}

// WirelessPacketLossOptions filters the packet-loss byNetwork call.
type WirelessPacketLossOptions struct {
	NetworkIDs []string
	Serials    []string
	Bands      []string
	Window     *TimeRangeWindow
}

type rawPacketLossSample struct {
	Total          int64   `json:"total"`
	Lost           int64   `json:"lost"`
	LossPercentage float64 `json:"lossPercentage"`
}

type rawPacketLossEntry struct {
	Network struct {
		ID string `json:"id"`
	} `json:"network"`
	Downstream *rawPacketLossSample `json:"downstream"`
	Upstream   *rawPacketLossSample `json:"upstream"`
	Total      *rawPacketLossSample `json:"total"`
}

// GetOrganizationWirelessPacketLossByNetwork returns per-network wireless
// packet-loss aggregates. Paginated via Link header; perPage 1000.
// MaxTimespan 90 days; no resolution parameter.
func (c *Client) GetOrganizationWirelessPacketLossByNetwork(ctx context.Context, orgID string, opts WirelessPacketLossOptions, ttl time.Duration) ([]WirelessPacketLossByNetwork, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{
			Endpoint: "organizations/{organizationId}/wireless/devices/packetLoss/byNetwork",
			Message:  "missing organization id",
		}}
	}
	v := url.Values{"perPage": []string{"1000"}}
	if opts.Window != nil {
		v.Set("t0", opts.Window.T0.UTC().Format("2006-01-02T15:04:05Z"))
		v.Set("t1", opts.Window.T1.UTC().Format("2006-01-02T15:04:05Z"))
	}
	for _, id := range opts.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range opts.Serials {
		v.Add("serials[]", s)
	}
	for _, b := range opts.Bands {
		v.Add("bands[]", b)
	}
	var raw []rawPacketLossEntry
	if _, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/wireless/devices/packetLoss/byNetwork",
		orgID, v, ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]WirelessPacketLossByNetwork, 0, len(raw))
	for _, e := range raw {
		row := WirelessPacketLossByNetwork{NetworkID: e.Network.ID}
		if e.Downstream != nil {
			row.Downstream = &WirelessPacketLossSample{
				TotalPackets: e.Downstream.Total,
				LostPackets:  e.Downstream.Lost,
				LossPercent:  e.Downstream.LossPercentage,
			}
		}
		if e.Upstream != nil {
			row.Upstream = &WirelessPacketLossSample{
				TotalPackets: e.Upstream.Total,
				LostPackets:  e.Upstream.Lost,
				LossPercent:  e.Upstream.LossPercentage,
			}
		}
		if e.Total != nil {
			row.Total = &WirelessPacketLossSample{
				TotalPackets: e.Total.Total,
				LostPackets:  e.Total.Lost,
				LossPercent:  e.Total.LossPercentage,
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// WirelessDeviceEthernetStatus holds the ethernet/power status for one AP from
// GET /organizations/{organizationId}/wireless/devices/ethernet/statuses.
//
// Wire shape (per item):
//
//	{
//	  "serial": "Q2...",
//	  "name": "...",
//	  "network": {"id": "N_..."},
//	  "model": "MR...",
//	  "power": {"ac": {"isConnected": bool}, "poe": {"isConnected": bool, "maximum": F}},
//	  "ports": [{"name": "LAN", "linkNegotiationCapability": [...], "poe": {"isConnected": bool}, "linkNeg": {...}, "speed": "1000Mbps", "duplex": "full", "enabled": bool}]
//	}
//
// We simplify to primary/secondary ports (first two LAN ports by index).
type WirelessDeviceEthernetStatus struct {
	Serial    string
	Name      string
	NetworkID string
	Model     string
	Power     string // "ac" | "poe" | "unknown"
	Primary   WirelessEthernetPort
	Secondary *WirelessEthernetPort
}

// WirelessEthernetPort describes one physical ethernet port on an AP.
type WirelessEthernetPort struct {
	Name   string
	Speed  string // e.g. "1000Mbps", "10Gbps"
	Duplex string // "full" | "half" | ""
	PoeEnabled bool
}

// WirelessDeviceEthernetOptions filters the ethernet-statuses call.
type WirelessDeviceEthernetOptions struct {
	NetworkIDs []string
	Serials    []string
}

type rawEthernetPort struct {
	Name   string `json:"name"`
	Speed  string `json:"speed,omitempty"`
	Duplex string `json:"duplex,omitempty"`
	Poe    struct {
		IsConnected bool `json:"isConnected"`
	} `json:"poe"`
}

type rawEthernetEntry struct {
	Serial  string `json:"serial"`
	Name    string `json:"name,omitempty"`
	Network struct {
		ID string `json:"id"`
	} `json:"network"`
	Model string `json:"model,omitempty"`
	Power struct {
		Ac struct {
			IsConnected bool `json:"isConnected"`
		} `json:"ac"`
		Poe struct {
			IsConnected bool    `json:"isConnected"`
			Maximum     float64 `json:"maximum,omitempty"`
		} `json:"poe"`
	} `json:"power"`
	Ports []rawEthernetPort `json:"ports"`
}

// GetOrganizationWirelessDevicesEthernetStatuses returns the ethernet/power
// snapshot for every wireless device in the org. Paginated via Link header;
// perPage 1000. Snapshot endpoint — no time parameters.
func (c *Client) GetOrganizationWirelessDevicesEthernetStatuses(ctx context.Context, orgID string, opts WirelessDeviceEthernetOptions, ttl time.Duration) ([]WirelessDeviceEthernetStatus, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{
			Endpoint: "organizations/{organizationId}/wireless/devices/ethernet/statuses",
			Message:  "missing organization id",
		}}
	}
	v := url.Values{"perPage": []string{"1000"}}
	for _, id := range opts.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range opts.Serials {
		v.Add("serials[]", s)
	}
	var raw []rawEthernetEntry
	if _, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/wireless/devices/ethernet/statuses",
		orgID, v, ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]WirelessDeviceEthernetStatus, 0, len(raw))
	for _, e := range raw {
		row := WirelessDeviceEthernetStatus{
			Serial:    e.Serial,
			Name:      e.Name,
			NetworkID: e.Network.ID,
			Model:     e.Model,
		}
		// Determine power source: prefer AC, then PoE.
		switch {
		case e.Power.Ac.IsConnected:
			row.Power = "ac"
		case e.Power.Poe.IsConnected:
			row.Power = "poe"
		default:
			row.Power = "unknown"
		}
		// Map ports: first port → Primary, second → Secondary.
		if len(e.Ports) > 0 {
			p := e.Ports[0]
			row.Primary = WirelessEthernetPort{
				Name:       p.Name,
				Speed:      p.Speed,
				Duplex:     p.Duplex,
				PoeEnabled: p.Poe.IsConnected,
			}
		}
		if len(e.Ports) > 1 {
			p := e.Ports[1]
			secondary := WirelessEthernetPort{
				Name:       p.Name,
				Speed:      p.Speed,
				Duplex:     p.Duplex,
				PoeEnabled: p.Poe.IsConnected,
			}
			row.Secondary = &secondary
		}
		out = append(out, row)
	}
	return out, nil
}

// WirelessCpuLoadPoint is one interval sample from
// GET /organizations/{organizationId}/wireless/devices/system/cpu/load/history.
//
// Wire shape (per item):
//
//	{
//	  "serial": "Q2...",
//	  "network": {"id": "N_..."},
//	  "history": [{"startTs": "...", "endTs": "...", "util": {"average": {"percentage": F}}}]
//	}
//
// Flattened to one row per (serial, interval).
type WirelessCpuLoadPoint struct {
	StartTs   time.Time
	EndTs     time.Time
	Serial    string
	NetworkID string
	CpuLoad5  float64 // 5-min average CPU load % (util.average.percentage)
}

// WirelessCpuLoadOptions filters the CPU-load history call.
type WirelessCpuLoadOptions struct {
	NetworkIDs []string
	Serials    []string
	Window     *TimeRangeWindow
	Interval   time.Duration
}

type rawCpuInterval struct {
	StartTs time.Time `json:"startTs"`
	EndTs   time.Time `json:"endTs"`
	Util    struct {
		Average struct {
			Percentage float64 `json:"percentage"`
		} `json:"average"`
	} `json:"util"`
}

type rawCpuLoadEntry struct {
	Serial  string `json:"serial"`
	Network struct {
		ID string `json:"id"`
	} `json:"network"`
	History []rawCpuInterval `json:"history"`
}

// GetOrganizationWirelessDevicesCpuLoadHistory returns per-device CPU-load
// history samples flattened to one (serial, interval) per row.
// Paginated via Link header; perPage 1000. MaxTimespan 1 day per docs.
func (c *Client) GetOrganizationWirelessDevicesCpuLoadHistory(ctx context.Context, orgID string, opts WirelessCpuLoadOptions, ttl time.Duration) ([]WirelessCpuLoadPoint, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{
			Endpoint: "organizations/{organizationId}/wireless/devices/system/cpu/load/history",
			Message:  "missing organization id",
		}}
	}
	v := url.Values{"perPage": []string{"1000"}}
	if opts.Window != nil {
		v.Set("t0", opts.Window.T0.UTC().Format("2006-01-02T15:04:05Z"))
		v.Set("t1", opts.Window.T1.UTC().Format("2006-01-02T15:04:05Z"))
		if opts.Interval > 0 {
			v.Set("interval", strconv.Itoa(int(opts.Interval.Seconds())))
		} else if opts.Window.Resolution > 0 {
			v.Set("interval", strconv.Itoa(int(opts.Window.Resolution.Seconds())))
		}
	} else if opts.Interval > 0 {
		v.Set("interval", strconv.Itoa(int(opts.Interval.Seconds())))
	}
	for _, id := range opts.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range opts.Serials {
		v.Add("serials[]", s)
	}
	var raw []rawCpuLoadEntry
	if _, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/wireless/devices/system/cpu/load/history",
		orgID, v, ttl, &raw); err != nil {
		return nil, err
	}
	var out []WirelessCpuLoadPoint
	for _, e := range raw {
		for _, h := range e.History {
			out = append(out, WirelessCpuLoadPoint{
				StartTs:   h.StartTs,
				EndTs:     h.EndTs,
				Serial:    e.Serial,
				NetworkID: e.Network.ID,
				CpuLoad5:  h.Util.Average.Percentage,
			})
		}
	}
	return out, nil
}
