package meraki

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// /administered/* endpoint wrappers. These are scoped to the API key's
// identity (not a specific org) so they don't take an organizationId in the
// path, although `GetAdministeredLicensingSubscriptions` accepts
// `organizationIds[]` as a required query param.
//
// Shapes verified via ctx7 / developer.cisco.com on 2026-04-18.

// AdministeredIdentity is the response of `GET /administered/identities/me`.
// The API authentication block is preserved as-is so downstream UI can
// surface twoFactor/SAML state without this wrapper having to reshape it.
type AdministeredIdentity struct {
	Name                string                        `json:"name,omitempty"`
	Email               string                        `json:"email,omitempty"`
	LastUsedDashboardAt *time.Time                    `json:"lastUsedDashboardAt,omitempty"`
	Authentication      *AdministeredIdentityAuth     `json:"authentication,omitempty"`
}

// AdministeredIdentityAuth mirrors the nested `authentication` block on the
// identities/me response.
type AdministeredIdentityAuth struct {
	Mode      string                             `json:"mode,omitempty"`
	API       *AdministeredIdentityAPI           `json:"api,omitempty"`
	TwoFactor *AdministeredIdentityTwoFactor     `json:"twoFactor,omitempty"`
	SAML      *AdministeredIdentitySAML          `json:"saml,omitempty"`
}

type AdministeredIdentityAPI struct {
	Key struct {
		Created bool `json:"created"`
	} `json:"key"`
}

type AdministeredIdentityTwoFactor struct {
	Enabled bool `json:"enabled"`
}

type AdministeredIdentitySAML struct {
	Enabled bool `json:"enabled"`
}

// GetAdministeredIdentitiesMe returns the identity of the API key's owner.
// Not paginated. Used by CheckHealth to surface the user's email in the
// AppConfig "Connection" card so operators can confirm they're talking to
// the right tenant.
func (c *Client) GetAdministeredIdentitiesMe(ctx context.Context, ttl time.Duration) (*AdministeredIdentity, error) {
	var out AdministeredIdentity
	if err := c.Get(ctx, "administered/identities/me", "", nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AdministeredSubscription is one entry in the response of
// `GET /administered/licensing/subscription/subscriptions`. Covers orgs on
// the newer subscription licensing model (as opposed to per-device
// licensing); populates the fallback path in `handleLicensesOverview` /
// `handleLicensesList` when `/licenses` returns a 400 for such orgs.
type AdministeredSubscription struct {
	SubscriptionID  string                           `json:"subscriptionId"`
	Name            string                           `json:"name,omitempty"`
	Description     string                           `json:"description,omitempty"`
	Status          string                           `json:"status,omitempty"`
	StartDate       *time.Time                       `json:"startDate,omitempty"`
	EndDate         *time.Time                       `json:"endDate,omitempty"`
	LastUpdatedAt   *time.Time                       `json:"lastUpdatedAt,omitempty"`
	WebOrderID      string                           `json:"webOrderId,omitempty"`
	Type            string                           `json:"type,omitempty"`
	RenewalRequested bool                            `json:"renewalRequested,omitempty"`
	ProductTypes    []string                         `json:"productTypes,omitempty"`
	Entitlements    []AdministeredSubscriptionEntitlement `json:"entitlements,omitempty"`
	Counts          *AdministeredSubscriptionCounts  `json:"counts,omitempty"`
}

// AdministeredSubscriptionEntitlement is one SKU entitlement with its seat
// distribution across networks. Meraki's overview endpoint flattens these
// into per-subscription seat counts.
type AdministeredSubscriptionEntitlement struct {
	SKU   string                         `json:"sku"`
	Seats AdministeredSubscriptionSeats  `json:"seats"`
}

// AdministeredSubscriptionSeats is the seat-count block nested on each
// entitlement.
type AdministeredSubscriptionSeats struct {
	Assigned  int64 `json:"assigned"`
	Available int64 `json:"available"`
	Limit     int64 `json:"limit"`
}

// AdministeredSubscriptionCounts mirrors the top-level `counts` block
// returned alongside entitlements. Not every subscription payload includes
// it; pointer type lets the handler detect absence.
type AdministeredSubscriptionCounts struct {
	Seats         AdministeredSubscriptionSeats `json:"seats"`
	Networks      int64                         `json:"networks"`
	Organizations int64                         `json:"organizations"`
}

// AdministeredSubscriptionOptions filters the paged subscriptions feed.
// OrganizationIDs is REQUIRED by Meraki — the handler asserts it before
// calling through.
type AdministeredSubscriptionOptions struct {
	OrganizationIDs []string
	SubscriptionIDs []string
	Statuses        []string
	ProductTypes    []string
}

func (o AdministeredSubscriptionOptions) values() url.Values {
	v := url.Values{"perPage": []string{"1000"}}
	for _, id := range o.OrganizationIDs {
		v.Add("organizationIds[]", id)
	}
	for _, id := range o.SubscriptionIDs {
		v.Add("subscriptionIds[]", id)
	}
	for _, s := range o.Statuses {
		v.Add("statuses[]", s)
	}
	for _, p := range o.ProductTypes {
		v.Add("productTypes[]", p)
	}
	return v
}

// GetAdministeredLicensingSubscriptions returns every subscription the API
// key's organizations hold. Paginated via Link header. The caller MUST pass
// at least one organizationId — Meraki returns a 400 otherwise. Cache is
// keyed on the full option set so different filter combinations don't share
// entries.
func (c *Client) GetAdministeredLicensingSubscriptions(ctx context.Context, opts AdministeredSubscriptionOptions, ttl time.Duration) ([]AdministeredSubscription, error) {
	if len(opts.OrganizationIDs) == 0 {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "administered/licensing/subscription/subscriptions", Message: "at least one organizationId is required"}}
	}
	var out []AdministeredSubscription
	// The endpoint is not org-scoped in the path; pass an empty orgID so the
	// cache key falls back to the global namespace. The query-param filter
	// still narrows to the requested orgs.
	if _, err := c.GetAll(ctx,
		"administered/licensing/subscription/subscriptions",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// IsSubscriptionLicensingError reports whether the given error looks like
// Meraki's "this org uses subscription licensing" rejection of /licenses or
// /licenses/overview. Matching is done on the error message/body because
// Meraki doesn't surface a dedicated error code for this case; we check for
// the word "subscription" (case-insensitive) plus a 400 status to avoid
// false positives on unrelated 400 responses.
func IsSubscriptionLicensingError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if !errorsAsAPI(err, &apiErr) {
		return false
	}
	if apiErr.Status != 400 {
		return false
	}
	haystack := strings.ToLower(apiErr.Message)
	if strings.Contains(haystack, "subscription") {
		return true
	}
	for _, e := range apiErr.Errors {
		if strings.Contains(strings.ToLower(e), "subscription") {
			return true
		}
	}
	return false
}

// AdministeredSeatSummary is a subscription-derived licensing KPI bucket.
// `handleLicensesOverview` uses this when falling back to subscription data
// to synthesise the same (active, expiring30, expired, total) shape that a
// per-device-licensed org would return from /licenses/overview.
type AdministeredSeatSummary struct {
	Active     int64
	Expiring30 int64
	Expired    int64
	Total      int64
	// EarliestExpiration is the minimum endDate across currently-active
	// subscriptions — gives the licensing UI something to surface as the
	// "next renewal" date in place of the coterm expiration.
	EarliestExpiration time.Time
}

// SummariseSeats collapses a subscription list into a single KPI row. Kept
// here (rather than in the handler) so the Go unit tests can exercise the
// bucketing logic in isolation from the frame-emission path.
//
// Bucket rules:
//   - Active: subscription Status=="active" AND EndDate is either nil or in
//     the future.
//   - Expired: subscription EndDate is in the past OR Status=="expired".
//   - Expiring30: Active AND EndDate within 30 days of `now`.
//   - Total: seat count across every entitlement's `limit` field, summed
//     over every subscription regardless of status (mirrors what the
//     overview endpoint reports as `total` for PDL orgs).
func SummariseSeats(subs []AdministeredSubscription, now time.Time) AdministeredSeatSummary {
	var s AdministeredSeatSummary
	thirtyDaysOut := now.Add(30 * 24 * time.Hour)
	for _, sub := range subs {
		seats := seatLimit(sub)
		s.Total += seats

		expired := false
		if sub.EndDate != nil && sub.EndDate.Before(now) {
			expired = true
		}
		if strings.EqualFold(sub.Status, "expired") {
			expired = true
		}
		if expired {
			s.Expired += seats
			continue
		}

		active := strings.EqualFold(sub.Status, "active") ||
			strings.EqualFold(sub.Status, "out_of_compliance")
		if !active {
			continue
		}
		s.Active += seats
		if sub.EndDate != nil {
			if sub.EndDate.Before(thirtyDaysOut) {
				s.Expiring30 += seats
			}
			if s.EarliestExpiration.IsZero() || sub.EndDate.Before(s.EarliestExpiration) {
				s.EarliestExpiration = sub.EndDate.UTC()
			}
		}
	}
	return s
}

// seatLimit returns the best-available seat-limit count for one subscription.
// Prefers the top-level `counts.seats.limit`, falling back to summing
// entitlement limits when the top-level block is absent.
func seatLimit(sub AdministeredSubscription) int64 {
	if sub.Counts != nil && sub.Counts.Seats.Limit > 0 {
		return sub.Counts.Seats.Limit
	}
	var sum int64
	for _, e := range sub.Entitlements {
		sum += e.Seats.Limit
	}
	return sum
}

// errorsAsAPI unwraps any of the typed Meraki errors to the embedded
// APIError — used by IsSubscriptionLicensingError to inspect status+body
// regardless of which typed subclass wraps the APIError.
func errorsAsAPI(err error, out **APIError) bool {
	// Walk typed subclasses first; the embedded APIError is what callers
	// introspect. errors.As against *APIError does NOT auto-unwrap the typed
	// subclasses because they only embed the value — we match on each type.
	if u, ok := err.(*UnauthorizedError); ok {
		*out = &u.APIError
		return true
	}
	if n, ok := err.(*NotFoundError); ok {
		*out = &n.APIError
		return true
	}
	if r, ok := err.(*RateLimitError); ok {
		*out = &r.APIError
		return true
	}
	if sErr, ok := err.(*ServerError); ok {
		*out = &sErr.APIError
		return true
	}
	if p, ok := err.(*PartialSuccessError); ok {
		*out = &p.APIError
		return true
	}
	if apiErr, ok := err.(*APIError); ok {
		*out = apiErr
		return true
	}
	return false
}

// Ensure strconv is imported even if no current call site consumes it — keeps
// the file self-contained if later edits pull in numeric query params (e.g.
// startDate range filters use [lt]/[gt]/[lte]/[gte] suffixes on the same
// field).
var _ = strconv.Itoa
