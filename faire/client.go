package faire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is Faire's production External API v2 endpoint.
	DefaultBaseURL = "https://www.faire.com/external-api/v2"

	defaultMaxRetries = 3
	maxErrorBodySize  = 64 << 10
)

// AuthenticationMode identifies the credential scheme used by a Client.
type AuthenticationMode string

const (
	// AuthenticationModeAccessToken uses a brand-specific X-FAIRE-ACCESS-TOKEN header.
	AuthenticationModeAccessToken AuthenticationMode = "ACCESS_TOKEN"
	// AuthenticationModeOAuth uses X-FAIRE-APP-CREDENTIALS and X-FAIRE-OAUTH-ACCESS-TOKEN headers.
	AuthenticationModeOAuth AuthenticationMode = "OAUTH"
)

// Config configures a Client. Set either AccessToken or both AppCredentials and OAuthAccessToken.
type Config struct {
	// BaseURL overrides Faire's production endpoint. It is useful for tests.
	BaseURL string
	// HTTPClient performs API requests. A client with a sensible timeout is used when nil.
	HTTPClient *http.Client
	// AccessToken is a direct brand API token sent in the X-FAIRE-ACCESS-TOKEN header.
	AccessToken string
	// AppCredentials is the Base64-encoded application ID and secret required by Faire OAuth requests.
	AppCredentials string
	// OAuthAccessToken authorizes the application to act for a Faire user.
	OAuthAccessToken string
	// MaxRetries limits retries for idempotent requests after a retryable response. Zero uses the default.
	MaxRetries int
}

// Client communicates with Faire's External API v2 using one immutable authentication mode.
type Client struct {
	baseURL            *url.URL
	httpClient         *http.Client
	authenticationMode AuthenticationMode
	accessToken        string
	appCredentials     string
	oauthAccessToken   string
	maxRetries         int

	// Brands provides operations on the current brand profile.
	Brands *BrandsService
	// Orders provides operations for fulfillment and brand orders.
	Orders *OrdersService
	// Inventory provides operations for stock levels.
	Inventory *InventoryService
	// Prices provides operations for product-variant prices.
	Prices *PricesService
	// Products provides product, variant, review, taxonomy, image, and prepack operations.
	Products *ProductsService
	// Retailers provides read-only retailer profile operations.
	Retailers *RetailersService
}

// ConfigFromEnvironment builds a Config from Faire environment variables.
//
// Set FAIRE_ACCESS_TOKEN for a direct brand API token. Alternatively, set
// FAIRE_APP_CREDENTIALS and FAIRE_OAUTH_ACCESS_TOKEN for OAuth. As an alternative to
// FAIRE_APP_CREDENTIALS, set both FAIRE_APPLICATION_ID and FAIRE_APPLICATION_SECRET; this function
// encodes them safely. FAIRE_BASE_URL optionally overrides the production API URL.
func ConfigFromEnvironment() (Config, error) {
	config := Config{
		BaseURL:          os.Getenv("FAIRE_BASE_URL"),
		AccessToken:      os.Getenv("FAIRE_ACCESS_TOKEN"),
		AppCredentials:   os.Getenv("FAIRE_APP_CREDENTIALS"),
		OAuthAccessToken: os.Getenv("FAIRE_OAUTH_ACCESS_TOKEN"),
	}

	if config.AppCredentials == "" {
		applicationID := os.Getenv("FAIRE_APPLICATION_ID")
		applicationSecret := os.Getenv("FAIRE_APPLICATION_SECRET")
		if applicationID != "" || applicationSecret != "" {
			if applicationID == "" || applicationSecret == "" {
				return Config{}, fmt.Errorf("faire: both FAIRE_APPLICATION_ID and FAIRE_APPLICATION_SECRET must be set")
			}
			config.AppCredentials = EncodeAppCredentials(applicationID, applicationSecret)
		}
	}

	return config, nil
}

// NewClientFromEnvironment creates a Client using Faire credentials from environment variables.
func NewClientFromEnvironment() (*Client, error) {
	config, err := ConfigFromEnvironment()
	if err != nil {
		return nil, err
	}

	return NewClient(config)
}

// NewClient creates a validated Client and initializes each API service.
func NewClient(config Config) (*Client, error) {
	hasAccessToken := config.AccessToken != ""
	hasOAuthCredentials := config.AppCredentials != "" || config.OAuthAccessToken != ""
	if hasAccessToken && hasOAuthCredentials {
		return nil, fmt.Errorf("faire: direct access-token and OAuth credentials cannot be combined")
	}
	if !hasAccessToken && (config.AppCredentials == "" || config.OAuthAccessToken == "") {
		return nil, fmt.Errorf("faire: set AccessToken or both AppCredentials and OAuthAccessToken")
	}

	authenticationMode := AuthenticationModeOAuth
	if hasAccessToken {
		authenticationMode = AuthenticationModeAccessToken
	}

	baseURL := DefaultBaseURL
	if config.BaseURL != "" {
		baseURL = config.BaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("faire: parse base URL: %w", err)
	}
	if parsedBaseURL.Scheme != "https" && parsedBaseURL.Scheme != "http" {
		return nil, fmt.Errorf("faire: base URL must use HTTP or HTTPS")
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	maxRetries := config.MaxRetries
	if maxRetries == 0 {
		maxRetries = defaultMaxRetries
	}
	if maxRetries < 0 {
		return nil, fmt.Errorf("faire: max retries cannot be negative")
	}

	client := &Client{
		baseURL:            parsedBaseURL,
		httpClient:         httpClient,
		authenticationMode: authenticationMode,
		accessToken:        config.AccessToken,
		appCredentials:     config.AppCredentials,
		oauthAccessToken:   config.OAuthAccessToken,
		maxRetries:         maxRetries,
	}
	client.Brands = &BrandsService{client: client}
	client.Orders = &OrdersService{client: client}
	client.Inventory = &InventoryService{client: client}
	client.Prices = &PricesService{client: client}
	client.Products = &ProductsService{client: client}
	client.Retailers = &RetailersService{client: client}

	return client, nil
}

// AuthenticationMode returns the credential scheme this client uses without exposing its credentials.
func (c *Client) AuthenticationMode() AuthenticationMode {
	return c.authenticationMode
}

// Ptr returns a pointer to value, which is useful for optional API model fields.
func Ptr[T any](value T) *T {
	return new(value)
}

// newRequest creates an authenticated HTTP request for a Faire API path.
func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body any) (*http.Request, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	endpoint.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("faire: encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("faire: create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if c.authenticationMode == AuthenticationModeAccessToken {
		request.Header.Set("X-FAIRE-ACCESS-TOKEN", c.accessToken)
	} else {
		request.Header.Set("X-FAIRE-APP-CREDENTIALS", c.appCredentials)
		request.Header.Set("X-FAIRE-OAUTH-ACCESS-TOKEN", c.oauthAccessToken)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	return request, nil
}

// doJSON executes a JSON API request, decodes a successful response into destination, and reports close failures.
func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body, destination any) (err error) {
	response, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer func() {
		// Preserve an API or decoding failure because it identifies the operation that failed.
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("faire: close response body: %w", closeErr)
		}
	}()

	if destination == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return fmt.Errorf("faire: decode response: %w", err)
	}
	return nil
}

// do executes an API request, retrying only idempotent methods after transient failures.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		request, err := c.newRequest(ctx, method, path, query, body)
		if err != nil {
			return nil, err
		}

		response, err := c.httpClient.Do(request)
		if err != nil {
			if !isIdempotent(method) || attempt >= c.maxRetries || ctx.Err() != nil {
				return nil, fmt.Errorf("faire: execute request: %w", err)
			}
			if err := waitForRetry(ctx, attempt, ""); err != nil {
				return nil, err
			}
			continue
		}

		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return response, nil
		}
		if isRetryableStatus(response.StatusCode) && isIdempotent(method) && attempt < c.maxRetries {
			retryAfter := response.Header.Get("Retry-After")
			if err := response.Body.Close(); err != nil {
				return nil, fmt.Errorf("faire: close retry response body: %w", err)
			}
			if err := waitForRetry(ctx, attempt, retryAfter); err != nil {
				return nil, err
			}
			continue
		}

		return nil, newAPIError(response)
	}
}

// isIdempotent reports whether retrying method cannot create a duplicate resource.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// isRetryableStatus reports whether a response normally represents a transient server or rate-limit failure.
func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

// waitForRetry waits for the server-provided delay or a bounded exponential backoff.
func waitForRetry(ctx context.Context, attempt int, retryAfter string) error {
	delay := time.Duration(250*(1<<attempt)) * time.Millisecond
	if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil && seconds >= 0 {
		delay = seconds
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
