package meraki

import (
	"context"
	"net/url"
	"time"
)

// Organization mirrors the subset of `GET /organizations` fields that the plugin uses.
type Organization struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	URL             string   `json:"url,omitempty"`
	API             APIInfo  `json:"api,omitempty"`
	Licensing       License  `json:"licensing,omitempty"`
	Cloud           Cloud    `json:"cloud,omitempty"`
	Management      Manage   `json:"management,omitempty"`
	DevicesCount    int      `json:"-"`
	NetworksCount   int      `json:"-"`
	ProductTypes    []string `json:"-"`
}

type APIInfo struct {
	Enabled bool `json:"enabled"`
}

type License struct {
	Model string `json:"model,omitempty"`
}

type Cloud struct {
	Region Region `json:"region,omitempty"`
}

type Region struct {
	Name string `json:"name,omitempty"`
	Host Host   `json:"host,omitempty"`
}

type Host struct {
	Name string `json:"name,omitempty"`
}

type Manage struct {
	Details []ManageDetail `json:"details,omitempty"`
}

type ManageDetail struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GetOrganizations fetches every organization accessible to the API key.
// TTL controls caching; pass 0 to force a live fetch.
func (c *Client) GetOrganizations(ctx context.Context, ttl time.Duration) ([]Organization, error) {
	var orgs []Organization
	_, err := c.GetAll(ctx, "organizations", "", url.Values{"perPage": []string{"1000"}}, ttl, &orgs)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

// GetOrganization fetches a single organization. Returns a NotFoundError if missing.
func (c *Client) GetOrganization(ctx context.Context, orgID string, ttl time.Duration) (*Organization, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}", Message: "missing organization id"}}
	}
	var org Organization
	if err := c.Get(ctx, "organizations/"+url.PathEscape(orgID), orgID, nil, ttl, &org); err != nil {
		return nil, err
	}
	return &org, nil
}
