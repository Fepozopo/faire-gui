package application

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"gioui.org/app"
	"gioui.org/layout"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
)

// TestProfileSummaryUsesProfileValues verifies that profile data takes precedence over saved metadata.
func TestProfileSummaryUsesProfileValues(t *testing.T) {
	t.Parallel()

	profile := &faire.BrandProfile{
		BrandID:  faire.Ptr(faire.BrandID("brand-123")),
		Name:     faire.Ptr("Verified Brand"),
		Currency: faire.Ptr("USD"),
		Locale:   faire.Ptr("en-US"),
	}

	got := profileSummary(connections.Connection{
		Label:   "Saved Brand",
		BrandID: faire.BrandID("saved-brand"),
	}, profile)
	want := "Connected to Verified Brand • Brand ID: brand-123 • Currency: USD • Locale: en-US"
	if got != want {
		t.Fatalf("profileSummary() = %q, want %q", got, want)
	}
}

// TestProfileSummaryFallsBackToSavedBrandID verifies that incomplete API profiles preserve useful saved metadata.
func TestProfileSummaryFallsBackToSavedBrandID(t *testing.T) {
	t.Parallel()

	got := profileSummary(connections.Connection{
		Label:   "Saved Brand",
		BrandID: faire.BrandID("saved-brand"),
	}, &faire.BrandProfile{})
	want := "Connected to Saved Brand • Brand ID: saved-brand"
	if got != want {
		t.Fatalf("profileSummary() = %q, want %q", got, want)
	}
}

// TestProfileLoadErrorMessageHidesResponseBodies verifies that user-visible errors never expose API response data.
func TestProfileLoadErrorMessageHidesResponseBodies(t *testing.T) {
	t.Parallel()

	message := profileLoadErrorMessage(&faire.APIError{
		StatusCode: http.StatusInternalServerError,
		Body:       "sensitive response content",
	})
	if strings.Contains(message, "sensitive response content") {
		t.Fatalf("profileLoadErrorMessage() exposed the API response body: %q", message)
	}
	if !strings.Contains(message, "HTTP 500") {
		t.Fatalf("profileLoadErrorMessage() = %q, want HTTP status", message)
	}
}

// TestProfileLoadErrorMessageExplainsCredentialRejection verifies that rejected credentials have an actionable message.
func TestProfileLoadErrorMessageExplainsCredentialRejection(t *testing.T) {
	t.Parallel()

	message := profileLoadErrorMessage(&faire.APIError{StatusCode: http.StatusUnauthorized})
	if !strings.Contains(message, "credentials") {
		t.Fatalf("profileLoadErrorMessage() = %q, want credential guidance", message)
	}
}

// TestNewDesktopUIConfiguresScrollableListsAndMaskedToken verifies the persistent Gio controls required by the two scrollable screens.
func TestNewDesktopUIConfiguresScrollableListsAndMaskedToken(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")

	if ui.brandsList.Axis != 1 || ui.connectionsList.Axis != 1 {
		t.Fatalf("list axes = (%d, %d), want both vertical", ui.brandsList.Axis, ui.connectionsList.Axis)
	}
	if !ui.accessTokenEditor.SingleLine || ui.accessTokenEditor.Mask != '•' {
		t.Fatalf("access-token editor configuration = {SingleLine:%t Mask:%q}, want single-line bullet mask", ui.accessTokenEditor.SingleLine, ui.accessTokenEditor.Mask)
	}
}

// TestCancelEditorReturnsToDirectTokenCreation verifies cancellation resets every persistent Gio editor, including transient token text.
func TestCancelEditorReturnsToDirectTokenCreation(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.editorMode = connectionEditorEnvironmentImport
	ui.editing = connections.Connection{ID: "connection-id"}
	ui.labelEditor.SetText("Imported Brand")
	ui.brandIDEditor.SetText("brand-id")
	ui.environmentEditor.SetText("API_TOKEN_21C")
	ui.accessTokenEditor.SetText("transient-token")

	ui.resetEditor()

	if ui.editorMode != connectionEditorCreate {
		t.Fatalf("editorMode = %d, want %d", ui.editorMode, connectionEditorCreate)
	}
	if ui.editing != (connections.Connection{}) {
		t.Fatalf("editing = %#v, want zero value", ui.editing)
	}
	if ui.labelEditor.Text() != "" || ui.brandIDEditor.Text() != "" || ui.environmentEditor.Text() != "" || ui.accessTokenEditor.Text() != "" {
		t.Fatalf("editor fields were not cleared: label=%q brandID=%q environment=%q token=%q", ui.labelEditor.Text(), ui.brandIDEditor.Text(), ui.environmentEditor.Text(), ui.accessTokenEditor.Text())
	}
}

// TestSelectConnectionScrollsToStatus verifies profile loading resets a deeply scrolled Brands list so its status feedback is visible.
func TestSelectConnectionScrollsToStatus(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, new(app.Window), nil, nil, "")
	ui.brandsList.Position.First = 12
	ui.brandsList.Position.Offset = -24
	ui.brandsList.Position.BeforeEnd = true

	ui.selectConnection("connection-id")

	if ui.brandsList.Position != (layout.Position{}) {
		t.Fatalf("brands list position = %#v, want zero position", ui.brandsList.Position)
	}
	if ui.status != "Saved connections are unavailable. Restart the app after resolving the credential-store issue." {
		t.Fatalf("status = %q, want unavailable-connections guidance", ui.status)
	}
}

// TestBeginMetadataEditScrollsToForm verifies an edit request resets a deeply scrolled connection list so the editor is visible.
func TestBeginMetadataEditScrollsToForm(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, new(app.Window), nil, nil, "")
	ui.connectionsList.Position.First = 12
	ui.connectionsList.Position.Offset = -24
	ui.connectionsList.Position.BeforeEnd = true
	connection := connections.Connection{ID: "connection-id", Label: "Brand", BrandID: faire.BrandID("brand-id")}

	ui.beginMetadataEdit(connection)

	if ui.connectionsList.Position != (layout.Position{}) {
		t.Fatalf("connections list position = %#v, want zero position", ui.connectionsList.Position)
	}
	if ui.editorMode != connectionEditorMetadata || ui.labelEditor.Text() != "Brand" || ui.brandIDEditor.Text() != "brand-id" {
		t.Fatalf("metadata editor was not prepared: mode=%d label=%q brandID=%q", ui.editorMode, ui.labelEditor.Text(), ui.brandIDEditor.Text())
	}
}

// TestReconcileRowControlsRemovesDeletedConnections verifies stable Gio click state is retained only for active connection rows.
func TestReconcileRowControlsRemovesDeletedConnections(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, []connections.Connection{{ID: "kept"}}, "")
	kept := ui.rowControlsFor("kept")
	ui.rowControlsFor("deleted")

	ui.reconcileRowControls()

	if got := ui.rowControls["kept"]; got != kept {
		t.Fatalf("kept row controls = %p, want original %p", got, kept)
	}
	if _, ok := ui.rowControls["deleted"]; ok {
		t.Fatal("deleted row controls still exist")
	}
}

// TestRequestDeleteRequiresConfirmationState verifies row deletion only opens metadata-only modal state.
func TestRequestDeleteRequiresConfirmationState(t *testing.T) {
	connection := connections.Connection{ID: "connection-id", Label: "Brand"}
	ui := newDesktopUI(context.Background(), func() {}, new(app.Window), nil, nil, "")

	ui.requestDelete(connection)

	if !ui.deleteDialog.open || ui.deleteDialog.connection != connection {
		t.Fatalf("delete dialog = %#v, want open dialog for %#v", ui.deleteDialog, connection)
	}
}

// TestExplicitEnvironmentTokenReadsOnlyTheNamedVariable verifies explicit imports trim only the variable name.
func TestExplicitEnvironmentTokenReadsOnlyTheNamedVariable(t *testing.T) {
	const environmentName = "FAIRE_GUI_IMPORT_TEST_TOKEN"
	const accessToken = "test-direct-token"
	t.Setenv(environmentName, accessToken)

	got, err := explicitEnvironmentToken(" " + environmentName + " ")
	if err != nil {
		t.Fatalf("explicitEnvironmentToken() error = %v", err)
	}
	if got != accessToken {
		t.Fatalf("explicitEnvironmentToken() = %q, want %q", got, accessToken)
	}
}

// TestExplicitEnvironmentTokenRejectsEmptyVariables verifies imports require a non-empty explicit source.
func TestExplicitEnvironmentTokenRejectsEmptyVariables(t *testing.T) {
	t.Setenv("FAIRE_GUI_IMPORT_EMPTY_TEST_TOKEN", "")

	if _, err := explicitEnvironmentToken("FAIRE_GUI_IMPORT_EMPTY_TEST_TOKEN"); err == nil {
		t.Fatal("explicitEnvironmentToken() error = nil, want missing-or-empty variable error")
	}
}
