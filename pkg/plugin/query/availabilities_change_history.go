package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// deviceAvailabilityChangesTTL: availability flap events arrive within 60s of a real
// transition on Meraki's side. 30s keeps panels close to live without hammering the
// endpoint — same cadence as the current-state DeviceAvailabilities kind and the alerts
// feed so KPI rows built from multiple change-style endpoints stay coherent. Matches the
// §7.3-D proposal in todos.txt.
const deviceAvailabilityChangesTTL = 30 * time.Second

// handleDeviceAvailabilitiesChangeHistory emits a single table frame with one row per
// state-transition entry. Filters on q.Serials / q.ProductTypes / q.NetworkIDs (all
// pushed to the server as repeated `serials[]`, `productTypes[]`, `networkIds[]` params).
// q.Metrics doubles as the Statuses filter (§G.21 — a natural fit since it's a slice of
// string and the Meraki endpoint accepts a repeated `statuses[]` param).
//
// The frame carries pre-computed oldStatus / newStatus columns (extracted from the
// details.old/details.new envelope) so the UI can render the transition as a simple
// "online → offline" badge without post-processing.
//
// drilldownUrl follows the same §1.12 pattern as handleDevices /
// handleDeviceAvailabilities — one URL per row keyed on the device's productType, so a
// table that spans product families still routes each row to the right detail page.
func handleDeviceAvailabilitiesChangeHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceAvailabilityChanges: orgId is required")
	}

	reqOpts := meraki.DeviceAvailabilityChangeOptions{
		Serials:      q.Serials,
		ProductTypes: q.ProductTypes,
		NetworkIDs:   q.NetworkIDs,
		Statuses:     q.Metrics,
	}
	if from := toRFCTime(tr.From); !from.IsZero() {
		reqOpts.TSStart = &from
	}
	if to := toRFCTime(tr.To); !to.IsZero() {
		reqOpts.TSEnd = &to
	}

	changes, err := client.GetOrganizationDevicesAvailabilitiesChangeHistory(ctx, q.OrgID, reqOpts, deviceAvailabilityChangesTTL)
	if err != nil {
		return nil, err
	}

	var (
		ts          []time.Time
		serial      []string
		name        []string
		productType []string
		model       []string
		networkID   []string
		networkName []string
		oldStatus   []string
		newStatus   []string
		drilldown   []string
	)
	for _, ch := range changes {
		var t time.Time
		if ch.TS != nil {
			t = ch.TS.UTC()
		}
		ts = append(ts, t)
		serial = append(serial, ch.Device.Serial)
		name = append(name, ch.Device.Name)
		productType = append(productType, ch.Device.ProductType)
		model = append(model, ch.Device.Model)
		networkID = append(networkID, ch.Network.ID)
		networkName = append(networkName, ch.Network.Name)
		oldStatus = append(oldStatus, statusValueFromDetails(ch.Details.Old))
		newStatus = append(newStatus, statusValueFromDetails(ch.Details.New))
		drilldown = append(drilldown, deviceDrilldownURL(opts.PluginPathPrefix, ch.Device.ProductType, ch.Device.Serial))
	}

	return []*data.Frame{
		data.NewFrame("device_availability_changes",
			data.NewField("ts", nil, ts),
			data.NewField("serial", nil, serial),
			data.NewField("name", nil, name),
			data.NewField("productType", nil, productType),
			data.NewField("model", nil, model),
			data.NewField("network_id", nil, networkID),
			data.NewField("network_name", nil, networkName),
			data.NewField("oldStatus", nil, oldStatus),
			data.NewField("newStatus", nil, newStatus),
			data.NewField("drilldownUrl", nil, drilldown),
		),
	}, nil
}

// statusValueFromDetails pulls the "status" entry out of a details.old/details.new
// envelope. Meraki uses a list of (name, value) pairs so future transitions (e.g.
// reachability, tags) can be tracked without a schema change — we're only interested in
// the "status" pair today. Returns "" when the envelope is empty or doesn't carry a
// status entry.
func statusValueFromDetails(entries []meraki.DeviceAvailabilityChangeValue) string {
	for _, e := range entries {
		if e.Name == "status" {
			return e.Value
		}
	}
	return ""
}
