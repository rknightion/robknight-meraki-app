package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleOrgProductTypes emits a single wide frame with one int64 field per
// Meraki product family, giving the count of devices with that productType in
// the organisation. Populated from the existing devices list (cached), so no
// new endpoint call — cheap enough to load on every app mount.
//
// Frontend uses the presence of >0 for a family to decide whether to show
// that family's nav page (Appliances / Access Points / Switches / Cameras /
// Cellular Gateways / Sensors). Families with zero devices are hidden by
// default; admins can force them back via the "Show empty families" toggle
// in the Configuration page.
//
// Field names match MerakiProductType in src/types.ts so the frontend can
// map field → route without a lookup table.
func handleOrgProductTypes(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("orgProductTypes: orgId is required")
	}
	devices, err := client.GetOrganizationDevices(ctx, q.OrgID, nil, devicesTTL)
	if err != nil {
		return nil, err
	}

	counts := map[string]int64{}
	for _, d := range devices {
		pt := strings.TrimSpace(d.ProductType)
		if pt == "" {
			continue
		}
		counts[pt]++
	}

	// Deterministic field order — important for tests and for downstream
	// organize transforms that reference fields by name.
	families := []string{"appliance", "wireless", "switch", "camera", "cellularGateway", "sensor", "systemsManager"}
	fields := make([]*data.Field, 0, len(families))
	for _, f := range families {
		fields = append(fields, data.NewField(f, nil, []int64{counts[f]}))
	}

	// Catch any family we haven't hard-coded above (future Meraki additions)
	// by sorting the remaining keys and appending. Keeps the frame forward-
	// compatible if Meraki ships a new productType name we haven't seen.
	known := map[string]struct{}{}
	for _, f := range families {
		known[f] = struct{}{}
	}
	var extra []string
	for k := range counts {
		if _, ok := known[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		fields = append(fields, data.NewField(k, nil, []int64{counts[k]}))
	}

	return []*data.Frame{data.NewFrame("org_product_types", fields...)}, nil
}
