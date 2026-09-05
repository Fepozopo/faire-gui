package application

import (
	"context"
	"reflect"
	"testing"

	"github.com/Fepozopo/faire-gui/connections"
)

// TestSortedConnectionsByLabel verifies UI connection lists sort labels without
// changing the caller's persistence-order slice and resolve matching labels by ID.
func TestSortedConnectionsByLabel(t *testing.T) {
	connectionsToSort := []connections.Connection{
		{ID: "z", Label: "zebra"},
		{ID: "b", Label: "Alpha"},
		{ID: "a", Label: " alpha "},
		{ID: "m", Label: "Moon"},
	}

	sorted := sortedConnectionsByLabel(connectionsToSort)
	want := []connections.Connection{
		{ID: "a", Label: " alpha "},
		{ID: "b", Label: "Alpha"},
		{ID: "m", Label: "Moon"},
		{ID: "z", Label: "zebra"},
	}
	if !reflect.DeepEqual(sorted, want) {
		t.Fatalf("sortedConnectionsByLabel() = %#v, want %#v", sorted, want)
	}
	if connectionsToSort[0].ID != "z" || connectionsToSort[1].ID != "b" {
		t.Fatalf("sortedConnectionsByLabel() changed input slice: %#v", connectionsToSort)
	}
}

// TestNewDesktopUISortsSavedConnections verifies the shared UI connection slice,
// used by both the picker and Saved Connections page, is sorted at construction.
func TestNewDesktopUISortsSavedConnections(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, []connections.Connection{
		{ID: "z", Label: "Zebra"},
		{ID: "a", Label: "alpha"},
	}, "")

	if got, want := []string{ui.connections[0].Label, ui.connections[1].Label}, []string{"alpha", "Zebra"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UI connection labels = %v, want %v", got, want)
	}
}
