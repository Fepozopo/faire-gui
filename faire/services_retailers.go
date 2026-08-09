package faire

import (
	"context"
	"net/http"
	"net/url"
)

// RetailersService provides read-only retailer profile operations.
type RetailersService struct {
	client *Client
}

// GetPublicProfile retrieves the public profile for a retailer by its Faire ID.
func (s *RetailersService) GetPublicProfile(ctx context.Context, retailerID RetailerID) (*RetailerProfile, error) {
	path := "/retailers/public/" + url.PathEscape(string(retailerID))
	var profile RetailerProfile
	if err := s.client.doJSON(ctx, http.MethodGet, path, nil, nil, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}
