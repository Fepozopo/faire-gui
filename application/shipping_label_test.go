package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"gioui.org/layout"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// TestShippingLabelClicks verifies each reprint gesture opens only its own shipment's label and ignores unavailable labels and controls belonging to another order.
func TestShippingLabelClicks(t *testing.T) {
	for _, test := range []struct {
		name          string
		clickedOrder  faire.OrderID
		clickedIndex  int
		clickTracking bool
		want          []string
	}{
		{name: "first label", clickedOrder: "order-1", clickedIndex: 0, want: []string{"https://cdn.faire.com/first.pdf"}},
		{name: "second label", clickedOrder: "order-1", clickedIndex: 1, want: []string{"https://cdn.faire.com/second.pdf"}},
		{name: "missing label", clickedOrder: "order-1", clickedIndex: 2},
		{name: "another order", clickedOrder: "order-2", clickedIndex: 0},
		{name: "tracking stays independent", clickedOrder: "order-1", clickedIndex: 0, clickTracking: true, want: []string{"https://www.ups.com/track?loc=en_US&tracknum=TRACK-1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
			ui.orders.view.orderDetail = orders.Detail{
				OrderID: "order-1",
				Shipments: []orders.DetailShipment{
					{ShippingLabelURL: "https://cdn.faire.com/first.pdf", TrackingURL: "https://www.ups.com/track?loc=en_US&tracknum=TRACK-1"},
					{ShippingLabelURL: "https://cdn.faire.com/second.pdf"},
					{},
				},
			}
			var opened []string
			ui.openBrowserURL = func(rawURL string) error {
				opened = append(opened, rawURL)
				return nil
			}
			if test.clickTracking {
				ui.shipmentTrackingControlFor(test.clickedOrder, test.clickedIndex).Click()
			} else {
				ui.shipmentLabelControlFor(test.clickedOrder, test.clickedIndex).Click()
			}
			ui.handleExistingShipmentEvents(layout.Context{})
			// The next frame must not repeat an already consumed reprint gesture.
			ui.handleExistingShipmentEvents(layout.Context{})
			if !reflect.DeepEqual(opened, test.want) {
				t.Fatalf("click on order %q shipment %d (tracking=%t) opened %q, want %q", test.clickedOrder, test.clickedIndex, test.clickTracking, opened, test.want)
			}
		})
	}
}

// TestOpenShippingLabelStatus verifies browser launch results produce actionable visible feedback without exposing a signed URL or system error details.
func TestOpenShippingLabelStatus(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "opened", want: "Shipping label opened in your browser. Print it from the browser or PDF viewer."},
		{name: "launch failure", err: errors.New("sensitive browser failure details"), want: "Could not open the shipping label. Try again or refresh the order."},
	} {
		t.Run(test.name, func(t *testing.T) {
			ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
			openedURL := ""
			ui.openBrowserURL = func(rawURL string) error {
				openedURL = rawURL
				return test.err
			}
			ui.openShippingLabel("https://cdn.faire.com/label.pdf?token=private")
			if openedURL != "https://cdn.faire.com/label.pdf?token=private" {
				t.Fatalf("openShippingLabel opened %q, want the original signed URL", openedURL)
			}
			if got := ui.orders.view.orderDetailStatus; got != test.want {
				t.Fatalf("browser result %v produced detail status %q, want %q", test.err, got, test.want)
			}
		})
	}
}
