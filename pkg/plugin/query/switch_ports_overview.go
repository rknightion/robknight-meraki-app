package query

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleSwitchPortsOverview emits a wide single-row frame with aggregate port
// counts + PoE draw across the org (optionally filtered to one or more
// serials). Replaces the client-side `reduce` transform chain previously used
// by the Switches KPI row, which fell foul of the `filterByValue+reduce`
// fragility documented in todos.txt §G.20.
//
// Reuses `switchPortsTTL` so the cache entry is shared with `handleSwitchPorts`
// — both handlers hit the same endpoint with the same options, so the Meraki
// round-trip is only paid once per TTL window.
func handleSwitchPortsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPortsOverview: orgId is required")
	}
	// See `handleSwitchPorts` for why we don't forward Serials/NetworkIDs
	// to the Meraki API — filtering client-side keeps the cache entry
	// shared with the fleet query and avoids an inconsistency in the
	// bySwitch endpoint's handling of serial filters.
	switches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, meraki.SwitchPortStatusOptions{}, switchPortsTTL)
	if err != nil {
		return nil, err
	}

	var serialFilter map[string]struct{}
	if len(q.Serials) > 0 {
		serialFilter = make(map[string]struct{}, len(q.Serials))
		for _, s := range q.Serials {
			if s != "" {
				serialFilter[s] = struct{}{}
			}
		}
	}
	var networkFilter map[string]struct{}
	if len(q.NetworkIDs) > 0 {
		networkFilter = make(map[string]struct{}, len(q.NetworkIDs))
		for _, n := range q.NetworkIDs {
			if n != "" {
				networkFilter[n] = struct{}{}
			}
		}
	}

	var (
		portCount         int64
		portsConnected    int64
		portsDisconnected int64
		portsDisabled     int64
		clientCount       int64
		poeTotalWatts     float64
	)
	for _, sw := range switches {
		if serialFilter != nil {
			if _, ok := serialFilter[sw.Serial]; !ok {
				continue
			}
		}
		if networkFilter != nil {
			if _, ok := networkFilter[sw.Network.ID]; !ok {
				continue
			}
		}
		for _, p := range sw.Ports {
			portCount++
			if !p.Enabled {
				portsDisabled++
			}
			switch p.Status {
			case "Connected":
				portsConnected++
			case "Disconnected":
				portsDisconnected++
			}
			clientCount += p.ClientCount
			poeTotalWatts += p.PowerUsageInWatts
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_ports_overview",
			data.NewField("portCount", nil, []int64{portCount}),
			data.NewField("portsConnected", nil, []int64{portsConnected}),
			data.NewField("portsDisconnected", nil, []int64{portsDisconnected}),
			data.NewField("portsDisabled", nil, []int64{portsDisabled}),
			data.NewField("clientCount", nil, []int64{clientCount}),
			data.NewField("poeTotalWatts", nil, []float64{poeTotalWatts}),
		),
	}, nil
}
