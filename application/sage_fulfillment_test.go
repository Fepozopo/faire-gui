package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// TestParseSageFulfillmentRequestAcceptsHTTPJSON verifies the UTF-8 JSON body posted by the Sage HTTP client.
func TestParseSageFulfillmentRequestAcceptsHTTPJSON(t *testing.T) {
	payload := []byte(`{"protocolVersion":1,"requestId":"a1b2-c3d4","source":{"companyCode":"BSC","workstation":"PACK-1"},"document":{"salesOrderNo":"SO-1","invoiceNo":"INV-1","faireDisplayId":"ABC123","salesSource":"BSC","shipVia":"UPS","freightAmount":0},"shipTo":{},"lines":[{"sageLineKey":"1","itemCode":"SKU-1","quantityShipped":1,"quantityBackordered":0}]}`)

	request, err := parseSageFulfillmentRequest(payload)
	if err != nil {
		t.Fatalf("parseSageFulfillmentRequest() error = %v", err)
	}
	if request.Document.FaireDisplayID != "ABC123" || request.Lines[0].QuantityShipped != 1 {
		t.Fatalf("parsed request = %#v, want display ID and shipped line", request)
	}
}

// TestDecodeSageFulfillmentHTTPPayloadAcceptsServerXMLHTTPBSTR verifies the temporary endpoint accepts ServerXMLHTTP's UTF-16LE body without a byte-order mark.
func TestDecodeSageFulfillmentHTTPPayloadAcceptsServerXMLHTTPBSTR(t *testing.T) {
	text := `{"protocolVersion":1}`
	codeUnits := utf16.Encode([]rune(text))
	payload := make([]byte, 0, len(codeUnits)*2)
	for _, codeUnit := range codeUnits {
		payload = append(payload, byte(codeUnit), byte(codeUnit>>8))
	}

	decoded, err := decodeSageFulfillmentHTTPPayload(payload)
	if err != nil {
		t.Fatalf("decodeSageFulfillmentHTTPPayload() error = %v", err)
	}
	if string(decoded) != text {
		t.Fatalf("decoded payload = %q, want %q", decoded, text)
	}
}

// TestSageFulfillmentHTTPHandlerRejectsNonBSDC01 verifies the temporary direct endpoint does not trust an arbitrary network peer.
func TestSageFulfillmentHTTPHandlerRejectsNonBSDC01(t *testing.T) {
	handler := sageFulfillmentHTTPHandler(context.Background(), func(sageFulfillmentInbound) {})
	request := httptest.NewRequest(http.MethodPost, sageFulfillmentHTTPPath, nil)
	request.RemoteAddr = "192.168.128.99:54321"
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("HTTP status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

// TestSageBackorderedVariantsUsesParentKitSKU verifies an exploded Sage component selects its parent kit SKU's Faire variant.
func TestSageBackorderedVariantsUsesParentKitSKU(t *testing.T) {
	lines := []sageFulfillmentLine{
		{SageLineKey: "kit-line", ItemCode: "KIT-100", QuantityShipped: 1},
		{SageLineKey: "component-line", ItemCode: "COMP-100", QuantityBackordered: 1, SalesKitLineKey: "kit-line", ExplodedKitItem: "Y"},
	}
	items := []orders.DetailItem{{SKU: "KIT-100", VariantID: "variant-kit", AvailabilityEligible: true}}

	selected, unresolved := sageBackorderedVariants(lines, items)
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %#v, want none", unresolved)
	}
	if _, found := selected[faire.VariantID("variant-kit")]; !found || len(selected) != 1 {
		t.Fatalf("selected = %#v, want parent kit variant", selected)
	}
}

// TestSageBackorderedVariantsRejectsAmbiguousSKUs verifies automatic availability updates never guess between duplicate Faire variants.
func TestSageBackorderedVariantsRejectsAmbiguousSKUs(t *testing.T) {
	lines := []sageFulfillmentLine{{SageLineKey: "1", ItemCode: "SKU-1", QuantityBackordered: 1}}
	items := []orders.DetailItem{
		{SKU: "SKU-1", VariantID: "variant-a", AvailabilityEligible: true},
		{SKU: "SKU-1", VariantID: "variant-b", AvailabilityEligible: true},
	}

	selected, unresolved := sageBackorderedVariants(lines, items)
	if len(selected) != 0 || len(unresolved) != 1 {
		t.Fatalf("selected = %#v, unresolved = %#v, want one unresolved line", selected, unresolved)
	}
}

// TestConnectionForSageSalesSourceReversesConfiguredBrandMapping verifies Sage BSC source resolves only the saved BSC brand connection.
func TestConnectionForSageSalesSourceReversesConfiguredBrandMapping(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, []connections.Connection{
		{ID: "bsc", Label: "BSC", BrandID: faire.BrandID("b_9yilp4yy")},
		{ID: "asc", Label: "ASC", BrandID: faire.BrandID("b_56pfaass")},
	}, "")

	connection, found := ui.connectionForSageSalesSource(" bsc ")
	if !found || connection.ID != "bsc" {
		t.Fatalf("connectionForSageSalesSource() = %#v, %t; want BSC connection", connection, found)
	}
}

// TestSafeSageRequestIDAcceptsOnlyBoundedSeparatorFreeValues verifies request IDs permit protocol-safe values and reject separators or unbounded data.
func TestSafeSageRequestIDAcceptsOnlyBoundedSeparatorFreeValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid", value: "a1b2-c3d4", want: true},
		{name: "empty", value: "", want: false},
		{name: "slash", value: "request/id", want: false},
		{name: "newline", value: "request\nid", want: false},
		{name: "too long", value: strings.Repeat("a", 129), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := safeSageRequestID(test.value); got != test.want {
				t.Fatalf("safeSageRequestID(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}
