package faire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestConfigFromEnvironmentEncodesCredentials verifies that separate application credentials are encoded correctly.
func TestConfigFromEnvironmentEncodesCredentials(t *testing.T) {
	t.Setenv("FAIRE_ACCESS_TOKEN", "")
	t.Setenv("FAIRE_APP_CREDENTIALS", "")
	t.Setenv("FAIRE_APPLICATION_ID", "application-id")
	t.Setenv("FAIRE_APPLICATION_SECRET", "application-secret")
	t.Setenv("FAIRE_OAUTH_ACCESS_TOKEN", "oauth-token")
	t.Setenv("FAIRE_BASE_URL", "https://example.test/api")

	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatalf("ConfigFromEnvironment() error = %v", err)
	}
	if config.AppCredentials != "YXBwbGljYXRpb24taWQ6YXBwbGljYXRpb24tc2VjcmV0" {
		t.Fatalf("AppCredentials = %q", config.AppCredentials)
	}
	if config.OAuthAccessToken != "oauth-token" {
		t.Fatalf("OAuthAccessToken = %q", config.OAuthAccessToken)
	}
	if config.BaseURL != "https://example.test/api" {
		t.Fatalf("BaseURL = %q", config.BaseURL)
	}
}

// TestBrandsProfileAddsAuthenticationHeaders verifies that services use the configured credentials and decode models.
func TestBrandsProfileAddsAuthenticationHeaders(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) *http.Response {
		if request.URL.Path != "/brands/profile" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("X-FAIRE-APP-CREDENTIALS") != "credentials" {
			t.Fatalf("app credentials header = %q", request.Header.Get("X-FAIRE-APP-CREDENTIALS"))
		}
		if request.Header.Get("X-FAIRE-OAUTH-ACCESS-TOKEN") != "token" {
			t.Fatalf("OAuth header = %q", request.Header.Get("X-FAIRE-OAUTH-ACCESS-TOKEN"))
		}
		return testResponse(request, http.StatusOK, `{"brand_id":"b_123","name":"Acme"}`)
	})

	profile, err := client.Brands.Profile(context.Background())
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if profile.BrandID == nil || *profile.BrandID != BrandID("b_123") {
		t.Fatalf("BrandID = %#v", profile.BrandID)
	}
	if profile.Name == nil || *profile.Name != "Acme" {
		t.Fatalf("Name = %#v", profile.Name)
	}
}

// TestDirectAccessTokenAuthentication verifies that direct-token clients send only Faire's direct-token header.
func TestDirectAccessTokenAuthentication(t *testing.T) {
	client, err := NewClient(Config{
		BaseURL:     "https://example.test",
		AccessToken: "brand-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) *http.Response {
			if request.Header.Get("X-FAIRE-ACCESS-TOKEN") != "brand-token" {
				t.Fatalf("access token header = %q", request.Header.Get("X-FAIRE-ACCESS-TOKEN"))
			}
			if request.Header.Get("X-FAIRE-APP-CREDENTIALS") != "" || request.Header.Get("X-FAIRE-OAUTH-ACCESS-TOKEN") != "" {
				t.Fatal("direct-token request must not include OAuth headers")
			}
			return testResponse(request, http.StatusOK, `{"brand_id":"b_123"}`)
		})},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.AuthenticationMode() != AuthenticationModeAccessToken {
		t.Fatalf("AuthenticationMode() = %q", client.AuthenticationMode())
	}
	if _, err := client.Brands.Profile(context.Background()); err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
}

// TestNewClientRejectsMixedCredentials verifies that a request can never send direct and OAuth credentials together.
func TestNewClientRejectsMixedCredentials(t *testing.T) {
	_, err := NewClient(Config{
		AccessToken:      "brand-token",
		AppCredentials:   "app-credentials",
		OAuthAccessToken: "oauth-token",
	})
	if err == nil {
		t.Fatal("NewClient() error = nil, want mixed-credential validation error")
	}
}

// TestOrdersListBuildsQuery verifies that typed list controls serialize to Faire's query contract.
func TestOrdersListBuildsQuery(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) *http.Response {
		if request.URL.Path != "/orders" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		want := url.Values{
			"cursor":          {"next"},
			"excluded_states": {"PROCESSING,CANCELED"},
			"limit":           {"25"},
			"sort_by":         {"UPDATED_AT"},
			"created_at_min":  {"2025-03-21T00:00:00Z"},
		}
		if request.URL.Query().Encode() != want.Encode() {
			t.Fatalf("query = %q, want %q", request.URL.Query().Encode(), want.Encode())
		}
		return testResponse(request, http.StatusOK, `{"orders":[],"cursor":"next"}`)
	})

	_, err := client.Orders.List(context.Background(), &OrderListOptions{
		Limit:          Ptr(int64(25)),
		ExcludedStates: []OrderState{OrderStateProcessing, OrderStateCanceled},
		SortBy:         Ptr(OrderSortByUpdatedAt),
		Cursor:         Ptr("next"),
		CreatedAtMin:   Ptr("2025-03-21T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

// TestProductUpdatePreservesExplicitFalse verifies optional pointer fields can serialize intentional false values.
func TestProductUpdatePreservesExplicitFalse(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) *http.Response {
		if request.Method != http.MethodPatch {
			t.Fatalf("method = %q", request.Method)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if value, ok := payload["allow_sales_when_out_of_stock"]; !ok || value != false {
			t.Fatalf("allow_sales_when_out_of_stock = %#v", value)
		}
		return testResponse(request, http.StatusOK, `{"id":"p_123","allow_sales_when_out_of_stock":false}`)
	})

	product, err := client.Products.Update(context.Background(), ProductID("p_123"), Product{
		AllowSalesWhenOutOfStock: Ptr(false),
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if product.AllowSalesWhenOutOfStock == nil || *product.AllowSalesWhenOutOfStock {
		t.Fatalf("AllowSalesWhenOutOfStock = %#v", product.AllowSalesWhenOutOfStock)
	}
}

// TestAPIErrorRetainsFailureDetails verifies callers can inspect HTTP failures with errors.As.
func TestAPIErrorRetainsFailureDetails(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) *http.Response {
		response := testResponse(request, http.StatusBadRequest, "invalid product")
		response.Header.Set("X-Request-ID", "request-123")
		return response
	})

	err := client.Products.Delete(context.Background(), ProductID("p_123"))
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("Delete() error = %v, want APIError", err)
	}
	if apiError.StatusCode != http.StatusBadRequest || apiError.RequestID != "request-123" {
		t.Fatalf("APIError = %#v", apiError)
	}
}

// newTestClient creates a client backed by an in-memory HTTP transport.
func newTestClient(t *testing.T, handler func(*http.Request) *http.Response) *Client {
	t.Helper()
	client, err := NewClient(Config{
		BaseURL:          "https://example.test",
		AppCredentials:   "credentials",
		OAuthAccessToken: "token",
		HTTPClient:       &http.Client{Transport: roundTripFunc(handler)},
		MaxRetries:       1,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

// testResponse creates an in-memory response for a request handled by roundTripFunc.
func testResponse(request *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

// roundTripFunc adapts a request handler into an http.RoundTripper for transport tests.
type roundTripFunc func(*http.Request) *http.Response

// RoundTrip executes the adapted in-memory request handler.
func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request), nil
}
