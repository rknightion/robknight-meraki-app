// Package query implements the Meraki-specific query handlers that translate
// MerakiQuery objects (sent by the nested datasource) into Grafana data.Frames.
//
// The dispatcher is deliberately shallow: each QueryKind maps to one handler
// function that talks to the Meraki API via the shared meraki.Client owned by
// the app plugin. Per-query errors are captured as frame notices so a single
// misconfigured panel doesn't kill every other panel in the request batch.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// QueryKind enumerates the Meraki-backed operations the dispatcher knows how
// to run. The string values are the wire-format discriminator shared with
// src/datasource/types.ts (MerakiQuery.kind).
type QueryKind string

const (
	KindOrganizations         QueryKind = "organizations"
	KindNetworks              QueryKind = "networks"
	KindDevices               QueryKind = "devices"
	KindDeviceStatusOverview  QueryKind = "deviceStatusOverview"
	KindSensorReadingsLatest  QueryKind = "sensorReadingsLatest"
	KindSensorReadingsHistory QueryKind = "sensorReadingsHistory"
	KindSensorAlertSummary    QueryKind = "sensorAlertSummary"
)

// MerakiQuery mirrors the TypeScript MerakiQuery shape. It is the per-panel
// payload inside QueryRequest.Queries.
type MerakiQuery struct {
	RefID           string    `json:"refId"`
	Kind            QueryKind `json:"kind"`
	OrgID           string    `json:"orgId,omitempty"`
	NetworkIDs      []string  `json:"networkIds,omitempty"`
	Serials         []string  `json:"serials,omitempty"`
	ProductTypes    []string  `json:"productTypes,omitempty"`
	Metrics         []string  `json:"metrics,omitempty"`
	TimespanSeconds int       `json:"timespanSeconds,omitempty"`
	Hide            bool      `json:"hide,omitempty"`
}

// TimeRange is Grafana's panel time range in unix milliseconds (same encoding
// as backend.DataQuery.TimeRange once JSON-serialized by the datasource).
type TimeRange struct {
	From int64 `json:"from"` // unix ms
	To   int64 `json:"to"`   // unix ms
}

// QueryRequest is the POST body sent to /resources/query.
type QueryRequest struct {
	Range         TimeRange     `json:"range"`
	MaxDataPoints int64         `json:"maxDataPoints"`
	IntervalMs    int64         `json:"intervalMs"`
	Queries       []MerakiQuery `json:"queries"`
}

// QueryResponse is the wire shape returned to the datasource. Each frame has
// already been serialized via data.FrameToJSON so the datasource can forward
// it to Grafana without re-parsing.
type QueryResponse struct {
	Frames []json.RawMessage `json:"frames"`
}

// MetricFindRequest is the POST body sent to /resources/metricFind. Variable
// queries always carry a single MerakiQuery.
type MetricFindRequest struct {
	Query MerakiQuery `json:"query"`
}

// MetricFindValue is one {text, value} pair returned by a variable query.
// Value is interface{} so we can return strings (most common) or numbers.
type MetricFindValue struct {
	Text  string      `json:"text"`
	Value interface{} `json:"value,omitempty"`
}

// MetricFindResponse is the wire shape returned to the datasource.
type MetricFindResponse struct {
	Values []MetricFindValue `json:"values"`
}

// handlerFn is the common signature every per-kind handler implements. Handlers return one or
// more frames so that long-format data (e.g. sensor history) can be split into per-series
// frames with labels — Grafana's timeseries panel infers series from labeled value fields, so
// a single long-format frame renders as a flat table instead of a chart.
type handlerFn func(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange) ([]*data.Frame, error)

// handlers maps a QueryKind to its implementation. Kept in one place so the
// dispatcher logic stays tiny.
var handlers = map[QueryKind]handlerFn{
	KindOrganizations:         handleOrganizations,
	KindNetworks:              handleNetworks,
	KindDevices:               handleDevices,
	KindDeviceStatusOverview:  handleDeviceStatusOverview,
	KindSensorReadingsLatest:  handleSensorReadingsLatest,
	KindSensorReadingsHistory: handleSensorReadingsHistory,
	KindSensorAlertSummary:    handleSensorAlertSummary,
}

// Handle dispatches each MerakiQuery in req.Queries to its handler and
// aggregates the serialized frames. A per-query failure is captured as a
// notice on a synthetic error frame (named "<refId>_error") so one bad query
// does not blank the whole panel. Returns an error only when the request
// envelope itself is malformed (nil req, unknown kind, etc.).
func Handle(ctx context.Context, client *meraki.Client, req *QueryRequest) (*QueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("query: nil request")
	}
	if client == nil {
		return nil, fmt.Errorf("query: meraki client not configured")
	}
	resp := &QueryResponse{Frames: make([]json.RawMessage, 0, len(req.Queries))}
	for _, q := range req.Queries {
		if q.Hide {
			continue
		}
		frames, err := runOne(ctx, client, q, req.Range)
		if len(frames) == 0 {
			// Handler returned (nil/empty, err) — manufacture an error frame so
			// the panel still gets a visible notice rather than a blank
			// response. When err is also nil we still emit an empty-but-named
			// frame so consumers can key by RefID.
			frames = []*data.Frame{data.NewFrame(errorFrameName(q.RefID))}
		}
		if err != nil {
			// Attach the error as a notice on the first frame only — repeating
			// it on every frame would clutter the UI. First frame wins because
			// Grafana surfaces notices from the primary frame by default.
			frames[0].AppendNotices(data.Notice{
				Severity: data.NoticeSeverityError,
				Text:     err.Error(),
			})
		}
		for _, frame := range frames {
			frame.RefID = q.RefID
			raw, marshalErr := data.FrameToJSON(frame, data.IncludeAll)
			if marshalErr != nil {
				// Extremely unlikely — fall back to a stub error frame so the
				// response is still structurally valid.
				stub := data.NewFrame(errorFrameName(q.RefID))
				stub.RefID = q.RefID
				stub.AppendNotices(data.Notice{
					Severity: data.NoticeSeverityError,
					Text:     fmt.Sprintf("failed to serialize frame: %v", marshalErr),
				})
				raw, _ = data.FrameToJSON(stub, data.IncludeAll)
			}
			resp.Frames = append(resp.Frames, raw)
		}
	}
	return resp, nil
}

// runOne looks up the handler for q.Kind and invokes it. Unknown kinds become
// errors so the caller can turn them into frame notices.
func runOne(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange) ([]*data.Frame, error) {
	h, ok := handlers[q.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown query kind %q", q.Kind)
	}
	return h(ctx, client, q, tr)
}

// HandleMetricFind runs a single variable-hydration query. Unlike Handle,
// failures bubble up as plain errors because variable queries have no frame
// concept to attach notices to.
func HandleMetricFind(ctx context.Context, client *meraki.Client, req *MetricFindRequest) (*MetricFindResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("metricFind: nil request")
	}
	if client == nil {
		return nil, fmt.Errorf("metricFind: meraki client not configured")
	}
	return runMetricFind(ctx, client, req.Query)
}

func errorFrameName(refID string) string {
	if refID == "" {
		return "error"
	}
	return refID + "_error"
}

// toRFCTime converts a unix-ms epoch to a UTC time.Time. Zero input returns
// the zero time so callers can detect "not set".
func toRFCTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
