package application

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/features/orders"
	sagepolicy "github.com/Fepozopo/faire-gui/sage"
)

// TestSageCompletedFulfillmentResultBuildsAllowedWriteback verifies successful external shipment submission yields sequential tracking, package-one shipped allocations, and policy freight.
func TestSageCompletedFulfillmentResultBuildsAllowedWriteback(t *testing.T) {
	request := sageFulfillmentRequest{
		RequestID: "request-1",
		Document:  sageFulfillmentDocument{SalesOrderNo: "SO-1", InvoiceNo: "INV-1"},
		Lines: []sageFulfillmentLine{
			{ItemCode: "SKU-1", ItemType: "1", QuantityShipped: 2},
			{ItemCode: "SKU-2", ItemType: "1", QuantityShipped: 1, QuantityBackordered: 3},
			{ItemCode: "SKU-3", ItemType: "1", QuantityBackordered: 1},
		},
	}
	markup := 4.0
	result, valid := sageCompletedFulfillmentResult(request, orders.Detail{Shipments: []orders.DetailShipment{{TrackingCode: "1Z123"}, {TrackingCode: "9400"}}}, []sageExternalShipment{{TrackingNumber: "1Z123", CostMinor: 500}, {TrackingNumber: "9400", CostMinor: 250}}, sageFulfillmentPolicy{rule: sagepolicy.ShipCodeRule{FreightWriteback: "Cost", AdditionalMarkupFixed: &markup}})
	if !valid {
		t.Fatal("sageCompletedFulfillmentResult() valid = false, want true")
	}
	if len(result.Tracking) != 2 || result.Tracking[0].PackageNumber != 1 || result.Tracking[1].PackageNumber != 2 {
		t.Fatalf("tracking = %#v, want sequential package tracking", result.Tracking)
	}
	if len(result.PackageItems) != 2 || result.PackageItems[0].PackageNumber != 1 || result.PackageItems[1].ItemCode != "SKU-2" {
		t.Fatalf("package items = %#v, want only positive shipped Sage lines in package 1", result.PackageItems)
	}
	if result.FreightAmountMinor == nil || *result.FreightAmountMinor != 1150 {
		t.Fatalf("freight = %v, want 1150 cents", result.FreightAmountMinor)
	}
	formatted := formatSageFulfillmentResult(result)
	for _, line := range []string{"Tracking:1|1Z123", "Tracking:2|9400", "PackageItem:1|SKU-1|1|2", "FreightAmount:11.50"} {
		if !strings.Contains(formatted, line) {
			t.Fatalf("formatted result %q does not contain %q", formatted, line)
		}
	}
}

// TestSageSimulatedLabelFulfillmentResultBuildsOneTestPackage verifies the test-only action never depends on a Faire response and still uses the normal Sage writeback shape.
func TestSageSimulatedLabelFulfillmentResultBuildsOneTestPackage(t *testing.T) {
	request := sageFulfillmentRequest{RequestID: "test-request", Document: sageFulfillmentDocument{SalesOrderNo: "SO-1", InvoiceNo: "INV-1"}, Lines: []sageFulfillmentLine{{ItemCode: "SKU-1", ItemType: "1", QuantityShipped: 1}}}
	result := sageSimulatedLabelFulfillmentResult(request, sageSimulatedLabelShipment(), sageFulfillmentPolicy{rule: sagepolicy.ShipCodeRule{FreightWriteback: "Cost"}})
	if len(result.Tracking) != 1 || result.Tracking[0].PackageNumber != 1 || result.Tracking[0].TrackingNumber != sageSimulatedLabelTrackingNumber {
		t.Fatalf("simulated tracking = %#v, want one deterministic test package", result.Tracking)
	}
	if len(result.PackageItems) != 1 || result.PackageItems[0].ItemCode != "SKU-1" {
		t.Fatalf("simulated package items = %#v, want shipped line in package 1", result.PackageItems)
	}
	if result.FreightAmountMinor == nil || *result.FreightAmountMinor != 1000 {
		t.Fatalf("simulated freight = %v, want $10.00", result.FreightAmountMinor)
	}
}

// TestSageFreightAmountMinorOmitsFreight verifies an Omit rule does not overwrite Sage freight even when package costs exist.
func TestSageFreightAmountMinorOmitsFreight(t *testing.T) {
	if amount := sageFreightAmountMinor(sagepolicy.ShipCodeRule{FreightWriteback: "Omit"}, []int64{1234}); amount != nil {
		t.Fatalf("sageFreightAmountMinor() = %d, want nil for Omit", *amount)
	}
}

// TestSageFulfillmentStoreReplaysUnacknowledgedTerminalResult verifies terminal results survive restart and block a duplicate document shipment until Sage acknowledges writeback.
func TestSageFulfillmentStoreReplaysUnacknowledgedTerminalResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sage-fulfillment-sessions.json")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, err := loadSageFulfillmentStoreFile(path, now)
	if err != nil {
		t.Fatalf("loadSageFulfillmentStoreFile() error = %v", err)
	}
	request := sageFulfillmentRequest{RequestID: "request-1", Document: sageFulfillmentDocument{SalesOrderNo: "SO-1 ", InvoiceNo: "INV-1 "}}
	if _, err := store.begin(request, now); err != nil {
		t.Fatalf("begin() error = %v", err)
	}
	result := sageFulfillmentResult{RequestID: request.RequestID, Status: "COMPLETED", SalesOrderNo: request.Document.SalesOrderNo, InvoiceNo: request.Document.InvoiceNo, Tracking: []sageFulfillmentTracking{{PackageNumber: 1, TrackingNumber: "1Z123"}}}
	if err := store.complete(request, sageFulfillmentStateCompleted, result, now); err != nil {
		t.Fatalf("complete() error = %v", err)
	}
	store, err = loadSageFulfillmentStoreFile(path, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("reload store error = %v", err)
	}
	if replay, found, err := store.replay(request); err != nil || !found || replay.Tracking[0].TrackingNumber != "1Z123" {
		t.Fatalf("replay same request = %#v, %t, %v; want persisted result", replay, found, err)
	}
	duplicateDocument := request
	duplicateDocument.RequestID = "request-2"
	duplicateDocument.Document.SalesOrderNo = "SO-1"
	duplicateDocument.Document.InvoiceNo = "INV-1"
	if replay, found, err := store.replay(duplicateDocument); err != nil || !found || replay.RequestID != duplicateDocument.RequestID || replay.SalesOrderNo != duplicateDocument.Document.SalesOrderNo || replay.InvoiceNo != duplicateDocument.Document.InvoiceNo {
		t.Fatalf("replay unacknowledged document = %#v, %t, %v; want result aliased to retry request and document", replay, found, err)
	}
	if _, err := store.acknowledge(duplicateDocument.RequestID, sageWritebackApplied, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("acknowledge() error = %v", err)
	}
	if store.current != nil {
		t.Fatalf("acknowledge(APPLIED) retained recovery record %#v; want nil", store.current)
	}
	thirdRequest := duplicateDocument
	thirdRequest.RequestID = "request-3"
	if _, found, err := store.replay(thirdRequest); err != nil || found {
		t.Fatalf("replay acknowledged new request found = %t, error = %v; want false, nil", found, err)
	}
}

// TestSageFulfillmentStoreMigratesVersionOneHistory verifies startup compacts pre-one-slot history to its newest non-applied record.
func TestSageFulfillmentStoreMigratesVersionOneHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sage-fulfillment-sessions.json")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	older := sageFulfillmentRecord{Request: sageFulfillmentRequest{RequestID: "older", Document: sageFulfillmentDocument{SalesOrderNo: "SO-1", InvoiceNo: "INV-1"}}, UpdatedAtUTC: now.Add(-time.Hour), TerminalResult: &sageFulfillmentResult{RequestID: "older", Status: "COMPLETED"}, WritebackState: sageWritebackFailed}
	newer := sageFulfillmentRecord{Request: sageFulfillmentRequest{RequestID: "newer", Document: sageFulfillmentDocument{SalesOrderNo: "SO-2", InvoiceNo: "INV-2"}}, UpdatedAtUTC: now, TerminalResult: &sageFulfillmentResult{RequestID: "newer", Status: "COMPLETED"}, WritebackState: sageWritebackPending}
	legacy, err := json.Marshal(sageFulfillmentStoreDocument{Version: 1, Records: map[string]sageFulfillmentRecord{"older": older, "newer": newer}})
	if err != nil {
		t.Fatalf("marshal legacy store: %v", err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}

	store, err := loadSageFulfillmentStoreFile(path, now)
	if err != nil {
		t.Fatalf("loadSageFulfillmentStoreFile() error = %v", err)
	}
	if store.current == nil || store.current.Request.RequestID != newer.Request.RequestID {
		t.Fatalf("migrated current = %#v; want newest record", store.current)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated store: %v", err)
	}
	var document sageFulfillmentStoreDocument
	if err := json.Unmarshal(migrated, &document); err != nil {
		t.Fatalf("unmarshal migrated store: %v", err)
	}
	if document.Version != sageFulfillmentStoreVersion || document.Current == nil || len(document.Records) != 0 {
		t.Fatalf("migrated document = %#v; want version %d with one current record", document, sageFulfillmentStoreVersion)
	}
}

// TestSageFulfillmentStoreReplacesPriorDocumentResult verifies beginning a different Sage document deliberately discards the prior pending result so the operator is never blocked from the next order.
func TestSageFulfillmentStoreReplacesPriorDocumentResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sage-fulfillment-sessions.json")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, err := loadSageFulfillmentStoreFile(path, now)
	if err != nil {
		t.Fatalf("loadSageFulfillmentStoreFile() error = %v", err)
	}
	first := sageFulfillmentRequest{RequestID: "request-1", Document: sageFulfillmentDocument{SalesOrderNo: "SO-1", InvoiceNo: "INV-1"}}
	if _, err := store.begin(first, now); err != nil {
		t.Fatalf("begin(first) error = %v", err)
	}
	firstResult := sageFulfillmentResult{RequestID: first.RequestID, Status: "COMPLETED", SalesOrderNo: first.Document.SalesOrderNo, InvoiceNo: first.Document.InvoiceNo, Tracking: []sageFulfillmentTracking{{PackageNumber: 1, TrackingNumber: "1Z123"}}}
	if err := store.complete(first, sageFulfillmentStateCompleted, firstResult, now); err != nil {
		t.Fatalf("complete(first) error = %v", err)
	}
	second := sageFulfillmentRequest{RequestID: "request-2", Document: sageFulfillmentDocument{SalesOrderNo: "SO-2", InvoiceNo: "INV-2"}}
	if _, err := store.begin(second, now.Add(time.Minute)); err != nil {
		t.Fatalf("begin(second) error = %v", err)
	}
	if store.current == nil || store.current.Request.RequestID != second.RequestID {
		t.Fatalf("current recovery record = %#v; want second request", store.current)
	}
	if _, found, err := store.replay(first); err != nil || found {
		t.Fatalf("replay(replaced first) = found %t, error %v; want false, nil", found, err)
	}
}

// TestApplySageFulfillmentDetailWaitsForReplacementDetail verifies a prior order-detail screen cannot fail a new Sage session while its direct lookup is still replacing the view.
func TestApplySageFulfillmentDetailWaitsForReplacementDetail(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	responded := false
	ui.sageFulfillment = &sageFulfillmentSession{request: sageFulfillmentRequest{RequestID: "request-1"}, orderID: "new-order", responders: []func(sageFulfillmentResult){func(sageFulfillmentResult) { responded = true }}}
	ui.orders.view.orderDetailID = "old-order"
	ui.orders.view.orderDetailConnectionID = "old-connection"
	ui.orders.view.orderDetail = orders.Detail{OrderID: "old-order"}
	ui.orders.view.orderDetailStatus = "Simulated label result was sent to Sage."

	ui.applySageFulfillmentDetail()

	if responded || ui.sageFulfillment == nil {
		t.Fatal("applySageFulfillmentDetail() completed the new session from stale order detail")
	}
}

// TestSageFulfillmentStoreDoesNotReplayCancellationForNewRequest verifies a cancelled current record does not lock the sales order into immediate cancellation when the next request replaces it.
func TestSageFulfillmentStoreDoesNotReplayCancellationForNewRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sage-fulfillment-sessions.json")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, err := loadSageFulfillmentStoreFile(path, now)
	if err != nil {
		t.Fatalf("loadSageFulfillmentStoreFile() error = %v", err)
	}
	cancelled := sageFulfillmentRequest{RequestID: "cancelled-request", Document: sageFulfillmentDocument{SalesOrderNo: "SO-1", InvoiceNo: "INV-1"}}
	if _, err := store.begin(cancelled, now); err != nil {
		t.Fatalf("begin() error = %v", err)
	}
	if err := store.complete(cancelled, sageFulfillmentStateCancelled, sageFulfillmentResult{RequestID: cancelled.RequestID, Status: "CANCELLED", SalesOrderNo: "SO-1", InvoiceNo: "INV-1"}, now); err != nil {
		t.Fatalf("complete() error = %v", err)
	}
	retry := cancelled
	retry.RequestID = "new-request"
	if _, found, err := store.replay(retry); err != nil || found {
		t.Fatalf("replay cancelled document found = %t, error = %v; want false, nil", found, err)
	}
}

// TestSageFulfillmentShipmentAllowedBlocksReview verifies unresolved mappings and unconfirmed imported availability cannot complete an external Sage shipment.
func TestSageFulfillmentShipmentAllowedBlocksReview(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.sageFulfillment = &sageFulfillmentSession{preselectionApplied: true, unresolved: []string{"SKU-1"}}
	if ui.sageFulfillmentShipmentAllowed() {
		t.Fatal("sageFulfillmentShipmentAllowed() = true with unresolved mapping")
	}
	ui.sageFulfillment.unresolved = nil
	ui.sageFulfillment.availabilityRequired = true
	if ui.sageFulfillmentShipmentAllowed() {
		t.Fatal("sageFulfillmentShipmentAllowed() = true before availability confirmation")
	}
	ui.sageFulfillment.availabilityConfirmed = true
	if !ui.sageFulfillmentShipmentAllowed() {
		t.Fatal("sageFulfillmentShipmentAllowed() = false after review and availability confirmation")
	}
}
