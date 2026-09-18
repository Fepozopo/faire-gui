package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

const (
	// sageFulfillmentProtocolVersion is the version emitted by sage/LaunchFaireFulfillment.vbs.
	sageFulfillmentProtocolVersion = 1
	// maxSageFulfillmentRequests bounds queued transport requests so workers never wait on the frame loop.
	maxSageFulfillmentRequests = 4
	// maxSageFulfillmentRequestsPerFrame preserves Gio responsiveness during a local request burst.
	maxSageFulfillmentRequestsPerFrame = 2
)

// sageFulfillmentRequest is the credential-free Sage Shipping Data Entry snapshot received over local IPC.
// It intentionally contains only fulfillment data; Faire credentials remain in the saved connection store.
type sageFulfillmentRequest struct {
	ProtocolVersion int                     `json:"protocolVersion"`
	RequestID       string                  `json:"requestId"`
	Source          sageFulfillmentSource   `json:"source"`
	Document        sageFulfillmentDocument `json:"document"`
	ShipTo          sageFulfillmentAddress  `json:"shipTo"`
	Lines           []sageFulfillmentLine   `json:"lines"`
}

// sageFulfillmentSource identifies the local Sage origin without supplying a user credential.
type sageFulfillmentSource struct {
	CompanyCode string `json:"companyCode"`
	Workstation string `json:"workstation"`
}

// sageFulfillmentDocument identifies the Sage document, target Faire order, and connection-selection source.
type sageFulfillmentDocument struct {
	SalesOrderNo   string  `json:"salesOrderNo"`
	InvoiceNo      string  `json:"invoiceNo"`
	FaireDisplayID string  `json:"faireDisplayId"`
	SalesSource    string  `json:"salesSource"`
	ShipVia        string  `json:"shipVia"`
	FreightAmount  float64 `json:"freightAmount"`
}

// sageFulfillmentAddress is the shipping-address snapshot reserved for shipment and future label workflows.
type sageFulfillmentAddress struct {
	Name       string `json:"name"`
	Company    string `json:"company"`
	Address1   string `json:"address1"`
	Address2   string `json:"address2"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
}

// sageFulfillmentLine carries one affected Sage fulfillment line and its kit relationship metadata.
type sageFulfillmentLine struct {
	SageLineKey         string  `json:"sageLineKey"`
	ItemCode            string  `json:"itemCode"`
	ItemType            string  `json:"itemType"`
	Description         string  `json:"description"`
	QuantityShipped     float64 `json:"quantityShipped"`
	QuantityBackordered float64 `json:"quantityBackordered"`
	SalesKitLineKey     string  `json:"salesKitLineKey"`
	ExplodedKitItem     string  `json:"explodedKitItem"`
}

// sageFulfillmentState identifies the durable workflow phase without coupling recovery data to a Gio widget.
type sageFulfillmentState string

const (
	// sageFulfillmentStateReceived is persisted before the GUI starts loading an order.
	sageFulfillmentStateReceived sageFulfillmentState = "RECEIVED"
	// sageFulfillmentStateAvailabilityPending waits for the operator's explicit availability confirmation.
	sageFulfillmentStateAvailabilityPending sageFulfillmentState = "AVAILABILITY_PENDING"
	// sageFulfillmentStateShipmentReady permits the operator to submit the external shipment after all required availability work succeeded.
	sageFulfillmentStateShipmentReady sageFulfillmentState = "SHIPMENT_READY"
	// sageFulfillmentStateShipmentPending waits for Faire to persist the external shipment.
	sageFulfillmentStateShipmentPending sageFulfillmentState = "SHIPMENT_PENDING"
	// sageFulfillmentStateCompleted retains an immutable completed response for Sage replay.
	sageFulfillmentStateCompleted sageFulfillmentState = "COMPLETED"
	// sageFulfillmentStateCancelled retains an immutable cancellation response for Sage replay.
	sageFulfillmentStateCancelled sageFulfillmentState = "CANCELLED"
	// sageFulfillmentStateFailed retains an immutable failure response for Sage replay.
	sageFulfillmentStateFailed sageFulfillmentState = "FAILED"
)

// sageFulfillmentTracking is one numbered package tracking record that Sage is authorized to replace.
type sageFulfillmentTracking struct {
	PackageNumber  int    `json:"packageNumber"`
	TrackingNumber string `json:"trackingNumber"`
}

// sageFulfillmentPackageItem is one Sage item allocation assigned to a numbered package.
type sageFulfillmentPackageItem struct {
	PackageNumber int     `json:"packageNumber"`
	ItemCode      string  `json:"itemCode"`
	ItemType      string  `json:"itemType"`
	Quantity      float64 `json:"quantity"`
}

// sageFulfillmentResult is the typed terminal response allowed by the Sage bridge.
// FreightAmountMinor is nil when policy requires Sage to retain its existing freight amount.
type sageFulfillmentResult struct {
	RequestID          string                       `json:"requestId"`
	Status             string                       `json:"status"`
	SalesOrderNo       string                       `json:"salesOrderNo"`
	InvoiceNo          string                       `json:"invoiceNo"`
	Tracking           []sageFulfillmentTracking    `json:"tracking,omitempty"`
	PackageItems       []sageFulfillmentPackageItem `json:"packageItems,omitempty"`
	FreightAmountMinor *int64                       `json:"freightAmountMinor,omitempty"`
	Error              string                       `json:"error,omitempty"`
}

// sageFulfillmentInbound transfers a validated IPC request to the Gio frame goroutine.
// respond is safe to call once and never exposes a transport connection to UI code.
type sageFulfillmentInbound struct {
	request sageFulfillmentRequest
	respond func(sageFulfillmentResult)
}

// sageFulfillmentListenerResult transfers a listener startup or runtime failure to the Gio frame goroutine without exposing operating-system error details in the UI.
type sageFulfillmentListenerResult struct {
	failed bool
}

// sageFulfillmentSession retains the active Sage-launched workflow while the user reviews the Order Details page.
type sageFulfillmentSession struct {
	request                 sageFulfillmentRequest
	orderID                 faire.OrderID
	responders              []func(sageFulfillmentResult)
	state                   sageFulfillmentState
	status                  string
	unresolved              []string
	preselectionApplied     bool
	availabilityRequired    bool
	availabilityConfirmed   bool
	detailRefreshRequested  bool
	submittedShipments      []sageExternalShipment
	cancelButton            widget.Clickable
	simulateLabelButton     widget.Clickable
	confirmCancelButton     widget.Clickable
	confirmSimulationButton widget.Clickable
	keepSessionButton       widget.Clickable
	cancelRequested         bool
	simulationRequested     bool
}

// sageFulfillmentRecovery retains a terminal result in the Order Details UI after the HTTP response has been sent.
// It gives the operator copyable tracking and freight information when Sage reports a writeback failure.
type sageFulfillmentRecovery struct {
	result              sageFulfillmentResult
	writebackState      sageWritebackState
	status              string
	copyButton          widget.Clickable
	trackingCopyButtons []widget.Clickable
}

// parseSageFulfillmentRequest decodes and validates the narrow protocol-v1 request before it reaches UI state.
func parseSageFulfillmentRequest(payload []byte) (sageFulfillmentRequest, error) {
	var request sageFulfillmentRequest
	if err := json.Unmarshal(payload, &request, json.RejectUnknownMembers(true)); err != nil {
		return sageFulfillmentRequest{}, fmt.Errorf("invalid Sage fulfillment request: %w", err)
	}
	if request.ProtocolVersion != sageFulfillmentProtocolVersion {
		return sageFulfillmentRequest{}, fmt.Errorf("unsupported Sage fulfillment protocol version %d", request.ProtocolVersion)
	}
	if !safeSageRequestID(request.RequestID) {
		return sageFulfillmentRequest{}, fmt.Errorf("invalid Sage fulfillment request ID")
	}
	if strings.TrimSpace(request.Document.SalesOrderNo) == "" || strings.TrimSpace(request.Document.InvoiceNo) == "" {
		return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request is missing its sales order or invoice")
	}
	if _, err := orders.NormalizeDisplayID(request.Document.FaireDisplayID); err != nil {
		return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request has an invalid Faire display ID")
	}
	if strings.TrimSpace(request.Document.SalesSource) == "" {
		return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request is missing its sales source")
	}
	if len(request.Lines) == 0 {
		return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request has no shipped or backordered lines")
	}

	affected := false
	for _, line := range request.Lines {
		if strings.TrimSpace(line.SageLineKey) == "" || strings.TrimSpace(line.ItemCode) == "" {
			return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request contains a line without an identity or item code")
		}
		if line.QuantityShipped < 0 || line.QuantityBackordered < 0 {
			return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request contains a negative quantity")
		}
		affected = affected || line.QuantityShipped > 0 || line.QuantityBackordered > 0
	}
	if !affected {
		return sageFulfillmentRequest{}, fmt.Errorf("Sage fulfillment request has no shipped or backordered quantities")
	}
	return request, nil
}

// formatSageFulfillmentResult returns the line-oriented ASCII-safe terminal response expected by the Sage script.
func formatSageFulfillmentResult(result sageFulfillmentResult) string {
	var response strings.Builder
	response.WriteString("RequestID:")
	response.WriteString(sageResultValue(result.RequestID))
	response.WriteString("\r\nStatus:")
	response.WriteString(sageResultValue(result.Status))
	response.WriteString("\r\nSalesOrderNo:")
	response.WriteString(sageResultValue(result.SalesOrderNo))
	response.WriteString("\r\nInvoiceNo:")
	response.WriteString(sageResultValue(result.InvoiceNo))
	response.WriteString("\r\n")
	for _, tracking := range result.Tracking {
		response.WriteString("Tracking:")
		response.WriteString(itoa(tracking.PackageNumber))
		response.WriteString("|")
		response.WriteString(sageResultValue(tracking.TrackingNumber))
		response.WriteString("\r\n")
	}
	for _, item := range result.PackageItems {
		response.WriteString("PackageItem:")
		response.WriteString(itoa(item.PackageNumber))
		response.WriteString("|")
		response.WriteString(sageResultValue(item.ItemCode))
		response.WriteString("|")
		response.WriteString(sageResultValue(item.ItemType))
		response.WriteString("|")
		response.WriteString(formatSageQuantity(item.Quantity))
		response.WriteString("\r\n")
	}
	if result.FreightAmountMinor != nil {
		response.WriteString("FreightAmount:")
		response.WriteString(formatDollarAmount(*result.FreightAmountMinor))
		response.WriteString("\r\n")
	}
	response.WriteString(resultErrorLine(result.Error))
	response.WriteString("Done\r\n")
	return response.String()
}

// resultErrorLine omits an empty error so successful or cancelled results remain compact.
func resultErrorLine(value string) string {
	if value == "" {
		return ""
	}
	return "Error:" + sageResultValue(value) + "\r\n"
}

// sageResultValue removes protocol-breaking line breaks from UI-safe response values.
func sageResultValue(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
}

// safeSageRequestID allows the UUID-like request IDs emitted by Sage while keeping local state bounded.
func safeSageRequestID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

// startSageFulfillmentListener starts the temporary direct HTTP transport only after the user has opted in.
func (ui *DesktopUI) startSageFulfillmentListener() {
	if !ui.sageFulfillmentEnabled || ui.sageFulfillmentCancel != nil || ui.sageFulfillmentStore == nil {
		return
	}
	listenerContext, cancel := context.WithCancel(ui.ctx)
	ui.sageFulfillmentCancel = cancel
	ui.startWorker(func() {
		err := serveSageFulfillmentHTTP(listenerContext, ui.publishSageFulfillmentRequest, ui.sageFulfillmentStore.replay, ui.acknowledgeSageFulfillment)
		if err == nil || listenerContext.Err() != nil {
			return
		}
		select {
		case ui.sageFulfillmentListenerResults <- sageFulfillmentListenerResult{failed: true}:
			ui.invalidate()
		case <-ui.ctx.Done():
		}
	})
}

// setSageFulfillmentEnabled persists the user's opt-in choice and opens or closes the temporary HTTP listener.
// Disabling is rejected during an active session so the outstanding Sage request always receives one terminal result.
func (ui *DesktopUI) setSageFulfillmentEnabled(enabled bool) {
	if enabled && ui.sageFulfillmentStore == nil {
		store, err := loadSageFulfillmentStore()
		if err != nil {
			ui.sageFulfillmentSettingsMessage = "Cannot enable Sage fulfillment: local recovery data is unavailable. Close the app and repair or remove the recovery-data file, then retry."
			ui.status = ui.sageFulfillmentSettingsMessage
			ui.invalidate()
			return
		}
		ui.sageFulfillmentStore = store
		ui.sageFulfillmentStoreError = ""
	}
	if enabled && ui.sageShipCodeRules == nil {
		ui.sageFulfillmentSettingsMessage = "Cannot enable Sage fulfillment: the packaged Ship Via policy is unavailable."
		ui.status = ui.sageFulfillmentSettingsMessage
		ui.invalidate()
		return
	}
	if ui.sageFulfillmentEnabled == enabled {
		return
	}
	if !enabled && ui.sageFulfillment != nil {
		ui.sageFulfillmentSettingsMessage = "Finish or cancel the active Sage fulfillment session before disabling the integration."
		ui.status = ui.sageFulfillmentSettingsMessage
		ui.invalidate()
		return
	}
	if err := saveSageFulfillmentSettings(enabled); err != nil {
		ui.sageFulfillmentSettingsMessage = "Could not save the Sage fulfillment setting. The integration state was not changed."
		ui.status = ui.sageFulfillmentSettingsMessage
		ui.invalidate()
		return
	}

	ui.sageFulfillmentEnabled = enabled
	if enabled {
		ui.startSageFulfillmentListener()
		ui.sageFulfillmentSettingsMessage = "Sage fulfillment integration is enabled. Windows may request firewall permission for the temporary HTTP endpoint."
	} else {
		if ui.sageFulfillmentCancel != nil {
			ui.sageFulfillmentCancel()
			ui.sageFulfillmentCancel = nil
		}
		ui.sageFulfillmentSettingsMessage = "Sage fulfillment integration is disabled and its HTTP endpoint is closed."
	}
	ui.status = ui.sageFulfillmentSettingsMessage
	ui.invalidate()
}

// publishSageFulfillmentRequest queues a validated transport request without allowing a worker to mutate Gio state.
func (ui *DesktopUI) publishSageFulfillmentRequest(inbound sageFulfillmentInbound) {
	select {
	case ui.sageFulfillmentRequests <- inbound:
		ui.invalidate()
	default:
		ui.respondSageInboundTerminal(inbound, sageFulfillmentFailure(inbound.request, "Faire GUI is busy with local fulfillment requests. Retry from Sage after the current request is handled."))
	}
}

// acknowledgeSageFulfillment records a worker-thread acknowledgement and publishes only a safe UI event after durable storage succeeds.
func (ui *DesktopUI) acknowledgeSageFulfillment(requestID string, state sageWritebackState) error {
	if ui.sageFulfillmentStore == nil {
		return fmt.Errorf("Sage fulfillment recovery data is unavailable")
	}
	result, err := ui.sageFulfillmentStore.acknowledge(requestID, state, time.Now().UTC())
	if err != nil {
		return err
	}
	select {
	case ui.sageFulfillmentAcknowledgements <- sageFulfillmentAcknowledgement{requestID: requestID, state: state, result: result}:
		ui.invalidate()
	default:
	}
	return nil
}

// sageFulfillmentAcknowledgement transfers a persisted Sage acknowledgement to the Gio frame goroutine.
type sageFulfillmentAcknowledgement struct {
	requestID string
	state     sageWritebackState
	result    sageFulfillmentResult
}

// drainSageFulfillmentAcknowledgements updates only the visible recovery state after the Sage bridge has durably acknowledged writeback.
func (ui *DesktopUI) drainSageFulfillmentAcknowledgements() {
	for {
		select {
		case acknowledgement := <-ui.sageFulfillmentAcknowledgements:
			if ui.sageFulfillmentRecovery == nil || !sameSageDocument(sageFulfillmentDocument{SalesOrderNo: ui.sageFulfillmentRecovery.result.SalesOrderNo, InvoiceNo: ui.sageFulfillmentRecovery.result.InvoiceNo}, sageFulfillmentDocument{SalesOrderNo: acknowledgement.result.SalesOrderNo, InvoiceNo: acknowledgement.result.InvoiceNo}) {
				continue
			}
			ui.sageFulfillmentRecovery.writebackState = acknowledgement.state
			if acknowledgement.state == sageWritebackApplied {
				ui.sageFulfillmentRecovery.status = "Sage confirmed writeback was applied. Recovery data remains visible until you leave this order."
			} else {
				ui.sageFulfillmentRecovery.status = "Sage writeback failed. Copy this recovery data, correct Sage, then rerun the Sage fulfillment action to replay the saved result."
			}
		default:
			return
		}
	}
}

// drainSageFulfillmentListenerResults makes a listener bind/runtime failure actionable and restores the Settings enable action for retry.
func (ui *DesktopUI) drainSageFulfillmentListenerResults() {
	for {
		select {
		case result := <-ui.sageFulfillmentListenerResults:
			if !result.failed {
				continue
			}
			if ui.sageFulfillmentCancel != nil {
				ui.sageFulfillmentCancel()
				ui.sageFulfillmentCancel = nil
			}
			ui.sageFulfillmentEnabled = false
			_ = saveSageFulfillmentSettings(false)
			ui.sageFulfillmentSettingsMessage = "Sage fulfillment integration could not open TCP port 18080. Close any other Faire GUI instance using that port, then enable the integration again."
			ui.status = ui.sageFulfillmentSettingsMessage
			ui.invalidate()
		default:
			return
		}
	}
}

// drainSageFulfillmentRequests applies a bounded number of queued local requests on Gio's frame goroutine.
func (ui *DesktopUI) drainSageFulfillmentRequests() {
	if ui.preparingStartup {
		return
	}
	for range maxSageFulfillmentRequestsPerFrame {
		select {
		case inbound := <-ui.sageFulfillmentRequests:
			ui.openSageFulfillmentSession(inbound)
		default:
			return
		}
	}
}

// openSageFulfillmentSession resolves the request's sales source to a saved connection and starts the existing display-ID lookup workflow.
func (ui *DesktopUI) openSageFulfillmentSession(inbound sageFulfillmentInbound) {
	if ui.sageFulfillment != nil {
		if ui.sageFulfillment.request.RequestID == inbound.request.RequestID && sameSageDocument(ui.sageFulfillment.request.Document, inbound.request.Document) {
			ui.sageFulfillment.responders = append(ui.sageFulfillment.responders, inbound.respond)
			return
		}
		ui.respondSageInboundTerminal(inbound, sageFulfillmentFailure(inbound.request, "A Sage fulfillment session is already open in Faire GUI. Finish or cancel it before starting another shipment."))
		return
	}
	if ui.sageFulfillmentStore == nil {
		inbound.respond(sageFulfillmentFailure(inbound.request, ui.sageFulfillmentStoreError))
		return
	}
	if replay, found, err := ui.sageFulfillmentStore.replay(inbound.request); err != nil {
		ui.respondSageInboundTerminal(inbound, sageFulfillmentFailure(inbound.request, "Sage fulfillment request identity could not be verified."))
		return
	} else if found {
		inbound.respond(replay)
		return
	}
	record, err := ui.sageFulfillmentStore.begin(inbound.request, time.Now().UTC())
	if err != nil {
		inbound.respond(sageFulfillmentFailure(inbound.request, "Faire GUI could not persist Sage fulfillment recovery data. No Faire shipment was created."))
		return
	}
	if ui.manager == nil || ui.orders.store == nil {
		ui.respondSageInboundTerminal(inbound, sageFulfillmentFailure(inbound.request, "Faire GUI is still preparing local data. Retry from Sage in a moment."))
		return
	}
	if ui.orders.dataActionConnectionID != "" {
		ui.respondSageInboundTerminal(inbound, sageFulfillmentFailure(inbound.request, "Faire GUI is rebuilding local order data. Retry from Sage after it finishes."))
		return
	}
	connection, found := ui.connectionForSageSalesSource(inbound.request.Document.SalesSource)
	if !found {
		ui.respondSageInboundTerminal(inbound, sageFulfillmentFailure(inbound.request, "No saved Faire connection matches Sage sales source '"+inbound.request.Document.SalesSource+"'. Configure that connection's brand ID, then retry from Sage."))
		return
	}
	displayID, _ := orders.NormalizeDisplayID(inbound.request.Document.FaireDisplayID)
	orderID, _ := orders.OrderIDFromDisplayID(displayID)
	// A prior recovery panel belongs to another request; its persisted record remains available, but it must not be mistaken for this session's order status.
	ui.sageFulfillmentRecovery = nil
	ui.sageFulfillment = &sageFulfillmentSession{
		request:               inbound.request,
		orderID:               orderID,
		responders:            []func(sageFulfillmentResult){inbound.respond},
		state:                 record.State,
		availabilityConfirmed: record.State == sageFulfillmentStateShipmentReady || record.State == sageFulfillmentStateShipmentPending,
		submittedShipments:    record.ExternalShipments,
		status:                "Refreshing Faire order " + displayID + " from Sage Shipping Data Entry…",
	}
	if ui.window != nil {
		// Gio delegates foreground policy to Windows; ActionRaise is best effort when another app owns focus.
		ui.window.Perform(system.ActionRaise)
	}
	if ui.activeConnectionID != connection.ID {
		ui.setActiveConnection(connection)
	}
	ui.openSageFulfillmentOrderDetail()
}

// connectionForSageSalesSource reverses the existing brand-to-sales-source policy for saved, credential-free connections.
func (ui *DesktopUI) connectionForSageSalesSource(salesSource string) (connections.Connection, bool) {
	wanted := strings.ToUpper(strings.TrimSpace(salesSource))
	var matched connections.Connection
	found := false
	for _, connection := range ui.connections {
		source, configured := orders.SalesSourceForBrand(connection.BrandID)
		if !configured || !strings.EqualFold(string(source), wanted) {
			continue
		}
		if found {
			return connections.Connection{}, false
		}
		matched = connection
		found = true
	}
	return matched, found
}

// applySageFulfillmentDetail waits for the existing detail loader, then imports only confidently matched Sage backorders into the reversible availability draft.
func (ui *DesktopUI) applySageFulfillmentDetail() {
	session := ui.sageFulfillment
	if session == nil || session.preselectionApplied || ui.orders.view.orderDetailLoading {
		return
	}
	// An older detail remains visible while the direct display-ID lookup runs. Do not turn that harmless transition into a terminal failure.
	if ui.orders.view.orderDetailID != session.orderID || ui.orders.view.orderDetailConnectionID != ui.activeConnectionID || ui.orders.view.orderDetailLoading {
		return
	}
	if ui.orders.view.orderDetail.OrderID != session.orderID {
		if ui.orders.view.orderDetailStatus != "" {
			ui.finishSageFulfillmentSession(sageFulfillmentFailure(session.request, "Faire could not open the requested order. "+ui.orders.view.orderDetailStatus))
		}
		return
	}
	if session.state == sageFulfillmentStateShipmentPending && len(ui.orders.view.orderDetail.Shipments) > 0 {
		completed, valid := sageCompletedFulfillmentResult(session.request, ui.orders.view.orderDetail, session.submittedShipments, sagePolicyForShipVia(ui.sageShipCodeRules, session.request.Document.ShipVia))
		if valid {
			ui.finishSageFulfillmentSession(completed)
			return
		}
		session.status = "Faire has shipment data, but the saved Sage recovery record cannot verify its tracking and cost details. Use manual recovery."
		ui.orders.view.orderDetailStatus = session.status
		return
	}

	selected, unresolved := sageBackorderedVariants(session.request.Lines, ui.orders.view.orderDetail.Items)
	for variantID := range selected {
		ui.orders.view.pendingUnavailable[variantID] = struct{}{}
	}
	session.unresolved = unresolved
	session.preselectionApplied = true
	session.availabilityRequired = len(selected) > 0
	if session.availabilityRequired {
		if ui.sageFulfillmentStore == nil || ui.sageFulfillmentStore.updateState(session.request, sageFulfillmentStateAvailabilityPending, time.Now().UTC()) != nil {
			ui.finishSageFulfillmentSession(sageFulfillmentFailure(session.request, "Faire GUI could not persist the Sage availability-review state. No shipment was created."))
			return
		}
	}
	if len(unresolved) > 0 {
		session.status = "Review required: " + strings.Join(unresolved, " ")
	} else if session.availabilityRequired {
		session.state = sageFulfillmentStateAvailabilityPending
		session.status = "Sage backorders were preselected as out of stock. Review them, then use Update availability to confirm before recording the shipment."
	} else {
		session.status = "Sage reported no backordered lines. Continue with shipment information when ready."
	}
	ui.orders.view.orderDetailStatus = session.status
	ui.invalidate()
}

// sageFulfillmentShipmentAllowed prevents a Sage-launched external shipment until line mappings and imported availability decisions are resolved.
func (ui *DesktopUI) sageFulfillmentShipmentAllowed() bool {
	session := ui.sageFulfillment
	if session == nil {
		return true
	}
	if !session.preselectionApplied {
		return false
	}
	if len(session.unresolved) > 0 {
		return false
	}
	if session.availabilityRequired && !session.availabilityConfirmed {
		return false
	}
	return true
}

// sageBackorderedVariants returns the safely matched Faire variants for positive Sage backorders and explains every unresolved line.
func sageBackorderedVariants(lines []sageFulfillmentLine, items []orders.DetailItem) (map[faire.VariantID]struct{}, []string) {
	byLineKey := make(map[string]sageFulfillmentLine, len(lines))
	for _, line := range lines {
		byLineKey[line.SageLineKey] = line
	}
	bySKU := make(map[string][]orders.DetailItem, len(items))
	for _, item := range items {
		key := strings.ToUpper(strings.TrimSpace(item.SKU))
		if key != "" {
			bySKU[key] = append(bySKU[key], item)
		}
	}

	selected := make(map[faire.VariantID]struct{})
	unresolved := make([]string, 0)
	for _, line := range lines {
		if line.QuantityBackordered <= 0 {
			continue
		}
		itemCode := line.ItemCode
		if sageExplodedKitItem(line.ExplodedKitItem) {
			parent, found := byLineKey[line.SalesKitLineKey]
			if !found || strings.TrimSpace(parent.ItemCode) == "" {
				unresolved = append(unresolved, "Sage kit component "+line.ItemCode+" could not be resolved to its parent kit SKU.")
				continue
			}
			itemCode = parent.ItemCode
		}
		matches := bySKU[strings.ToUpper(strings.TrimSpace(itemCode))]
		if len(matches) != 1 || !matches[0].AvailabilityEligible || matches[0].VariantID == "" {
			unresolved = append(unresolved, "Sage item "+itemCode+" does not map uniquely to an eligible Faire order variant.")
			continue
		}
		selected[matches[0].VariantID] = struct{}{}
	}
	return selected, unresolved
}

// sageExplodedKitItem recognizes the Sage truthy values used by known kit data while keeping unknown values non-exploded until verified.
func sageExplodedKitItem(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "Y", "YES", "T", "TRUE", "1":
		return true
	default:
		return false
	}
}

// handleSageFulfillmentEvents requires a second explicit confirmation for cancellation or simulated label purchase so the Sage-window activation click cannot end the session.
func (ui *DesktopUI) handleSageFulfillmentEvents(gtx layout.Context) {
	if session := ui.sageFulfillment; session != nil {
		if session.cancelRequested {
			if session.confirmCancelButton.Clicked(gtx) {
				ui.finishSageFulfillmentSession(sageFulfillmentResult{
					RequestID:    session.request.RequestID,
					Status:       "CANCELLED",
					SalesOrderNo: session.request.Document.SalesOrderNo,
					InvoiceNo:    session.request.Document.InvoiceNo,
				})
				ui.returnToOrdersTableAfterSageCancellation()
				return
			}
			if session.keepSessionButton.Clicked(gtx) {
				session.cancelRequested = false
				session.status = "Sage fulfillment remains active."
				ui.invalidate()
			}
			return
		}
		if session.simulationRequested {
			if session.confirmSimulationButton.Clicked(gtx) {
				ui.simulateSageLabelPurchase()
				return
			}
			if session.keepSessionButton.Clicked(gtx) {
				session.simulationRequested = false
				session.status = "Sage fulfillment remains active."
				ui.invalidate()
			}
			return
		}
		if session.cancelButton.Clicked(gtx) {
			session.cancelRequested = true
			session.status = "Cancel this Sage fulfillment session? Sage shipment data will not be changed."
			ui.invalidate()
			return
		}
		if session.simulateLabelButton.Clicked(gtx) && ui.sageFulfillmentShipmentAllowed() {
			session.simulationRequested = true
			session.status = "Simulate a label purchase? This makes no Faire API call, but it will send test tracking and $10.00 freight to Sage."
			ui.invalidate()
			return
		}
	}
	if recovery := ui.sageFulfillmentRecovery; recovery != nil {
		for index := range recovery.trackingCopyButtons {
			if !recovery.trackingCopyButtons[index].Clicked(gtx) {
				continue
			}
			gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(recovery.result.Tracking[index].TrackingNumber))})
			recovery.status = "Tracking number copied. Rerun the Sage fulfillment action to retry writeback; the saved Faire result will be replayed."
			ui.invalidate()
			return
		}
		if recovery.copyButton.Clicked(gtx) {
			gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(formatSageRecoveryData(recovery.result)))})
			recovery.status = "Recovery report copied. Rerun the Sage fulfillment action to retry writeback; the saved Faire result will be replayed."
			ui.invalidate()
		}
	}
}

// simulateSageLabelPurchase creates a clearly marked one-package test result without calling Faire. It exists only to exercise Sage tracking, freight, acknowledgement, and recovery behavior until Faire provides a label-purchase API.
func (ui *DesktopUI) simulateSageLabelPurchase() {
	session := ui.sageFulfillment
	if session == nil || !ui.sageFulfillmentShipmentAllowed() {
		return
	}
	for _, line := range session.request.Lines {
		if line.QuantityShipped > 0 && (!safeSageResultField(line.ItemCode) || !safeSageResultField(line.ItemType)) {
			ui.orders.view.orderDetailStatus = "Sage item code or item type cannot be returned through the safe simulated-label protocol. Use manual recovery."
			return
		}
	}
	shipment := sageSimulatedLabelShipment()
	if ui.sageFulfillmentStore == nil || ui.sageFulfillmentStore.updateShipmentPending(session.request, []sageExternalShipment{shipment}, time.Now().UTC()) != nil {
		ui.orders.view.orderDetailStatus = "Faire GUI could not persist the simulated label state. No Sage writeback was attempted."
		return
	}
	session.state = sageFulfillmentStateShipmentPending
	session.submittedShipments = []sageExternalShipment{shipment}
	result := sageSimulatedLabelFulfillmentResult(session.request, shipment, sagePolicyForShipVia(ui.sageShipCodeRules, session.request.Document.ShipVia))
	ui.finishSageFulfillmentSession(result)
	ui.orders.view.orderDetailStatus = "Simulated label result was sent to Sage. No Faire API call or label purchase was made."
}

// returnToOrdersTableAfterSageCancellation closes the Sage-requested detail screen, clears its direct lookup, and reloads the main local Orders table.
// Incrementing detailRequestID prevents an in-flight detail response from reopening or overwriting the returned table state.
func (ui *DesktopUI) returnToOrdersTableAfterSageCancellation() {
	ui.orders.detailRequestID++
	ui.orders.view.orderDetailOpen = false
	ui.orders.view.orderDetailLoading = false
	ui.orders.view.orderDetail = orders.Detail{}
	ui.orders.view.orderDetailID = ""
	ui.orders.view.orderDetailConnectionID = ""
	ui.orders.view.orderDetailStatus = ""
	ui.orders.view.resetPendingUnavailable()
	ui.orders.view.resetShipmentForm()
	ui.orders.view.search.SetText("")
	ui.orders.view.searchActive = false
	ui.orders.view.state.SelectedIDs = make(map[faire.OrderID]struct{})
	ui.startOrdersLoad(ordersLoadLocalOnly)
	ui.invalidate()
}

// finishSageFulfillmentSession persists one terminal response before delivering it to Sage, then retains a visible recovery banner without blocking later requests.
func (ui *DesktopUI) finishSageFulfillmentSession(result sageFulfillmentResult) {
	session := ui.sageFulfillment
	if session == nil {
		return
	}
	state := sageFulfillmentStateFailed
	switch result.Status {
	case "COMPLETED":
		state = sageFulfillmentStateCompleted
	case "CANCELLED":
		state = sageFulfillmentStateCancelled
	}
	if ui.sageFulfillmentStore == nil {
		result = sageFulfillmentFailure(session.request, "Faire GUI could not persist the terminal Sage fulfillment result. No Sage writeback was attempted.")
	} else if err := ui.sageFulfillmentStore.complete(session.request, state, result, time.Now().UTC()); err != nil {
		result = sageFulfillmentFailure(session.request, "Faire GUI could not persist the terminal Sage fulfillment result. No Sage writeback was attempted.")
		_ = ui.sageFulfillmentStore.complete(session.request, sageFulfillmentStateFailed, result, time.Now().UTC())
	}
	ui.sageFulfillment = nil
	ui.sageFulfillmentRecovery = &sageFulfillmentRecovery{result: result, writebackState: sageWritebackPending, status: "Sage writeback is pending. Keep this result available and retry the Sage action if writeback does not complete.", trackingCopyButtons: make([]widget.Clickable, len(result.Tracking))}
	for _, respond := range session.responders {
		respond(result)
	}
	ui.invalidate()
}

// sageFulfillmentFailure creates a document-correlated failed terminal result without exposing internal errors.
func sageFulfillmentFailure(request sageFulfillmentRequest, message string) sageFulfillmentResult {
	return sageFulfillmentResult{
		RequestID:    request.RequestID,
		Status:       "FAILED",
		SalesOrderNo: request.Document.SalesOrderNo,
		InvoiceNo:    request.Document.InvoiceNo,
		Error:        message,
	}
}

// respondSageInboundTerminal durably records a terminal result that occurs before an interactive session exists, then sends it through the request's one-shot responder.
func (ui *DesktopUI) respondSageInboundTerminal(inbound sageFulfillmentInbound, result sageFulfillmentResult) {
	if ui.sageFulfillmentStore != nil {
		if _, err := ui.sageFulfillmentStore.begin(inbound.request, time.Now().UTC()); err == nil {
			_ = ui.sageFulfillmentStore.complete(inbound.request, sageFulfillmentStateFailed, result, time.Now().UTC())
		}
	}
	inbound.respond(result)
}

// layoutSageFulfillmentSession renders the active Sage session state above Order Details so the operator knows Sage is waiting.
func (ui *DesktopUI) layoutSageFulfillmentSession(gtx layout.Context) layout.Dimensions {
	if session := ui.sageFulfillment; session != nil {
		return outlinedPanel(gtx, shipmentPanelBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Sage fulfillment session").Layout),
							layout.Rigid(bodyText(ui.theme, session.status, mutedTextColor)),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.layoutSageFulfillmentSessionActions(gtx, session)
					}),
				)
			})
		})
	}
	recovery := ui.sageFulfillmentRecovery
	if recovery == nil {
		return layout.Dimensions{}
	}
	return outlinedPanel(gtx, shipmentPanelBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutSageFulfillmentRecoveryDetails(gtx, recovery)
				}),
				layout.Rigid(primaryButton(ui.theme, &recovery.copyButton, "Copy recovery report")),
			)
		})
	})
}

// layoutSageFulfillmentRecoveryDetails shows the support-oriented recovery report and gives each tracking number a paste-ready copy control.
func (ui *DesktopUI) layoutSageFulfillmentRecoveryDetails(gtx layout.Context, recovery *sageFulfillmentRecovery) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Sage fulfillment recovery").Layout),
		layout.Rigid(bodyText(ui.theme, recovery.status, mutedTextColor)),
		layout.Rigid(bodyText(ui.theme, formatSageRecoveryMetadata(recovery.result), mutedTextColor)),
	}
	for index, tracking := range recovery.result.Tracking {
		button := &recovery.trackingCopyButtons[index]
		tracking := tracking
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(bodyText(ui.theme, "Package "+itoa(tracking.PackageNumber)+" tracking: "+tracking.TrackingNumber, mutedTextColor)),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return copyIconButton(gtx, button)
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutSageFulfillmentSessionActions renders either a deliberate confirmation pair or the ordinary session controls.
func (ui *DesktopUI) layoutSageFulfillmentSessionActions(gtx layout.Context, session *sageFulfillmentSession) layout.Dimensions {
	if session.cancelRequested {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(dangerButton(ui.theme, &session.confirmCancelButton, "Confirm cancel")),
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &session.keepSessionButton, "Keep working")),
		)
	}
	if session.simulationRequested {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(dangerButton(ui.theme, &session.confirmSimulationButton, "Confirm simulated label")),
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &session.keepSessionButton, "Keep working")),
		)
	}
	if ui.sageFulfillmentShipmentAllowed() {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(primaryButton(ui.theme, &session.simulateLabelButton, "Simulate label purchase (test)")),
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &session.cancelButton, "Cancel Sage session")),
		)
	}
	return primaryButton(ui.theme, &session.cancelButton, "Cancel Sage session")(gtx)
}

// sageFulfillmentResponder makes terminal session responses idempotent even when a transport and a UI failure race.
type sageFulfillmentResponder struct {
	once sync.Once
	send func(sageFulfillmentResult)
}

// respond sends result at most once for this request.
func (responder *sageFulfillmentResponder) respond(result sageFulfillmentResult) {
	responder.once.Do(func() {
		responder.send(result)
	})
}
