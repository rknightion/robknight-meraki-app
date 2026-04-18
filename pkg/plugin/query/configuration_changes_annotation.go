package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// configurationChangesAnnotationTTL matches configurationChangesTTL — both
// views read from the same endpoint, so they should expire together.
const configurationChangesAnnotationTTL = 5 * time.Minute

// handleConfigurationChangesAnnotation reshapes the configurationChanges feed
// into a Grafana annotation frame. Columns: time, title, text, tags. Consumers
// are scene AnnotationDataLayer configs (§4.4.3 1f) that overlay admin-driven
// config churn on any timeseries panel.
//
// Why a separate handler (vs reshaping in the client): the existing
// handleConfigurationChanges returns a wide table with nine columns for the
// Audit Log page. The annotation layer needs a narrow time-ordered frame with
// stable column names; mixing both shapes on one kind would force every
// consumer to do client-side reshaping. Two handlers, one endpoint, one TTL —
// the underlying HTTP response is cached so we pay for the Meraki call once.
func handleConfigurationChangesAnnotation(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("configurationChangesAnnotation: orgId is required")
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

	changes, err := client.GetOrganizationConfigurationChanges(ctx, q.OrgID, opts, configurationChangesAnnotationTTL)
	if err != nil {
		return nil, err
	}

	var (
		times  []time.Time
		titles []string
		texts  []string
		tags   []string
	)
	for _, ch := range changes {
		var t time.Time
		if ch.TS != nil {
			t = ch.TS.UTC()
		}
		times = append(times, t)

		// Title: "<adminName> — <label>" reads naturally on a single-line
		// annotation marker; we fall back to admin email or ID if the friendly
		// name is absent (API keys, revoked admins).
		actor := ch.AdminName
		if actor == "" {
			actor = ch.AdminEmail
		}
		if actor == "" {
			actor = ch.AdminID
		}
		label := ch.Label
		if label == "" {
			label = ch.Page
		}
		title := label
		if actor != "" {
			title = actor + " — " + label
		}
		titles = append(titles, title)

		// Text: before → after, on one line, trimmed to keep the hover popover
		// readable. Grafana's annotation popover supports multi-line text but
		// trimming avoids a full-screen overlay when someone updates an ACL.
		body := ch.OldValue + " → " + ch.NewValue
		if len(body) > 400 {
			body = body[:400] + "…"
		}
		texts = append(texts, body)

		// Tags: comma-separated (Grafana's annotation data-frame contract).
		// networkId present for network-scoped changes, absent for org-level
		// — either way we emit the `page` tag so operators can filter by
		// area of the dashboard that was touched.
		var tagParts []string
		if ch.Page != "" {
			tagParts = append(tagParts, "page:"+ch.Page)
		}
		if ch.NetworkID != "" {
			tagParts = append(tagParts, "network:"+ch.NetworkID)
		}
		tags = append(tags, strings.Join(tagParts, ","))
	}

	return []*data.Frame{
		data.NewFrame("configuration_changes_annotation",
			data.NewField("time", nil, times),
			data.NewField("title", nil, titles),
			data.NewField("text", nil, texts),
			data.NewField("tags", nil, tags),
		),
	}, nil
}
