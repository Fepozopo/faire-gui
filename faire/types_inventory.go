package faire

// InventoryQuantityType describes whether an inventory level is tracked numerically.
type InventoryQuantityType string

const (
	// InventoryQuantityTypeQuantity indicates a numerical inventory level.
	InventoryQuantityTypeQuantity InventoryQuantityType = "QUANTITY"
	// InventoryQuantityTypeUntracked indicates inventory is not tracked.
	InventoryQuantityTypeUntracked InventoryQuantityType = "UNTRACKED"
)

// InventoryQuantity represents a tracked, untracked, or negative inventory quantity.
type InventoryQuantity struct {
	Type     *InventoryQuantityType `json:"type,omitempty"`
	Quantity *int64                 `json:"quantity,omitempty"`
}

// VariantInventory contains on-hand, committed, and available inventory for one variant.
type VariantInventory struct {
	OnHandQuantity    *InventoryQuantity `json:"on_hand_quantity,omitempty"`
	CommittedQuantity *InventoryQuantity `json:"committed_quantity,omitempty"`
	AvailableQuantity *InventoryQuantity `json:"available_quantity,omitempty"`
}

// InventoryResponse maps requested SKUs or variant IDs to their inventory data.
type InventoryResponse struct {
	Inventories map[string]VariantInventory `json:"inventories,omitempty"`
}

// UpdateOnHandInventoryItem identifies a variant and its replacement on-hand quantity.
type UpdateOnHandInventoryItem struct {
	SKU              *string    `json:"sku,omitempty"`
	ProductVariantID *VariantID `json:"product_variant_id,omitempty"`
	OnHandQuantity   *int64     `json:"on_hand_quantity,omitempty"`
}

// UpdateOnHandInventoryRequest updates on-hand inventory for several variants.
type UpdateOnHandInventoryRequest struct {
	Inventories []UpdateOnHandInventoryItem `json:"inventories,omitempty"`
}

// UpdateOnHandInventoryResponse maps each updated SKU or variant ID to its inventory data.
type UpdateOnHandInventoryResponse struct {
	Inventories map[string]VariantInventory `json:"inventories,omitempty"`
}

// UpdateVariantInventoryItem identifies a variant and its updated selling inventory level.
type UpdateVariantInventoryItem struct {
	SKU              *string    `json:"sku,omitempty"`
	CurrentQuantity  *int64     `json:"current_quantity,omitempty"`
	Discontinued     *bool      `json:"discontinued,omitempty"`
	BackorderedUntil *string    `json:"backordered_until,omitempty"`
	ProductVariantID *VariantID `json:"product_variant_id,omitempty"`
}

// UpdateVariantInventoryRequest updates variant inventory, discontinuation, and backorder state.
type UpdateVariantInventoryRequest struct {
	Inventories []UpdateVariantInventoryItem `json:"inventories,omitempty"`
}

// UpdateVariantInventoryResponse contains variants after their inventory levels are updated.
type UpdateVariantInventoryResponse struct {
	Variants []ProductVariant `json:"variants,omitempty"`
}

// VariantPriceByID updates the regional prices of one product variant ID.
type VariantPriceByID struct {
	ProductVariantID *VariantID     `json:"product_variant_id,omitempty"`
	Prices           []VariantPrice `json:"prices,omitempty"`
}

// UpdatePricesByVariantIDRequest updates regional prices for several product variant IDs.
type UpdatePricesByVariantIDRequest struct {
	Prices []VariantPriceByID `json:"prices,omitempty"`
}

// VariantPriceBySKU updates the regional prices of one SKU.
type VariantPriceBySKU struct {
	SKU    *string        `json:"sku,omitempty"`
	Prices []VariantPrice `json:"prices,omitempty"`
}

// UpdatePricesBySKURequest updates regional prices for several SKUs.
type UpdatePricesBySKURequest struct {
	Prices []VariantPriceBySKU `json:"prices,omitempty"`
}

// PriceUpdateResult contains the resulting regional prices for one updated identifier.
type PriceUpdateResult struct {
	Prices []VariantPrice `json:"prices,omitempty"`
}

// UpdatePricesResponse maps each updated SKU or variant ID to its resulting prices.
type UpdatePricesResponse struct {
	Results map[string]PriceUpdateResult `json:"results,omitempty"`
}
