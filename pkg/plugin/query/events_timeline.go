package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleNetworkEventsTimeline fetches the same events as handleNetworkEvents,
// but aggregates them into a barchart-ready wide frame: one `ts` column plus
// one int64 column per observed event category. This replaces a client-side
// `groupingToMatrix` transform that emitted string cells and confused the
// barchart viz ("No numeric fields found").
//
// Bucket size is derived from the panel time range — see eventsTimelineBucket.
// Empty buckets are materialised with zeros so the barchart gets a continuous
// x-axis instead of gaps between observed events.
func handleNetworkEventsTimeline(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	networkIDs, truncated, err := resolveNetworkEventsTargets(ctx, client, q)
	if err != nil {
		return nil, err
	}
	if len(networkIDs) == 0 {
		return nil, fmt.Errorf("networkEventsTimeline: at least one networkId is required")
	}

	reqOpts := meraki.NetworkEventsOptions{
		IncludedEventTypes: q.Metrics,
	}
	if len(q.ProductTypes) > 0 {
		reqOpts.ProductType = q.ProductTypes[0]
	}
	if len(q.Serials) > 0 {
		reqOpts.DeviceSerial = q.Serials[0]
	}
	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if !from.IsZero() {
		reqOpts.TSStart = &from
	}
	if !to.IsZero() {
		reqOpts.TSEnd = &to
	}

	var events []meraki.NetworkEvent
	for _, networkID := range networkIDs {
		got, err := client.GetNetworkEvents(ctx, networkID, reqOpts, networkEventsTTL)
		if err != nil {
			return nil, err
		}
		events = append(events, got...)
	}

	// Build bucket list covering the full panel span so the barchart's x-axis
	// doesn't skip over quiet windows.
	bucket := eventsTimelineBucket(from, to)
	bucketStarts := makeBucketStarts(from, to, bucket)

	// Count events per (bucket, category). Collect the full category set so
	// the output schema is stable.
	counts := make(map[time.Time]map[string]int64, len(bucketStarts))
	for _, b := range bucketStarts {
		counts[b] = map[string]int64{}
	}
	catSet := map[string]struct{}{}
	for _, e := range events {
		if e.OccurredAt == nil {
			continue
		}
		cat := e.Category
		if cat == "" {
			cat = "(uncategorised)"
		}
		catSet[cat] = struct{}{}
		b := floorToBucket(e.OccurredAt.UTC(), bucket, from)
		row, ok := counts[b]
		if !ok {
			// Event outside the panel range (shouldn't happen — the API
			// honours tsStart/tsEnd — but be defensive).
			continue
		}
		row[cat]++
	}

	categories := make([]string, 0, len(catSet))
	for c := range catSet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	// Materialise fields. ts is always present; one int64 field per category.
	tsCol := make([]time.Time, 0, len(bucketStarts))
	tsCol = append(tsCol, bucketStarts...)

	catCols := make(map[string][]int64, len(categories))
	for _, c := range categories {
		catCols[c] = make([]int64, len(bucketStarts))
	}
	for i, b := range bucketStarts {
		row := counts[b]
		for _, c := range categories {
			catCols[c][i] = row[c]
		}
	}

	fields := make([]*data.Field, 0, 1+len(categories))
	fields = append(fields, data.NewField("ts", nil, tsCol))
	for _, c := range categories {
		fields = append(fields, data.NewField(c, nil, catCols[c]))
	}

	frame := data.NewFrame("network_events_timeline", fields...)
	if truncated {
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     fmt.Sprintf("Events truncated: queried only the first %d networks in this organisation. Pick a specific network for the full feed.", networkEventsAllFanoutCap),
		})
	}
	return []*data.Frame{frame}, nil
}

// eventsTimelineBucket returns a reasonable bucket size for the given span.
// Matches what an operator would expect: 5m buckets when zoomed in to a few
// hours, 1h buckets across a day or two, 1d buckets across weeks.
func eventsTimelineBucket(from, to time.Time) time.Duration {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return time.Hour
	}
	span := to.Sub(from)
	switch {
	case span <= 6*time.Hour:
		return 5 * time.Minute
	case span <= 48*time.Hour:
		return time.Hour
	default:
		return 24 * time.Hour
	}
}

// makeBucketStarts enumerates every bucket start in [from, to), anchored to
// `from`. Returns an empty slice when the range is zero or inverted.
func makeBucketStarts(from, to time.Time, bucket time.Duration) []time.Time {
	if from.IsZero() || to.IsZero() || !to.After(from) || bucket <= 0 {
		return nil
	}
	out := make([]time.Time, 0, int(to.Sub(from)/bucket)+1)
	for t := from; t.Before(to); t = t.Add(bucket) {
		out = append(out, t)
	}
	return out
}

// floorToBucket snaps `t` down to the bucket start closest to `from`. When
// `from` is zero the bucket is anchored to the unix epoch, which keeps unit
// tests deterministic without requiring a panel range.
func floorToBucket(t time.Time, bucket time.Duration, from time.Time) time.Time {
	anchor := from
	if anchor.IsZero() {
		anchor = time.Unix(0, 0).UTC()
	}
	delta := t.Sub(anchor)
	if delta < 0 {
		return anchor
	}
	n := int64(delta / bucket)
	return anchor.Add(time.Duration(n) * bucket)
}
