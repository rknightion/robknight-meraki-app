package meraki

import (
	"context"
	"net/url"
	"time"
)

// Network mirrors the fields the plugin consumes from `GET /organizations/{orgId}/networks`.
type Network struct {
	ID                   string   `json:"id"`
	OrganizationID       string   `json:"organizationId"`
	Name                 string   `json:"name"`
	ProductTypes         []string `json:"productTypes"`
	TimeZone             string   `json:"timeZone,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	EnrollmentString     string   `json:"enrollmentString,omitempty"`
	URL                  string   `json:"url,omitempty"`
	Notes                string   `json:"notes,omitempty"`
	IsBoundToConfigTemplate bool  `json:"isBoundToConfigTemplate,omitempty"`
}

// GetOrganizationNetworks paginates through every network for the given org.
// productTypes, if non-empty, is added as a repeated `productTypes[]` filter.
func (c *Client) GetOrganizationNetworks(ctx context.Context, orgID string, productTypes []string, ttl time.Duration) ([]Network, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/networks", Message: "missing organization id"}}
	}
	params := url.Values{"perPage": []string{"1000"}}
	for _, pt := range productTypes {
		params.Add("productTypes[]", pt)
	}
	var networks []Network
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/networks",
		orgID, params, ttl, &networks)
	if err != nil {
		return nil, err
	}
	return networks, nil
}
