package query

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"golang.org/x/sync/errgroup"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// networkGeoTTL — networks rarely move; per §4.4.4-D plan TTL is 1 h.
// Devices are the actual carrier of lat/lng (the networks endpoint does
// not expose coordinates) so we reuse devicesTTL for the underlying
// /organizations/{orgId}/devices fetch and only memoise the per-network
// centroid here. A long TTL is safe because operators don't relocate
// physical devices on the order of minutes.
const networkGeoTTL = 1 * time.Hour //nolint:unused // reserved for a future per-network centroid cache wrapper

// deviceLldpCdpTTL — per §4.4.1-g exception. LLDP/CDP neighbour tables
// stabilise once a topology is wired and rarely flip during the day.
// Singleflight in the underlying client coalesces concurrent identical
// /devices/{serial}/lldpCdp calls.
const deviceLldpCdpTTL = 15 * time.Minute

// deviceLldpCdpFanoutCap caps the per-network fan-out so a query that
// inadvertently selects every device in a 200-device network does not
// blow through the 10 rps per-org rate budget. 50 devices × 1 call =
// 5 s under the default limiter — comfortable margin. Larger graphs are
// gated to a network-scoped variable per §4.4.4-D ("DO NOT attempt
// org-wide fan-out by default").
const deviceLldpCdpFanoutCap = 50

// deviceLldpCdpConcurrency caps the in-flight goroutines for the
// per-device fan-out. The rate limiter handles long-term pacing; this
// cap controls peak burst so we don't queue 50 goroutines that each
// hold an HTTP connection. 8 mirrors the worker count used in
// handleSwitchPortConfig and handleApClients.
const deviceLldpCdpConcurrency = 8

// handleNetworkGeo emits a single table frame: networkId, name, lat, lng.
// Coordinates are derived by averaging the lat/lng of every geo-tagged
// device in each network. Networks with no geo-tagged devices are
// dropped from the row set and counted in a data.Notice attached to the
// frame so operators can see "X networks lack coordinates" without
// having to inspect the raw data.
//
// Why we derive coordinates from devices instead of networks: Meraki's
// /organizations/{orgId}/networks endpoint does NOT carry lat/lng on
// the network resource (verified 2026-04-18 against the OpenAPI spec).
// Coordinates live on devices via Meraki Dashboard's "set device
// location" workflow. One cached /organizations/{orgId}/devices call
// gives us everything we need without a per-network fan-out.
func handleNetworkGeo(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("networkGeo: orgId is required")
	}

	// Fetch networks + devices in parallel — they're independent and the
	// devices fetch typically dominates.
	var (
		networks []meraki.Network
		devices  []meraki.Device
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		nets, err := client.GetOrganizationNetworks(gctx, q.OrgID, q.ProductTypes, networksTTL)
		if err != nil {
			return fmt.Errorf("networkGeo: networks: %w", err)
		}
		networks = nets
		return nil
	})
	g.Go(func() error {
		devs, err := client.GetOrganizationDevices(gctx, q.OrgID, q.ProductTypes, devicesTTL)
		if err != nil {
			return fmt.Errorf("networkGeo: devices: %w", err)
		}
		devices = devs
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Aggregate device coordinates per network. We average across every
	// geo-tagged device in the network. Devices with (0, 0) are treated
	// as unset — Meraki uses (0, 0) as the "not configured" sentinel
	// because (0, 0) is in the Atlantic Ocean off the coast of Africa,
	// which is essentially never a real Meraki deployment.
	type centroidAccum struct {
		latSum float64
		lngSum float64
		count  int
	}
	byNetwork := make(map[string]*centroidAccum, len(networks))
	for _, d := range devices {
		if d.NetworkID == "" {
			continue
		}
		if d.Lat == 0 && d.Lng == 0 {
			continue
		}
		acc := byNetwork[d.NetworkID]
		if acc == nil {
			acc = &centroidAccum{}
			byNetwork[d.NetworkID] = acc
		}
		acc.latSum += d.Lat
		acc.lngSum += d.Lng
		acc.count++
	}

	// Stable order on the output: sort networks by id so test fixtures
	// can match on row[0] without flakiness.
	sort.Slice(networks, func(i, j int) bool { return networks[i].ID < networks[j].ID })

	var (
		networkIDs []string
		names      []string
		lats       []float64
		lngs       []float64
		dropped    int
	)
	for _, n := range networks {
		acc := byNetwork[n.ID]
		if acc == nil || acc.count == 0 {
			dropped++
			continue
		}
		networkIDs = append(networkIDs, n.ID)
		names = append(names, n.Name)
		lats = append(lats, acc.latSum/float64(acc.count))
		lngs = append(lngs, acc.lngSum/float64(acc.count))
	}

	frame := data.NewFrame("network_geo",
		data.NewField("networkId", nil, networkIDs),
		data.NewField("name", nil, names),
		data.NewField("lat", nil, lats),
		data.NewField("lng", nil, lngs),
	)

	if dropped > 0 {
		if frame.Meta == nil {
			frame.Meta = &data.FrameMeta{}
		}
		frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text: fmt.Sprintf(
				"%d network(s) lack coordinates and were dropped from the map. Set device locations in Meraki Dashboard to populate the geomap.",
				dropped,
			),
		})
	}

	return []*data.Frame{frame}, nil
}

// handleDeviceLldpCdp emits TWO frames per the Grafana Node Graph viz
// contract:
//
//   - "nodes" frame: id, title, subtitle, mainstat
//   - "edges" frame: id, source, target
//
// Per §4.4.4-D the link graph is gated to per-network scope by default
// because we have no lab org to measure org-wide LLDP/CDP fan-out cost
// safely. The handler accepts q.Serials directly OR derives them from
// q.NetworkIDs (one /organizations/{orgId}/devices call, filtered
// in-memory). Either filter is required — a fully-empty filter set
// returns an error rather than fanning out across the org.
//
// Node data:
//
//   - id        — Meraki serial (or advertised neighbour id when the
//                 device is outside the org).
//   - title     — device name (falls back to serial when name is blank).
//   - subtitle  — model + productType (e.g. "MS220-8P · switch").
//   - mainstat  — "in-org" / "external" so the viz can tell at a glance
//                 which nodes are Meraki devices we know about and which
//                 are upstream/peer devices we only learned via LLDP.
//
// Edge data:
//
//   - id     — "<source>__<target>" canonical key (sorted) so duplicate
//              CDP+LLDP entries for the same physical link collapse to
//              one edge.
//   - source — Meraki serial of the source device.
//   - target — Meraki serial of the in-org neighbour, or the
//              advertised id for external neighbours.
//
// Frames are returned in (nodes, edges) order. Grafana's Node Graph viz
// looks at frame meta `preferredVisualisationType: "nodeGraph"` to
// auto-detect both frames; we set that on each so the viz lights up
// without manual options.
func handleDeviceLldpCdp(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceLldpCdp: orgId is required")
	}
	if len(q.Serials) == 0 && len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("deviceLldpCdp: at least one networkId or serial is required (org-wide fan-out is intentionally disabled — §4.4.4-D)")
	}

	// We always pull the org devices list — even when the caller passed
	// q.Serials — because the node-frame title/subtitle columns need
	// device.Name and device.Model, and we want the in-org membership
	// set for marking edges as internal vs external.
	devices, err := client.GetOrganizationDevices(ctx, q.OrgID, nil, devicesTTL)
	if err != nil {
		return nil, fmt.Errorf("deviceLldpCdp: devices: %w", err)
	}

	deviceBySerial := make(map[string]meraki.Device, len(devices))
	for _, d := range devices {
		deviceBySerial[d.Serial] = d
	}

	// Resolve which serials to fan out over.
	var targetSerials []string
	if len(q.Serials) > 0 {
		targetSerials = append(targetSerials, q.Serials...)
	} else {
		networkSet := make(map[string]struct{}, len(q.NetworkIDs))
		for _, nid := range q.NetworkIDs {
			networkSet[nid] = struct{}{}
		}
		for _, d := range devices {
			if _, in := networkSet[d.NetworkID]; in {
				targetSerials = append(targetSerials, d.Serial)
			}
		}
	}
	sort.Strings(targetSerials)
	if len(targetSerials) > deviceLldpCdpFanoutCap {
		targetSerials = targetSerials[:deviceLldpCdpFanoutCap]
	}

	// Per-device fan-out with bounded concurrency. The Meraki rate
	// limiter handles long-term pacing; the semaphore here bounds the
	// in-flight HTTP-connection burst.
	type result struct {
		serial    string
		neighbors []meraki.LldpCdpNeighbor
		err       error
	}
	results := make([]result, len(targetSerials))
	sem := make(chan struct{}, deviceLldpCdpConcurrency)
	var wg sync.WaitGroup
	for i, serial := range targetSerials {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, serial string) {
			defer wg.Done()
			defer func() { <-sem }()
			neighbors, err := client.GetDeviceLldpCdp(ctx, serial, deviceLldpCdpTTL)
			results[i] = result{serial: serial, neighbors: neighbors, err: err}
		}(i, serial)
	}
	wg.Wait()

	// Aggregate first — we need the full set of edges before we can
	// emit the nodes frame (because external neighbours become nodes
	// too, and we want to avoid emitting an edge to a node we never
	// declared).
	type edgeKey struct{ a, b string }
	edges := make(map[edgeKey]struct{})
	externalNeighbors := make(map[string]meraki.LldpCdpNeighbor)
	internalSerials := make(map[string]struct{}, len(targetSerials))
	for _, s := range targetSerials {
		internalSerials[s] = struct{}{}
	}

	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		for _, n := range r.neighbors {
			target := n.NeighborID
			if target == "" {
				continue
			}
			// Canonicalise edge key (sorted endpoints) so a CDP entry
			// from A->B and a matching LLDP entry from B->A collapse
			// to one row in the edges frame.
			a, b := r.serial, target
			if a > b {
				a, b = b, a
			}
			edges[edgeKey{a, b}] = struct{}{}
			if _, in := internalSerials[target]; !in {
				// First-seen wins — later entries from the same external
				// neighbour just refresh the description, not the keying.
				if _, seen := externalNeighbors[target]; !seen {
					externalNeighbors[target] = n
				}
			}
		}
	}

	// Build nodes frame. We always emit one row per target serial (so
	// even an isolated device shows on the graph) plus one row per
	// external neighbour we observed.
	var (
		nodeIDs       []string
		nodeTitles    []string
		nodeSubtitles []string
		nodeMain      []string
	)
	addedNodes := make(map[string]struct{})
	for _, serial := range targetSerials {
		if _, dup := addedNodes[serial]; dup {
			continue
		}
		addedNodes[serial] = struct{}{}
		dev := deviceBySerial[serial]
		title := dev.Name
		if title == "" {
			title = serial
		}
		subtitle := dev.Model
		if dev.ProductType != "" {
			if subtitle != "" {
				subtitle += " · "
			}
			subtitle += dev.ProductType
		}
		nodeIDs = append(nodeIDs, serial)
		nodeTitles = append(nodeTitles, title)
		nodeSubtitles = append(nodeSubtitles, subtitle)
		nodeMain = append(nodeMain, "in-org")
	}
	// External neighbours — sort for deterministic output.
	externalKeys := make([]string, 0, len(externalNeighbors))
	for k := range externalNeighbors {
		externalKeys = append(externalKeys, k)
	}
	sort.Strings(externalKeys)
	for _, k := range externalKeys {
		if _, dup := addedNodes[k]; dup {
			continue
		}
		addedNodes[k] = struct{}{}
		n := externalNeighbors[k]
		nodeIDs = append(nodeIDs, k)
		nodeTitles = append(nodeTitles, k)
		nodeSubtitles = append(nodeSubtitles, n.NeighborDescription)
		nodeMain = append(nodeMain, "external")
	}

	// Build edges frame. Sort by canonicalised key for determinism.
	edgeKeys := make([]edgeKey, 0, len(edges))
	for k := range edges {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Slice(edgeKeys, func(i, j int) bool {
		if edgeKeys[i].a != edgeKeys[j].a {
			return edgeKeys[i].a < edgeKeys[j].a
		}
		return edgeKeys[i].b < edgeKeys[j].b
	})
	var (
		edgeIDs     []string
		edgeSources []string
		edgeTargets []string
	)
	for _, k := range edgeKeys {
		// Skip any edge whose endpoints didn't end up in the nodes frame
		// — this should be impossible after the aggregation above, but
		// guarding against it keeps the Node Graph viz from rendering
		// orphan edges.
		if _, ok := addedNodes[k.a]; !ok {
			continue
		}
		if _, ok := addedNodes[k.b]; !ok {
			continue
		}
		edgeIDs = append(edgeIDs, k.a+"__"+k.b)
		edgeSources = append(edgeSources, k.a)
		edgeTargets = append(edgeTargets, k.b)
	}

	nodesFrame := data.NewFrame("nodes",
		data.NewField("id", nil, nodeIDs),
		data.NewField("title", nil, nodeTitles),
		data.NewField("subtitle", nil, nodeSubtitles),
		data.NewField("mainstat", nil, nodeMain),
	)
	nodesFrame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}

	edgesFrame := data.NewFrame("edges",
		data.NewField("id", nil, edgeIDs),
		data.NewField("source", nil, edgeSources),
		data.NewField("target", nil, edgeTargets),
	)
	edgesFrame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}

	return []*data.Frame{nodesFrame, edgesFrame}, firstErr
}
