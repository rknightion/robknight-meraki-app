package query

import (
	"context"
	"fmt"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// runMetricFind maps a MerakiQuery to a flat {text, value} list suitable for
// Grafana's variable picker. Only the kinds that are meaningfully enumerable
// are supported; calling with anything else is a user error (returns a
// plain error — unlike the panel path there's nothing to attach a notice to).
func runMetricFind(ctx context.Context, client *meraki.Client, q MerakiQuery) (*MetricFindResponse, error) {
	switch q.Kind {
	case KindOrganizations:
		orgs, err := client.GetOrganizations(ctx, organizationsTTL)
		if err != nil {
			return nil, err
		}
		values := make([]MetricFindValue, 0, len(orgs))
		for _, o := range orgs {
			values = append(values, MetricFindValue{Text: o.Name, Value: o.ID})
		}
		return &MetricFindResponse{Values: values}, nil

	case KindNetworks:
		if q.OrgID == "" {
			return nil, fmt.Errorf("metricFind networks: orgId is required")
		}
		networks, err := client.GetOrganizationNetworks(ctx, q.OrgID, q.ProductTypes, networksTTL)
		if err != nil {
			return nil, err
		}
		values := make([]MetricFindValue, 0, len(networks))
		for _, n := range networks {
			values = append(values, MetricFindValue{Text: n.Name, Value: n.ID})
		}
		return &MetricFindResponse{Values: values}, nil

	case KindDevices:
		if q.OrgID == "" {
			return nil, fmt.Errorf("metricFind devices: orgId is required")
		}
		devices, err := client.GetOrganizationDevices(ctx, q.OrgID, q.ProductTypes, devicesTTL)
		if err != nil {
			return nil, err
		}
		values := make([]MetricFindValue, 0, len(devices))
		for _, d := range devices {
			// Name may be blank for freshly onboarded gear; serial is the
			// actual primary key so we fall back to it as the display label.
			text := d.Name
			if text == "" {
				text = d.Serial
			}
			values = append(values, MetricFindValue{Text: text, Value: d.Serial})
		}
		return &MetricFindResponse{Values: values}, nil

	case KindSensorReadingsLatest, KindSensorReadingsHistory:
		// Both sensor kinds share the same metric vocabulary. When a variable
		// query targets either, we return the static list so the UI can
		// populate a "metric" dropdown.
		values := make([]MetricFindValue, 0, len(metricNames))
		for _, m := range metricNames {
			values = append(values, MetricFindValue{Text: m, Value: m})
		}
		return &MetricFindResponse{Values: values}, nil

	case KindCameraAnalyticsZones:
		// Zone enumeration is per-camera (the /zones endpoint is device-scoped),
		// so the caller must supply a serial. We return one {text, value} per
		// configured zone where the text is "<type>: <label>" — the Meraki
		// zone object carries both — and the value is the raw zone id used in
		// subsequent /zones/{zoneId}/history calls.
		if len(q.Serials) == 0 || q.Serials[0] == "" {
			return nil, fmt.Errorf("metricFind cameraAnalyticsZones: serial is required")
		}
		zones, err := client.GetDeviceCameraAnalyticsZones(ctx, q.Serials[0], cameraAnalyticsZonesTTL)
		if err != nil {
			return nil, err
		}
		values := make([]MetricFindValue, 0, len(zones))
		for _, z := range zones {
			// Compose a human-friendly label. When either `type` or `label`
			// is blank we still want a non-empty text entry so the variable
			// picker isn't a list of colons — fall back to the zoneId.
			text := z.Type
			if text != "" && z.Label != "" {
				text = text + ": " + z.Label
			} else if text == "" {
				text = z.Label
			}
			if text == "" {
				text = z.ZoneID
			}
			values = append(values, MetricFindValue{Text: text, Value: z.ZoneID})
		}
		return &MetricFindResponse{Values: values}, nil

	default:
		return nil, fmt.Errorf("metricFind: unsupported kind %q", q.Kind)
	}
}

// Ensure the meraki import is retained even if future refactors drop a code
// path — runMetricFind only references the client via methods, so Go's
// unused-import check could flag it during aggressive edits. Keeping a typed
// nil reference here is free at runtime and documents the dependency.
var _ = (*meraki.Client)(nil)
