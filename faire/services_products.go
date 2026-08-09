package faire

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ProductsService provides product, variant, image, review, taxonomy, and prepack operations.
type ProductsService struct {
	client *Client
}

// List retrieves one filtered, cursor-paginated page of products.
func (s *ProductsService) List(ctx context.Context, options *ProductListOptions) (*ProductPage, error) {
	var page ProductPage
	if err := s.client.doJSON(ctx, http.MethodGet, "/products", productListQuery(options), nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// Create creates one product and its initial variants.
func (s *ProductsService) Create(ctx context.Context, product Product) (*Product, error) {
	var created Product
	if err := s.client.doJSON(ctx, http.MethodPost, "/products", nil, product, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// Get retrieves a product by its Faire ID.
func (s *ProductsService) Get(ctx context.Context, productID ProductID) (*Product, error) {
	var product Product
	if err := s.client.doJSON(ctx, http.MethodGet, productPath(productID), nil, nil, &product); err != nil {
		return nil, err
	}
	return &product, nil
}

// Update patches a product by its Faire ID.
func (s *ProductsService) Update(ctx context.Context, productID ProductID, product Product) (*Product, error) {
	var updated Product
	if err := s.client.doJSON(ctx, http.MethodPatch, productPath(productID), nil, product, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete permanently deletes a product by its Faire ID.
func (s *ProductsService) Delete(ctx context.Context, productID ProductID) error {
	return s.client.doJSON(ctx, http.MethodDelete, productPath(productID), nil, nil, nil)
}

// DeleteImage removes an image from a product.
func (s *ProductsService) DeleteImage(ctx context.Context, productID ProductID, imageID ImageID) error {
	path := productPath(productID) + "/images/" + url.PathEscape(string(imageID))
	return s.client.doJSON(ctx, http.MethodDelete, path, nil, nil, nil)
}

// ListPrepacks retrieves all prepacks for a product.
func (s *ProductsService) ListPrepacks(ctx context.Context, productID ProductID) (*PrepackListResponse, error) {
	var response PrepackListResponse
	if err := s.client.doJSON(ctx, http.MethodGet, productPath(productID)+"/prepacks", nil, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreatePrepack creates one prepack for a product.
func (s *ProductsService) CreatePrepack(ctx context.Context, productID ProductID, prepack Prepack) (*Prepack, error) {
	var created Prepack
	if err := s.client.doJSON(ctx, http.MethodPost, productPath(productID)+"/prepacks", nil, prepack, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// CreatePrepacks creates multiple prepacks for a product in one request.
func (s *ProductsService) CreatePrepacks(ctx context.Context, productID ProductID, request CreatePrepacksRequest) (*CreatePrepacksResponse, error) {
	var response CreatePrepacksResponse
	if err := s.client.doJSON(ctx, http.MethodPost, productPath(productID)+"/prepacks/batch", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// GetPrepack retrieves a prepack by product and prepack ID.
func (s *ProductsService) GetPrepack(ctx context.Context, productID ProductID, prepackID PrepackID) (*Prepack, error) {
	var prepack Prepack
	if err := s.client.doJSON(ctx, http.MethodGet, prepackPath(productID, prepackID), nil, nil, &prepack); err != nil {
		return nil, err
	}
	return &prepack, nil
}

// DeletePrepack deletes one prepack from a product.
func (s *ProductsService) DeletePrepack(ctx context.Context, productID ProductID, prepackID PrepackID) error {
	return s.client.doJSON(ctx, http.MethodDelete, prepackPath(productID, prepackID), nil, nil, nil)
}

// UpdateVariantOptionSets replaces the available variant-option values for a product.
func (s *ProductsService) UpdateVariantOptionSets(ctx context.Context, productID ProductID, request PatchVariantOptionSetsRequest) (*PatchVariantOptionSetsResponse, error) {
	var response PatchVariantOptionSetsResponse
	if err := s.client.doJSON(ctx, http.MethodPatch, productPath(productID)+"/variant-option-sets", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateVariant creates a variant for an existing product.
func (s *ProductsService) CreateVariant(ctx context.Context, productID ProductID, variant ProductVariant) (*ProductVariant, error) {
	var created ProductVariant
	if err := s.client.doJSON(ctx, http.MethodPost, productPath(productID)+"/variants", nil, variant, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateVariant patches a variant belonging to a product.
func (s *ProductsService) UpdateVariant(ctx context.Context, productID ProductID, variantID VariantID, variant ProductVariant) (*ProductVariant, error) {
	var updated ProductVariant
	if err := s.client.doJSON(ctx, http.MethodPatch, variantPath(productID, variantID), nil, variant, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteVariant deletes a variant from a product.
func (s *ProductsService) DeleteVariant(ctx context.Context, productID ProductID, variantID VariantID) error {
	return s.client.doJSON(ctx, http.MethodDelete, variantPath(productID, variantID), nil, nil, nil)
}

// DeleteVariantImage removes an image from a product variant.
func (s *ProductsService) DeleteVariantImage(ctx context.Context, productID ProductID, variantID VariantID, imageID ImageID) error {
	path := variantPath(productID, variantID) + "/images/" + url.PathEscape(string(imageID))
	return s.client.doJSON(ctx, http.MethodDelete, path, nil, nil, nil)
}

// ListReviews retrieves one cursor-paginated page of product reviews.
func (s *ProductsService) ListReviews(ctx context.Context, options *ProductReviewsOptions) (*ProductReviewPage, error) {
	var page ProductReviewPage
	if err := s.client.doJSON(ctx, http.MethodGet, "/products/reviews", productReviewQuery(options), nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// ListTaxonomyTypes retrieves the available product taxonomy types.
func (s *ProductsService) ListTaxonomyTypes(ctx context.Context) (*TaxonomyTypesResponse, error) {
	var response TaxonomyTypesResponse
	if err := s.client.doJSON(ctx, http.MethodGet, "/products/types", nil, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UploadImage uploads a base64-encoded image and returns its Faire CDN URL.
func (s *ProductsService) UploadImage(ctx context.Context, request UploadImageRequest) (*UploadImageResponse, error) {
	var response UploadImageResponse
	if err := s.client.doJSON(ctx, http.MethodPost, "/products/upload-image", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// productListQuery translates optional product-list controls into Faire query parameters.
func productListQuery(options *ProductListOptions) url.Values {
	if options == nil {
		return nil
	}
	query := url.Values{}
	setInt64(query, "limit", options.Limit)
	setInt64(query, "page", options.Page)
	setString(query, "updated_at_min", options.UpdatedAtMin)
	setString(query, "sku", options.SKU)
	if options.IncludeDeleted != nil {
		query.Set("include_deleted", strconv.FormatBool(*options.IncludeDeleted))
	}
	setString(query, "cursor", options.Cursor)
	return query
}

// productReviewQuery translates optional review-list controls into Faire query parameters.
func productReviewQuery(options *ProductReviewsOptions) url.Values {
	if options == nil {
		return nil
	}
	query := url.Values{}
	setInt64(query, "limit", options.Limit)
	setString(query, "cursor", options.Cursor)
	return query
}

// productPath returns the escaped API path for a product.
func productPath(productID ProductID) string {
	return "/products/" + url.PathEscape(string(productID))
}

// variantPath returns the escaped API path for a product variant.
func variantPath(productID ProductID, variantID VariantID) string {
	return productPath(productID) + "/variants/" + url.PathEscape(string(variantID))
}

// prepackPath returns the escaped API path for a product prepack.
func prepackPath(productID ProductID, prepackID PrepackID) string {
	return productPath(productID) + "/prepacks/" + url.PathEscape(string(prepackID))
}
