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
