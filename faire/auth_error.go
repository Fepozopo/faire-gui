package faire

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

// EncodeAppCredentials encodes a Faire application ID and secret for the X-FAIRE-APP-CREDENTIALS header.
func EncodeAppCredentials(applicationID, applicationSecret string) string {
	return base64.StdEncoding.EncodeToString([]byte(applicationID + ":" + applicationSecret))
}

// APIError describes a non-success HTTP response returned by Faire.
type APIError struct {
	StatusCode int
	Status     string
	RequestID  string
	Body       string
}

// Error returns a concise description of the failed Faire API request.
func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("faire: API request failed: %s", e.Status)
	}
	return fmt.Sprintf("faire: API request failed: %s: %s", e.Status, e.Body)
}

// newAPIError reads and closes a failed API response before returning it as an APIError.
func newAPIError(response *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBodySize))
	closeErr := response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("faire: read error response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("faire: close error response body: %w", closeErr)
	}

	return &APIError{
		StatusCode: response.StatusCode,
		Status:     response.Status,
		RequestID:  response.Header.Get("X-Request-ID"),
		Body:       string(body),
	}
}
