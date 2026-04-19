package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleConfigurationChangesTimeline buckets the same changes returned by
// handleConfigurationChanges into a wide, numeric frame `{ts, <page1>, <page2>, ...}`.
// Each page-value becomes its own int64 column whose entries count the changes that fell
// into each time bucket. Empty buckets are materialised with zeros so the stacked bar
// chart gets a continuous x-axis even in quiet windows.
//
// Why a dedicated handler (and not a client-side transform): the original panel wired a
// `groupingToMatrix` transform with `valueField: 'page'` — the matrix cells ended up as
// STRINGS, which made the timeseries viz report "data is missing a number field" and
// render a blank chart. This mirrors the events-timeline pattern (see events_timeline.go)
// which replaced an identical client-side shape for exactly the same reason.
//
// Bucket sizing reuses `eventsTimelineBucket` to stay consistent with the other audit-
// surface panels (Events, Alerts timelines), so a user switching between them sees the
// same granularity for the same time window.
func handleConfigurationChangesTimeline(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("configurationChangesTimeline: orgId is required")
	}

	opts := meraki.ConfigurationChangesOptions{
		AdminID: firstNonEmpty(q.Metrics),
	}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if !from.IsZero() {
		opts.TSStart = &from
	}
	if !to.IsZero() {
		opts.TSEnd = &to
	}

	changes, err := client.GetOrganizationConfigurationChanges(ctx, q.OrgID, opts, configurationChangesTTL)
	if err != nil {
		return nil, err
	}

	bucket := eventsTimelineBucket(from, to)
	bucketStarts := makeBucketStarts(from, to, bucket)

	counts := make(map[time.Time]map[string]int64, len(bucketStarts))
	for _, b := range bucketStarts {
		counts[b] = map[string]int64{}
	}
	pageSet := map[string]struct{}{}
	for _, ch := range changes {
		if ch.TS == nil {
			continue
		}
		page := ch.Page
		if page == "" {
			page = "(unspecified)"
		}
		pageSet[page] = struct{}{}
		b := floorToBucket(ch.TS.UTC(), bucket, from)
		row, ok := counts[b]
		if !ok {
			continue
		}
		row[page]++
	}

	pages := make([]string, 0, len(pageSet))
	for p := range pageSet {
		pages = append(pages, p)
	}
	sort.Strings(pages)

	tsCol := make([]time.Time, 0, len(bucketStarts))
	tsCol = append(tsCol, bucketStarts...)

	pageCols := make(map[string][]int64, len(pages))
	for _, p := range pages {
		pageCols[p] = make([]int64, len(bucketStarts))
	}
	for i, b := range bucketStarts {
		row := counts[b]
		for _, p := range pages {
			pageCols[p][i] = row[p]
		}
	}

	fields := make([]*data.Field, 0, 1+len(pages))
	fields = append(fields, data.NewField("ts", nil, tsCol))
	for _, p := range pages {
		fields = append(fields, data.NewField(p, nil, pageCols[p]))
	}

	// When there are no buckets (inverted/zero range) or no observed pages, emit a
	// single-column frame so the panel still gets a numeric field to render with. The
	// timeseries viz needs at least one non-time number field; without this it falls
	// back to the same "missing number field" error the old transform produced.
	if len(pages) == 0 {
		fields = append(fields, data.NewField("changes", nil, make([]int64, len(bucketStarts))))
	}

	return []*data.Frame{data.NewFrame("configuration_changes_timeline", fields...)}, nil
}
