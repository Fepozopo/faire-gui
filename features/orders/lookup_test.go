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
