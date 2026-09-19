package orders

import (
	"errors"
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestOrderIDFromDisplayID verifies safe display-ID normalization and conversion.
func TestOrderIDFromDisplayID(t *testing.T) {
	got, err := OrderIDFromDisplayID(" \t#ANMQ69YVJB\n ")
	if err != nil {
		t.Fatalf("OrderIDFromDisplayID() error = %v", err)
	}
	if want := faire.OrderID("bo_anmq69yvjb"); got != want {
		t.Fatalf("OrderIDFromDisplayID() = %q, want %q", got, want)
	}
}

// TestDisplayIDFromOrderID verifies Faire order IDs are converted locally to their visible uppercase display IDs.
func TestDisplayIDFromOrderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		orderID faire.OrderID
		want    string
	}{
		{name: "lowercase API prefix", orderID: "bo_83bavnwg8v", want: "83BAVNWG8V"},
		{name: "uppercase API prefix", orderID: "BO_83bavnwg8v", want: "83BAVNWG8V"},
		{name: "display ID without prefix", orderID: "83bavnwg8v", want: "83BAVNWG8V"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DisplayIDFromOrderID(test.orderID); got != test.want {
				t.Fatalf("DisplayIDFromOrderID(%q) = %q, want %q", test.orderID, got, test.want)
			}
		})
	}
}

// TestOrderIDFromDisplayIDRejectsUnsafeInput verifies only canonical ASCII display IDs become API IDs.
func TestOrderIDFromDisplayIDRejectsUnsafeInput(t *testing.T) {
	inputs := []string{"", "#", "bo_anmq69yvjb", "ANMQ69YVJ!", "ＡＮＭＱ６９ＹＶＪＢ", "# #ANMQ69YVJB"}
	for _, input := range inputs {
		_, err := OrderIDFromDisplayID(input)
		if !errors.Is(err, ErrInvalidDisplayID) {
			t.Errorf("OrderIDFromDisplayID(%q) error = %v, want ErrInvalidDisplayID", input, err)
		}
	}
}
