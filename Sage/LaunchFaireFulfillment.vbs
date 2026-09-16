' LaunchFaireFulfillment starts a Faire fulfillment session from Sage 100 Shipping Data Entry.
' It posts a versioned request to the temporary direct HTTP endpoint on RMT01,
' waits for one typed terminal result with bounded network timeouts, and applies
' validated shipment data to Sage.
'
' Protocol version 1 uses one UTF-8 HTTP request and a text response:
'   POST http://RMT01:18080/v1/sage/fulfillment receives the JSON request.
'   HTTP 200 returns typed result lines followed by Done.
' RMT01 must firewall this temporary test endpoint to BSDC01 only; it is not an
' encrypted production transport.
'
' Result lines are deliberately whitelisted instead of allowing arbitrary Sage field writes:
'   RequestID:<id>
'   Status:COMPLETED|CANCELLED|FAILED
'   SalesOrderNo:<value>
'   InvoiceNo:<value>
'   Tracking:<package no>|<tracking id>
'   PackageItem:<package no>|<item code>|<quantity>
'   FreightAmount:<decimal>
'   Error:<safe user-facing message>
'   KeepAlive
'   Done
'
' A terminal result is tied to its RequestID. A later production transport must
' persist and replay the result rather than repeat an irreversible Faire operation.

Const FAIRE_PROTOCOL_VERSION = 1
' FAIRE_GUI_WORKSTATION is deliberately separate from the Sage server because the
' desktop Faire GUI listens for temporary HTTP requests on RMT01.
Const FAIRE_GUI_WORKSTATION = "RMT01"
Const FAIRE_HTTP_PORT = 18080
Const FAIRE_HTTP_PATH = "/v1/sage/fulfillment"
Const HTTP_STATUS_OK = 200
' ServerXMLHTTP uses these millisecond limits for DNS, TCP, upload, and fulfillment response waits.
Const HTTP_RESOLVE_TIMEOUT_MS = 5000
Const HTTP_CONNECT_TIMEOUT_MS = 5000
Const HTTP_SEND_TIMEOUT_MS = 15000
Const HTTP_RECEIVE_TIMEOUT_MS = 300000
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
	sRequestID = CreateRequestID()
	sRequestJSON = ""
	sError = ""
	If Not BuildFulfillmentRequest(sRequestID, sRequestJSON, sError) Then
		retMsg = oUI.MessageBox("", sError, "Icon=Exclamation, Title=Unable To Start Faire Fulfillment, Style=OK")
		Exit Sub
	End If

	Set fulfillmentHttp = CreateObject("MSXML2.ServerXMLHTTP.6.0")
	If Not SendFulfillmentRequest(fulfillmentHttp, sRequestJSON, sError) Then
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

	If Not ValidateFulfillmentResult(sRequestID, sResponseRequestID, sResponseSalesOrderNo, sResponseInvoiceNo, sError) Then
		retMsg = oUI.MessageBox("", sError, "Icon=Exclamation, Title=Faire Fulfillment Result Rejected, Style=OK")
		Exit Sub
	End If
	If Not ApplyFulfillmentResult(trackingRecords, packageRecords, bHasFreightAmount, sFreightAmount, sError) Then
		retMsg = oUI.MessageBox("", sError & vbCrLf & vbCrLf & "The Faire result should remain available for retry using request " & sRequestID & ".", "Icon=Exclamation, Title=Sage Writeback Failed, Style=OK")
		Exit Sub
	End If

	retVal = oScript.InvokeButton("BT_TRACK")
	retVal = oUIObj.HandleScriptUI()
	retMsg = oUI.MessageBox("", "Faire fulfillment was completed and Sage tracking data was updated.", "Icon=Info, Title=Faire Fulfillment Complete, Style=OK")
End Sub

' BuildFulfillmentRequest reads the current Sage shipment and returns its explicit,
' versioned JSON snapshot. errorMessage receives a safe error for missing required data.
Function BuildFulfillmentRequest(requestID, ByRef requestJSON, ByRef errorMessage)
	BuildFulfillmentRequest = False
	errorMessage = ""
	Set oShipping = oBusObj

	If Not TryGetRequiredString(oShipping, "InvoiceNo$", "the active invoice number", sInvoiceNo, errorMessage) Then Exit Function
	If Not TryGetOptionalString(oShipping, "ShipVia$", sShipVia) Then sShipVia = ""
	If Not TryGetOptionalNumber(oShipping, "FreightAmt", nFreightAmount) Then nFreightAmount = 0

	retVal = oShipping.ReadAdditional("SalesOrderNo")
	hSalesOrder = oShipping.GetChildHandle("SalesOrderNo")
	If hSalesOrder = 0 Then
		errorMessage = "Unable to read the related Sage sales order from the active Shipping Data Entry document."
		Exit Function
	End If
	Set oSalesOrder = oShipping.AsObject(hSalesOrder)
	If Not TryGetRequiredString(oSalesOrder, "SalesOrderNo$", "the sales order number", sSalesOrderNo, errorMessage) Then Exit Function
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
	requestJSON = requestJSON & """salesOrderNo"":""" & EscapeJSON(sSalesOrderNo) & ""","
	requestJSON = requestJSON & """invoiceNo"":""" & EscapeJSON(sInvoiceNo) & ""","
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

	' SendFulfillmentRequest posts the UTF-8 JSON document through ServerXMLHTTP with
	' explicit timeouts so a network outage cannot indefinitely freeze Sage's UI.
	Function SendFulfillmentRequest(httpRequest, requestJSON, ByRef errorMessage)
		SendFulfillmentRequest = False
		errorMessage = ""
		On Error Resume Next
		Err.Clear
		httpRequest.setTimeouts HTTP_RESOLVE_TIMEOUT_MS, HTTP_CONNECT_TIMEOUT_MS, HTTP_SEND_TIMEOUT_MS, HTTP_RECEIVE_TIMEOUT_MS
		httpRequest.Open "POST", GetFulfillmentURL(), False
		httpRequest.setRequestHeader "Content-Type", "application/json; charset=utf-8"
		requestBody = UTF8Bytes(requestJSON)
		httpRequest.Send requestBody
		If Err.Number <> 0 Then
			' Capture the COM error before clearing Err so timeout and firewall failures are actionable.
			nHttpError = Err.Number
			sHttpErrorDescription = Err.Description
			errorMessage = "Unable to connect to the Faire GUI on workstation '" & FAIRE_GUI_WORKSTATION & "'. Error " & CStr(nHttpError)
			If sHttpErrorDescription <> "" Then errorMessage = errorMessage & ": " & sHttpErrorDescription
			Err.Clear
			On Error GoTo 0
			Exit Function
		End If
		If httpRequest.status <> HTTP_STATUS_OK Then
			sHttpResponse = Trim(httpRequest.responseText)
			errorMessage = "Faire GUI returned HTTP status " & CStr(httpRequest.status)
			If sHttpResponse <> "" Then errorMessage = errorMessage & ": " & sHttpResponse
			errorMessage = errorMessage & ". No Sage shipment data was changed."
			On Error GoTo 0
			Exit Function
		End If
		On Error GoTo 0
		SendFulfillmentRequest = True
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
				responseRequestID = sValue
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
				If UBound(values) <> 2 Or Trim(values(0)) = "" Or Trim(values(1)) = "" Or Not IsNumeric(Trim(values(2))) Then
					errorMessage = "Faire GUI returned invalid package item data."
					Exit Function
				End If
				packageRecords.Add CStr(packageRecords.Count), Array(Trim(values(0)), Trim(values(1)), CSng(Trim(values(2))))
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
	Function ValidateFulfillmentResult(expectedRequestID, responseRequestID, responseSalesOrderNo, responseInvoiceNo, ByRef errorMessage)
		ValidateFulfillmentResult = False
		If responseRequestID <> expectedRequestID Then
			errorMessage = "The Faire result belongs to a different fulfillment request and was not applied."
			Exit Function
		End If
		If responseSalesOrderNo <> sSalesOrderNo Or responseInvoiceNo <> sInvoiceNo Then
			errorMessage = "The Faire result does not match the active Sage sales order and invoice."
			Exit Function
		End If
		ValidateFulfillmentResult = True
	End Function

	' ApplyFulfillmentResult replaces existing Sage tracking/package records and optionally
	' updates freight after the typed Faire result has passed identity validation.
	Function ApplyFulfillmentResult(trackingRecords, packageRecords, hasFreightAmount, freightAmount, ByRef errorMessage)
		ApplyFulfillmentResult = False
		If trackingRecords.Count = 0 Then
			errorMessage = "Faire completed without a tracking record, so Sage writeback was not applied."
			Exit Function
		End If
		If Not RemoveExistingTrackingRecords(errorMessage) Then Exit Function
		If Not RemoveExistingPackageRecords(errorMessage) Then Exit Function

		For Each recordKey In trackingRecords.Keys
			record = trackingRecords.Item(recordKey)
			If Not WriteTrackingRecord(record(0), record(1), errorMessage) Then Exit Function
		Next
		For Each recordKey In packageRecords.Keys
			record = packageRecords.Item(recordKey)
			If Not WritePackageItemRecord(record(0), record(1), record(2), errorMessage) Then Exit Function
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

	' RemoveExistingTrackingRecords clears the active invoice's tracking records because
	' the approved business rule is to replace, rather than merge, existing records.
	Function RemoveExistingTrackingRecords(ByRef errorMessage)
		RemoveExistingTrackingRecords = False
		hTracking = oSession.GetObject("SO_InvoiceTracking_bus")
		If hTracking = 0 Then
			errorMessage = "Sage could not open the invoice tracking business object."
			Exit Function
		End If
		Set oTracking = oSession.AsObject(hTracking)
		retVal = oTracking.RemoveTrackingRecords(sInvoiceNo)
		Set oTracking = Nothing
		If retVal = 0 Then
			errorMessage = "Sage could not remove existing invoice tracking records: " & oBusObj.LastErrorMsg
			Exit Function
		End If
		RemoveExistingTrackingRecords = True
	End Function

	' RemoveExistingPackageRecords clears the active invoice's package allocations before
	' the final result writes their replacement values.
	Function RemoveExistingPackageRecords(ByRef errorMessage)
		RemoveExistingPackageRecords = False
		hPackageTracking = oSession.GetObject("SO_PackageTrackingByItem_bus")
		If hPackageTracking = 0 Then
			errorMessage = "Sage could not open the package tracking business object."
			Exit Function
		End If
		Set oPackageTracking = oSession.AsObject(hPackageTracking)
		retVal = oPackageTracking.RemoveRecords(sInvoiceNo)
		Set oPackageTracking = Nothing
		If retVal = 0 Then
			errorMessage = "Sage could not remove existing package tracking records: " & oBusObj.LastErrorMsg
			Exit Function
		End If
		RemoveExistingPackageRecords = True
	End Function

	' WriteTrackingRecord creates one invoice/package tracking row using the validated
	' package number and tracking identifier returned by the Faire GUI.
	Function WriteTrackingRecord(packageNo, trackingID, ByRef errorMessage)
		WriteTrackingRecord = False
		hTracking = oSession.GetObject("SO_InvoiceTracking_bus")
		If hTracking = 0 Then
			errorMessage = "Sage could not open the invoice tracking business object."
			Exit Function
		End If
		Set oTracking = oSession.AsObject(hTracking)
		retVal = oTracking.SetKeyValue("InvoiceNo$", sInvoiceNo)
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

	' WritePackageItemRecord creates or merges one item allocation for a package. Existing
	' quantities are added because multiple Faire package lines can reference one Sage item.
	Function WritePackageItemRecord(packageNo, itemCode, quantity, ByRef errorMessage)
		WritePackageItemRecord = False
		hPackageTracking = oSession.GetObject("SO_PackageTrackingByItem_bus")
		If hPackageTracking = 0 Then
			errorMessage = "Sage could not open the package tracking business object."
			Exit Function
		End If
		Set oPackageTracking = oSession.AsObject(hPackageTracking)
		retVal = oPackageTracking.SetKeyValue("InvoiceNo$", sInvoiceNo)
		retVal = oPackageTracking.SetKeyValue("PackageNo$", packageNo)
		retVal = oPackageTracking.SetKeyValue("ItemCode$", itemCode)
		retVal = oPackageTracking.SetKey()
		If retVal = 1 Then
			retVal = oPackageTracking.GetValue("Quantity", nExistingQuantity)
		Else
			nExistingQuantity = 0
		End If
		retVal = oPackageTracking.SetValue("Quantity", CSng(nExistingQuantity) + CSng(quantity))
		If retVal <> 0 Then retVal = oPackageTracking.Write()
		Set oPackageTracking = Nothing
		If retVal = 0 Then
			errorMessage = "Sage could not write package item '" & itemCode & "': " & oBusObj.LastErrorMsg
			Exit Function
		End If
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

	' GetFulfillmentURL returns the temporary direct HTTP endpoint hosted by Faire GUI on RMT01.
	' It does not use the Sage session workstation because Sage runs on the Sage server.
	Function GetFulfillmentURL()
		GetFulfillmentURL = "http://" & FAIRE_GUI_WORKSTATION & ":" & CStr(FAIRE_HTTP_PORT) & FAIRE_HTTP_PATH
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
