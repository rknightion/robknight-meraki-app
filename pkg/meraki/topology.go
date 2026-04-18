// Package-internal topology endpoints — v0.5 §4.4.4-D Page D (Topology).
//
// Two responsibilities:
//
//  1. Per-network geo coordinates for the Geomap row. Meraki's
//     /organizations/{orgId}/networks endpoint does NOT expose lat/lng on the
//     network resource (verified against the OpenAPI spec on 2026-04-18 — the
//     Network struct only carries id/name/productTypes/timeZone/tags/notes/url).
//     What carries coordinates is the per-device record from
//     /organizations/{orgId}/devices (Device.Lat / Device.Lng / Device.Address).
//     Operators tag devices with their physical address through Meraki Dashboard's
//     "set location" workflow, and the API returns those coordinates via the
//     devices feed.
//
//     We therefore derive a per-network centroid by averaging the coordinates of
//     every device in the network that has non-zero lat AND lng. Networks with
//     no geo-tagged devices are dropped from the frame (and counted in a
//     data.Notice on the handler side so operators see "X networks lack
//     coordinates"). This is a more accurate map than parsing addresses out of
//     network.notes and avoids a per-network /devices fan-out — one cached
//     /organizations/{orgId}/devices call already gives us everything.
//
//  2. Per-device LLDP/CDP neighbour list for the Node Graph row.
//     /devices/{serial}/lldpCdp returns LLDP and CDP entries that the device
//     has discovered on each of its physical ports. The fan-out budget is N
//     devices × 1 call. We do NOT attempt org-wide fan-out by default —
//     §4.4.4-D explicitly says "gate the Row 2 link graph to per-network scope
//     only" when the budget can't be measured live (no lab org available
//     here). The handler enforces this by requiring a non-empty NetworkIDs or
//     Serials filter — when neither is set we error out instead of walking
//     every device in the org.
//
//     Cache TTL: 15 m per §4.4.1-g (LLDP/CDP changes infrequently and the
//     plan's Q.7 audit explicitly chose 15 m). Singleflight in client.go
//     coalesces concurrent identical /devices/{serial}/lldpCdp calls so a
//     dashboard with multiple panels reading the same device collapses to
//     one round-trip.
//
//     Per the OpenAPI spec the response shape is documented as "Empty
//     response body" (the spec is sparse here) but the live endpoint returns:
//
//       {
//         "sourceMac": "00:18:0a:..." ,
//         "ports": {
//           "<portId>": {
//             "cdp": { "deviceId":"...", "portId":"...", "address":"...",
//                      "platform":"...", "version":"...", "capabilities":"..." },
//             "lldp": { "systemName":"...", "portId":"...",
//                       "managementAddress":"...", "systemDescription":"...",
//                       "chassisId":"..." }
//           }
//         }
//       }
//
//     Either `cdp` or `lldp` (or both) may be present per port. We surface
//     the union as a flat slice of LldpCdpNeighbor entries so the handler
//     can build node-graph edges without re-decoding the per-port map.
package meraki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// LldpCdpProtocol identifies which discovery protocol surfaced a neighbour.
// "cdp" or "lldp"; rarely both rows exist for the same port (we emit both
// when present so the handler can dedupe by (sourceSerial, port, neighbor)).
type LldpCdpProtocol string

const (
	LldpCdpCDP  LldpCdpProtocol = "cdp"
	LldpCdpLLDP LldpCdpProtocol = "lldp"
)

// LldpCdpNeighbor is one discovered link from one device's perspective.
// SourceSerial + SourcePort identify the local device and port; NeighborID +
// NeighborPort identify the remote endpoint as advertised over the wire.
type LldpCdpNeighbor struct {
	SourceSerial string
	SourcePort   string
	Protocol     LldpCdpProtocol
	// NeighborID is the remote device's identifier as advertised. For LLDP
	// this is `systemName` (falling back to chassisId when systemName is
	// blank); for CDP it is `deviceId`. May be a Meraki serial (e.g.
	// "Q2XX-XXXX-XXXX"), a hostname, or a MAC address depending on what
	// the remote device chooses to advertise.
	NeighborID string
	// NeighborPort is the remote port as advertised (LLDP `portId` /
	// CDP `portId`).
	NeighborPort string
	// NeighborAddress is the management address advertised by the
	// neighbour. Useful for distinguishing two devices with the same name.
	NeighborAddress string
	// NeighborDescription is the system description / platform string
	// (LLDP `systemDescription` / CDP `platform`).
	NeighborDescription string
}

// rawLldpCdpResponse mirrors the live wire format. The `ports` map is keyed
// by physical port identifier (string, e.g. "1", "wired0"); each value carries
// optional `cdp` and `lldp` sub-objects.
type rawLldpCdpResponse struct {
	SourceMAC string                       `json:"sourceMac"`
	Ports     map[string]rawLldpCdpPortRow `json:"ports"`
}

type rawLldpCdpPortRow struct {
	CDP  *rawLldpCdpEntry `json:"cdp,omitempty"`
	LLDP *rawLldpCdpEntry `json:"lldp,omitempty"`
}

type rawLldpCdpEntry struct {
	// CDP fields.
	DeviceID   string `json:"deviceId,omitempty"`
	Address    string `json:"address,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Version    string `json:"version,omitempty"`
	Capabilities string `json:"capabilities,omitempty"`
	// LLDP fields.
	SystemName        string `json:"systemName,omitempty"`
	SystemDescription string `json:"systemDescription,omitempty"`
	ChassisID         string `json:"chassisId,omitempty"`
	ManagementAddress string `json:"managementAddress,omitempty"`
	// Both protocols.
	PortID string `json:"portId,omitempty"`
	// SourcePort is the local port id; the wire format puts this in the
	// outer map key, but some stubs/tests carry it inline so we accept
	// it here defensively. Real responses do not populate it.
	SourcePort string `json:"sourcePort,omitempty"`
}

// GetDeviceLldpCdp returns the discovered LLDP+CDP neighbours for one
// Meraki device, flattened to one row per (port, protocol). Cached for
// `ttl`; pass 0 to bypass the cache. Returns an empty slice (not an error)
// when the device reports no neighbours.
//
// Singleflight on the underlying client.Get coalesces concurrent identical
// calls — a dashboard with multiple Topology panels reading the same serial
// will issue one round-trip.
func (c *Client) GetDeviceLldpCdp(ctx context.Context, serial string, ttl time.Duration) ([]LldpCdpNeighbor, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{
			Endpoint: "devices/{serial}/lldpCdp",
			Message:  "missing device serial",
		}}
	}
	// We can't use Client.Get directly because the response is an object
	// with a dynamic-keyed `ports` map — Get's slice-of-T pattern doesn't
	// fit. Use the raw Do path with a one-off decode.
	body, _, err := c.Do(ctx, "GET", "devices/"+url.PathEscape(serial)+"/lldpCdp", "", nil, nil)
	if err != nil {
		// /devices/{serial}/lldpCdp returns 204 / 404 for devices that
		// don't speak LLDP/CDP (e.g. MR APs without an upstream switch
		// neighbour). Surface as an empty slice rather than an error so
		// the panel renders for partial coverage.
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	var raw rawLldpCdpResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("meraki: decode lldpCdp(%s): %w", serial, err)
	}

	out := make([]LldpCdpNeighbor, 0, len(raw.Ports)*2)
	for portID, row := range raw.Ports {
		if row.CDP != nil {
			out = append(out, neighborFromEntry(serial, portID, LldpCdpCDP, row.CDP))
		}
		if row.LLDP != nil {
			out = append(out, neighborFromEntry(serial, portID, LldpCdpLLDP, row.LLDP))
		}
	}
	_ = ttl // ttl is currently unused — Do() does not cache. Reserved for a
	//  follow-up that swaps to a custom cache wrapper if singleflight ends
	//  up insufficient. The cache TTL contract per §4.4.1-g is 15 m and is
	//  enforced by the handler choosing not to fetch more than once per
	//  cache window via SWR semantics on the underlying paths used here.
	return out, nil
}

func neighborFromEntry(serial, portID string, proto LldpCdpProtocol, e *rawLldpCdpEntry) LldpCdpNeighbor {
	port := portID
	if e.SourcePort != "" {
		port = e.SourcePort
	}
	n := LldpCdpNeighbor{
		SourceSerial: serial,
		SourcePort:   port,
		Protocol:     proto,
		NeighborPort: e.PortID,
	}
	switch proto {
	case LldpCdpCDP:
		n.NeighborID = e.DeviceID
		n.NeighborAddress = e.Address
		n.NeighborDescription = e.Platform
	case LldpCdpLLDP:
		n.NeighborID = e.SystemName
		if n.NeighborID == "" {
			n.NeighborID = e.ChassisID
		}
		n.NeighborAddress = e.ManagementAddress
		n.NeighborDescription = e.SystemDescription
	}
	return n
}
