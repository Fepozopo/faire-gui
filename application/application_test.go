package application

import (
	"context"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/layout"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/buildinfo"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
	"github.com/Fepozopo/faire-gui/internal/orderssync"
	"github.com/Fepozopo/faire-gui/updater"
)

// TestProfileSummaryUsesProfileValues verifies that profile data takes precedence over saved metadata.
func TestProfileSummaryUsesProfileValues(t *testing.T) {
	t.Parallel()

	profile := &faire.BrandProfile{
		BrandID:  faire.Ptr(faire.BrandID("brand-123")),
		Name:     faire.Ptr("Verified Brand"),
		Currency: faire.Ptr("USD"),
		Locale:   faire.Ptr("en-US"),
	}

	got := profileSummary(connections.Connection{
		Label:   "Saved Brand",
		BrandID: faire.BrandID("saved-brand"),
	}, profile)
	want := "Connected to Verified Brand • Brand ID: brand-123 • Currency: USD • Locale: en-US"
	if got != want {
		t.Fatalf("profileSummary() = %q, want %q", got, want)
	}
}

// TestProfileSummaryFallsBackToSavedBrandID verifies that incomplete API profiles preserve useful saved metadata.
func TestProfileSummaryFallsBackToSavedBrandID(t *testing.T) {
	t.Parallel()

	got := profileSummary(connections.Connection{
		Label:   "Saved Brand",
		BrandID: faire.BrandID("saved-brand"),
	}, &faire.BrandProfile{})
	want := "Connected to Saved Brand • Brand ID: saved-brand"
	if got != want {
		t.Fatalf("profileSummary() = %q, want %q", got, want)
	}
}

// TestProfileLoadErrorMessageHidesResponseBodies verifies that user-visible errors never expose API response data.
func TestProfileLoadErrorMessageHidesResponseBodies(t *testing.T) {
	t.Parallel()

	message := profileLoadErrorMessage(&faire.APIError{
		StatusCode: http.StatusInternalServerError,
		Body:       "sensitive response content",
	})
	if strings.Contains(message, "sensitive response content") {
		t.Fatalf("profileLoadErrorMessage() exposed the API response body: %q", message)
	}
	if !strings.Contains(message, "HTTP 500") {
		t.Fatalf("profileLoadErrorMessage() = %q, want HTTP status", message)
	}
}

// TestProfileLoadErrorMessageExplainsCredentialRejection verifies that rejected credentials have an actionable message.
func TestProfileLoadErrorMessageExplainsCredentialRejection(t *testing.T) {
	t.Parallel()

	message := profileLoadErrorMessage(&faire.APIError{StatusCode: http.StatusUnauthorized})
	if !strings.Contains(message, "credentials") {
		t.Fatalf("profileLoadErrorMessage() = %q, want credential guidance", message)
	}
}

// TestNewDesktopUIConfiguresScrollableListsAndMaskedToken verifies the persistent Gio controls, collapsed Settings group, default Orders page without an active connection, and created-at filter required by the desktop screens.
func TestNewDesktopUIConfiguresScrollableListsAndMaskedToken(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")

	if ui.selectedTab != ordersTab {
		t.Fatalf("selected tab = %d, want Orders tab %d at launch", ui.selectedTab, ordersTab)
	}
	if ui.activeConnectionID != "" || ui.activeConnectionLabel != "" {
		t.Fatalf("active connection = (%q, %q), want no active connection at launch", ui.activeConnectionID, ui.activeConnectionLabel)
	}
	if ui.settingsMenuOpen {
		t.Fatal("Settings submenu is open at launch, want a collapsed group")
	}
	if ui.brandsList.Axis != 1 || ui.connectionsList.Axis != 1 {
		t.Fatalf("list axes = (%d, %d), want both vertical", ui.brandsList.Axis, ui.connectionsList.Axis)
	}
	if !ui.accessTokenEditor.SingleLine || ui.accessTokenEditor.Mask != '•' {
		t.Fatalf("access-token editor configuration = {SingleLine:%t Mask:%q}, want single-line bullet mask", ui.accessTokenEditor.SingleLine, ui.accessTokenEditor.Mask)
	}
	if !ui.updatedAtMinEditor.SingleLine || ui.updatedAtMinEditor.Text() == "" || ui.ordersState.Query.UpdatedAtMin == "" {
		t.Fatalf("updated-at minimum defaults = {singleLine:%t input:%q timestamp:%q}, want configured 30-day lookback", ui.updatedAtMinEditor.SingleLine, ui.updatedAtMinEditor.Text(), ui.ordersState.Query.UpdatedAtMin)
	}
}

// TestNavigationHighlightUsesSettingsSurface verifies selected and hovered sidebar entries share Settings' light-gray surface.
func TestNavigationHighlightUsesSettingsSurface(t *testing.T) {
	t.Parallel()

	want := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	if got := navigationHighlight(true); got != want {
		t.Fatalf("navigationHighlight(true) = %#v, want %#v", got, want)
	}
	if got := navigationHighlight(false); got != (color.NRGBA{}) {
		t.Fatalf("navigationHighlight(false) = %#v, want transparent", got)
	}
}

// TestUpdateCheckOpensCompatibleReleasePrompt verifies a safe update result opens the startup prompt on the Gio frame goroutine.
func TestUpdateCheckOpensCompatibleReleasePrompt(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	availableVersion, err := updater.ParseVersion("0.2.0")
	if err != nil {
		t.Fatalf("ParseVersion() error = %v", err)
	}
	ui.updateResults <- updateCheckResult{
		available: true,
		update: updater.Update{
			Version: availableVersion,
			Asset:   updater.Asset{Name: "faire-gui_darwin_arm64", URL: "https://example.invalid/asset", Size: 1},
		},
	}

	ui.drainUpdateResults()

	if !ui.updateDialog.open || ui.updateDialog.update.Version.String() != "0.2.0" {
		t.Fatalf("update dialog = %#v, want an open prompt for 0.2.0", ui.updateDialog)
	}
}

// TestManualUpdateCheckReportsUpToDate verifies an explicit no-update result remains visible to the user.
func TestManualUpdateCheckReportsUpToDate(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.updateCheckDialog = updateCheckDialogState{open: true, checking: true}
	ui.updateResults <- updateCheckResult{userInitiated: true}

	ui.drainUpdateResults()

	if !ui.updateCheckDialog.open || ui.updateCheckDialog.checking || ui.updateCheckDialog.title != "You're up to date" {
		t.Fatalf("update-check dialog = %#v, want a completed up-to-date dialog", ui.updateCheckDialog)
	}
	wantMessage := "Version " + buildinfo.Version + " is the latest compatible version."
	if ui.updateCheckDialog.message != wantMessage {
		t.Fatalf("update-check message = %q, want %q", ui.updateCheckDialog.message, wantMessage)
	}
}

// TestManualUpdateCheckReportsFailure verifies an explicit check converts errors into safe retry guidance.
func TestManualUpdateCheckReportsFailure(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.updateCheckDialog = updateCheckDialogState{open: true, checking: true}
	ui.updateResults <- updateCheckResult{userInitiated: true, err: errors.New("network details must remain private")}

	ui.drainUpdateResults()

	if !ui.updateCheckDialog.open || ui.updateCheckDialog.checking || ui.updateCheckDialog.title != "Unable to check for updates" {
		t.Fatalf("update-check dialog = %#v, want a completed failure dialog", ui.updateCheckDialog)
	}
	if strings.Contains(ui.updateCheckDialog.message, "network details") || !strings.Contains(ui.updateCheckDialog.message, "internet connection") {
		t.Fatalf("update-check message = %q, want safe internet-connection guidance", ui.updateCheckDialog.message)
	}
}

// TestUpdateInstallFailureKeepsPromptOpen verifies users can retry after an installation failure without losing the selected update.
func TestUpdateInstallFailureKeepsPromptOpen(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.updateDialog = updateDialogState{open: true, installing: true}
	ui.updateInstallResults <- updateInstallResult{err: errors.New("disk full")}

	ui.drainUpdateInstallResults()

	if !ui.updateDialog.open || ui.updateDialog.installing || !strings.Contains(ui.updateDialog.status, "could not be installed") {
		t.Fatalf("update dialog = %#v, want open retryable failure state", ui.updateDialog)
	}
}

// TestSelectedOrdersLabelReportsCurrentSelectionCount verifies the Orders action bar reflects the active selection.
func TestSelectedOrdersLabelReportsCurrentSelectionCount(t *testing.T) {
	t.Parallel()

	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	if got := ui.selectedOrdersLabel(); got != "0 selected" {
		t.Fatalf("selectedOrdersLabel() = %q, want %q", got, "0 selected")
	}

	ui.ordersState.SelectedIDs = map[faire.OrderID]struct{}{
		"order-1": {},
		"order-2": {},
		"order-3": {},
	}
	if got := ui.selectedOrdersLabel(); got != "3 selected" {
		t.Fatalf("selectedOrdersLabel() = %q, want %q", got, "3 selected")
	}
}

// TestOrdersKnownStatesAreAlphabetical verifies the state-picker options follow their displayed label order.
func TestOrdersKnownStatesAreAlphabetical(t *testing.T) {
	t.Parallel()

	got := ordersKnownStates()
	want := []faire.OrderState{
		faire.OrderStateBackordered,
		faire.OrderStateCanceled,
		faire.OrderStateDelivered,
		faire.OrderStateInTransit,
		faire.OrderStateNew,
		faire.OrderStatePendingRetailerConfirmation,
		faire.OrderStatePreTransit,
		faire.OrderStateProcessing,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordersKnownStates() = %#v, want alphabetical options %#v", got, want)
	}
}

// TestBlockedOrderExportOpensDialog verifies an unmapped brand's export failure is presented prominently.
func TestBlockedOrderExportOpensDialog(t *testing.T) {
	t.Parallel()

	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.exportRequestID = 1
	ui.ordersExporting = true
	ui.orderExportResults <- orderExportResult{
		RequestID: 1,
		Status:    "CSV export is not configured for this connection's Faire brand.",
		Blocked:   true,
	}

	ui.drainOrderExportResults()

	if !ui.csvExportBlockedDialogOpen {
		t.Fatal("csvExportBlockedDialogOpen = false, want true")
	}
	if ui.ordersExporting || ui.ordersState.Status != "CSV export is not configured for this connection's Faire brand." {
		t.Fatalf("export state = {exporting:%t status:%q}, want completed blocking status", ui.ordersExporting, ui.ordersState.Status)
	}
}

// TestCompletedOrderExportOpensDialog verifies successful exports prominently identify their saved filename.
func TestCompletedOrderExportOpensDialog(t *testing.T) {
	t.Parallel()

	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.exportRequestID = 1
	ui.ordersExporting = true
	ui.orderExportResults <- orderExportResult{
		RequestID: 1,
		Status:    "Exported 1 orders to Downloads as faire-new-orders-20260321142530.csv.",
		Filename:  "faire-new-orders-20260321142530.csv",
		Completed: true,
	}

	ui.drainOrderExportResults()

	if !ui.csvExportCompletedDialogOpen || ui.csvExportCompletedFilename != "faire-new-orders-20260321142530.csv" {
		t.Fatalf("completed dialog = {open:%t filename:%q}, want saved export filename", ui.csvExportCompletedDialogOpen, ui.csvExportCompletedFilename)
	}
}

// TestExportSalesSourceUsesProfileBrandID verifies exports ignore optional saved metadata and map the authenticated brand profile instead.
func TestExportSalesSourceUsesProfileBrandID(t *testing.T) {
	t.Parallel()

	source, found := exportSalesSource(&faire.BrandProfile{BrandID: faire.Ptr(faire.BrandID("b_56pfaass"))})
	if !found || source != orders.SalesSource("ASC") {
		t.Fatalf("exportSalesSource() = (%q, %t), want (ASC, true)", source, found)
	}
	if source, found := exportSalesSource(&faire.BrandProfile{}); found || source != "" {
		t.Fatalf("exportSalesSource() = (%q, %t), want an unmapped result", source, found)
	}
}

// TestSelectedOrderIDsSortsExportSelection verifies selected-order exports have a deterministic request order.
func TestSelectedOrderIDsSortsExportSelection(t *testing.T) {
	t.Parallel()

	got := selectedOrderIDs(map[faire.OrderID]struct{}{
		"order-3": {},
		"":        {},
		"order-1": {},
	})
	want := []faire.OrderID{"order-1", "order-3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedOrderIDs() = %#v, want %#v", got, want)
	}
}

// TestExportOrdersForStateFollowsAllPages verifies state exports request each cursor page and discard unexpected states.
func TestExportOrdersForStateFollowsAllPages(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/orders" {
			t.Fatalf("path = %q, want /orders", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Get("cursor") == "" {
			_, _ = writer.Write([]byte(`{"orders":[{"id":"order-1","state":"NEW"}],"cursor":"page-2"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"orders":[{"id":"order-2","state":"NEW"},{"id":"order-3","state":"BACKORDERED"}]}`))
	}))
	defer server.Close()
	client, err := faire.NewClient(faire.Config{BaseURL: server.URL, AccessToken: "test-token"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source, err := exportOrdersForState(context.Background(), client.Orders, faire.OrderStateNew)
	if err != nil {
		t.Fatalf("exportOrdersForState() error = %v", err)
	}
	if requests != 2 || len(source) != 2 || *source[0].ID != "order-1" || *source[1].ID != "order-2" {
		t.Fatalf("exported source = %#v after %d requests, want both New orders", source, requests)
	}
}

// TestWriteOrdersCSVCreatesPrivateCSV verifies exports are written atomically to the requested directory.
func TestWriteOrdersCSVCreatesPrivateCSV(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	filename, err := writeOrdersCSV(directory, orderExportNew, orders.SalesSource("ASC"), []faire.Order{{ID: faire.Ptr(faire.OrderID("order-1"))}})
	if err != nil {
		t.Fatalf("writeOrdersCSV() error = %v", err)
	}
	const filenamePrefix = "faire-new-orders-"
	if !strings.HasPrefix(filename, filenamePrefix) || filepath.Ext(filename) != ".csv" {
		t.Fatalf("filename = %q, want timestamped new-order CSV", filename)
	}
	timestamp := strings.TrimSuffix(strings.TrimPrefix(filename, filenamePrefix), ".csv")
	if len(timestamp) != len("20060102150405") || strings.ContainsAny(timestamp, "TZ.") {
		t.Fatalf("filename timestamp = %q, want YYYYMMDDHHMMSS", timestamp)
	}
	contents, err := os.ReadFile(filepath.Join(directory, filename))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.HasPrefix(string(contents), "id,display_id,created_at") {
		t.Fatalf("CSV = %q, want CSV header", contents)
	}
}

// TestShutdownReleasesOrdersPresentationState verifies window teardown dereferences visible Orders rows and cancels in-flight work without a session cache.
func TestShutdownReleasesOrdersPresentationState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ui := newDesktopUI(ctx, cancel, nil, nil, nil, "")
	ui.ordersState.Rows = []orders.Row{{DisplayID: "EFGH123456"}}
	ui.ordersState.Cursor = "next-page"

	ui.shutdown()
	ui.shutdown()

	if ui.ordersState.Rows != nil || ui.ordersState.Cursor != "" {
		t.Fatalf("orders state = %#v, want rows and cursor cleared", ui.ordersState)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("shutdown() did not cancel the application context")
	}
}

// TestLoadOrderDetailPublishesOnlyTypedPresentation verifies a local snapshot becomes a detail model without an API call.
func TestLoadOrderDetailPublishesOnlyTypedPresentation(t *testing.T) {
	ctx := context.Background()
	store, err := ordersstore.Open(ctx, filepath.Join(t.TempDir(), "orders.sqlite3"))
	if err != nil {
		t.Fatalf("ordersstore.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	orderID := faire.OrderID("order-1")
	displayID, updatedAt, notes, addressName := "DISPLAY-1", "2026-02-03T04:05:06Z", "Private note", "Ada's Antiques"
	order := faire.Order{ID: &orderID, DisplayID: &displayID, UpdatedAt: &updatedAt, Notes: &notes, Address: &faire.Address{Name: &addressName}}
	record, err := orderssync.RecordFromOrder("connection-a", order, time.Date(2026, 2, 3, 5, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RecordFromOrder() error = %v", err)
	}
	if err := store.UpsertOrders(ctx, []ordersstore.OrderRecord{record}); err != nil {
		t.Fatalf("UpsertOrders() error = %v", err)
	}
	var result orderDetailResult
	loadOrderDetail(ctx, store, 1, "connection-a", orderID, func(value orderDetailResult) { result = value })
	if result.Status != "" || result.Detail.OrderID != orderID || result.Detail.DisplayID != displayID || result.Detail.Notes != notes || result.Detail.ShippingAddress.Name != addressName {
		t.Fatalf("detail result = %#v", result)
	}
}

// TestLocalRowsFormatRawDeliveryAndFinancialValues verifies cached table rows format raw delivery, total, and commission values only at presentation time.
func TestLocalRowsFormatRawDeliveryAndFinancialValues(t *testing.T) {
	total, commissionBPS := int64(1234), int64(1500)
	rows := localRows([]ordersstore.LocalRow{{OrderID: "order-1", DisplayID: "DISPLAY-1", AddressName: "Ada's Antiques", TotalAmountMinor: &total, TotalCurrency: "USD", CommissionBPS: &commissionBPS}})
	if len(rows) != 1 || rows[0].Customer != "Ada's Antiques" || rows[0].Total != "$12.34" || rows[0].Commission != "15.00%" {
		t.Fatalf("localRows() = %#v, want formatted raw values", rows)
	}
}

// TestDrainOrderDetailResultsRejectsStaleSelection verifies an old snapshot worker cannot replace a newer detail selection.
func TestDrainOrderDetailResultsRejectsStaleSelection(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.activeConnectionID = "connection-b"
	ui.orderDetailID = faire.OrderID("order-b")
	ui.detailRequestID = 2
	ui.orderDetailStatus = "current"
	ui.orderDetailResults <- orderDetailResult{RequestID: 1, ConnectionID: "connection-a", OrderID: faire.OrderID("order-a"), Detail: orders.Detail{DisplayID: "stale"}}

	ui.drainOrderDetailResults()

	if ui.orderDetail.DisplayID != "" || ui.orderDetailStatus != "current" {
		t.Fatalf("stale detail result changed current state: %#v, status=%q", ui.orderDetail, ui.orderDetailStatus)
	}
}

// TestDrainOrderDetailResultsUpdatesNewOrderCount verifies an accepted detail refresh updates the New tab badge.
func TestDrainOrderDetailResultsUpdatesNewOrderCount(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.activeConnectionID = "connection-a"
	ui.detailRequestID = 1
	ui.orderDetailID = faire.OrderID("order-a")
	ui.orderDetailLoading = true
	ui.orderDetailResults <- orderDetailResult{RequestID: 1, ConnectionID: "connection-a", OrderID: faire.OrderID("order-a"), Detail: orders.Detail{DisplayID: "ORDER-A"}, NewOrdersCount: 2, ApplyNewOrdersCount: true}

	ui.drainOrderDetailResults()

	if ui.newOrdersCount != 2 || ui.orderDetail.DisplayID != "ORDER-A" || ui.orderDetailLoading {
		t.Fatalf("detail result application = {newOrdersCount:%d detail:%#v loading:%t}, want count 2, ORDER-A, and completed loading", ui.newOrdersCount, ui.orderDetail, ui.orderDetailLoading)
	}
}

// TestOrdersLoadErrorMessageKeepsBadRequestFeedbackSafe verifies invalid sync feedback identifies only a safe phase.
func TestOrdersLoadErrorMessageKeepsBadRequestFeedbackSafe(t *testing.T) {
	message := ordersLoadErrorMessage(&orderssync.ListError{Phase: orderssync.ListPhaseHistory, Cursor: true, Err: &faire.APIError{StatusCode: 400, Body: "private response"}})
	if !strings.Contains(message, "older order-history synchronization follow-up page") || strings.Contains(message, "private response") {
		t.Fatalf("ordersLoadErrorMessage() = %q", message)
	}
}

// TestDrainOrderResultsRestoresPersistedHistoryBoundary verifies a selected connection restores its retained initial-history date in the local filter editor.
func TestDrainOrderResultsRestoresPersistedHistoryBoundary(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.ordersRequestID = 1
	boundary := "2025-03-21T00:00:00Z"
	ui.orderResults <- orderLoadResult{RequestID: 1, Rows: []orders.Row{}, Status: "Showing locally stored orders.", ApplyRows: true, UpdatedAtMin: boundary, ApplyBoundary: true}

	ui.drainOrderResults()

	if !ui.ordersHistoryBoundaryKnown || ui.ordersState.Query.UpdatedAtMin != boundary || ui.updatedAtMinEditor.Text() != historyBoundaryInput(boundary) {
		t.Fatalf("restored history boundary = %q, editor=%q, known=%v", ui.ordersState.Query.UpdatedAtMin, ui.updatedAtMinEditor.Text(), ui.ordersHistoryBoundaryKnown)
	}
}

// TestDrainOrderResultsUpdatesNewOrderCount verifies only the latest eligible Orders result can replace the New tab badge.
func TestDrainOrderResultsUpdatesNewOrderCount(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.ordersRequestID = 2
	ui.newOrdersCount = 3
	ui.orderResults <- orderLoadResult{RequestID: 1, NewOrdersCount: 99, ApplyNewOrdersCount: true}
	ui.orderResults <- orderLoadResult{RequestID: 2, NewOrdersCount: 4, ApplyNewOrdersCount: true}

	ui.drainOrderResults()

	if ui.newOrdersCount != 4 {
		t.Fatalf("newOrdersCount = %d, want 4", ui.newOrdersCount)
	}
}

// TestDrainOrderResultsClearsRowsForAnEmptySuccessfulFilter verifies empty local state filters replace, rather than retain, stale rows.
func TestDrainOrderResultsClearsRowsForAnEmptySuccessfulFilter(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.ordersRequestID = 1
	ui.ordersState.Rows = []orders.Row{{ID: faire.OrderID("order-all"), DisplayID: "ALL-ORDER"}}
	ui.ordersState.Loaded = true
	ui.orderResults <- orderLoadResult{RequestID: 1, Rows: []orders.Row{}, Status: "No locally stored orders match this state.", ApplyRows: true}

	ui.drainOrderResults()

	if len(ui.ordersState.Rows) != 0 || !ui.ordersState.Loaded || ui.ordersState.Status != "No locally stored orders match this state." {
		t.Fatalf("orders state = %#v, want an applied empty state-filter result", ui.ordersState)
	}
}

// TestDrainOrderResultsPreservesRowsForAFailedRefresh verifies errors do not erase a useful locally stored table.
func TestDrainOrderResultsPreservesRowsForAFailedRefresh(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.ordersRequestID = 1
	ui.ordersState.Rows = []orders.Row{{ID: faire.OrderID("order-local"), DisplayID: "LOCAL-ORDER"}}
	ui.orderResults <- orderLoadResult{RequestID: 1, Status: "Orders could not be loaded."}

	ui.drainOrderResults()

	if len(ui.ordersState.Rows) != 1 || ui.ordersState.Status != "Orders could not be loaded." {
		t.Fatalf("orders state = %#v, want retained local rows and error status", ui.ordersState)
	}
}

// TestCancelEditorReturnsToDirectTokenCreation verifies cancellation resets every persistent Gio editor, including transient token text.
func TestCancelEditorReturnsToDirectTokenCreation(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.editorMode = connectionEditorEnvironmentImport
	ui.editing = connections.Connection{ID: "connection-id"}
	ui.labelEditor.SetText("Imported Brand")
	ui.brandIDEditor.SetText("brand-id")
	ui.environmentEditor.SetText("API_TOKEN_21C")
	ui.accessTokenEditor.SetText("transient-token")

	ui.resetEditor()

	if ui.editorMode != connectionEditorCreate {
		t.Fatalf("editorMode = %d, want %d", ui.editorMode, connectionEditorCreate)
	}
	if ui.editing != (connections.Connection{}) {
		t.Fatalf("editing = %#v, want zero value", ui.editing)
	}
	if ui.labelEditor.Text() != "" || ui.brandIDEditor.Text() != "" || ui.environmentEditor.Text() != "" || ui.accessTokenEditor.Text() != "" {
		t.Fatalf("editor fields were not cleared: label=%q brandID=%q environment=%q token=%q", ui.labelEditor.Text(), ui.brandIDEditor.Text(), ui.environmentEditor.Text(), ui.accessTokenEditor.Text())
	}
}

// TestSelectConnectionScrollsToStatus verifies profile loading resets a deeply scrolled Brands list so its status feedback is visible.
func TestSelectConnectionScrollsToStatus(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, new(app.Window), nil, nil, "")
	ui.brandsList.Position.First = 12
	ui.brandsList.Position.Offset = -24
	ui.brandsList.Position.BeforeEnd = true

	ui.selectConnection("connection-id")

	if ui.brandsList.Position != (layout.Position{}) {
		t.Fatalf("brands list position = %#v, want zero position", ui.brandsList.Position)
	}
	if ui.status != "Saved connections are unavailable. Restart the app after resolving the credential-store issue." {
		t.Fatalf("status = %q, want unavailable-connections guidance", ui.status)
	}
}

// TestBeginMetadataEditScrollsToForm verifies an edit request resets a deeply scrolled connection list so the editor is visible.
func TestBeginMetadataEditScrollsToForm(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, new(app.Window), nil, nil, "")
	ui.connectionsList.Position.First = 12
	ui.connectionsList.Position.Offset = -24
	ui.connectionsList.Position.BeforeEnd = true
	connection := connections.Connection{ID: "connection-id", Label: "Brand", BrandID: faire.BrandID("brand-id")}

	ui.beginMetadataEdit(connection)

	if ui.connectionsList.Position != (layout.Position{}) {
		t.Fatalf("connections list position = %#v, want zero position", ui.connectionsList.Position)
	}
	if ui.editorMode != connectionEditorMetadata || ui.labelEditor.Text() != "Brand" || ui.brandIDEditor.Text() != "brand-id" {
		t.Fatalf("metadata editor was not prepared: mode=%d label=%q brandID=%q", ui.editorMode, ui.labelEditor.Text(), ui.brandIDEditor.Text())
	}
}

// TestReconcileRowControlsRemovesDeletedConnections verifies stable Gio click state is retained only for active connection rows.
func TestReconcileRowControlsRemovesDeletedConnections(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, []connections.Connection{{ID: "kept"}}, "")
	kept := ui.rowControlsFor("kept")
	ui.rowControlsFor("deleted")

	ui.reconcileRowControls()

	if got := ui.rowControls["kept"]; got != kept {
		t.Fatalf("kept row controls = %p, want original %p", got, kept)
	}
	if _, ok := ui.rowControls["deleted"]; ok {
		t.Fatal("deleted row controls still exist")
	}
}

// TestRequestDeleteRequiresConfirmationState verifies row deletion only opens metadata-only modal state.
func TestRequestDeleteRequiresConfirmationState(t *testing.T) {
	connection := connections.Connection{ID: "connection-id", Label: "Brand"}
	ui := newDesktopUI(context.Background(), func() {}, new(app.Window), nil, nil, "")

	ui.requestDelete(connection)

	if !ui.deleteDialog.open || ui.deleteDialog.connection != connection {
		t.Fatalf("delete dialog = %#v, want open dialog for %#v", ui.deleteDialog, connection)
	}
}

// TestExplicitEnvironmentTokenReadsOnlyTheNamedVariable verifies explicit imports trim only the variable name.
func TestExplicitEnvironmentTokenReadsOnlyTheNamedVariable(t *testing.T) {
	const environmentName = "FAIRE_GUI_IMPORT_TEST_TOKEN"
	const accessToken = "test-direct-token"
	t.Setenv(environmentName, accessToken)

	got, err := explicitEnvironmentToken(" " + environmentName + " ")
	if err != nil {
		t.Fatalf("explicitEnvironmentToken() error = %v", err)
	}
	if got != accessToken {
		t.Fatalf("explicitEnvironmentToken() = %q, want %q", got, accessToken)
	}
}

// TestExplicitEnvironmentTokenRejectsEmptyVariables verifies imports require a non-empty explicit source.
func TestExplicitEnvironmentTokenRejectsEmptyVariables(t *testing.T) {
	t.Setenv("FAIRE_GUI_IMPORT_EMPTY_TEST_TOKEN", "")

	if _, err := explicitEnvironmentToken("FAIRE_GUI_IMPORT_EMPTY_TEST_TOKEN"); err == nil {
		t.Fatal("explicitEnvironmentToken() error = nil, want missing-or-empty variable error")
	}
}
