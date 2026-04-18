package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// networksTTL: networks can be added/removed during the day but the list is
// generally static. 15 minutes keeps variable refreshes cheap while still
// letting new networks show up reasonably quickly.
const networksTTL = 15 * time.Minute

// handleNetworks emits one row per network under the requested org. We fold
// multi-valued fields (productTypes, tags) into comma-joined strings so the
// frame stays flat and table-friendly.
func handleNetworks(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("networks: orgId is required")
	}
	networks, err := client.GetOrganizationNetworks(ctx, q.OrgID, q.ProductTypes, networksTTL)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(networks))
	orgIDs := make([]string, 0, len(networks))
	names := make([]string, 0, len(networks))
	productTypes := make([]string, 0, len(networks))
	timeZones := make([]string, 0, len(networks))
	tags := make([]string, 0, len(networks))
	urls := make([]string, 0, len(networks))
	for _, n := range networks {
		ids = append(ids, n.ID)
		orgIDs = append(orgIDs, n.OrganizationID)
		names = append(names, n.Name)
		productTypes = append(productTypes, strings.Join(n.ProductTypes, ","))
		timeZones = append(timeZones, n.TimeZone)
		tags = append(tags, strings.Join(n.Tags, ","))
		urls = append(urls, n.URL)
	}

	return []*data.Frame{
		data.NewFrame("networks",
			data.NewField("id", nil, ids),
			data.NewField("organizationId", nil, orgIDs),
			data.NewField("name", nil, names),
			data.NewField("productTypes", nil, productTypes),
			data.NewField("timeZone", nil, timeZones),
			data.NewField("tags", nil, tags),
			data.NewField("url", nil, urls),
		),
	}, nil
}

// handleNetworksCount emits a single wide frame with `{count}` so stat panels
// can bind via an organize+reduce chain without a client-side filterByValue
// (todos.txt §G.20). Shares the networks 15-minute cache.
func handleNetworksCount(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("networksCount: orgId is required")
	}
	networks, err := client.GetOrganizationNetworks(ctx, q.OrgID, q.ProductTypes, networksTTL)
	if err != nil {
		return nil, err
	}
	return []*data.Frame{
		data.NewFrame("networks_count",
			data.NewField("count", nil, []int64{int64(len(networks))}),
		),
	}, nil
}
