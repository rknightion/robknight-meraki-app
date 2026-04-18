package query

import (
	"context"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// organizationsTTL caches the full visible-orgs list. Orgs change rarely and
// the list fits in a few KB, so a generous TTL is fine.
const organizationsTTL = 1 * time.Hour

// handleOrganizations emits one row per organization visible to the API key.
// Shape: id, name, url, apiEnabled. The datasource uses this both for the
// org-picker dropdown and as a template variable source.
func handleOrganizations(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	_ = q // Organizations has no per-query filters.
	orgs, err := client.GetOrganizations(ctx, organizationsTTL)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(orgs))
	names := make([]string, 0, len(orgs))
	urls := make([]string, 0, len(orgs))
	apiEnabled := make([]bool, 0, len(orgs))
	for _, o := range orgs {
		ids = append(ids, o.ID)
		names = append(names, o.Name)
		urls = append(urls, o.URL)
		apiEnabled = append(apiEnabled, o.API.Enabled)
	}

	return []*data.Frame{
		data.NewFrame("organizations",
			data.NewField("id", nil, ids),
			data.NewField("name", nil, names),
			data.NewField("url", nil, urls),
			data.NewField("apiEnabled", nil, apiEnabled),
		),
	}, nil
}
