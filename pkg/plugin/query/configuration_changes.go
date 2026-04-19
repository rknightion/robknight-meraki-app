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
// Frame shape: one long-format row per change entry. Columns are ordered so the default
// table viz reads top-to-bottom as "when → who → scope → what → before/after → how":
//
//	ts, adminName, adminEmail, adminId, networkName, networkId, ssidName, ssidNumber,
//	page, label, oldValue, newValue, clientType, networkUrl
//
// networkName, ssidName, ssidNumber, clientType, and networkUrl were added in v0.8 (audit-
// log polish pass) so readers can understand org-level vs network-scoped changes without
// looking up GUIDs. oldValue/newValue remain raw JSON-encoded strings — the scene panel
// surfaces them as-is so operators can inspect the full diff in cell tooltips or via the
// panel's field inspector.
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
		ts          []time.Time
		adminName   []string
		adminEmail  []string
		adminID     []string
		networkName []string
		networkID   []string
		ssidName    []string
		ssidNumber  []*int64
		page        []string
		label       []string
		oldValue    []string
		newValue    []string
		clientType  []string
		networkURL  []string
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
		networkName = append(networkName, ch.NetworkName)
		networkID = append(networkID, ch.NetworkID)
		ssidName = append(ssidName, ch.SSIDName)
		// ssidNumber is a nullable int in the Meraki response — keep the nullability so the
		// table viz renders empty cells as blanks rather than 0s that look like SSID 0.
		if ch.SSIDNumber != nil {
			v := int64(*ch.SSIDNumber)
			ssidNumber = append(ssidNumber, &v)
		} else {
			ssidNumber = append(ssidNumber, nil)
		}
		page = append(page, ch.Page)
		label = append(label, ch.Label)
		oldValue = append(oldValue, ch.OldValue)
		newValue = append(newValue, ch.NewValue)
		var ct string
		if ch.Client != nil {
			ct = ch.Client.Type
		}
		clientType = append(clientType, ct)
		networkURL = append(networkURL, ch.NetworkURL)
	}

	return []*data.Frame{
		data.NewFrame("configuration_changes",
			data.NewField("ts", nil, ts),
			data.NewField("adminName", nil, adminName),
			data.NewField("adminEmail", nil, adminEmail),
			data.NewField("adminId", nil, adminID),
			data.NewField("networkName", nil, networkName),
			data.NewField("networkId", nil, networkID),
			data.NewField("ssidName", nil, ssidName),
			data.NewField("ssidNumber", nil, ssidNumber),
			data.NewField("page", nil, page),
			data.NewField("label", nil, label),
			data.NewField("oldValue", nil, oldValue),
			data.NewField("newValue", nil, newValue),
			data.NewField("clientType", nil, clientType),
			data.NewField("networkUrl", nil, networkURL),
		),
	}, nil
}
