package meraki

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Phase 11 (Network events) endpoint wrapper. Path, pagination mode, and
// response shape verified via ctx7 against the canonical
// /openapi/api_meraki_api_v1_openapispec dataset on 2026-04-17.
//
// Coverage:
//   - /networks/{networkId}/events (cursor-paginated via startingAfter)

// NetworkEvent is one entry in the response of
// `GET /networks/{networkId}/events`. `EventData` is a free-form map because
// Meraki's per-event data carries completely different keys by productType
// (wireless events have `ssid`/`client`, switch events have `port`, etc.).
type NetworkEvent struct {
	OccurredAt        *time.Time     `json:"occurredAt,omitempty"`
	NetworkID         string         `json:"networkId,omitempty"`
	Type              string         `json:"type,omitempty"`
	Description       string         `json:"description,omitempty"`
	Category          string         `json:"category,omitempty"`
	ClientID          string         `json:"clientId,omitempty"`
	ClientMac         string         `json:"clientMac,omitempty"`
	ClientDescription string         `json:"clientDescription,omitempty"`
	DeviceSerial      string         `json:"deviceSerial,omitempty"`
	DeviceName        string         `json:"deviceName,omitempty"`
	SsidNumber        *int           `json:"ssidNumber,omitempty"`
	// ProductType is populated by some event categories (wireless, switch,
	// etc.) but not all. The handler falls back to q.ProductTypes[0] when the
	// event's own value is empty so drilldown URLs still work.
	ProductType string         `json:"productType,omitempty"`
	EventData   map[string]any `json:"eventData,omitempty"`
}

// NetworkEventsPage is one page of the cursor-paginated events feed. Meraki
// returns `pageStartAt` / `pageEndAt` ISO8601 markers we use to advance the
// `startingAfter` cursor on the next request.
type NetworkEventsPage struct {
	Events      []NetworkEvent `json:"events"`
	PageStartAt *time.Time     `json:"pageStartAt,omitempty"`
	PageEndAt   *time.Time     `json:"pageEndAt,omitempty"`
}

// NetworkEventsOptions filters the /networks/{id}/events call. productType is
// REQUIRED when the network has multiple device types (mixed MX+MS+MR etc.);
// for single-product networks the endpoint accepts requests without one. The
// wrapper does NOT enforce that — callers handle the 400 from upstream.
type NetworkEventsOptions struct {
	ProductType        string
	IncludedEventTypes []string
	ExcludedEventTypes []string
	DeviceMac          string
	DeviceSerial       string
	DeviceName         string
	ClientIP           string
	ClientMac          string
	ClientName         string
	PerPage            int
	// StartingAfter is used internally by the wrapper's pagination loop; the
	// first call passes whatever the caller supplied (typically "").
	StartingAfter string
	// TSStart/TSEnd narrow to events at or after/before the given times.
	// Meraki accepts these via the startingBefore/startingAfter alternatives
	// but we keep a simple tsStart/tsEnd pair for parity with other options
	// structs. When empty the endpoint defaults to a 7-day window.
	TSStart *time.Time
	TSEnd   *time.Time
}

func (o NetworkEventsOptions) values() url.Values {
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
	if o.ProductType != "" {
		v.Set("productType", o.ProductType)
	}
	for _, t := range o.IncludedEventTypes {
		v.Add("includedEventTypes[]", t)
	}
	for _, t := range o.ExcludedEventTypes {
		v.Add("excludedEventTypes[]", t)
	}
	if o.DeviceMac != "" {
		v.Set("deviceMac", o.DeviceMac)
	}
	if o.DeviceSerial != "" {
		v.Set("deviceSerial", o.DeviceSerial)
	}
	if o.DeviceName != "" {
		v.Set("deviceName", o.DeviceName)
	}
	if o.ClientIP != "" {
		v.Set("clientIp", o.ClientIP)
	}
	if o.ClientMac != "" {
		v.Set("clientMac", o.ClientMac)
	}
	if o.ClientName != "" {
		v.Set("clientName", o.ClientName)
	}
	if o.StartingAfter != "" {
		v.Set("startingAfter", o.StartingAfter)
	}
	return v
}

// maxNetworkEventsPages bounds the pagination loop so a misbehaving backend
// can't stall the request indefinitely. 20 pages × 1000 events per page
// = 20 000 events, well above what any single panel refresh should return.
const maxNetworkEventsPages = 20

// GetNetworkEvents concatenates up to maxNetworkEventsPages of the
// startingAfter-paginated events feed. Pagination is NOT Link-header based
// here — Meraki's /events endpoint is one of the few v1 endpoints that only
// exposes cursor pagination via pageStartAt/pageEndAt. The loop breaks when:
//
//   - the page is shorter than perPage (no more data)
//   - pageEndAt is nil (server withheld the cursor, nothing to advance on)
//   - pageEndAt didn't advance vs the previous iteration (cycle-safe)
//
// Caching uses the caller's initial opts (before any cursor is set) so a
// repeat panel refresh inside TTL short-circuits the full walk.
func (c *Client) GetNetworkEvents(ctx context.Context, networkID string, opts NetworkEventsOptions, ttl time.Duration) ([]NetworkEvent, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/events", Message: "missing network id"}}
	}

	// Resolve the perPage we'll compare against when deciding whether a page
	// is a terminal short-page. We want the same rule values() would pick.
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 1000
	}

	var all []NetworkEvent
	var lastEnd *time.Time
	for page := 0; page < maxNetworkEventsPages; page++ {
		var body NetworkEventsPage
		if err := c.Get(ctx,
			"networks/"+url.PathEscape(networkID)+"/events",
			"", opts.values(), ttl, &body); err != nil {
			return nil, err
		}
		all = append(all, body.Events...)

		// Terminal short-page.
		if len(body.Events) < perPage {
			break
		}
		// Server withheld the cursor — stop so we don't spin on the same
		// cursor forever.
		if body.PageEndAt == nil {
			break
		}
		// Cursor didn't advance — break to avoid pathological loops if the
		// server echoes back the same marker.
		if lastEnd != nil && body.PageEndAt.Equal(*lastEnd) {
			break
		}
		lastEnd = body.PageEndAt
		opts.StartingAfter = body.PageEndAt.UTC().Format(time.RFC3339)
	}
	return all, nil
}
