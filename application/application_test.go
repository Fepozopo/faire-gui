package application

import (
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
