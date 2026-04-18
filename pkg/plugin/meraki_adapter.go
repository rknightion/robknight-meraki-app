package plugin

import (
	"context"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
)

// merakiAdapter bridges the plugin's *meraki.Client to the narrow
// alerts.MerakiAPI interface consumed by the reconciler. The reconciler
// deliberately depends on a minimal surface (just GetOrganizations) so this
// tiny shim is the whole adaptation — if the reconciler grows new calls we
// widen MerakiAPI and extend this file.
//
// The TTL argument on meraki.Client.GetOrganizations is elided to 0 here so
// the reconciler always hits the client's default caching behaviour (a 1h
// TTL per pkg/meraki/CLAUDE.md). We do not want a bespoke TTL for alert
// reconciliation — it should share the same cache entry every other panel
// populates.
type merakiAdapter struct {
	c *meraki.Client
}

func (a *merakiAdapter) GetOrganizations(ctx context.Context) ([]alerts.Organization, error) {
	orgs, err := a.c.GetOrganizations(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.Organization, len(orgs))
	for i, o := range orgs {
		out[i] = alerts.Organization{ID: o.ID, Name: o.Name}
	}
	return out, nil
}
