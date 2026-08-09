package faire

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// BrandsService provides operations on the brand associated with the current credentials.
type BrandsService struct {
	client *Client
}

// Profile retrieves the current brand's profile.
func (s *BrandsService) Profile(ctx context.Context) (*BrandProfile, error) {
	var profile BrandProfile
	if err := s.client.doJSON(ctx, http.MethodGet, "/brands/profile", nil, nil, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// OrdersService provides operations for listing and fulfilling Faire brand orders.
type OrdersService struct {
	client *Client
}

// List retrieves one filtered, cursor-paginated page of orders.
func (s *OrdersService) List(ctx context.Context, options *OrderListOptions) (*OrderPage, error) {
	query := orderListQuery(options)
	var page OrderPage
	if err := s.client.doJSON(ctx, http.MethodGet, "/orders", query, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// Get retrieves an order by its Faire ID.
func (s *OrdersService) Get(ctx context.Context, orderID OrderID) (*Order, error) {
	var order Order
	if err := s.client.doJSON(ctx, http.MethodGet, orderPath(orderID), nil, nil, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// Cancel cancels an order using the supplied cancellation reason and optional note.
func (s *OrdersService) Cancel(ctx context.Context, orderID OrderID, request CancelOrderRequest) (*Order, error) {
	var order Order
	if err := s.client.doJSON(ctx, http.MethodPut, orderPath(orderID)+"/cancel", nil, request, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateItemsAvailability updates the availability status of variants in an order.
func (s *OrdersService) UpdateItemsAvailability(ctx context.Context, orderID OrderID, request UpdateOrderItemsAvailabilityRequest) (*Order, error) {
	var order Order
	if err := s.client.doJSON(ctx, http.MethodPost, orderPath(orderID)+"/items/availability", nil, request, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// DownloadPackingSlipPDF retrieves the order packing slip PDF in the requested IANA time zone and reports close failures.
func (s *OrdersService) DownloadPackingSlipPDF(ctx context.Context, orderID OrderID, timezone string) (pdf []byte, err error) {
	query := url.Values{}
	query.Set("timezone", timezone)
	response, err := s.client.do(ctx, http.MethodGet, orderPath(orderID)+"/packing-slip-pdf", query, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		// Preserve a read error because it explains why the returned PDF is incomplete.
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("faire: close packing slip PDF response: %w", closeErr)
		}
	}()

	pdf, err = io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("faire: read packing slip PDF: %w", err)
	}
	return pdf, nil
}

// MoveToProcessing moves a new order into the processing state.
func (s *OrdersService) MoveToProcessing(ctx context.Context, orderID OrderID, request MoveOrderToProcessingRequest) (*Order, error) {
	var order Order
	if err := s.client.doJSON(ctx, http.MethodPut, orderPath(orderID)+"/processing", nil, request, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// AddShipments adds shipment and tracking information to an order.
func (s *OrdersService) AddShipments(ctx context.Context, orderID OrderID, request AddShipmentsRequest) (*Order, error) {
	var order Order
	if err := s.client.doJSON(ctx, http.MethodPost, orderPath(orderID)+"/shipments", nil, request, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// orderListQuery translates optional order-list controls into Faire query parameters.
func orderListQuery(options *OrderListOptions) url.Values {
	if options == nil {
		return nil
	}
	query := url.Values{}
	setInt64(query, "limit", options.Limit)
	setInt64(query, "page", options.Page)
	setString(query, "updated_at_min", options.UpdatedAtMin)
	if len(options.ExcludedStates) > 0 {
		states := make([]string, len(options.ExcludedStates))
		for index, state := range options.ExcludedStates {
			states[index] = string(state)
		}
		query.Set("excluded_states", joinCommaSeparated(states))
	}
	setString(query, "ship_after_max", options.ShipAfterMax)
	setString(query, "created_at_min", options.CreatedAtMin)
	if options.SortBy != nil {
		query.Set("sort_by", string(*options.SortBy))
	}
	setString(query, "cursor", options.Cursor)
	if options.OriginalOrderID != nil {
		query.Set("original_order_id", string(*options.OriginalOrderID))
	}
	return query
}

// orderPath returns the escaped API path for an order.
func orderPath(orderID OrderID) string {
	return "/orders/" + url.PathEscape(string(orderID))
}

// setInt64 adds a query parameter when value is present.
func setInt64(query url.Values, name string, value *int64) {
	if value != nil {
		query.Set(name, strconv.FormatInt(*value, 10))
	}
}

// setString adds a query parameter when value is present.
func setString(query url.Values, name string, value *string) {
	if value != nil {
		query.Set(name, *value)
	}
}

// joinCommaSeparated joins values for Faire query parameters that accept a comma-separated string.
func joinCommaSeparated(values []string) string {
	return joinStrings(values, ",")
}

// joinStrings joins values without importing strings in each service implementation.
func joinStrings(values []string, separator string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += separator + value
	}
	return result
}
