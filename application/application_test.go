package application

import (
	"context"
	"net/http"
	"strings"
	"testing"

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

// TestCancelEditorReturnsToDirectTokenCreation verifies cancellation resets every connection-editor field.
func TestCancelEditorReturnsToDirectTokenCreation(t *testing.T) {
	application := newApplication(context.Background(), nil, nil, "")
	application.editorMode = connectionEditorEnvironmentImport
	application.editorConnection = connections.Connection{ID: "connection-id"}
	application.editorLabel.Set("Imported Brand")
	application.editorBrandID.Set("brand-id")
	application.environmentName.Set("API_TOKEN_21C")

	application.cancelEditor()

	if application.editorMode != connectionEditorCreate {
		t.Fatalf("editorMode = %d, want %d", application.editorMode, connectionEditorCreate)
	}
	if application.editorConnection != (connections.Connection{}) {
		t.Fatalf("editorConnection = %#v, want zero value", application.editorConnection)
	}
	if application.editorLabel.Get() != "" || application.editorBrandID.Get() != "" || application.environmentName.Get() != "" {
		t.Fatalf("editor fields were not cleared: label=%q brandID=%q environment=%q", application.editorLabel.Get(), application.editorBrandID.Get(), application.environmentName.Get())
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
