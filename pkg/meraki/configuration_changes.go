package meraki

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Configuration-changes endpoint wrapper (todos.txt §7.3-C). Path, pagination mode, and
// response shape verified via ctx7 against the canonical OpenAPI spec and the Cisco Meraki
// developer docs on 2026-04-18.
//
// Coverage:
//   - /organizations/{organizationId}/configurationChanges (Link-header paginated)
//
// Why this endpoint matters: Meraki explicitly calls it out as a low-cost alternative to
// frequent polling for "what changed?" questions. Panels backed by this endpoint surface
// an organisation-wide Change Log (who changed what, from which page or API endpoint,
// with the before/after values) — useful for incident review and drift detection without
// round-tripping every config endpoint on every dashboard refresh.

// ConfigurationChange is one row of the configurationChanges log. Field set mirrors
// Meraki's documented v1 response: an array of flat objects with JSON-encoded before/after
// value strings. We keep oldValue/newValue as raw strings (not pre-decoded) so the UI can
// render whatever makes sense — full JSON tree, single-line diff, or a "changed N fields"
// tally — without losing information.
type ConfigurationChange struct {
	TS         *time.Time `json:"ts,omitempty"`
	AdminName  string     `json:"adminName,omitempty"`
	AdminEmail string     `json:"adminEmail,omitempty"`
	AdminID    string     `json:"adminId,omitempty"`
	Page       string     `json:"page,omitempty"`
	Label      string     `json:"label,omitempty"`
	OldValue   string     `json:"oldValue,omitempty"`
	NewValue   string     `json:"newValue,omitempty"`
	// NetworkID is present on network-scoped changes (missing for org-level ones). We keep
	// it because the filters scene variable binds to it.
	NetworkID string `json:"networkId,omitempty"`
}

// ConfigurationChangesOptions filters the configurationChanges call. All fields are
// optional — the Meraki endpoint returns the last 365 days when unspecified, which is too
// much to render; handlers must set a narrower TSStart/TSEnd derived from the panel time
// range.
type ConfigurationChangesOptions struct {
	TSStart   *time.Time
	TSEnd     *time.Time
	NetworkID string
	AdminID   string
	// PerPage is clamped to Meraki's documented 3-100000 range; default 5000 gives a
	// comfortable page size for Link-header walks without blowing up the JSON buffer.
	PerPage int
}

func (o ConfigurationChangesOptions) values() url.Values {
	v := url.Values{}
	per := o.PerPage
	if per <= 0 {
		per = 5000
	}
	if per < 3 {
		per = 3
	}
	if per > 100000 {
		per = 100000
	}
	v.Set("perPage", strconv.Itoa(per))
	if o.TSStart != nil && !o.TSStart.IsZero() {
		v.Set("t0", o.TSStart.UTC().Format(time.RFC3339))
	}
	if o.TSEnd != nil && !o.TSEnd.IsZero() {
		v.Set("t1", o.TSEnd.UTC().Format(time.RFC3339))
	}
	if o.NetworkID != "" {
		v.Set("networkId", o.NetworkID)
	}
	if o.AdminID != "" {
		v.Set("adminId", o.AdminID)
	}
	return v
}

// GetOrganizationConfigurationChanges walks the Link-header-paginated change log for the
// given org. perPage defaults to 5000 so a typical 1-day window is served in a single
// page; older / busier orgs will pick up multiple pages via client.GetAll.
func (c *Client) GetOrganizationConfigurationChanges(ctx context.Context, orgID string, opts ConfigurationChangesOptions, ttl time.Duration) ([]ConfigurationChange, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/configurationChanges", Message: "missing organization id"}}
	}
	var out []ConfigurationChange
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/configurationChanges",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
