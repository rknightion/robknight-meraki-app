package query

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleSwitchPortsOverview emits a wide single-row frame with aggregate port
// counts + PoE draw. Replaces the client-side `reduce` transform chain
// previously used by the Switches KPI row (todos.txt §G.20).
//
// Data source split mirrors handleSwitchPorts:
//   - When `q.Serials` names specific switches → fan out to device-scoped
//     statuses (rich shape with clientCount + powerUsageInWh). This is how
//     the per-switch detail page's Clients / PoE stat tiles get populated.
//   - Otherwise → org-level aggregated `bySwitch/statuses` feed. The
//     aggregated feed lacks clientCount / powerUsageInWh, so the fleet KPI
//     row will show portCount + status breakdown but not those fields.
func handleSwitchPortsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPortsOverview: orgId is required")
	}
	switches, truncated, err := fetchSwitchesForAggregate(ctx, client, q)
	if err != nil {
		return nil, err
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
			poeTotalWatts += p.PowerUsageInWh
		}
	}

	frame := data.NewFrame("switch_ports_overview",
		data.NewField("portCount", nil, []int64{portCount}),
		data.NewField("portsConnected", nil, []int64{portsConnected}),
		data.NewField("portsDisconnected", nil, []int64{portsDisconnected}),
		data.NewField("portsDisabled", nil, []int64{portsDisabled}),
		data.NewField("clientCount", nil, []int64{clientCount}),
		data.NewField("poeTotalWatts", nil, []float64{poeTotalWatts}),
	)
	if truncated {
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     fmt.Sprintf("PoE + client totals show port counts only — this org has more than %d switches, so per-device fan-out was skipped to protect the Meraki rate budget.", fleetFanoutCap),
		})
	}
	return []*data.Frame{frame}, nil
}
