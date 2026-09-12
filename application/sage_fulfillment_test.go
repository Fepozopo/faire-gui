package application

import (
	"context"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// TestParseSageFulfillmentRequestAcceptsUTF16PipePayload verifies the exact UTF-16LE chunk format emitted by the Sage FileSystemObject writer.
func TestParseSageFulfillmentRequestAcceptsUTF16PipePayload(t *testing.T) {
	payload := sageUTF16PipePayload(`{"protocolVersion":1,"requestId":"a1b2-c3d4","source":{"companyCode":"BSC","workstation":"PACK-1"},"document":{"salesOrderNo":"SO-1","invoiceNo":"INV-1","faireDisplayId":"ABC123","salesSource":"BSC","shipVia":"UPS","freightAmount":0},"shipTo":{},"lines":[{"sageLineKey":"1","itemCode":"SKU-1","quantityShipped":1,"quantityBackordered":0}]}`)

	decoded, err := decodeSagePipePayload(payload)
	if err != nil {
		t.Fatalf("decodeSagePipePayload() error = %v", err)
	}
	request, err := parseSageFulfillmentRequest(decoded)
	if err != nil {
		t.Fatalf("parseSageFulfillmentRequest() error = %v", err)
	}
	if request.Document.FaireDisplayID != "ABC123" || request.Lines[0].QuantityShipped != 1 {
		t.Fatalf("parsed request = %#v, want display ID and shipped line", request)
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

// sageUTF16PipePayload reproduces Sage's UTF-16LE JSON-chunk stream with a terminal Done line.
func sageUTF16PipePayload(request string) []byte {
	text := request + "\r\nDone\r\n"
	encoded := utf16.Encode([]rune(text))
	payload := make([]byte, 2, len(encoded)*2+2)
	payload[0], payload[1] = 0xFF, 0xFE
	for _, codeUnit := range encoded {
		payload = append(payload, byte(codeUnit), byte(codeUnit>>8))
	}
	return payload
}

// TestSafeSageRequestIDRejectsPipeBreakingInput verifies local pipe names cannot be constructed from untrusted separators.
func TestSafeSageRequestIDRejectsPipeBreakingInput(t *testing.T) {
	for _, value := range []string{"", "request/id", "request\nid", strings.Repeat("a", 129)} {
		if safeSageRequestID(value) {
			t.Fatalf("safeSageRequestID(%q) = true, want false", value)
		}
	}
}
