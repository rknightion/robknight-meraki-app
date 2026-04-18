package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// configurationChangesTTL: the change log isn't refreshed continuously on Meraki's side
// (entries append whenever an admin edits configuration), so 5 minutes is plenty for
// panel auto-refresh without drowning the /configurationChanges endpoint. Matches the
// §7.3-C proposal in todos.txt.
const configurationChangesTTL = 5 * time.Minute

// handleConfigurationChanges emits a single table frame with one row per change log
// entry. Filters on q.NetworkIDs[0] (single network — the Meraki endpoint only accepts
// one) and q.Metrics[0] (admin ID — §G.21 scalar overload, first non-empty entry).
//
// The endpoint supports t0/t1/timespan. We thread the panel's TimeRange into TSStart /
// TSEnd so a user picking "last 24 hours" gets exactly that window. When the panel range
// exceeds Meraki's documented 365-day cap, we let the server surface the 400 instead of
// silently truncating — the configurationChanges spec is stricter than our KnownEndpointRanges
// Resolve() flow, which is meant for timeseries endpoints with resolution parameters.
//
// Frame shape: one long-format row per change entry, column order optimised for the
// default Grafana table viz (time first, then actor, then context, then payload diff).
// oldValue/newValue are JSON-encoded strings — the scene panel leaves them as-is so users
// can inspect them in the panel's field inspector.
func handleConfigurationChanges(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("configurationChanges: orgId is required")
	}

	opts := meraki.ConfigurationChangesOptions{
		AdminID: firstNonEmpty(q.Metrics),
	}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	if from := toRFCTime(tr.From); !from.IsZero() {
		opts.TSStart = &from
	}
	if to := toRFCTime(tr.To); !to.IsZero() {
		opts.TSEnd = &to
	}

	changes, err := client.GetOrganizationConfigurationChanges(ctx, q.OrgID, opts, configurationChangesTTL)
	if err != nil {
		return nil, err
	}

	var (
		ts         []time.Time
		adminName  []string
		adminEmail []string
		adminID    []string
		page       []string
		label      []string
		networkID  []string
		oldValue   []string
		newValue   []string
	)
	for _, ch := range changes {
		var t time.Time
		if ch.TS != nil {
			t = ch.TS.UTC()
		}
		ts = append(ts, t)
		adminName = append(adminName, ch.AdminName)
		adminEmail = append(adminEmail, ch.AdminEmail)
		adminID = append(adminID, ch.AdminID)
		page = append(page, ch.Page)
		label = append(label, ch.Label)
		networkID = append(networkID, ch.NetworkID)
		oldValue = append(oldValue, ch.OldValue)
		newValue = append(newValue, ch.NewValue)
	}

	return []*data.Frame{
		data.NewFrame("configuration_changes",
			data.NewField("ts", nil, ts),
			data.NewField("adminName", nil, adminName),
			data.NewField("adminEmail", nil, adminEmail),
			data.NewField("adminId", nil, adminID),
			data.NewField("page", nil, page),
			data.NewField("label", nil, label),
			data.NewField("networkId", nil, networkID),
			data.NewField("oldValue", nil, oldValue),
			data.NewField("newValue", nil, newValue),
		),
	}, nil
}
