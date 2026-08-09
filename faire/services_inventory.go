package faire

import (
	"context"
	"net/http"
	"net/url"
)

// InventoryService provides product-variant inventory operations.
type InventoryService struct {
	client *Client
}

// GetByVariantIDs retrieves inventory levels for the comma-separated variant IDs required by Faire.
func (s *InventoryService) GetByVariantIDs(ctx context.Context, ids string) (*InventoryResponse, error) {
	query := url.Values{"ids": []string{ids}}
	var inventory InventoryResponse
	if err := s.client.doJSON(ctx, http.MethodGet, "/product-inventory/by-product-variant-ids", query, nil, &inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}

// UpdateByVariantIDs replaces on-hand inventory levels identified by product variant IDs.
func (s *InventoryService) UpdateByVariantIDs(ctx context.Context, request UpdateOnHandInventoryRequest) (*UpdateOnHandInventoryResponse, error) {
	var response UpdateOnHandInventoryResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, "/product-inventory/by-product-variant-ids", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// GetBySKUs retrieves inventory levels for the comma-separated SKUs required by Faire.
func (s *InventoryService) GetBySKUs(ctx context.Context, skus string) (*InventoryResponse, error) {
	query := url.Values{"skus": []string{skus}}
	var inventory InventoryResponse
	if err := s.client.doJSON(ctx, http.MethodGet, "/product-inventory/by-skus", query, nil, &inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}

// UpdateBySKUs replaces on-hand inventory levels identified by SKUs.
func (s *InventoryService) UpdateBySKUs(ctx context.Context, request UpdateOnHandInventoryRequest) (*UpdateOnHandInventoryResponse, error) {
	var response UpdateOnHandInventoryResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, "/product-inventory/by-skus", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateSellingLevelsByVariantIDs updates the current selling inventory, discontinuation, and backorder state by variant ID.
func (s *InventoryService) UpdateSellingLevelsByVariantIDs(ctx context.Context, request UpdateVariantInventoryRequest) (*UpdateVariantInventoryResponse, error) {
	var response UpdateVariantInventoryResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, "/products/variants/inventory-levels-by-product-variant-ids", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateSellingLevelsBySKUs updates the current selling inventory, discontinuation, and backorder state by SKU.
func (s *InventoryService) UpdateSellingLevelsBySKUs(ctx context.Context, request UpdateVariantInventoryRequest) (*UpdateVariantInventoryResponse, error) {
	var response UpdateVariantInventoryResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, "/products/variants/inventory-levels-by-skus", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// PricesService provides regional product-variant price operations.
type PricesService struct {
	client *Client
}

// UpdateByVariantIDs updates regional prices for product variants identified by Faire variant IDs.
func (s *PricesService) UpdateByVariantIDs(ctx context.Context, request UpdatePricesByVariantIDRequest) (*UpdatePricesResponse, error) {
	var response UpdatePricesResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, "/product-prices/by-product-variant-ids", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateBySKUs updates regional prices for product variants identified by SKU.
func (s *PricesService) UpdateBySKUs(ctx context.Context, request UpdatePricesBySKURequest) (*UpdatePricesResponse, error) {
	var response UpdatePricesResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, "/product-prices/by-skus", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
