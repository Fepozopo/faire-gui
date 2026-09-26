' LaunchFaireFulfillment starts a Faire fulfillment session from Sage 100 Shipping Data Entry.
' It posts a versioned request to the direct HTTP endpoint on RMT01,
' waits for one typed terminal result with bounded network timeouts, and applies
' validated shipment data to Sage.
'
' Protocol version 1 uses one UTF-8 HTTP request and a text response:
'   POST http://RMT01:18080/v1/sage/fulfillment receives the JSON request.
'   HTTP 200 returns typed result lines followed by Done.
' RMT01 must firewall this test endpoint to BSDC01 only; it is not an
' encrypted transport.
'
' Result lines are deliberately whitelisted instead of allowing arbitrary Sage field writes:
'   RequestID:<id>
'   Status:COMPLETED|CANCELLED|FAILED
'   SalesOrderNo:<value>
'   InvoiceNo:<value>
'   Tracking:<package no>|<tracking id>
'   PackageItem:<package no>|<item code>|<item type>|<quantity>
'   FreightAmount:<decimal>
'   Error:<safe user-facing message>
'   KeepAlive
'   Done
'
' After Sage attempts writeback it sends a second narrow POST to
' /v1/sage/fulfillment/ack with APPLIED or WRITEBACK_FAILED. The acknowledgement
' lets the GUI retain recovery data without ever writing Sage records itself.
'
' A terminal result is tied to its RequestID. A later production transport must
' persist and replay the result rather than repeat an irreversible Faire operation.

Const FAIRE_PROTOCOL_VERSION = 1
' FAIRE_GUI_WORKSTATION is deliberately separate from the Sage server because the
' desktop Faire GUI listens for HTTP requests on RMT01.
Const FAIRE_GUI_WORKSTATION = "RMT01"
Const FAIRE_HTTP_PORT = 18080
Const FAIRE_HTTP_PATH = "/v1/sage/fulfillment"
Const FAIRE_HTTP_ACK_PATH = "/v1/sage/fulfillment/ack"
Const HTTP_STATUS_OK = 200
' ServerXMLHTTP uses these millisecond limits for DNS, TCP, upload, and fulfillment response waits.
Const HTTP_RESOLVE_TIMEOUT_MS = 5000
Const HTTP_CONNECT_TIMEOUT_MS = 5000
Const HTTP_SEND_TIMEOUT_MS = 15000
Const HTTP_RECEIVE_TIMEOUT_MS = 600000
' ADODB stream constants produce a UTF-8 byte array for ServerXMLHTTP instead of
' allowing it to serialize the VBScript string as UTF-16 BSTR text.
Const ADO_TYPE_BINARY = 1
Const ADO_TYPE_TEXT = 2
Const UTF8_BOM_LENGTH = 3
Const SAGE_BACKORDERED_QUANTITY_FIELD = "QUANTITYBACKORDERED"
Const SAGE_SALES_SOURCE_FIELD = "UDF_SALE_SOURCE$"

Main()

' Main coordinates the full Sage-to-Faire session and only performs writeback after
' the returned request and document identifiers match the active Sage document.
Sub Main
	If oSession.UI = 0 Then
		Exit Sub
	End If

	Set oUI = oSession.AsObject(oSession.UI)
	sRequestID = CanonicalProtocolRequestID(CreateRequestID())
	sRequestJSON = ""
	sSalesOrderNo = ""
	sInvoiceNo = ""
	sError = ""
	If Not BuildFulfillmentRequest(sRequestID, sRequestJSON, sSalesOrderNo, sInvoiceNo, sError) Then
		retMsg = oUI.MessageBox("", sError, "Icon=Exclamation, Title=Unable To Start Faire Fulfillment, Style=OK")
		Exit Sub
	End If

	Set fulfillmentHttp = CreateObject("MSXML2.ServerXMLHTTP.6.0")
	If Not SendFulfillmentRequest(fulfillmentHttp, oUI, sRequestJSON, sError) Then
		retMsg = oUI.MessageBox("", sError, "Icon=Exclamation, Title=Faire GUI Not Running, Style=OK")
		Exit Sub
	End If

	' Sage Script Link does not expose VBA's Collection class; dictionaries retain the
	' typed result records while remaining available through the Windows scripting runtime.
	Set trackingRecords = CreateObject("Scripting.Dictionary")
	Set packageRecords = CreateObject("Scripting.Dictionary")
	sStatus = ""
	sResponseRequestID = ""
	sResponseSalesOrderNo = ""
	sResponseInvoiceNo = ""
	sResponseError = ""
	bHasFreightAmount = False
	sFreightAmount = ""
	If Not ReadFulfillmentResult(fulfillmentHttp.responseText, sRequestID, trackingRecords, packageRecords, sStatus, sResponseRequestID, sResponseSalesOrderNo, sResponseInvoiceNo, bHasFreightAmount, sFreightAmount, sResponseError, sError) Then
		retMsg = oUI.MessageBox("", sError, "Icon=Exclamation, Title=Faire Fulfillment Result Unavailable, Style=OK")
		Exit Sub
	End If

	If UCase(sStatus) = "CANCELLED" Then
		retMsg = oUI.MessageBox("", "The Faire fulfillment session was cancelled. No Sage shipment data was changed.", "Icon=Exclamation, Title=Faire Fulfillment Cancelled, Style=OK")
		Exit Sub
	End If
	If UCase(sStatus) <> "COMPLETED" Then
		If sResponseError = "" Then
			sResponseError = "The Faire GUI did not return a completed fulfillment result."
		End If
		retMsg = oUI.MessageBox("", sResponseError, "Icon=Exclamation, Title=Faire Fulfillment Failed, Style=OK")
		Exit Sub
	End If

	If Not ValidateFulfillmentResult(sRequestID, sSalesOrderNo, sInvoiceNo, sResponseRequestID, sResponseSalesOrderNo, sResponseInvoiceNo, sError) Then
		retMsg = oUI.MessageBox("", sError, "Icon=Exclamation, Title=Faire Fulfillment Result Rejected, Style=OK")
		Exit Sub
	End If
	If Not ApplyFulfillmentResult(sInvoiceNo, trackingRecords, packageRecords, bHasFreightAmount, sFreightAmount, sError) Then
		Call SendFulfillmentAcknowledgement(sRequestID, "WRITEBACK_FAILED")
		retMsg = oUI.MessageBox("", sError & vbCrLf & vbCrLf & "The Faire result should remain available for retry using request " & sRequestID & ".", "Icon=Exclamation, Title=Sage Writeback Failed, Style=OK")
		Exit Sub
	End If

	retVal = oScript.InvokeButton("BT_TRACK")
	retVal = oUIObj.HandleScriptUI()
	If Not SendFulfillmentAcknowledgement(sRequestID, "APPLIED") Then
		retMsg = oUI.MessageBox("", "Faire fulfillment was completed and Sage tracking data was updated, but the GUI did not receive its writeback acknowledgement. Keep request " & sRequestID & " for recovery.", "Icon=Exclamation, Title=Faire Fulfillment Acknowledgement Pending, Style=OK")
		Exit Sub
	End If
	retMsg = oUI.MessageBox("", "Faire fulfillment was completed and Sage tracking data was updated.", "Icon=Info, Title=Faire Fulfillment Complete, Style=OK")
End Sub

' BuildFulfillmentRequest reads the current Sage shipment and returns its explicit,
' versioned JSON snapshot plus the salesOrderNo/invoiceNo identity Main must later validate and write. errorMessage receives a safe error for missing required data.
Function BuildFulfillmentRequest(requestID, ByRef requestJSON, ByRef salesOrderNo, ByRef invoiceNo, ByRef errorMessage)
	BuildFulfillmentRequest = False
	errorMessage = ""
	Set oShipping = oBusObj

	If Not TryGetRequiredString(oShipping, "InvoiceNo$", "the active invoice number", invoiceNo, errorMessage) Then Exit Function
	If Not TryGetOptionalString(oShipping, "ShipVia$", sShipVia) Then sShipVia = ""
	If Not TryGetOptionalNumber(oShipping, "FreightAmt", nFreightAmount) Then nFreightAmount = 0

	retVal = oShipping.ReadAdditional("SalesOrderNo")
	hSalesOrder = oShipping.GetChildHandle("SalesOrderNo")
	If hSalesOrder = 0 Then
		errorMessage = "Unable to read the related Sage sales order from the active Shipping Data Entry document."
		Exit Function
	End If
	Set oSalesOrder = oShipping.AsObject(hSalesOrder)
	If Not TryGetRequiredString(oSalesOrder, "SalesOrderNo$", "the sales order number", salesOrderNo, errorMessage) Then Exit Function
	If Not TryGetRequiredString(oSalesOrder, "CustomerPoNo$", "the Faire order display ID in the customer PO number", sFaireDisplayID, errorMessage) Then Exit Function

	' This confirmed Sage 100 2025 UDF selects the Faire connection from the active sales order.
	If Not TryGetRequiredString(oSalesOrder, SAGE_SALES_SOURCE_FIELD, "the sales-source UDF on the sales order", sSalesSource, errorMessage) Then Exit Function

	Call TryGetOptionalString(oShipping, "ShipToName$", sShipToName)
	Call TryGetOptionalString(oShipping, "ShipToAddress3$", sShipToCompany)
	Call TryGetOptionalString(oShipping, "ShipToAddress1$", sShipToAddress1)
	Call TryGetOptionalString(oShipping, "ShipToAddress2$", sShipToAddress2)
	Call TryGetOptionalString(oShipping, "ShipToCity$", sShipToCity)
	Call TryGetOptionalString(oShipping, "ShipToState$", sShipToState)
	Call TryGetOptionalString(oShipping, "ShipToZipCode$", sShipToPostalCode)
	Call TryGetOptionalString(oShipping, "ShipToCountryCode$", sShipToCountry)
	Call TryGetOptionalString(oShipping, "EmailAddress$", sShipToEmail)
	Call TryGetOptionalString(oShipping, "TelephoneNo$", sShipToPhone)
	Set oSalesOrder = Nothing

	Set oLines = oSession.AsObject(oShipping.Lines)
	sLinesJSON = ""
	bHasFulfillmentLine = False
	retVal = oLines.MoveFirst()
	Do Until cBool(oLines.EOF)
		sLineJSON = ""
		If Not BuildFulfillmentLine(oLines, sLineJSON, bIncludeLine, errorMessage) Then
			Set oLines = Nothing
			Exit Function
		End If
		If bIncludeLine Then
			If bHasFulfillmentLine Then sLinesJSON = sLinesJSON & ","
			sLinesJSON = sLinesJSON & sLineJSON
			bHasFulfillmentLine = True
		End If
		retVal = oLines.MoveNext()
	Loop
	Set oLines = Nothing

	If Not bHasFulfillmentLine Then
		errorMessage = "There are no shipped or backordered item lines. Mark each item as shipped or backordered before starting Faire fulfillment."
		Exit Function
	End If

	requestJSON = "{"
	requestJSON = requestJSON & """protocolVersion"":" & FAIRE_PROTOCOL_VERSION & ","
	requestJSON = requestJSON & """requestId"":""" & EscapeJSON(requestID) & ""","
	requestJSON = requestJSON & """source"":{""companyCode"":""" & EscapeJSON(oSession.CompanyCode) & """,""workstation"":""" & EscapeJSON(oSession.WorkstationName) & """},"
	requestJSON = requestJSON & Chr(34) & "document" & Chr(34) & ":{"
	requestJSON = requestJSON & """salesOrderNo"":""" & EscapeJSON(salesOrderNo) & ""","
	requestJSON = requestJSON & """invoiceNo"":""" & EscapeJSON(invoiceNo) & ""","
	requestJSON = requestJSON & """faireDisplayId"":""" & EscapeJSON(sFaireDisplayID) & ""","
	requestJSON = requestJSON & """salesSource"":""" & EscapeJSON(sSalesSource) & ""","
	requestJSON = requestJSON & """shipVia"":""" & EscapeJSON(sShipVia) & ""","
	requestJSON = requestJSON & """freightAmount"":" & JSONNumber(nFreightAmount) & "},"
	requestJSON = requestJSON & Chr(34) & "shipTo" & Chr(34) & ":{"
	requestJSON = requestJSON & """name"":""" & EscapeJSON(sShipToName) & ""","
	requestJSON = requestJSON & """company"":""" & EscapeJSON(sShipToCompany) & ""","
	requestJSON = requestJSON & """address1"":""" & EscapeJSON(sShipToAddress1) & ""","
	requestJSON = requestJSON & """address2"":""" & EscapeJSON(sShipToAddress2) & ""","
	requestJSON = requestJSON & """city"":""" & EscapeJSON(sShipToCity) & ""","
	requestJSON = requestJSON & """state"":""" & EscapeJSON(sShipToState) & ""","
	requestJSON = requestJSON & """postalCode"":""" & EscapeJSON(sShipToPostalCode) & ""","
	requestJSON = requestJSON & """country"":""" & EscapeJSON(sShipToCountry) & ""","
	requestJSON = requestJSON & """email"":""" & EscapeJSON(sShipToEmail) & ""","
	requestJSON = requestJSON & """phone"":""" & EscapeJSON(sShipToPhone) & Chr(34) & "}," & Chr(34) & "lines" & Chr(34) & ":[" & sLinesJSON & "]}"
		BuildFulfillmentRequest = True
		Exit Function
	End Function

	' BuildFulfillmentLine creates one outbound line snapshot when Sage marked that line
	' shipped or backordered. The explicit backordered quantity is required so that zero
	' shipped quantity is never confused with an unselected or non-fulfillment line.
	Function BuildFulfillmentLine(oLine, ByRef lineJSON, ByRef includeLine, ByRef errorMessage)
		BuildFulfillmentLine = False
		includeLine = False
		If Not TryGetRequiredNumber(oLine, "QUANTITYSHIPPED", "the shipped quantity", nQuantityShipped, errorMessage) Then Exit Function
		If Not TryGetRequiredNumber(oLine, SAGE_BACKORDERED_QUANTITY_FIELD, "the backordered quantity", nQuantityBackordered, errorMessage) Then Exit Function
		If nQuantityShipped <= 0 And nQuantityBackordered <= 0 Then
			BuildFulfillmentLine = True
			Exit Function
		End If

		If Not TryGetRequiredString(oLine, "LineKey$", "the Sage line key", sLineKey, errorMessage) Then Exit Function
		If Not TryGetRequiredString(oLine, "ItemCode$", "the Sage item code", sItemCode, errorMessage) Then Exit Function
		Call TryGetOptionalString(oLine, "ItemType$", sItemType)
		If nQuantityShipped > 0 And Trim(sItemType) = "" Then
			errorMessage = "Unable to read the item type for shipped Sage item '" & sItemCode & "'. Package tracking requires ItemType$."
			Exit Function
		End If
		Call TryGetOptionalString(oLine, "ItemCodeDesc$", sItemDescription)
		Call TryGetOptionalString(oLine, "SalesKitLineKey$", sSalesKitLineKey)
		Call TryGetOptionalString(oLine, "ExplodedKitItem$", sExplodedKitItem)

		lineJSON = "{"
		lineJSON = lineJSON & """sageLineKey"":""" & EscapeJSON(sLineKey) & ""","
		lineJSON = lineJSON & """itemCode"":""" & EscapeJSON(sItemCode) & ""","
		lineJSON = lineJSON & """itemType"":""" & EscapeJSON(sItemType) & ""","
		lineJSON = lineJSON & """description"":""" & EscapeJSON(sItemDescription) & ""","
		lineJSON = lineJSON & """quantityShipped"":" & JSONNumber(nQuantityShipped) & ","
		lineJSON = lineJSON & """quantityBackordered"":" & JSONNumber(nQuantityBackordered) & ","
		lineJSON = lineJSON & """salesKitLineKey"":""" & EscapeJSON(sSalesKitLineKey) & ""","
		lineJSON = lineJSON & """explodedKitItem"":""" & EscapeJSON(sExplodedKitItem) & """}"
		includeLine = True
		BuildFulfillmentLine = True
	End Function

	' SendFulfillmentRequest posts UTF-8 JSON asynchronously through ServerXMLHTTP while periodically refreshing Sage's native progress dialog. waitForResponse(1) bounds each COM wait to one second, allowing the script host watchdog to observe UI activity during operator review.
	Function SendFulfillmentRequest(httpRequest, uiObject, requestJSON, ByRef errorMessage)
		SendFulfillmentRequest = False
		errorMessage = ""
		On Error Resume Next
		Err.Clear
		httpRequest.setTimeouts HTTP_RESOLVE_TIMEOUT_MS, HTTP_CONNECT_TIMEOUT_MS, HTTP_SEND_TIMEOUT_MS, HTTP_RECEIVE_TIMEOUT_MS
		httpRequest.Open "POST", GetFulfillmentURL(), True
		httpRequest.setRequestHeader "Content-Type", "application/json; charset=utf-8"
		requestBody = UTF8Bytes(requestJSON)
		retVal = uiObject.ProgressBar("init", "Faire fulfillment", "Waiting for Faire GUI review. Do not change this shipment in Sage.", 0, "")
		progressOpen = (Err.Number = 0)
		Err.Clear
		httpRequest.Send requestBody
		If Err.Number <> 0 Then
			nHttpError = Err.Number
			sHttpErrorDescription = Err.Description
			If progressOpen Then retVal = uiObject.ProgressBar("close", "Faire fulfillment", "", 0, "")
			errorMessage = "Unable to connect to the Faire GUI on workstation '" & FAIRE_GUI_WORKSTATION & "'. Error " & CStr(nHttpError)
			If sHttpErrorDescription <> "" Then errorMessage = errorMessage & ": " & sHttpErrorDescription
			Err.Clear
			On Error GoTo 0
			Exit Function
		End If

		progressPercent = 0
		waitSeconds = 0
		Do While httpRequest.readyState <> 4
			' MSXML ServerXMLHTTP waits at most one second here; the following ProgressBar update is deliberately repeated so Sage does not treat an operator review as a stalled script.
			Err.Clear
			retVal = httpRequest.waitForResponse(1)
			If Err.Number <> 0 Then
				nHttpError = Err.Number
				sHttpErrorDescription = Err.Description
				If progressOpen Then retVal = uiObject.ProgressBar("close", "Faire fulfillment", "", 0, "")
				errorMessage = "Faire GUI did not complete the fulfillment request. Error " & CStr(nHttpError)
				If sHttpErrorDescription <> "" Then errorMessage = errorMessage & ": " & sHttpErrorDescription
				Err.Clear
				On Error GoTo 0
				Exit Function
			End If
			waitSeconds = waitSeconds + 1
			progressPercent = (progressPercent + 5) Mod 100
			If progressOpen Then retVal = uiObject.ProgressBar("update", "Faire fulfillment", "Waiting for Faire GUI review (" & CStr(waitSeconds) & " seconds). Do not change this shipment in Sage.", progressPercent, "")
			' A ProgressBar rendering problem must not be mistaken for an HTTP failure on the next polling cycle.
			Err.Clear
		Loop

		If progressOpen Then retVal = uiObject.ProgressBar("update", "Faire fulfillment", "Writing tracking and freight to Sage...", 100, "")
		If httpRequest.status <> HTTP_STATUS_OK Then
			sHttpResponse = Trim(httpRequest.responseText)
			If progressOpen Then retVal = uiObject.ProgressBar("close", "Faire fulfillment", "", 0, "")
			errorMessage = "Faire GUI returned HTTP status " & CStr(httpRequest.status)
			If sHttpResponse <> "" Then errorMessage = errorMessage & ": " & sHttpResponse
			errorMessage = errorMessage & ". No Sage shipment data was changed."
			On Error GoTo 0
			Exit Function
		End If
		If progressOpen Then retVal = uiObject.ProgressBar("close", "Faire fulfillment", "", 0, "")
		On Error GoTo 0
		SendFulfillmentRequest = True
	End Function

	' SendFulfillmentAcknowledgement posts Sage's final writeback state through the
	' same listener. It never includes arbitrary Sage data or credentials.
	Function SendFulfillmentAcknowledgement(requestID, acknowledgementStatus)
		SendFulfillmentAcknowledgement = False
		On Error Resume Next
		Err.Clear
		Set acknowledgementHttp = CreateObject("MSXML2.ServerXMLHTTP.6.0")
		acknowledgementHttp.setTimeouts HTTP_RESOLVE_TIMEOUT_MS, HTTP_CONNECT_TIMEOUT_MS, HTTP_SEND_TIMEOUT_MS, HTTP_SEND_TIMEOUT_MS
		acknowledgementHttp.Open "POST", GetFulfillmentAckURL(), False
		acknowledgementHttp.setRequestHeader "Content-Type", "application/json; charset=utf-8"
		acknowledgementJSON = "{""protocolVersion"":" & FAIRE_PROTOCOL_VERSION & ",""requestId"":""" & EscapeJSON(requestID) & """,""status"":""" & EscapeJSON(acknowledgementStatus) & """}"
		acknowledgementHttp.Send UTF8Bytes(acknowledgementJSON)
		SendFulfillmentAcknowledgement = (Err.Number = 0 And acknowledgementHttp.status = 204)
		Err.Clear
		Set acknowledgementHttp = Nothing
		On Error GoTo 0
	End Function

	' ReadFulfillmentResult parses the completed HTTP response and permits only the
	' documented fields for later validated Sage writeback. trackingRecords and
	' packageRecords are Scripting.Dictionary instances containing typed array values.
	Function ReadFulfillmentResult(responseText, expectedRequestID, trackingRecords, packageRecords, ByRef status, ByRef responseRequestID, ByRef responseSalesOrderNo, ByRef responseInvoiceNo, ByRef hasFreightAmount, ByRef freightAmount, ByRef responseError, ByRef errorMessage)
		ReadFulfillmentResult = False
		errorMessage = ""
		bHasDone = False
		resultLines = Split(responseText, vbLf)
		For Each resultLine In resultLines
			sLine = Replace(resultLine, vbCr, "")
			If sLine <> "" Then
				If sLine = "Done" Then
					bHasDone = True
					Exit For
				End If
				If sLine <> "KeepAlive" Then
					If Not ParseResultLine(sLine, trackingRecords, packageRecords, status, responseRequestID, responseSalesOrderNo, responseInvoiceNo, hasFreightAmount, freightAmount, responseError, errorMessage) Then Exit Function
				End If
			End If
		Next
		If Not bHasDone Then
			errorMessage = "Faire GUI returned an incomplete HTTP fulfillment result."
			Exit Function
		End If
		If responseRequestID = "" Then
			errorMessage = "Faire GUI returned a result without a request ID."
			Exit Function
		End If
		If status = "" Then
			errorMessage = "Faire GUI returned a result without a status."
			Exit Function
		End If
		ReadFulfillmentResult = True
	End Function

	' ParseResultLine validates one typed result line and appends tracking or package data
	' to their Collections. It rejects unknown fields so the GUI cannot request arbitrary Sage writes.
	Function ParseResultLine(resultLine, trackingRecords, packageRecords, ByRef status, ByRef responseRequestID, ByRef responseSalesOrderNo, ByRef responseInvoiceNo, ByRef hasFreightAmount, ByRef freightAmount, ByRef responseError, ByRef errorMessage)
		ParseResultLine = False
		fields = Split(resultLine, ":", 2)
		If UBound(fields) <> 1 Then
			errorMessage = "Faire GUI returned an invalid result line."
			Exit Function
		End If
		sFieldName = UCase(Trim(fields(0)))
		sValue = fields(1)
		Select Case sFieldName
			Case "REQUESTID"
				responseRequestID = CanonicalProtocolRequestID(sValue)
			Case "STATUS"
				status = UCase(Trim(sValue))
				If status <> "COMPLETED" And status <> "CANCELLED" And status <> "FAILED" Then
					errorMessage = "Faire GUI returned an unsupported fulfillment status."
					Exit Function
				End If
			Case "SALESORDERNO"
				responseSalesOrderNo = sValue
			Case "INVOICENO"
				responseInvoiceNo = sValue
			Case "TRACKING"
				values = Split(sValue, "|")
				If UBound(values) <> 1 Or Trim(values(0)) = "" Or Trim(values(1)) = "" Then
					errorMessage = "Faire GUI returned invalid tracking data."
					Exit Function
				End If
				trackingRecords.Add CStr(trackingRecords.Count), Array(Trim(values(0)), Trim(values(1)))
			Case "PACKAGEITEM"
				values = Split(sValue, "|")
				If UBound(values) <> 3 Or Trim(values(0)) = "" Or Trim(values(1)) = "" Or Trim(values(2)) = "" Or Not IsNumeric(Trim(values(3))) Then
					errorMessage = "Faire GUI returned invalid package item data."
					Exit Function
				End If
				packageRecords.Add CStr(packageRecords.Count), Array(Trim(values(0)), Trim(values(1)), Trim(values(2)), CSng(Trim(values(3))))
			Case "FREIGHTAMOUNT"
				If Not IsNumeric(Trim(sValue)) Then
					errorMessage = "Faire GUI returned an invalid freight amount."
					Exit Function
				End If
				hasFreightAmount = True
				freightAmount = Trim(sValue)
			Case "ERROR"
				responseError = sValue
			Case Else
				errorMessage = "Faire GUI returned an unsupported result field '" & fields(0) & "'."
				Exit Function
		End Select
		ParseResultLine = True
	End Function

	' ValidateFulfillmentResult confirms that the completed result belongs to this active
	' Sage transaction before any tracking, package, or freight data is applied.
	' ValidateFulfillmentResult compares the final response against explicit Main-scope document values. Script Link procedure-local variables are not reliably visible inside helper functions, so no writeback identity depends on ambient variables.
	Function ValidateFulfillmentResult(expectedRequestID, expectedSalesOrderNo, expectedInvoiceNo, responseRequestID, responseSalesOrderNo, responseInvoiceNo, ByRef errorMessage)
		ValidateFulfillmentResult = False
		expectedRequestID = CanonicalProtocolRequestID(expectedRequestID)
		responseRequestID = CanonicalProtocolRequestID(responseRequestID)
		If responseRequestID <> expectedRequestID Then
			errorMessage = "The Faire result belongs to a different fulfillment request and was not applied. Expected request " & expectedRequestID & "; received request " & responseRequestID & "."
			Exit Function
		End If
		If responseSalesOrderNo <> expectedSalesOrderNo Or responseInvoiceNo <> expectedInvoiceNo Then
			errorMessage = "The Faire result does not match the active Sage sales order and invoice. Expected sales order '" & expectedSalesOrderNo & "' and invoice '" & expectedInvoiceNo & "'; received sales order '" & responseSalesOrderNo & "' and invoice '" & responseInvoiceNo & "'."
			Exit Function
		End If
		ValidateFulfillmentResult = True
	End Function

	' ApplyFulfillmentResult replaces invoiceNo's existing Sage tracking/package records and optionally updates freight after the typed Faire result has passed identity validation.
	Function ApplyFulfillmentResult(invoiceNo, trackingRecords, packageRecords, hasFreightAmount, freightAmount, ByRef errorMessage)
		ApplyFulfillmentResult = False
		If trackingRecords.Count = 0 Then
			errorMessage = "Faire completed without a tracking record, so Sage writeback was not applied."
			Exit Function
		End If
		If Not RemoveExistingTrackingRecords(invoiceNo, errorMessage) Then Exit Function
		If Not RemoveExistingPackageRecords(invoiceNo, errorMessage) Then Exit Function

		For Each recordKey In trackingRecords.Keys
			record = trackingRecords.Item(recordKey)
			If Not WriteTrackingRecord(invoiceNo, record(0), record(1), errorMessage) Then Exit Function
		Next
		For Each recordKey In packageRecords.Keys
			record = packageRecords.Item(recordKey)
			If Not WritePackageItemRecord(invoiceNo, record(0), record(1), record(2), record(3), errorMessage) Then Exit Function
		Next
		If hasFreightAmount Then
			retVal = oUIObj.InvokeChange("FREIGHTAMT", CSng(freightAmount))
			If retVal = 0 Then
				errorMessage = "Sage could not update Freight Amount: " & oBusObj.LastErrorMsg
				Exit Function
			End If
		End If
		ApplyFulfillmentResult = True
	End Function

	' RemoveExistingTrackingRecords clears invoiceNo's tracking records because the approved business rule is to replace, rather than merge, existing records.
	Function RemoveExistingTrackingRecords(invoiceNo, ByRef errorMessage)
		RemoveExistingTrackingRecords = False
		hTracking = oSession.GetObject("SO_InvoiceTracking_bus")
		If hTracking = 0 Then
			errorMessage = "Sage could not open the invoice tracking business object."
			Exit Function
		End If
		Set oTracking = oSession.AsObject(hTracking)
		retVal = oTracking.RemoveTrackingRecords(invoiceNo)
		Set oTracking = Nothing
		If retVal = 0 Then
			errorMessage = "Sage could not remove existing invoice tracking records: " & oBusObj.LastErrorMsg
			Exit Function
		End If
		RemoveExistingTrackingRecords = True
	End Function

	' RemoveExistingPackageRecords clears invoiceNo's package allocations before the final result writes their replacement values.
	Function RemoveExistingPackageRecords(invoiceNo, ByRef errorMessage)
		RemoveExistingPackageRecords = False
		hPackageTracking = oSession.GetObject("SO_PackageTrackingByItem_bus")
		If hPackageTracking = 0 Then
			errorMessage = "Sage could not open the package tracking business object."
			Exit Function
		End If
		Set oPackageTracking = oSession.AsObject(hPackageTracking)
		retVal = oPackageTracking.RemoveRecords(invoiceNo)
		Set oPackageTracking = Nothing
		If retVal = 0 Then
			errorMessage = "Sage could not remove existing package tracking records: " & oBusObj.LastErrorMsg
			Exit Function
		End If
		RemoveExistingPackageRecords = True
	End Function

	' WriteTrackingRecord creates one invoiceNo/package tracking row using the validated package number and tracking identifier returned by the Faire GUI.
	Function WriteTrackingRecord(invoiceNo, packageNo, trackingID, ByRef errorMessage)
		WriteTrackingRecord = False
		hTracking = oSession.GetObject("SO_InvoiceTracking_bus")
		If hTracking = 0 Then
			errorMessage = "Sage could not open the invoice tracking business object."
			Exit Function
		End If
		Set oTracking = oSession.AsObject(hTracking)
		retVal = oTracking.SetKeyValue("InvoiceNo$", invoiceNo)
		retVal = oTracking.SetKeyValue("PackageNo$", packageNo)
		retVal = oTracking.SetKey()
		retVal = oTracking.SetValue("TrackingID$", trackingID)
		If retVal <> 0 Then retVal = oTracking.Write()
		Set oTracking = Nothing
		If retVal = 0 Then
			errorMessage = "Sage could not write tracking number '" & trackingID & "': " & oBusObj.LastErrorMsg
			Exit Function
		End If
		WriteTrackingRecord = True
	End Function

	' WritePackageItemRecord creates or merges one invoiceNo itemType allocation for a package. Existing quantities are added because multiple Faire package lines can reference one Sage item.
	Function WritePackageItemRecord(invoiceNo, packageNo, itemCode, itemType, quantity, ByRef errorMessage)
		WritePackageItemRecord = False
		hPackageTracking = oSession.GetObject("SO_PackageTrackingByItem_bus")
		If hPackageTracking = 0 Then
			errorMessage = "Sage could not open the package tracking business object."
			Exit Function
		End If
		Set oPackageTracking = oSession.AsObject(hPackageTracking)
		retVal = oPackageTracking.SetKeyValue("InvoiceNo$", invoiceNo)
		If retVal = 0 Then
			errorMessage = "Sage rejected package tracking invoice '" & invoiceNo & "' for item '" & itemCode & "': " & oPackageTracking.LastErrorMsg
			Set oPackageTracking = Nothing
			Exit Function
		End If
		retVal = oPackageTracking.SetKeyValue("PackageNo$", packageNo)
		If retVal = 0 Then
			errorMessage = "Sage rejected package number '" & packageNo & "' for invoice '" & invoiceNo & "': " & oPackageTracking.LastErrorMsg
			Set oPackageTracking = Nothing
			Exit Function
		End If
		retVal = oPackageTracking.SetKeyValue("ItemCode$", itemCode)
		If retVal = 0 Then
			errorMessage = "Sage rejected package item code '" & itemCode & "' for invoice '" & invoiceNo & "': " & oPackageTracking.LastErrorMsg
			Set oPackageTracking = Nothing
			Exit Function
		End If
		retVal = oPackageTracking.SetKey()
		If retVal = 1 Then
			retVal = oPackageTracking.GetValue("Quantity", nExistingQuantity)
			If retVal = 0 Then
				errorMessage = "Sage could not read existing package quantity for item '" & itemCode & "' in package '" & packageNo & "': " & oPackageTracking.LastErrorMsg
				Set oPackageTracking = Nothing
				Exit Function
			End If
		Else
			nExistingQuantity = 0
		End If
		' Sage reports ItemType$ is not part of this business object's key, so set it only after locating or creating the keyed record.
		retVal = oPackageTracking.SetValue("ItemType$", itemType)
		If retVal = 0 Then
			errorMessage = "Sage rejected package item type '" & itemType & "' for item '" & itemCode & "': " & oPackageTracking.LastErrorMsg
			Set oPackageTracking = Nothing
			Exit Function
		End If
		retVal = oPackageTracking.SetValue("Quantity", CSng(nExistingQuantity) + CSng(quantity))
		If retVal <> 0 Then retVal = oPackageTracking.Write()
		If retVal = 0 Then
			errorMessage = "Sage could not write package item '" & itemCode & "' in invoice '" & invoiceNo & "', package '" & packageNo & "', quantity '" & CStr(quantity) & "': " & oPackageTracking.LastErrorMsg
			Set oPackageTracking = Nothing
			Exit Function
		End If
		Set oPackageTracking = Nothing
		WritePackageItemRecord = True
	End Function

	' TryGetRequiredString reads a non-empty Sage string field and identifies the missing
	' business value in errorMessage when the field is unavailable or blank.
	Function TryGetRequiredString(oObject, fieldName, displayName, ByRef value, ByRef errorMessage)
		TryGetRequiredString = False
		If Not TryGetOptionalString(oObject, fieldName, value) Or Trim(value) = "" Then
			errorMessage = "Unable to read " & displayName & " from Sage field '" & fieldName & "'. Verify the Sage 100 2025 field mapping and try again."
			Exit Function
		End If
		TryGetRequiredString = True
	End Function

	' TryGetRequiredNumber reads a Sage numeric field and reports a safe mapping error if
	' Sage does not expose the requested value.
	Function TryGetRequiredNumber(oObject, fieldName, displayName, ByRef value, ByRef errorMessage)
		TryGetRequiredNumber = False
		If Not TryGetOptionalNumber(oObject, fieldName, value) Then
			errorMessage = "Unable to read " & displayName & " from Sage field '" & fieldName & "'. Verify the Sage 100 2025 field mapping and try again."
			Exit Function
		End If
		TryGetRequiredNumber = True
	End Function

	' TryGetOptionalString reads a Sage string field without treating a missing optional
	' field as a fatal error. It returns True only when Sage returned a successful value.
	Function TryGetOptionalString(oObject, fieldName, ByRef value)
		TryGetOptionalString = False
		value = ""
		On Error Resume Next
		Err.Clear
		retVal = oObject.GetValue(fieldName, value)
		TryGetOptionalString = (Err.Number = 0 And retVal <> 0)
		Err.Clear
		On Error GoTo 0
	End Function

	' TryGetOptionalNumber reads a Sage numeric field without treating a missing optional
	' field as fatal. It returns True only when Sage returned a successful numeric value.
	Function TryGetOptionalNumber(oObject, fieldName, ByRef value)
		TryGetOptionalNumber = False
		value = 0
		On Error Resume Next
		Err.Clear
		retVal = oObject.GetValue(fieldName, value)
		TryGetOptionalNumber = (Err.Number = 0 And retVal <> 0 And IsNumeric(value))
		Err.Clear
		On Error GoTo 0
	End Function

	' UTF8Bytes uses ADODB.Stream's charset encoder so ServerXMLHTTP receives binary UTF-8,
	' not an implicit UTF-16 BSTR. The UTF-8 BOM is omitted because the GUI expects JSON bytes.
	Function UTF8Bytes(value)
		Set utf8Stream = CreateObject("ADODB.Stream")
		utf8Stream.Type = ADO_TYPE_TEXT
		utf8Stream.Charset = "utf-8"
		utf8Stream.Open
		utf8Stream.WriteText CStr(value)
		utf8Stream.Position = 0
		utf8Stream.Type = ADO_TYPE_BINARY
		utf8Stream.Position = UTF8_BOM_LENGTH
		UTF8Bytes = utf8Stream.Read
		utf8Stream.Close
		Set utf8Stream = Nothing
	End Function

	' GetFulfillmentURL returns the direct HTTP endpoint hosted by Faire GUI on RMT01.
	' It does not use the Sage session workstation because Sage runs on the Sage server.
	Function GetFulfillmentURL()
		GetFulfillmentURL = "http://" & FAIRE_GUI_WORKSTATION & ":" & CStr(FAIRE_HTTP_PORT) & FAIRE_HTTP_PATH
	End Function

	' GetFulfillmentAckURL returns the narrow post-writeback acknowledgement endpoint.
	Function GetFulfillmentAckURL()
		GetFulfillmentAckURL = "http://" & FAIRE_GUI_WORKSTATION & ":" & CStr(FAIRE_HTTP_PORT) & FAIRE_HTTP_ACK_PATH
	End Function

	' CreateRequestID returns a GUID-based identifier so the GUI can persist and replay one
	' fulfillment result without accidentally repeating an irreversible Faire operation.
	Function CreateRequestID()
		On Error Resume Next
		Err.Clear
		Set guidFactory = CreateObject("Scriptlet.TypeLib")
		If Err.Number = 0 Then
			CreateRequestID = Replace(Replace(guidFactory.GUID, "{", ""), "}", "")
			Set guidFactory = Nothing
		Else
			Err.Clear
			Randomize
			CreateRequestID = oSession.WorkstationName & "-" & Replace(CStr(Now), "/", "-") & "-" & Replace(CStr(Timer), ".", "-") & "-" & CStr(Int(Rnd() * 1000000))
		End If
		On Error GoTo 0
	End Function

	' CanonicalProtocolRequestID retains only the ASCII letters, digits, and hyphens allowed by the protocol. Sage fixed-width values can carry non-breaking spaces or control padding that Trim does not remove, but no such character is valid in a locally generated request ID.
	Function CanonicalProtocolRequestID(value)
		sInput = CStr(value)
		sCanonical = ""
		For nIndex = 1 To Len(sInput)
			sCharacter = Mid(sInput, nIndex, 1)
			nCharacter = AscW(sCharacter)
			If (nCharacter >= 48 And nCharacter <= 57) Or (nCharacter >= 65 And nCharacter <= 90) Or (nCharacter >= 97 And nCharacter <= 122) Or nCharacter = 45 Then
				sCanonical = sCanonical & sCharacter
			End If
		Next
		CanonicalProtocolRequestID = sCanonical
	End Function

	' EscapeJSON returns a JSON string value with quote, slash, and control characters
	' escaped so Sage descriptions and addresses cannot corrupt the request document.
	Function EscapeJSON(value)
		sValue = CStr(value)
		' Sage fixed-width character fields can include NUL padding, which JSON forbids as a literal character.
		sValue = Replace(sValue, Chr(0), "")
		sValue = Replace(sValue, "\", "\\")
		sValue = Replace(sValue, Chr(34), Chr(92) & Chr(34))
		sValue = Replace(sValue, vbCrLf, "\n")
		sValue = Replace(sValue, vbCr, "\n")
		sValue = Replace(sValue, vbLf, "\n")
		sValue = Replace(sValue, vbTab, "\t")
		EscapeJSON = sValue
	End Function

	' JSONNumber formats a numeric Sage value with a JSON decimal separator regardless of
	' the workstation's regional decimal separator.
	Function JSONNumber(value)
		JSONNumber = Replace(CStr(CDbl(value)), ",", ".")
	End Function
