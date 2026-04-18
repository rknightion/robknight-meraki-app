package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Phase 9 (Licensing / API Usage / Clients) handlers — the "Insights" family.
//
// Most of these endpoints are org-wide aggregates with short TTLs: API usage
// data refreshes every minute on Meraki's side, so caching for 60s keeps a
// dashboard responsive without hammering the backend. Licensing is long-lived
// (15 minutes); the KPI tiles update once every few panel refreshes, which is
// plenty for a resource-usage page.
const (
	licensesOverviewTTL      = 15 * time.Minute
	licensesListTTL          = 15 * time.Minute
	subscriptionsTTL         = 15 * time.Minute
	apiRequestsOverviewTTL   = 1 * time.Minute
	apiRequestsByIntervalTTL = 5 * time.Minute
	clientsOverviewTTL       = 5 * time.Minute
	topTTL                   = 5 * time.Minute

	clientsOverviewEndpoint       = "organizations/{organizationId}/clients/overview"
	apiRequestsByIntervalEndpoint = "organizations/{organizationId}/apiRequests/overview/responseCodes/byInterval"

	// apiRequestsMaxTimespan mirrors the documented 31-day cap on the
	// apiRequests/overview endpoint. We clamp locally so the server never
	// rejects with a 400 when the user picks a longer dashboard range.
	apiRequestsMaxTimespan = 31 * 24 * time.Hour

	// topDefaultTimespanSeconds is the default lookback for /summary/top/*
	// handlers when the caller leaves q.TimespanSeconds at 0.
	topDefaultTimespanSeconds = 86400
)

// handleLicensesOverview emits a single wide frame summarising org licensing
// state regardless of whether the org is on co-term or per-device licensing.
// We compute the same six KPI columns from either payload shape so the UI
// doesn't need to branch — downstream panels just bind to the column names.
//
// Co-term accounting: total = sum of licensedDeviceCounts; active = total when
// Status=="OK" (co-term orgs don't expose an expiring bucket at the overview
// level — the dashboard surfaces `cotermExpiration` as a separate field for
// the user to act on).
//
// Per-device accounting: read the five non-"unused" buckets directly. We
// intentionally leave `unused` out of `total` because an unused license isn't
// "in use" anywhere — that matches how the Meraki dashboard reports it.
func handleLicensesOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("licensesOverview: orgId is required")
	}
	overview, err := client.GetOrganizationLicensesOverview(ctx, q.OrgID, licensesOverviewTTL)
	if err != nil {
		// Subscription-licensed orgs return HTTP 400 from /licenses/overview.
		// Fall back to /administered/licensing/subscription/subscriptions and
		// synthesise the same KPI columns by bucketing subscriptions on end
		// date. Any other error is surfaced unchanged — the caller will turn
		// it into a frame notice.
		if meraki.IsSubscriptionLicensingError(err) {
			return subscriptionLicensesOverviewFrame(ctx, client, q.OrgID)
		}
		return nil, err
	}

	var (
		active, expiring30, expired, recentlyQueued, unusedActive, total int64
		coterm           bool
		cotermExpiration time.Time
		cotermStatus     string
	)

	if overview != nil {
		if overview.IsCoterm() {
			coterm = true
			cotermStatus = overview.Status
			if overview.ExpirationDate != nil {
				cotermExpiration = overview.ExpirationDate.UTC()
			}
			for _, c := range overview.LicensedDeviceCounts {
				total += c
			}
			// Co-term orgs don't expose an expiring bucket at the overview
			// level. We treat everything as active when Status=="OK" so the
			// KPI row reads "N active, 0 expired" — matching the Meraki
			// dashboard's own summary for a healthy co-term org.
			if overview.Status == "OK" {
				active = total
			}
		} else if overview.States != nil {
			active = overview.States.Active.Count
			expired = overview.States.Expired.Count
			expiring30 = overview.States.Expiring.Count
			recentlyQueued = overview.States.RecentlyQueued.Count
			unusedActive = overview.States.UnusedActive.Count
			total = active + expired + expiring30 + recentlyQueued + unusedActive
		}
	}

	frame := data.NewFrame("licenses_overview",
		data.NewField("active", nil, []int64{active}),
		data.NewField("expiring30", nil, []int64{expiring30}),
		data.NewField("expired", nil, []int64{expired}),
		data.NewField("recentlyQueued", nil, []int64{recentlyQueued}),
		data.NewField("unusedActive", nil, []int64{unusedActive}),
		data.NewField("total", nil, []int64{total}),
		data.NewField("coterm", nil, []bool{coterm}),
		data.NewField("cotermExpiration", nil, []time.Time{cotermExpiration}),
		data.NewField("cotermStatus", nil, []string{cotermStatus}),
	)
	// If /licenses/overview succeeded but returned an empty body, it's
	// almost always because the org is subscription-licensed but the endpoint
	// hasn't fully propagated the 400 we now handle above. Probe the fallback
	// regardless — if it returns data, swap to the synthesised frame; if it
	// 400s with a different error we emit the original empty frame with a
	// diagnostic notice.
	if !coterm && total == 0 {
		if subs, subErr := client.GetAdministeredLicensingSubscriptions(ctx, meraki.AdministeredSubscriptionOptions{OrganizationIDs: []string{q.OrgID}}, subscriptionsTTL); subErr == nil && len(subs) > 0 {
			return subscriptionFrameFromSubs(subs), nil
		}
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "No data returned by /organizations/{id}/licenses/overview or the subscription fallback. If this org is managed via LicenseHub / subscription, check that the API key has read scope on /administered/licensing.",
		})
	}
	return []*data.Frame{frame}, nil
}

// subscriptionLicensesOverviewFrame is the fallback path for orgs on the
// newer subscription licensing model. Shape mirrors the /licenses/overview
// frame so panels bind to the same field names regardless of which licensing
// model the org uses.
func subscriptionLicensesOverviewFrame(ctx context.Context, client *meraki.Client, orgID string) ([]*data.Frame, error) {
	subs, err := client.GetAdministeredLicensingSubscriptions(ctx, meraki.AdministeredSubscriptionOptions{OrganizationIDs: []string{orgID}}, subscriptionsTTL)
	if err != nil {
		return nil, err
	}
	return subscriptionFrameFromSubs(subs), nil
}

// subscriptionFrameFromSubs builds the licenses_overview frame from a
// subscription slice. Kept separate from the HTTP call so tests can exercise
// bucketing logic without stubbing the network.
func subscriptionFrameFromSubs(subs []meraki.AdministeredSubscription) []*data.Frame {
	summary := meraki.SummariseSeats(subs, time.Now().UTC())
	frame := data.NewFrame("licenses_overview",
		data.NewField("active", nil, []int64{summary.Active}),
		data.NewField("expiring30", nil, []int64{summary.Expiring30}),
		data.NewField("expired", nil, []int64{summary.Expired}),
		data.NewField("recentlyQueued", nil, []int64{int64(0)}),
		data.NewField("unusedActive", nil, []int64{int64(0)}),
		data.NewField("total", nil, []int64{summary.Total}),
		data.NewField("coterm", nil, []bool{false}),
		data.NewField("cotermExpiration", nil, []time.Time{summary.EarliestExpiration}),
		data.NewField("cotermStatus", nil, []string{"subscription"}),
	)
	frame.AppendNotices(data.Notice{
		Severity: data.NoticeSeverityInfo,
		Text:     "Licensing KPIs synthesised from /administered/licensing/subscription/subscriptions — this organization uses the subscription licensing model.",
	})
	return []*data.Frame{frame}
}

// handleLicensesList emits a table frame of per-device licenses. Co-term orgs
// don't have a per-device list (the `/licenses` endpoint 400s on co-term orgs),
// so we probe the overview first and short-circuit with an explanatory notice
// when we detect co-term.
//
// Filter plumbing: mirrors handleAlerts precedent — q.Metrics[0] is the state
// filter (active|expired|…), q.Serials[0] the deviceSerial, q.NetworkIDs[0]
// the networkId. Meraki's endpoint accepts a single value per filter, not
// repeated slices.
func handleLicensesList(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("licensesList: orgId is required")
	}

	// Quick probe: if the org is co-term, there's no list to fetch.
	if overview, err := client.GetOrganizationLicensesOverview(ctx, q.OrgID, licensesOverviewTTL); err == nil && overview != nil && overview.IsCoterm() {
		frame := emptyLicensesFrame()
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "Organization uses co-termination licensing; see overview KPIs.",
		})
		return []*data.Frame{frame}, nil
	}

	opts := meraki.LicenseListOptions{
		State: firstNonEmpty(q.Metrics),
	}
	if len(q.Serials) > 0 {
		opts.DeviceSerial = q.Serials[0]
	}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}

	licenses, err := client.GetOrganizationLicenses(ctx, q.OrgID, opts, licensesListTTL)
	if err != nil {
		// Subscription-licensed orgs reject /licenses with HTTP 400. Fall
		// back to the administered subscription list and emit a per-
		// subscription row in the same column shape — `licenseType` carries
		// the subscription type (enterpriseAgreement / termed), `state` the
		// subscription status, and `deviceSerial` is left blank because
		// subscriptions span an org, not a device.
		if meraki.IsSubscriptionLicensingError(err) {
			return subscriptionLicensesListFrame(ctx, client, q.OrgID)
		}
		return nil, err
	}

	var (
		ids              []string
		licenseTypes     []string
		states           []string
		serials          []string
		networkIDs       []string
		seatCounts       []int64
		activations      []time.Time
		expirations      []time.Time
		headLicenseIDs   []string
		daysUntilExpiry  []int64
	)
	now := time.Now().UTC()
	for _, l := range licenses {
		ids = append(ids, l.ID)
		licenseTypes = append(licenseTypes, l.LicenseType)
		states = append(states, l.State)
		serials = append(serials, l.DeviceSerial)
		networkIDs = append(networkIDs, l.NetworkID)
		seatCounts = append(seatCounts, l.SeatCount)

		if l.ActivationDate != nil {
			activations = append(activations, l.ActivationDate.UTC())
		} else {
			activations = append(activations, time.Time{})
		}
		if l.ExpirationDate != nil {
			exp := l.ExpirationDate.UTC()
			expirations = append(expirations, exp)
			daysUntilExpiry = append(daysUntilExpiry, int64(exp.Sub(now)/(24*time.Hour)))
		} else {
			expirations = append(expirations, time.Time{})
			// -1 sentinel: permanent / unknown expiration. The UI can render
			// this as "—" without needing to check for a null time.
			daysUntilExpiry = append(daysUntilExpiry, -1)
		}
		headLicenseIDs = append(headLicenseIDs, l.HeadLicenseID)
	}

	return []*data.Frame{
		data.NewFrame("licenses_list",
			data.NewField("id", nil, ids),
			data.NewField("licenseType", nil, licenseTypes),
			data.NewField("state", nil, states),
			data.NewField("deviceSerial", nil, serials),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("seatCount", nil, seatCounts),
			data.NewField("activationDate", nil, activations),
			data.NewField("expirationDate", nil, expirations),
			data.NewField("headLicenseId", nil, headLicenseIDs),
			data.NewField("daysUntilExpiry", nil, daysUntilExpiry),
		),
	}, nil
}

// subscriptionLicensesListFrame builds a licenses_list-shaped frame from the
// administered subscription endpoint so panels keep binding to the same
// column set for subscription-licensed orgs. `deviceSerial` stays blank
// because subscriptions are org-scoped; `networkId` carries subscription
// name instead so the UI still has a readable secondary label; `state` maps
// to subscription Status; `expirationDate` maps to subscription EndDate.
func subscriptionLicensesListFrame(ctx context.Context, client *meraki.Client, orgID string) ([]*data.Frame, error) {
	subs, err := client.GetAdministeredLicensingSubscriptions(ctx, meraki.AdministeredSubscriptionOptions{OrganizationIDs: []string{orgID}}, subscriptionsTTL)
	if err != nil {
		return nil, err
	}

	var (
		ids              []string
		licenseTypes     []string
		states           []string
		serials          []string
		networkIDs       []string
		seatCounts       []int64
		activations      []time.Time
		expirations      []time.Time
		headLicenseIDs   []string
		daysUntilExpiry  []int64
	)
	now := time.Now().UTC()
	for _, s := range subs {
		ids = append(ids, s.SubscriptionID)
		licenseTypes = append(licenseTypes, s.Type)
		states = append(states, s.Status)
		serials = append(serials, "")
		// Use the subscription name as a secondary identifier since
		// subscriptions aren't scoped to a network. Keeps panels that
		// previously keyed on `networkId` from breaking.
		networkIDs = append(networkIDs, s.Name)

		var seat int64
		if s.Counts != nil {
			seat = s.Counts.Seats.Limit
		}
		if seat == 0 {
			for _, e := range s.Entitlements {
				seat += e.Seats.Limit
			}
		}
		seatCounts = append(seatCounts, seat)

		if s.StartDate != nil {
			activations = append(activations, s.StartDate.UTC())
		} else {
			activations = append(activations, time.Time{})
		}
		if s.EndDate != nil {
			exp := s.EndDate.UTC()
			expirations = append(expirations, exp)
			daysUntilExpiry = append(daysUntilExpiry, int64(exp.Sub(now)/(24*time.Hour)))
		} else {
			expirations = append(expirations, time.Time{})
			daysUntilExpiry = append(daysUntilExpiry, -1)
		}
		headLicenseIDs = append(headLicenseIDs, "")
	}

	frame := data.NewFrame("licenses_list",
		data.NewField("id", nil, ids),
		data.NewField("licenseType", nil, licenseTypes),
		data.NewField("state", nil, states),
		data.NewField("deviceSerial", nil, serials),
		data.NewField("networkId", nil, networkIDs),
		data.NewField("seatCount", nil, seatCounts),
		data.NewField("activationDate", nil, activations),
		data.NewField("expirationDate", nil, expirations),
		data.NewField("headLicenseId", nil, headLicenseIDs),
		data.NewField("daysUntilExpiry", nil, daysUntilExpiry),
	)
	frame.AppendNotices(data.Notice{
		Severity: data.NoticeSeverityInfo,
		Text:     "Subscription-licensed org — rows sourced from /administered/licensing/subscription/subscriptions. `networkId` column carries the subscription name; `deviceSerial` is blank because subscriptions are org-scoped.",
	})
	return []*data.Frame{frame}, nil
}

// emptyLicensesFrame constructs the zero-row licenses_list frame with the
// full column set. Used for the co-term notice path so the UI binds the same
// columns regardless of which branch emitted the frame.
func emptyLicensesFrame() *data.Frame {
	return data.NewFrame("licenses_list",
		data.NewField("id", nil, []string{}),
		data.NewField("licenseType", nil, []string{}),
		data.NewField("state", nil, []string{}),
		data.NewField("deviceSerial", nil, []string{}),
		data.NewField("networkId", nil, []string{}),
		data.NewField("seatCount", nil, []int64{}),
		data.NewField("activationDate", nil, []time.Time{}),
		data.NewField("expirationDate", nil, []time.Time{}),
		data.NewField("headLicenseId", nil, []string{}),
		data.NewField("daysUntilExpiry", nil, []int64{}),
	)
}

// handleApiRequestsOverview aggregates the response-code tallies into four
// KPI buckets (success2xx, clientError4xx, tooMany429, serverError5xx) plus a
// total — matching the alerts_overview KPI wide-frame pattern. We separate
// 429 out of the 4xx bucket because rate-limit visibility is the primary
// reason this page exists; bundling it into "client errors" would hide it.
func handleApiRequestsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("apiRequestsOverview: orgId is required")
	}

	timespan := time.Duration(0)
	if tr.From > 0 && tr.To > tr.From {
		timespan = time.Duration(tr.To-tr.From) * time.Millisecond
	}
	if timespan <= 0 {
		timespan = 24 * time.Hour
	}
	if timespan > apiRequestsMaxTimespan {
		timespan = apiRequestsMaxTimespan
	}

	overview, err := client.GetOrganizationApiRequestsOverview(ctx, q.OrgID, timespan, apiRequestsOverviewTTL)
	if err != nil {
		return nil, err
	}

	var total, success2xx, clientError4xx, tooMany429, serverError5xx int64
	if overview != nil {
		for codeStr, count := range overview.ResponseCodeCounts {
			code := parseInt(codeStr)
			if code == 0 {
				continue
			}
			total += count
			switch {
			case code >= 200 && code < 300:
				success2xx += count
			case code == 429:
				tooMany429 += count
			case code >= 400 && code < 500:
				clientError4xx += count
			case code >= 500 && code < 600:
				serverError5xx += count
			}
		}
	}

	return []*data.Frame{
		data.NewFrame("api_requests_overview",
			data.NewField("total", nil, []int64{total}),
			data.NewField("success2xx", nil, []int64{success2xx}),
			data.NewField("clientError4xx", nil, []int64{clientError4xx}),
			data.NewField("tooMany429", nil, []int64{tooMany429}),
			data.NewField("serverError5xx", nil, []int64{serverError5xx}),
		),
	}, nil
}

// handleApiRequestsByInterval emits one timeseries frame per HTTP status
// class (2xx, 4xx, 429, 5xx) so Grafana's timeseries viz can colour each
// class distinctly. Empty classes are elided — a panel asking for this data
// probably wants to see the 4xx/5xx spikes, and extra zero-valued series
// clutter the legend without adding information.
//
// Per-class emission follows the G.18 convention: one frame per series with
// Labels{"class": "<class>"} on the value field and DisplayNameFromDS baked
// to the class name. A single long-format frame would render as an empty
// chart here.
func handleApiRequestsByInterval(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("apiRequestsByInterval: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("apiRequestsByInterval: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[apiRequestsByIntervalEndpoint]
	if !ok {
		return nil, fmt.Errorf("apiRequestsByInterval: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("apiRequestsByInterval: resolve window: %w", err)
	}

	opts := meraki.ApiRequestsByIntervalOptions{
		Window:   &window,
		Interval: window.Resolution,
	}
	entries, err := client.GetOrganizationApiRequestsByInterval(ctx, q.OrgID, opts, apiRequestsByIntervalTTL)
	if err != nil {
		return nil, err
	}

	// Group by class in chronological order. Using a canonical class set keeps
	// frame order deterministic (tests depend on it) and lets us elide empty
	// series without a second pass.
	type classBuf struct {
		ts    []time.Time
		value []int64
	}
	classes := map[string]*classBuf{
		"2xx": {},
		"4xx": {},
		"429": {},
		"5xx": {},
	}

	// Sort entries by start time so the frames we emit are already ordered.
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartTs.Before(entries[j].StartTs) })

	for _, e := range entries {
		bucketSums := map[string]int64{}
		for _, c := range e.Counts {
			class := classifyHTTPStatus(c.Code)
			if class == "" {
				continue
			}
			bucketSums[class] += c.Count
		}
		ts := e.StartTs.UTC()
		// Every class gets a data point per interval, even if zero — that
		// keeps timeseries panels from showing gaps that look like outages.
		for class, buf := range classes {
			buf.ts = append(buf.ts, ts)
			buf.value = append(buf.value, bucketSums[class])
		}
	}

	// Deterministic frame order for tests and stable legends.
	orderedClasses := []string{"2xx", "4xx", "429", "5xx"}
	frames := make([]*data.Frame, 0, len(orderedClasses))
	for _, class := range orderedClasses {
		buf := classes[class]
		// Elide a class entirely when no interval had any matching codes.
		// A non-empty class with all zeroes still emits — see note above.
		if !classHasAnyDataPoint(buf.value) {
			continue
		}
		labels := data.Labels{"class": class}
		valueField := data.NewField("value", labels, buf.value)
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: class,
		}
		frames = append(frames, data.NewFrame("api_requests_by_interval",
			data.NewField("ts", nil, buf.ts),
			valueField,
		))
	}

	if len(frames) == 0 {
		// Empty frame so panels bind successfully with "No data".
		return []*data.Frame{data.NewFrame("api_requests_by_interval",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []int64{}),
		)}, nil
	}
	return frames, nil
}

// handleClientsOverview emits a single wide frame with the client count and
// the six usage KPIs. All usage values stay in kb per Meraki's spec — the UI
// can apply Grafana's "kilobytes" unit for rendering.
func handleClientsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("clientsOverview: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("clientsOverview: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[clientsOverviewEndpoint]
	if !ok {
		return nil, fmt.Errorf("clientsOverview: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("clientsOverview: resolve window: %w", err)
	}

	opts := meraki.ClientsOverviewOptions{
		Window:     &window,
		Resolution: window.Resolution,
	}
	overview, err := client.GetOrganizationClientsOverview(ctx, q.OrgID, opts, clientsOverviewTTL)
	if err != nil {
		return nil, err
	}

	var (
		totalClients         int64
		usageTotalKb         float64
		usageDownstreamKb    float64
		usageUpstreamKb      float64
		avgUsageTotalKb      float64
		avgUsageDownstreamKb float64
		avgUsageUpstreamKb   float64
	)
	if overview != nil {
		totalClients = overview.Counts.Total
		usageTotalKb = overview.Usage.Overall.Total
		usageDownstreamKb = overview.Usage.Overall.Downstream
		usageUpstreamKb = overview.Usage.Overall.Upstream
		avgUsageTotalKb = overview.Usage.Average.Total
		avgUsageDownstreamKb = overview.Usage.Average.Downstream
		avgUsageUpstreamKb = overview.Usage.Average.Upstream
	}

	return []*data.Frame{
		data.NewFrame("clients_overview",
			data.NewField("totalClients", nil, []int64{totalClients}),
			data.NewField("usageTotalKb", nil, []float64{usageTotalKb}),
			data.NewField("usageDownstreamKb", nil, []float64{usageDownstreamKb}),
			data.NewField("usageUpstreamKb", nil, []float64{usageUpstreamKb}),
			data.NewField("avgUsageTotalKb", nil, []float64{avgUsageTotalKb}),
			data.NewField("avgUsageDownstreamKb", nil, []float64{avgUsageDownstreamKb}),
			data.NewField("avgUsageUpstreamKb", nil, []float64{avgUsageUpstreamKb}),
		),
	}, nil
}

// topTimespanFromQuery returns the timespan to request from a /summary/top/*
// endpoint. We read q.TimespanSeconds so the frontend can pass a fixed lookback
// independent of the panel's dashboard time range (these summaries don't have
// a timeseries dimension — the timespan is purely about how far back to
// aggregate).
func topTimespanFromQuery(q MerakiQuery) time.Duration {
	seconds := q.TimespanSeconds
	if seconds <= 0 {
		seconds = topDefaultTimespanSeconds
	}
	return time.Duration(seconds) * time.Second
}

func handleTopClients(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topClients: orgId is required")
	}
	opts := meraki.TopOptions{Timespan: topTimespanFromQuery(q)}
	clients, err := client.GetOrganizationTopClientsByUsage(ctx, q.OrgID, opts, topTTL)
	if err != nil {
		return nil, err
	}

	var (
		names       []string
		ids         []string
		macs        []string
		networkIDs  []string
		networkNms  []string
		usageSent   []float64
		usageRecv   []float64
		usageTotal  []float64
	)
	for _, c := range clients {
		names = append(names, c.Name)
		ids = append(ids, c.ID)
		macs = append(macs, c.MAC)
		networkIDs = append(networkIDs, c.Network.ID)
		networkNms = append(networkNms, c.Network.Name)
		usageSent = append(usageSent, c.Usage.Sent)
		usageRecv = append(usageRecv, c.Usage.Recv)
		usageTotal = append(usageTotal, c.Usage.Total)
	}

	return []*data.Frame{
		data.NewFrame("top_clients",
			data.NewField("name", nil, names),
			data.NewField("id", nil, ids),
			data.NewField("mac", nil, macs),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNms),
			data.NewField("usageSent", nil, usageSent),
			data.NewField("usageRecv", nil, usageRecv),
			data.NewField("usageTotal", nil, usageTotal),
		),
	}, nil
}

// handleTopDevices emits one row per top device. We also compute a drilldown
// URL per row so the UI can link directly to the device detail page — the URL
// is stable across refreshes and doesn't require the client to round-trip
// through a product-type lookup.
func handleTopDevices(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topDevices: orgId is required")
	}
	topOpts := meraki.TopOptions{Timespan: topTimespanFromQuery(q)}
	devices, err := client.GetOrganizationTopDevicesByUsage(ctx, q.OrgID, topOpts, topTTL)
	if err != nil {
		return nil, err
	}

	var (
		names        []string
		serials      []string
		macs         []string
		models       []string
		networkIDs   []string
		networkNms   []string
		productTypes []string
		usageSent    []float64
		usageRecv    []float64
		usageTotal   []float64
		drilldowns   []string
	)
	for _, d := range devices {
		names = append(names, d.Name)
		serials = append(serials, d.Serial)
		macs = append(macs, d.MAC)
		models = append(models, d.Model)
		networkIDs = append(networkIDs, d.Network.ID)
		networkNms = append(networkNms, d.Network.Name)
		productTypes = append(productTypes, d.ProductType)
		usageSent = append(usageSent, d.Usage.Sent)
		usageRecv = append(usageRecv, d.Usage.Recv)
		usageTotal = append(usageTotal, d.Usage.Total)
		drilldowns = append(drilldowns, deviceDrilldownURL(opts.PluginPathPrefix, d.ProductType, d.Serial))
	}

	return []*data.Frame{
		data.NewFrame("top_devices",
			data.NewField("name", nil, names),
			data.NewField("serial", nil, serials),
			data.NewField("mac", nil, macs),
			data.NewField("model", nil, models),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNms),
			data.NewField("productType", nil, productTypes),
			data.NewField("usageSent", nil, usageSent),
			data.NewField("usageRecv", nil, usageRecv),
			data.NewField("usageTotal", nil, usageTotal),
			data.NewField("drilldownUrl", nil, drilldowns),
		),
	}, nil
}

func handleTopDeviceModels(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topDeviceModels: orgId is required")
	}
	opts := meraki.TopOptions{Timespan: topTimespanFromQuery(q)}
	models, err := client.GetOrganizationTopDeviceModelsByUsage(ctx, q.OrgID, opts, topTTL)
	if err != nil {
		return nil, err
	}

	var (
		modelCol      []string
		counts        []int64
		usageTotal    []float64
		usageDownstream []float64
		usageUpstream   []float64
	)
	for _, m := range models {
		modelCol = append(modelCol, m.Model)
		counts = append(counts, m.Count)
		usageTotal = append(usageTotal, m.Usage.Total)
		usageDownstream = append(usageDownstream, m.Usage.Downstream)
		usageUpstream = append(usageUpstream, m.Usage.Upstream)
	}

	return []*data.Frame{
		data.NewFrame("top_device_models",
			data.NewField("model", nil, modelCol),
			data.NewField("count", nil, counts),
			data.NewField("usageTotal", nil, usageTotal),
			data.NewField("usageDownstream", nil, usageDownstream),
			data.NewField("usageUpstream", nil, usageUpstream),
		),
	}, nil
}

func handleTopSsids(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topSsids: orgId is required")
	}
	opts := meraki.TopOptions{Timespan: topTimespanFromQuery(q)}
	ssids, err := client.GetOrganizationTopSsidsByUsage(ctx, q.OrgID, opts, topTTL)
	if err != nil {
		return nil, err
	}

	var (
		names            []string
		clientCounts     []int64
		usageTotal       []float64
		usageDownstream  []float64
		usageUpstream    []float64
		usagePercentage  []float64
	)
	for _, s := range ssids {
		names = append(names, s.Name)
		clientCounts = append(clientCounts, s.Clients.Counts.Total)
		usageTotal = append(usageTotal, s.Usage.Total)
		usageDownstream = append(usageDownstream, s.Usage.Downstream)
		usageUpstream = append(usageUpstream, s.Usage.Upstream)
		usagePercentage = append(usagePercentage, s.Usage.Percentage)
	}

	return []*data.Frame{
		data.NewFrame("top_ssids",
			data.NewField("name", nil, names),
			data.NewField("clients", nil, clientCounts),
			data.NewField("usageTotal", nil, usageTotal),
			data.NewField("usageDownstream", nil, usageDownstream),
			data.NewField("usageUpstream", nil, usageUpstream),
			data.NewField("usagePercentage", nil, usagePercentage),
		),
	}, nil
}

// handleTopSwitchesByEnergy converts the raw joules the spec returns into
// kWh on emission (joules / 3,600,000). Downstream panels can then apply
// Grafana's "Energy / kilowatt-hour" unit without a scaling transform.
func handleTopSwitchesByEnergy(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topSwitchesByEnergy: orgId is required")
	}
	topOpts := meraki.TopOptions{Timespan: topTimespanFromQuery(q)}
	switches, err := client.GetOrganizationTopSwitchesByEnergyUsage(ctx, q.OrgID, topOpts, topTTL)
	if err != nil {
		return nil, err
	}

	var (
		names       []string
		serials     []string
		models      []string
		networkIDs  []string
		networkNms  []string
		energyKwh   []float64
		drilldowns  []string
	)
	for _, s := range switches {
		names = append(names, s.Name)
		serials = append(serials, s.Serial)
		models = append(models, s.Model)
		networkIDs = append(networkIDs, s.Network.ID)
		networkNms = append(networkNms, s.Network.Name)
		energyKwh = append(energyKwh, s.Usage.Total/3_600_000.0)
		drilldowns = append(drilldowns, deviceDrilldownURL(opts.PluginPathPrefix, "switch", s.Serial))
	}

	return []*data.Frame{
		data.NewFrame("top_switches_by_energy",
			data.NewField("name", nil, names),
			data.NewField("serial", nil, serials),
			data.NewField("model", nil, models),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNms),
			data.NewField("energyKwh", nil, energyKwh),
			data.NewField("drilldownUrl", nil, drilldowns),
		),
	}, nil
}

func handleTopNetworksByStatus(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topNetworksByStatus: orgId is required")
	}
	opts := meraki.TopOptions{Timespan: topTimespanFromQuery(q)}
	networks, err := client.GetOrganizationTopNetworksByStatus(ctx, q.OrgID, opts, topTTL)
	if err != nil {
		return nil, err
	}

	var (
		networkIDs      []string
		networkNames    []string
		overallStatus   []string
		wan1Status      []string
		wan2Status      []string
		productTypes    []string
		totalClients    []int64
		usageTotal      []float64
		usageDownstream []float64
		usageUpstream   []float64
	)
	for _, n := range networks {
		networkIDs = append(networkIDs, n.NetworkID)
		networkNames = append(networkNames, n.NetworkName)
		overallStatus = append(overallStatus, n.Statuses.OverallStatus)
		wan1Status = append(wan1Status, n.Statuses.Wan1Status)
		wan2Status = append(wan2Status, n.Statuses.Wan2Status)
		productTypes = append(productTypes, strings.Join(n.ProductTypes, ","))
		totalClients = append(totalClients, n.Clients.Counts.Total)
		usageTotal = append(usageTotal, n.Usage.Total)
		usageDownstream = append(usageDownstream, n.Usage.Downstream)
		usageUpstream = append(usageUpstream, n.Usage.Upstream)
	}

	return []*data.Frame{
		data.NewFrame("top_networks_by_status",
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("overallStatus", nil, overallStatus),
			data.NewField("wan1Status", nil, wan1Status),
			data.NewField("wan2Status", nil, wan2Status),
			data.NewField("productTypes", nil, productTypes),
			data.NewField("totalClients", nil, totalClients),
			data.NewField("usageTotal", nil, usageTotal),
			data.NewField("usageDownstream", nil, usageDownstream),
			data.NewField("usageUpstream", nil, usageUpstream),
		),
	}, nil
}

// classifyHTTPStatus maps a response code to one of the four KPI classes we
// surface (2xx, 4xx excluding 429, 429, 5xx). Codes outside these ranges
// return "" so the caller can skip them.
func classifyHTTPStatus(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code == 429:
		return "429"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return ""
	}
}

// classHasAnyDataPoint returns true when at least one sample in the class
// buffer is non-zero. Used to elide classes that never observed any traffic
// during the window — cleaner legend, no information lost.
func classHasAnyDataPoint(values []int64) bool {
	for _, v := range values {
		if v > 0 {
			return true
		}
	}
	return false
}

// parseInt returns the int form of a decimal string, or 0 on failure. The
// response code map carries keys like "200" / "429"; anything non-numeric
// (e.g. "total" from a partial-shape response) is ignored by the caller.
func parseInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
