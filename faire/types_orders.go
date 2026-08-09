// Package faire provides a typed client for Faire's External API v2.
package faire

// BrandID uniquely identifies a Faire brand.
type BrandID string

// OrderID uniquely identifies a Faire brand order.
type OrderID string

// ProductID uniquely identifies a Faire product.
type ProductID string

// VariantID uniquely identifies a Faire product variant.
type VariantID string

// ImageID uniquely identifies a Faire-hosted image.
type ImageID string

// PrepackID uniquely identifies a Faire prepack.
type PrepackID string

// RetailerID uniquely identifies a Faire retailer.
type RetailerID string

// ShipmentID uniquely identifies a Faire shipment.
type ShipmentID string

// BrandProfile contains information about the brand associated with the current API session.
type BrandProfile struct {
	BrandID  *BrandID `json:"brand_id,omitempty"`
	Name     *string  `json:"name,omitempty"`
	Currency *string  `json:"currency,omitempty"`
	Locale   *string  `json:"locale,omitempty"`
}

// OrderState describes the fulfillment state of an order.
type OrderState string

const (
	// OrderStateNew indicates a newly received order.
	OrderStateNew OrderState = "NEW"
	// OrderStateProcessing indicates the brand is processing the order.
	OrderStateProcessing OrderState = "PROCESSING"
	// OrderStatePreTransit indicates a shipment is awaiting carrier transit.
	OrderStatePreTransit OrderState = "PRE_TRANSIT"
	// OrderStateInTransit indicates a shipment is in transit.
	OrderStateInTransit OrderState = "IN_TRANSIT"
	// OrderStateDelivered indicates the order has been delivered.
	OrderStateDelivered OrderState = "DELIVERED"
	// OrderStateCanceled indicates the order was canceled.
	OrderStateCanceled OrderState = "CANCELED"
	// OrderStateBackordered indicates one or more items are backordered.
	OrderStateBackordered OrderState = "BACKORDERED"
	// OrderStatePendingRetailerConfirmation indicates the order needs retailer confirmation.
	OrderStatePendingRetailerConfirmation OrderState = "PENDING_RETAILER_CONFIRMATION"
)

// Order represents a Faire brand order and its fulfillment, payout, and retailer details.
type Order struct {
	ID                                    *OrderID            `json:"id,omitempty"`
	DisplayID                             *string             `json:"display_id,omitempty"`
	CreatedAt                             *string             `json:"created_at,omitempty"`
	UpdatedAt                             *string             `json:"updated_at,omitempty"`
	State                                 *OrderState         `json:"state,omitempty"`
	Items                                 []OrderItem         `json:"items,omitempty"`
	Shipments                             []Shipment          `json:"shipments,omitempty"`
	Address                               *Address            `json:"address,omitempty"`
	ShipAfter                             *string             `json:"ship_after,omitempty"`
	PayoutCosts                           *PayoutCosts        `json:"payout_costs,omitempty"`
	PaymentInitiatedAt                    *string             `json:"payment_initiated_at,omitempty"`
	OriginalOrderID                       *OrderID            `json:"original_order_id,omitempty"`
	RetailerID                            *RetailerID         `json:"retailer_id,omitempty"`
	Source                                *string             `json:"source,omitempty"`
	ExpectedShipDate                      *string             `json:"expected_ship_date,omitempty"`
	Customer                              *Customer           `json:"customer,omitempty"`
	BrandDiscounts                        []Discount          `json:"brand_discounts,omitempty"`
	RequestedShipDate                     *string             `json:"requested_ship_date,omitempty"`
	ProcessingAt                          *string             `json:"processing_at,omitempty"`
	IsFreeShipping                        *bool               `json:"is_free_shipping,omitempty"`
	FreeShippingReason                    *FreeShippingReason `json:"free_shipping_reason,omitempty"`
	FaireCoveredShippingCost              *Money              `json:"faire_covered_shipping_cost,omitempty"`
	EstimatedPayoutAt                     *string             `json:"estimated_payout_at,omitempty"`
	IsFulfilledByFaire                    *bool               `json:"is_fulfilled_by_faire,omitempty"`
	PurchaseOrderNumber                   *string             `json:"purchase_order_number,omitempty"`
	Notes                                 *string             `json:"notes,omitempty"`
	HasPendingRetailerCancellationRequest *bool               `json:"has_pending_retailer_cancellation_request,omitempty"`
	SalesRepName                          *string             `json:"sales_rep_name,omitempty"`
}

// OrderItemState describes the fulfillment state of an individual order item.
type OrderItemState string

const (
	// OrderItemStateCanceled indicates the item was canceled.
	OrderItemStateCanceled OrderItemState = "CANCELED"
	// OrderItemStateProcessing indicates the item is being prepared.
	OrderItemStateProcessing OrderItemState = "PROCESSING"
	// OrderItemStatePreTransit indicates the item is awaiting carrier transit.
	OrderItemStatePreTransit OrderItemState = "PRE_TRANSIT"
	// OrderItemStateInTransit indicates the item is in transit.
	OrderItemStateInTransit OrderItemState = "IN_TRANSIT"
	// OrderItemStateDelivered indicates the item was delivered.
	OrderItemStateDelivered OrderItemState = "DELIVERED"
	// OrderItemStateReturned indicates the item was returned.
	OrderItemStateReturned OrderItemState = "RETURNED"
	// OrderItemStateBackordered indicates the item is backordered.
	OrderItemStateBackordered OrderItemState = "BACKORDERED"
	// OrderItemStateDamagedOrMissing indicates the item was damaged or missing.
	OrderItemStateDamagedOrMissing OrderItemState = "DAMAGED_OR_MISSING"
	// OrderItemStatePendingRetailerConfirmation indicates the item needs retailer confirmation.
	OrderItemStatePendingRetailerConfirmation OrderItemState = "PENDING_RETAILER_CONFIRMATION"
)

// OrderItem represents a product variant included in an order.
type OrderItem struct {
	ID               *string         `json:"id,omitempty"`
	CreatedAt        *string         `json:"created_at,omitempty"`
	UpdatedAt        *string         `json:"updated_at,omitempty"`
	OrderID          *OrderID        `json:"order_id,omitempty"`
	ProductID        *ProductID      `json:"product_id,omitempty"`
	VariantID        *VariantID      `json:"variant_id,omitempty"`
	Quantity         *int64          `json:"quantity,omitempty"`
	SKU              *string         `json:"sku,omitempty"`
	PriceCents       *int64          `json:"price_cents,omitempty"`
	ProductName      *string         `json:"product_name,omitempty"`
	VariantName      *string         `json:"variant_name,omitempty"`
	IncludesTester   *bool           `json:"includes_tester,omitempty"`
	TesterPriceCents *int64          `json:"tester_price_cents,omitempty"`
	Customizations   []Customization `json:"customizations,omitempty"`
	Price            *Money          `json:"price,omitempty"`
	TesterPrice      *Money          `json:"tester_price,omitempty"`
	Discounts        []Discount      `json:"discounts,omitempty"`
	State            *OrderItemState `json:"state,omitempty"`
}

// Customization records a retailer-provided customization for an order item.
type Customization struct {
	Token *string `json:"token,omitempty"`
	Type  *string `json:"type,omitempty"`
	Value *string `json:"value,omitempty"`
}

// Money represents a monetary amount in the smallest unit for its currency.
type Money struct {
	AmountMinor *int64  `json:"amount_minor,omitempty"`
	Currency    *string `json:"currency,omitempty"`
}

// DiscountType describes how a Faire discount is calculated.
type DiscountType string

const (
	// DiscountTypeFlatAmount represents a fixed monetary discount.
	DiscountTypeFlatAmount DiscountType = "FLAT_AMOUNT"
	// DiscountTypePercentage represents a percentage-based discount.
	DiscountTypePercentage DiscountType = "PERCENTAGE"
	// DiscountTypeNone represents the absence of a discount calculation.
	DiscountTypeNone DiscountType = "NONE"
)

// Discount represents a discount applied to an order or order item.
type Discount struct {
	ID                   *string       `json:"id,omitempty"`
	Code                 *string       `json:"code,omitempty"`
	DiscountType         *DiscountType `json:"discount_type,omitempty"`
	DiscountAmountCents  *int64        `json:"discount_amount_cents,omitempty"`
	DiscountPercentage   *float64      `json:"discount_percentage,omitempty"`
	IncludesFreeShipping *bool         `json:"includes_free_shipping,omitempty"`
	DiscountAmount       *Money        `json:"discount_amount,omitempty"`
}

// ShippingType identifies the service used to ship an order.
type ShippingType string

const (
	// ShippingTypeShipOnYourOwn indicates the brand ships with its own carrier account.
	ShippingTypeShipOnYourOwn ShippingType = "SHIP_ON_YOUR_OWN"
	// ShippingTypeShipWithFaire indicates the shipment uses Faire's shipping service.
	ShippingTypeShipWithFaire ShippingType = "SHIP_WITH_FAIRE"
)

// Shipment represents fulfillment tracking information for an order.
type Shipment struct {
	ID               *ShipmentID   `json:"id,omitempty"`
	CreatedAt        *string       `json:"created_at,omitempty"`
	UpdatedAt        *string       `json:"updated_at,omitempty"`
	OrderID          *OrderID      `json:"order_id,omitempty"`
	MakerCostCents   *int64        `json:"maker_cost_cents,omitempty"`
	Carrier          *string       `json:"carrier,omitempty"`
	TrackingCode     *string       `json:"tracking_code,omitempty"`
	MakerCost        *Money        `json:"maker_cost,omitempty"`
	ShippingType     *ShippingType `json:"shipping_type,omitempty"`
	ShippingLabelURL *string       `json:"shipping_label_url,omitempty"`
}

// AddressType identifies whether a shipping address is residential or commercial.
type AddressType string

const (
	// AddressTypeResidential identifies a residential address.
	AddressTypeResidential AddressType = "RESIDENTIAL"
	// AddressTypeCommercial identifies a commercial address.
	AddressTypeCommercial AddressType = "COMMERCIAL"
	// AddressTypeMixed identifies an address with both residential and commercial use.
	AddressTypeMixed AddressType = "MIXED"
)

// Address is an order's delivery address.
type Address struct {
	ID          *string      `json:"id,omitempty"`
	Name        *string      `json:"name,omitempty"`
	Address1    *string      `json:"address1,omitempty"`
	Address2    *string      `json:"address2,omitempty"`
	PostalCode  *string      `json:"postal_code,omitempty"`
	City        *string      `json:"city,omitempty"`
	State       *string      `json:"state,omitempty"`
	StateCode   *string      `json:"state_code,omitempty"`
	PhoneNumber *string      `json:"phone_number,omitempty"`
	Country     *string      `json:"country,omitempty"`
	CountryCode *string      `json:"country_code,omitempty"`
	CompanyName *string      `json:"company_name,omitempty"`
	AddressType *AddressType `json:"address_type,omitempty"`
}

// PayoutCosts contains the fees, taxes, discounts, and expected payout for an order.
type PayoutCosts struct {
	PayoutFeeCents              *int64    `json:"payout_fee_cents,omitempty"`
	PayoutFeeBPS                *int64    `json:"payout_fee_bps,omitempty"`
	PayoutFlatFee               *Money    `json:"payout_flat_fee,omitempty"`
	CommissionCents             *int64    `json:"commission_cents,omitempty"`
	CommissionBPS               *int64    `json:"commission_bps,omitempty"`
	CommissionFlatFee           *Money    `json:"commission_flat_fee,omitempty"`
	PayoutFee                   *Money    `json:"payout_fee,omitempty"`
	Commission                  *Money    `json:"commission,omitempty"`
	TotalPayout                 *Money    `json:"total_payout,omitempty"`
	PayoutProtectionFee         *Money    `json:"payout_protection_fee,omitempty"`
	DamagedAndMissingItems      *Money    `json:"damaged_and_missing_items,omitempty"`
	NetTax                      *Money    `json:"net_tax,omitempty"`
	ShippingSubsidy             *Money    `json:"shipping_subsidy,omitempty"`
	Taxes                       []TaxItem `json:"taxes,omitempty"`
	SubtotalAfterBrandDiscounts *Money    `json:"subtotal_after_brand_discounts,omitempty"`
	TotalBrandDiscounts         *Money    `json:"total_brand_discounts,omitempty"`
}

// TaxableItemType identifies the charge to which a tax applies.
type TaxableItemType string

const (
	// TaxableItemTypeOrderItem identifies tax on ordered goods.
	TaxableItemTypeOrderItem TaxableItemType = "ORDER_ITEM"
	// TaxableItemTypeShipping identifies tax on shipping.
	TaxableItemTypeShipping TaxableItemType = "SHIPPING"
	// TaxableItemTypeInsiderMembership identifies tax on Faire Insider membership.
	TaxableItemTypeInsiderMembership TaxableItemType = "INSIDER_MEMBERSHIP"
	// TaxableItemTypeOrderCommission identifies tax on order commission.
	TaxableItemTypeOrderCommission TaxableItemType = "ORDER_COMMISSION"
	// TaxableItemTypeAdsCharge identifies tax on advertising charges.
	TaxableItemTypeAdsCharge TaxableItemType = "ADS_CHARGE"
)

// TaxType identifies a regional tax category.
type TaxType string

const (
	// TaxTypeCanadianTax identifies Canadian tax.
	TaxTypeCanadianTax TaxType = "CANADIAN_TAX"
	// TaxTypeVAT identifies value-added tax.
	TaxTypeVAT TaxType = "VAT"
	// TaxTypeVATReverseCharge identifies reverse-charge VAT.
	TaxTypeVATReverseCharge TaxType = "VAT_REVERSE_CHARGE"
	// TaxTypeIntraCommunitySupply identifies an intra-community supply tax treatment.
	TaxTypeIntraCommunitySupply TaxType = "INTRA_COMMUNITY_SUPPLY"
	// TaxTypeGST identifies goods and services tax.
	TaxTypeGST TaxType = "GST"
	// TaxTypeHST identifies harmonized sales tax.
	TaxTypeHST TaxType = "HST"
	// TaxTypePST identifies provincial sales tax.
	TaxTypePST TaxType = "PST"
	// TaxTypeEstimatedImportVAT identifies estimated import VAT.
	TaxTypeEstimatedImportVAT TaxType = "ESTIMATED_IMPORT_VAT"
	// TaxTypeImportVAT identifies import VAT.
	TaxTypeImportVAT TaxType = "IMPORT_VAT"
	// TaxTypeAustraliaGST identifies Australian GST.
	TaxTypeAustraliaGST TaxType = "AUSTRALIA_GST"
	// TaxTypeRecargo identifies a Spanish surcharge tax.
	TaxTypeRecargo TaxType = "RECARGO"
	// TaxTypeRecargoReverseCharge identifies a reverse-charge Spanish surcharge tax.
	TaxTypeRecargoReverseCharge TaxType = "RECARGO_REVERSE_CHARGE"
	// TaxTypeNewZealandGST identifies New Zealand GST.
	TaxTypeNewZealandGST TaxType = "NEW_ZEALAND_GST"
	// TaxTypeSalesTax identifies sales tax.
	TaxTypeSalesTax TaxType = "SALES_TAX"
)

// TaxEffect describes how a tax changes the brand payout.
type TaxEffect string

const (
	// TaxEffectIncreasesPayout indicates the tax increases the payout.
	TaxEffectIncreasesPayout TaxEffect = "INCREASES_PAYOUT"
	// TaxEffectDeductedFromPayout indicates the tax is deducted from the payout.
	TaxEffectDeductedFromPayout TaxEffect = "DEDUCTED_FROM_PAYOUT"
)

// TaxItem represents one tax component in an order payout.
type TaxItem struct {
	Value           *Money           `json:"value,omitempty"`
	TaxableItemType *TaxableItemType `json:"taxable_item_type,omitempty"`
	TaxType         *TaxType         `json:"tax_type,omitempty"`
	Effect          *TaxEffect       `json:"effect,omitempty"`
}

// Customer identifies the person associated with an order.
type Customer struct {
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
}

// FreeShippingReason explains why Faire did not charge shipping for an order.
type FreeShippingReason string

const (
	// FreeShippingReasonInsider indicates Faire Insider provided free shipping.
	FreeShippingReasonInsider FreeShippingReason = "INSIDER_FREE_SHIPPING"
	// FreeShippingReasonFaireDirect indicates Faire Direct provided free shipping.
	FreeShippingReasonFaireDirect FreeShippingReason = "FAIRE_DIRECT"
	// FreeShippingReasonBrandDiscount indicates a brand discount provided free shipping.
	FreeShippingReasonBrandDiscount FreeShippingReason = "BRAND_DISCOUNT"
	// FreeShippingReasonFirstOrder indicates a first-order promotion provided free shipping.
	FreeShippingReasonFirstOrder FreeShippingReason = "FIRST_ORDER"
	// FreeShippingReasonPromoCode indicates a promo code provided free shipping.
	FreeShippingReasonPromoCode FreeShippingReason = "PROMO_CODE"
	// FreeShippingReasonThreshold indicates an order threshold provided free shipping.
	FreeShippingReasonThreshold FreeShippingReason = "FREE_SHIPPING_THRESHOLD"
)

// OrderSortBy determines the field used to sort order-list results.
type OrderSortBy string

const (
	// OrderSortByUpdatedAt sorts orders by update time.
	OrderSortByUpdatedAt OrderSortBy = "UPDATED_AT"
	// OrderSortByCreatedAt sorts orders by creation time.
	OrderSortByCreatedAt OrderSortBy = "CREATED_AT"
)

// OrderListOptions controls filtering and cursor pagination for orders.
type OrderListOptions struct {
	Limit           *int64       `json:"-"`
	Page            *int64       `json:"-"`
	UpdatedAtMin    *string      `json:"-"`
	ExcludedStates  []OrderState `json:"-"`
	ShipAfterMax    *string      `json:"-"`
	CreatedAtMin    *string      `json:"-"`
	SortBy          *OrderSortBy `json:"-"`
	Cursor          *string      `json:"-"`
	OriginalOrderID *OrderID     `json:"-"`
}

// OrderPage is a cursor-paginated page of orders.
type OrderPage struct {
	Orders       []Order      `json:"orders,omitempty"`
	Page         *int64       `json:"page,omitempty"`
	Limit        *int64       `json:"limit,omitempty"`
	UpdatedAtMin *string      `json:"updated_at_min,omitempty"`
	SortBy       *OrderSortBy `json:"sort_by,omitempty"`
	Cursor       *string      `json:"cursor,omitempty"`
}

// CancelReason identifies why a brand canceled an order.
type CancelReason string

const (
	// CancelReasonRequestedByRetailer indicates the retailer requested cancellation.
	CancelReasonRequestedByRetailer CancelReason = "REQUESTED_BY_RETAILER"
	// CancelReasonRetailerNotGoodFit indicates the retailer is not a good fit.
	CancelReasonRetailerNotGoodFit CancelReason = "RETAILER_NOT_GOOD_FIT"
	// CancelReasonChangeReplaceOrder indicates the order will be changed or replaced.
	CancelReasonChangeReplaceOrder CancelReason = "CHANGE_REPLACE_ORDER"
	// CancelReasonItemOutOfStock indicates an item is out of stock.
	CancelReasonItemOutOfStock CancelReason = "ITEM_OUT_OF_STOCK"
	// CancelReasonIncorrectPricing indicates an incorrect price.
	CancelReasonIncorrectPricing CancelReason = "INCORRECT_PRICING"
	// CancelReasonOrderTooSmall indicates the order does not meet the brand minimum.
	CancelReasonOrderTooSmall CancelReason = "ORDER_TOO_SMALL"
	// CancelReasonRejectInternationalOrder indicates the brand rejects an international order.
	CancelReasonRejectInternationalOrder CancelReason = "REJECT_INTERNATIONAL_ORDER"
	// CancelReasonOther identifies an uncategorized cancellation reason.
	CancelReasonOther CancelReason = "OTHER"
)

// CancelOrderRequest supplies the reason and optional retailer-facing note for an order cancellation.
type CancelOrderRequest struct {
	Note   *string       `json:"note,omitempty"`
	Reason *CancelReason `json:"reason,omitempty"`
}

// ItemAvailability supplies an order item's current availability decision.
type ItemAvailability struct {
	AvailableQuantity *int64  `json:"available_quantity,omitempty"`
	Discontinued      *bool   `json:"discontinued,omitempty"`
	BackorderedUntil  *string `json:"backordered_until,omitempty"`
}

// UpdateOrderItemsAvailabilityRequest updates availability for variants in one order, keyed by variant ID.
type UpdateOrderItemsAvailabilityRequest struct {
	Availabilities map[VariantID]ItemAvailability `json:"availabilities,omitempty"`
}

// MoveOrderToProcessingRequest supplies an optional expected shipment date when processing begins.
type MoveOrderToProcessingRequest struct {
	ExpectedShipDate *string `json:"expected_ship_date,omitempty"`
}

// AddShipmentsRequest adds one or more shipment records to an order.
type AddShipmentsRequest struct {
	Shipments []Shipment `json:"shipments,omitempty"`
}
