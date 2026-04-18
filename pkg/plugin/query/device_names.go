package query

import (
	"context"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// resolveDeviceNames returns a serial → name lookup for the devices in an org.
//
// productTypes, when non-empty, narrows the `/devices` call to those product families
// (e.g. "sensor", "wireless", "switch"). An empty/omitted productTypes returns every
// device in the org, which is fine for mixed-inventory panels but wasteful when only
// one family's legend is needed. The underlying call shares the 5-minute devices cache
// with handleDevices, so a dashboard with multiple legend-driven panels still triggers
// at most one HTTP request per productTypes set per 5 min.
//
// A device with no configured Name is mapped to an empty string; callers should guard
// with `name != ""` and fall back to the raw serial to avoid empty legend entries.
func resolveDeviceNames(ctx context.Context, client *meraki.Client, orgID string, productTypes ...string) (map[string]string, error) {
	devices, err := client.GetOrganizationDevices(ctx, orgID, productTypes, devicesTTL)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(devices))
	for _, d := range devices {
		out[d.Serial] = d.Name
	}
	return out, nil
}
