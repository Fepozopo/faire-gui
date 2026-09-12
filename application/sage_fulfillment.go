package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf16"

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
	// sageFulfillmentProtocolVersion is the version emitted by Sage/LaunchFaireFulfillment.vbs.
	sageFulfillmentProtocolVersion = 1
	// maxSageFulfillmentRequests bounds queued local requests so pipe workers never wait on the frame loop.
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

// sageFulfillmentResult is the typed terminal response allowed by the Sage bridge.
// This initial listener emits only cancelled and failed results; shipment completion is added with the fulfillment UI.
type sageFulfillmentResult struct {
	RequestID    string
	Status       string
	SalesOrderNo string
	InvoiceNo    string
	Error        string
}

// sageFulfillmentInbound transfers a validated IPC request to the Gio frame goroutine.
// respond is safe to call once and never exposes a transport connection to UI code.
type sageFulfillmentInbound struct {
	request sageFulfillmentRequest
	respond func(sageFulfillmentResult)
}

// sageFulfillmentSession retains the active Sage-launched workflow while the user reviews the Order Details page.
type sageFulfillmentSession struct {
	request             sageFulfillmentRequest
	orderID             faire.OrderID
	respond             func(sageFulfillmentResult)
	status              string
	unresolved          []string
	preselectionApplied bool
	cancelButton        widget.Clickable
}

// parseSageFulfillmentRequest decodes and validates the narrow protocol-v1 request before it reaches UI state.
func parseSageFulfillmentRequest(payload []byte) (sageFulfillmentRequest, error) {
	var request sageFulfillmentRequest
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
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

// decodeSagePipePayload decodes the UTF-16LE payload written by Sage's FileSystemObject and concatenates its JSON chunks through Done.
func decodeSagePipePayload(payload []byte) ([]byte, error) {
	if len(payload) < 2 || payload[0] != 0xFF || payload[1] != 0xFE || len(payload)%2 != 0 {
		return nil, fmt.Errorf("Sage fulfillment payload is not UTF-16LE text")
	}
	codeUnits := make([]uint16, 0, (len(payload)-2)/2)
	for index := 2; index < len(payload); index += 2 {
		codeUnits = append(codeUnits, uint16(payload[index])|uint16(payload[index+1])<<8)
	}
	var builder strings.Builder
	for _, line := range strings.Split(string(utf16.Decode(codeUnits)), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "Done" {
			if builder.Len() == 0 {
				return nil, fmt.Errorf("Sage fulfillment payload is empty")
			}
			return []byte(builder.String()), nil
		}
		builder.WriteString(line)
	}
	return nil, fmt.Errorf("Sage fulfillment payload is missing its Done marker")
}

// formatSageFulfillmentResult returns the line-oriented ASCII-safe terminal response expected by the Sage script.
func formatSageFulfillmentResult(result sageFulfillmentResult) string {
	return "RequestID:" + sageResultValue(result.RequestID) + "\r\n" +
		"Status:" + sageResultValue(result.Status) + "\r\n" +
		"SalesOrderNo:" + sageResultValue(result.SalesOrderNo) + "\r\n" +
		"InvoiceNo:" + sageResultValue(result.InvoiceNo) + "\r\n" +
		resultErrorLine(result.Error) +
		"Done\r\n"
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

// safeSageRequestID allows the UUID-like request IDs emitted by Sage while keeping pipe names and local state bounded.
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

// startSageFulfillmentListener starts the platform-specific local transport once for this UI lifetime.
func (ui *DesktopUI) startSageFulfillmentListener() {
	if ui.sageFulfillmentListenerStarted {
		return
	}
	ui.sageFulfillmentListenerStarted = true
	ui.startWorker(func() {
		serveSageFulfillmentPipes(ui.ctx, ui.publishSageFulfillmentRequest)
	})
}

// publishSageFulfillmentRequest queues a validated request without allowing a pipe worker to mutate Gio state.
func (ui *DesktopUI) publishSageFulfillmentRequest(inbound sageFulfillmentInbound) {
	select {
	case ui.sageFulfillmentRequests <- inbound:
		ui.invalidate()
	default:
		inbound.respond(sageFulfillmentFailure(inbound.request, "Faire GUI is busy with local fulfillment requests. Retry from Sage after the current request is handled."))
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
		inbound.respond(sageFulfillmentFailure(inbound.request, "A Sage fulfillment session is already open in Faire GUI. Finish or cancel it before starting another shipment."))
		return
	}
	if ui.manager == nil || ui.orders.store == nil {
		inbound.respond(sageFulfillmentFailure(inbound.request, "Faire GUI is still preparing local data. Retry from Sage in a moment."))
		return
	}
	if ui.orders.dataActionConnectionID != "" {
		inbound.respond(sageFulfillmentFailure(inbound.request, "Faire GUI is rebuilding local order data. Retry from Sage after it finishes."))
		return
	}
	connection, found := ui.connectionForSageSalesSource(inbound.request.Document.SalesSource)
	if !found {
		inbound.respond(sageFulfillmentFailure(inbound.request, "No saved Faire connection matches Sage sales source '"+inbound.request.Document.SalesSource+"'. Configure that connection's brand ID, then retry from Sage."))
		return
	}
	displayID, _ := orders.NormalizeDisplayID(inbound.request.Document.FaireDisplayID)
	orderID, _ := orders.OrderIDFromDisplayID(displayID)
	ui.sageFulfillment = &sageFulfillmentSession{
		request: inbound.request,
		orderID: orderID,
		respond: inbound.respond,
		status:  "Opening Faire order " + displayID + " from Sage Shipping Data Entry…",
	}
	if ui.window != nil {
		// Gio delegates foreground policy to Windows; ActionRaise is best effort when another app owns focus.
		ui.window.Perform(system.ActionRaise)
	}
	if ui.activeConnectionID != connection.ID {
		ui.setActiveConnection(connection)
	}
	ui.orders.view.search.SetText(displayID)
	ui.loadOrderByDisplayID()
	ui.invalidate()
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
	if ui.orders.view.orderDetail.OrderID != session.orderID {
		if ui.orders.view.orderDetailStatus != "" {
			ui.finishSageFulfillmentSession(sageFulfillmentFailure(session.request, "Faire could not open the requested order. "+ui.orders.view.orderDetailStatus))
		}
		return
	}

	selected, unresolved := sageBackorderedVariants(session.request.Lines, ui.orders.view.orderDetail.Items)
	for variantID := range selected {
		ui.orders.view.pendingUnavailable[variantID] = struct{}{}
	}
	session.unresolved = unresolved
	session.preselectionApplied = true
	if len(unresolved) > 0 {
		session.status = "Review required: " + strings.Join(unresolved, " ")
	} else if len(selected) > 0 {
		session.status = "Sage backorders were preselected as out of stock. Review them, then use Update availability to confirm."
	} else {
		session.status = "Sage reported no backordered lines. Continue with shipment information when ready."
	}
	ui.orders.view.orderDetailStatus = session.status
	ui.invalidate()
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

// handleSageFulfillmentEvents completes a cancelled session before normal detail controls render for the frame.
func (ui *DesktopUI) handleSageFulfillmentEvents(gtx layout.Context) {
	if ui.sageFulfillment == nil || !ui.sageFulfillment.cancelButton.Clicked(gtx) {
		return
	}
	ui.finishSageFulfillmentSession(sageFulfillmentResult{
		RequestID:    ui.sageFulfillment.request.RequestID,
		Status:       "CANCELLED",
		SalesOrderNo: ui.sageFulfillment.request.Document.SalesOrderNo,
		InvoiceNo:    ui.sageFulfillment.request.Document.InvoiceNo,
	})
}

// finishSageFulfillmentSession sends one terminal response and clears the active session so another Sage request can be handled.
func (ui *DesktopUI) finishSageFulfillmentSession(result sageFulfillmentResult) {
	session := ui.sageFulfillment
	if session == nil {
		return
	}
	ui.sageFulfillment = nil
	session.respond(result)
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

// layoutSageFulfillmentSession renders the active Sage session state above Order Details so the operator knows Sage is waiting.
func (ui *DesktopUI) layoutSageFulfillmentSession(gtx layout.Context) layout.Dimensions {
	session := ui.sageFulfillment
	if session == nil {
		return layout.Dimensions{}
	}
	return outlinedPanel(gtx, shipmentPanelBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Sage fulfillment session").Layout),
						layout.Rigid(bodyText(ui.theme, session.status, mutedTextColor)),
					)
				}),
				layout.Rigid(primaryButton(ui.theme, &session.cancelButton, "Cancel Sage session")),
			)
		})
	})
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
