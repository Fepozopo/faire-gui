package faire

// ProductSaleState describes whether a product or variant is currently sellable.
type ProductSaleState string

const (
	// ProductSaleStateForSale indicates the product or variant is available for sale.
	ProductSaleStateForSale ProductSaleState = "FOR_SALE"
	// ProductSaleStateSalesPaused indicates sales are paused for the product or variant.
	ProductSaleStateSalesPaused ProductSaleState = "SALES_PAUSED"
)

// ProductLifecycleState describes a product or variant's publication lifecycle state.
type ProductLifecycleState string

const (
	// ProductLifecycleStateDraft indicates the product or variant is a draft.
	ProductLifecycleStateDraft ProductLifecycleState = "DRAFT"
	// ProductLifecycleStatePublished indicates the product or variant is published.
	ProductLifecycleStatePublished ProductLifecycleState = "PUBLISHED"
	// ProductLifecycleStateUnpublished indicates the product or variant is unpublished.
	ProductLifecycleStateUnpublished ProductLifecycleState = "UNPUBLISHED"
	// ProductLifecycleStateDeleted indicates the product or variant is deleted.
	ProductLifecycleStateDeleted ProductLifecycleState = "DELETED"
)

// Product represents a Faire product and its variants, merchandising, and ordering configuration.
type Product struct {
	ID                           *ProductID                 `json:"id,omitempty"`
	CreatedAt                    *string                    `json:"created_at,omitempty"`
	UpdatedAt                    *string                    `json:"updated_at,omitempty"`
	BrandID                      *BrandID                   `json:"brand_id,omitempty"`
	Name                         *string                    `json:"name,omitempty"`
	Description                  *string                    `json:"description,omitempty"`
	ShortDescription             *string                    `json:"short_description,omitempty"`
	SaleState                    *ProductSaleState          `json:"sale_state,omitempty"`
	LifecycleState               *ProductLifecycleState     `json:"lifecycle_state,omitempty"`
	Variants                     []ProductVariant           `json:"variants,omitempty"`
	IdempotenceToken             *string                    `json:"idempotence_token,omitempty"`
	UnitMultiplier               *int64                     `json:"unit_multiplier,omitempty"`
	MinimumOrderQuantity         *int64                     `json:"minimum_order_quantity,omitempty"`
	PerStyleMinimumOrderQuantity *int64                     `json:"per_style_minimum_order_quantity,omitempty"`
	AllowSalesWhenOutOfStock     *bool                      `json:"allow_sales_when_out_of_stock,omitempty"`
	Images                       []Image                    `json:"images,omitempty"`
	VariantOptionSets            []VariantOptionDefinition  `json:"variant_option_sets,omitempty"`
	TaxonomyType                 *TaxonomyType              `json:"taxonomy_type,omitempty"`
	Preorderable                 *bool                      `json:"preorderable,omitempty"`
	PreorderDetails              *ProductPreorderDetails    `json:"preorder_details,omitempty"`
	ProductAttributes            []ProductTaxonomyAttribute `json:"product_attributes,omitempty"`
	MadeInCountry                *string                    `json:"made_in_country,omitempty"`
}

// ProductVariant represents a purchasable configuration of a Faire product.
type ProductVariant struct {
	ID                  *VariantID               `json:"id,omitempty"`
	CreatedAt           *string                  `json:"created_at,omitempty"`
	UpdatedAt           *string                  `json:"updated_at,omitempty"`
	ProductID           *ProductID               `json:"product_id,omitempty"`
	Name                *string                  `json:"name,omitempty"`
	SaleState           *ProductSaleState        `json:"sale_state,omitempty"`
	LifecycleState      *ProductLifecycleState   `json:"lifecycle_state,omitempty"`
	IdempotenceToken    *string                  `json:"idempotence_token,omitempty"`
	SKU                 *string                  `json:"sku,omitempty"`
	AvailableQuantity   *int64                   `json:"available_quantity,omitempty"`
	BackorderedUntil    *string                  `json:"backordered_until,omitempty"`
	WholesalePriceCents *int64                   `json:"wholesale_price_cents,omitempty"`
	RetailPriceCents    *int64                   `json:"retail_price_cents,omitempty"`
	TariffCode          *string                  `json:"tariff_code,omitempty"`
	Images              []Image                  `json:"images,omitempty"`
	Options             []VariantOption          `json:"options,omitempty"`
	Prices              []VariantPrice           `json:"prices,omitempty"`
	PreorderDetails     *VariantPreorderDetails  `json:"variant_preorder_details,omitempty"`
	Measurements        *Measurements            `json:"measurements,omitempty"`
	GTIN                *string                  `json:"gtin,omitempty"`
	OrderabilityType    *VariantOrderabilityType `json:"orderability_type,omitempty"`
	CaseMeasurements    *Measurements            `json:"case_measurements,omitempty"`
}

// Image represents a Faire-hosted product or variant image.
type Image struct {
	ID          *ImageID `json:"id,omitempty"`
	Width       *int64   `json:"width,omitempty"`
	Height      *int64   `json:"height,omitempty"`
	Sequence    *int64   `json:"sequence,omitempty"`
	URL         *string  `json:"url,omitempty"`
	OriginalURL *string  `json:"original_url,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// VariantOption is one name/value combination that identifies a product variant.
type VariantOption struct {
	Name  *string `json:"name,omitempty"`
	Value *string `json:"value,omitempty"`
}

// VariantPrice specifies the wholesale and retail prices for a geographical constraint.
type VariantPrice struct {
	GeoConstraint  *PriceGeoConstraint `json:"geo_constraint,omitempty"`
	WholesalePrice *Money              `json:"wholesale_price,omitempty"`
	RetailPrice    *Money              `json:"retail_price,omitempty"`
}

// PriceGeoConstraint limits a variant price to a country or country group.
type PriceGeoConstraint struct {
	Country      *string `json:"country,omitempty"`
	CountryGroup *string `json:"country_group,omitempty"`
}

// VariantPreorderDetails contains preorder timing for a product variant.
type VariantPreorderDetails struct {
	ExpectedShipDate          *string `json:"expected_ship_date,omitempty"`
	ExpectedShipWindowEndDate *string `json:"expected_ship_window_end_date,omitempty"`
	StopSellingAt             *int64  `json:"stop_selling_at,omitempty"`
}

// MassUnit identifies the unit used to measure a variant's weight.
type MassUnit string

const (
	// MassUnitGrams represents grams.
	MassUnitGrams MassUnit = "GRAMS"
	// MassUnitKilograms represents kilograms.
	MassUnitKilograms MassUnit = "KILOGRAMS"
	// MassUnitOunces represents ounces.
	MassUnitOunces MassUnit = "OUNCES"
	// MassUnitPounds represents pounds.
	MassUnitPounds MassUnit = "POUNDS"
)

// DistanceUnit identifies the unit used to measure a variant's dimensions.
type DistanceUnit string

const (
	// DistanceUnitCentimeters represents centimeters.
	DistanceUnitCentimeters DistanceUnit = "CENTIMETERS"
	// DistanceUnitInches represents inches.
	DistanceUnitInches DistanceUnit = "INCHES"
	// DistanceUnitFeet represents feet.
	DistanceUnitFeet DistanceUnit = "FEET"
	// DistanceUnitMillimeters represents millimeters.
	DistanceUnitMillimeters DistanceUnit = "MILLIMETERS"
	// DistanceUnitMeters represents meters.
	DistanceUnitMeters DistanceUnit = "METERS"
	// DistanceUnitYards represents yards.
	DistanceUnitYards DistanceUnit = "YARDS"
)

// Measurements contains the weight and dimensions of a product variant or product case.
type Measurements struct {
	MassUnit     *MassUnit     `json:"mass_unit,omitempty"`
	Weight       *float64      `json:"weight,omitempty"`
	DistanceUnit *DistanceUnit `json:"distance_unit,omitempty"`
	Length       *float64      `json:"length,omitempty"`
	Width        *float64      `json:"width,omitempty"`
	Height       *float64      `json:"height,omitempty"`
}

// VariantOrderabilityType identifies whether a product variant is immediately available or preorderable.
type VariantOrderabilityType string

const (
	// VariantOrderabilityTypeImmediate identifies variants available immediately.
	VariantOrderabilityTypeImmediate VariantOrderabilityType = "IMMEDIATE"
	// VariantOrderabilityTypePreorder identifies variants available only by preorder.
	VariantOrderabilityTypePreorder VariantOrderabilityType = "PREORDER"
)

// VariantOptionDefinition defines an available option name and its ordered values for a product.
type VariantOptionDefinition struct {
	Name   *string  `json:"name,omitempty"`
	Values []string `json:"values,omitempty"`
}

// TaxonomyType represents a Faire product taxonomy category.
type TaxonomyType struct {
	ID   *string `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`
}

// ProductPreorderDetails contains preorder timing and activation behavior for a product.
type ProductPreorderDetails struct {
	OrderByDate               *string `json:"order_by_date,omitempty"`
	KeepActivePastOrderByDate *bool   `json:"keep_active_past_order_by_date,omitempty"`
	ExpectedShipDate          *string `json:"expected_ship_date,omitempty"`
	ExpectedShipWindowEndDate *string `json:"expected_ship_window_end_date,omitempty"`
}

// ProductTaxonomyAttribute represents a taxonomy attribute assigned to a product.
type ProductTaxonomyAttribute struct {
	Name  *string `json:"name,omitempty"`
	Value *string `json:"value,omitempty"`
}

// ProductListOptions controls filtering and cursor pagination for products.
type ProductListOptions struct {
	Limit          *int64  `json:"-"`
	Page           *int64  `json:"-"`
	UpdatedAtMin   *string `json:"-"`
	SKU            *string `json:"-"`
	IncludeDeleted *bool   `json:"-"`
	Cursor         *string `json:"-"`
}

// ProductPage is a cursor-paginated page of products.
type ProductPage struct {
	Products     []Product `json:"products,omitempty"`
	Page         *int64    `json:"page,omitempty"`
	Limit        *int64    `json:"limit,omitempty"`
	UpdatedAtMin *string   `json:"updated_at_min,omitempty"`
	Cursor       *string   `json:"cursor,omitempty"`
}

// ProductReviewWithDetails combines a product review with its retailer and product context.
type ProductReviewWithDetails struct {
	ProductReview *ProductReview `json:"product_review,omitempty"`
	RetailerID    *RetailerID    `json:"retailer_id,omitempty"`
	ProductInfo   *ProductInfo   `json:"product_info,omitempty"`
}

// ProductReview is a retailer review of a Faire product.
type ProductReview struct {
	ID           *string             `json:"id,omitempty"`
	Rating       *int64              `json:"rating,omitempty"`
	Comment      *string             `json:"comment,omitempty"`
	BrandOrderID *OrderID            `json:"brand_order_id,omitempty"`
	CreatedAt    *string             `json:"created_at,omitempty"`
	UpdatedAt    *string             `json:"updated_at,omitempty"`
	PublishedAt  *string             `json:"published_at,omitempty"`
	Images       []Image             `json:"images,omitempty"`
	Reply        *ProductReviewReply `json:"reply,omitempty"`
}

// ProductReviewReply is a brand's reply to a retailer review.
type ProductReviewReply struct {
	Comment   *string `json:"comment,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
}

// ProductInfo identifies the reviewed product.
type ProductInfo struct {
	ProductID *ProductID `json:"product_id,omitempty"`
	Name      *string    `json:"name,omitempty"`
}

// ProductReviewsOptions controls cursor pagination for product reviews.
type ProductReviewsOptions struct {
	Limit  *int64  `json:"-"`
	Cursor *string `json:"-"`
}

// ProductReviewPage is a cursor-paginated page of product reviews.
type ProductReviewPage struct {
	ProductReviewsWithDetails []ProductReviewWithDetails `json:"product_reviews_with_details,omitempty"`
	Limit                     *int64                     `json:"limit,omitempty"`
	Cursor                    *string                    `json:"cursor,omitempty"`
}

// TaxonomyTypesResponse contains the taxonomy types available on Faire.
type TaxonomyTypesResponse struct {
	TaxonomyTypes []TaxonomyType `json:"taxonomy_types,omitempty"`
}

// UploadImageRequest provides a base64-encoded image to upload to Faire.
type UploadImageRequest struct {
	Attachment *string `json:"attachment,omitempty"`
}

// UploadImageResponse contains the Faire CDN URL for an uploaded image.
type UploadImageResponse struct {
	URL *string `json:"url,omitempty"`
}

// Prepack represents a multi-item package of product variants.
type Prepack struct {
	ID               *PrepackID    `json:"id,omitempty"`
	IdempotenceToken *string       `json:"idempotence_token,omitempty"`
	CreatedAt        *string       `json:"created_at,omitempty"`
	UpdatedAt        *string       `json:"updated_at,omitempty"`
	Name             *string       `json:"name,omitempty"`
	Description      *string       `json:"description,omitempty"`
	Items            []PrepackItem `json:"items,omitempty"`
}

// PrepackItem specifies one variant and quantity in a prepack.
type PrepackItem struct {
	ID        *string    `json:"id,omitempty"`
	CreatedAt *string    `json:"created_at,omitempty"`
	UpdatedAt *string    `json:"updated_at,omitempty"`
	VariantID *VariantID `json:"variant_id,omitempty"`
	Quantity  *int64     `json:"quantity,omitempty"`
}

// PrepackListResponse contains the prepacks for one product.
type PrepackListResponse struct {
	Prepacks []Prepack `json:"prepacks,omitempty"`
}

// CreatePrepacksRequest creates multiple prepacks for a product.
type CreatePrepacksRequest struct {
	Prepacks []Prepack `json:"prepacks,omitempty"`
}

// CreatePrepacksResponse contains prepacks created by a batch request.
type CreatePrepacksResponse struct {
	Prepacks []Prepack `json:"prepacks,omitempty"`
}

// PatchVariantOptionSetsRequest replaces a product's available variant option values.
type PatchVariantOptionSetsRequest struct {
	VariantOptionSets []VariantOptionDefinition `json:"variant_option_sets,omitempty"`
}

// PatchVariantOptionSetsResponse contains the product's current variant option definitions.
type PatchVariantOptionSetsResponse struct {
	VariantOptionSets []VariantOptionDefinition `json:"variant_option_sets,omitempty"`
}

// RetailerProfile contains publicly available information about a Faire retailer.
type RetailerProfile struct {
	RetailerID *RetailerID `json:"retailer_id,omitempty"`
	Name       *string     `json:"name,omitempty"`
	IsInsider  *bool       `json:"is_insider,omitempty"`
}
