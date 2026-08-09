package connections

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestManagerDirectTokenConnectionKeepsSecretOutOfMetadata verifies direct-token connections construct isolated clients.
func TestManagerDirectTokenConnectionKeepsSecretOutOfMetadata(t *testing.T) {
	manager, metadataPath := newTestManager(t)
	connection, err := manager.Save(context.Background(), Connection{
		Label:              "Brand 21C",
		AuthenticationMode: faire.AuthenticationModeAccessToken,
	}, Credentials{AccessToken: "direct-secret"})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if connection.ID == "" {
		t.Fatal("Save() returned an empty connection ID")
	}

	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(metadata), "direct-secret") || strings.Contains(string(metadata), "access_token") {
		t.Fatalf("metadata unexpectedly contains credentials: %s", metadata)
	}

	client, selected, err := manager.Client(context.Background(), connection.ID, ClientOptions{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) *http.Response {
			if request.Header.Get("X-FAIRE-ACCESS-TOKEN") != "direct-secret" {
				t.Fatalf("access-token header = %q", request.Header.Get("X-FAIRE-ACCESS-TOKEN"))
			}
			if request.Header.Get("X-FAIRE-OAUTH-ACCESS-TOKEN") != "" || request.Header.Get("X-FAIRE-APP-CREDENTIALS") != "" {
				t.Fatal("direct-token request contains OAuth headers")
			}
			return testResponse(request, http.StatusOK, `{}`)
		})},
	})
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if selected != connection {
		t.Fatalf("selected connection = %#v, want %#v", selected, connection)
	}
	if client.AuthenticationMode() != faire.AuthenticationModeAccessToken {
		t.Fatalf("AuthenticationMode() = %q", client.AuthenticationMode())
	}
	if _, err := client.Brands.Profile(context.Background()); err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
}

// TestManagerOAuthConnectionBuildsOAuthClient verifies OAuth secrets produce only OAuth request headers.
func TestManagerOAuthConnectionBuildsOAuthClient(t *testing.T) {
	manager, _ := newTestManager(t)
	connection, err := manager.Save(context.Background(), Connection{
		Label:              "OAuth Brand",
		AuthenticationMode: faire.AuthenticationModeOAuth,
	}, Credentials{
		AppCredentials:   "app-credentials",
		OAuthAccessToken: "oauth-secret",
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	client, _, err := manager.Client(context.Background(), connection.ID, ClientOptions{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) *http.Response {
			if request.Header.Get("X-FAIRE-APP-CREDENTIALS") != "app-credentials" {
				t.Fatalf("app-credentials header = %q", request.Header.Get("X-FAIRE-APP-CREDENTIALS"))
			}
			if request.Header.Get("X-FAIRE-OAUTH-ACCESS-TOKEN") != "oauth-secret" {
				t.Fatalf("OAuth header = %q", request.Header.Get("X-FAIRE-OAUTH-ACCESS-TOKEN"))
			}
			if request.Header.Get("X-FAIRE-ACCESS-TOKEN") != "" {
				t.Fatal("OAuth request contains a direct-token header")
			}
			return testResponse(request, http.StatusOK, `{}`)
		})},
	})
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if client.AuthenticationMode() != faire.AuthenticationModeOAuth {
		t.Fatalf("AuthenticationMode() = %q", client.AuthenticationMode())
	}
	if _, err := client.Brands.Profile(context.Background()); err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
}

// TestManagerDeleteRemovesConnectionAndCredentials verifies deleted connections cannot be selected again.
func TestManagerDeleteRemovesConnectionAndCredentials(t *testing.T) {
	manager, _ := newTestManager(t)
	connection, err := manager.Save(context.Background(), Connection{
		Label:              "Disposable Brand",
		AuthenticationMode: faire.AuthenticationModeAccessToken,
	}, Credentials{AccessToken: "direct-secret"})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := manager.Delete(context.Background(), connection.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, _, err := manager.Client(context.Background(), connection.ID, ClientOptions{}); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("Client() error = %v, want ErrConnectionNotFound", err)
	}
}

// newTestManager creates a file-backed metadata repository and an in-memory credential store.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	metadataPath := t.TempDir() + "/connections.json"
	repository, err := NewFileConnectionRepository(metadataPath)
	if err != nil {
		t.Fatalf("NewFileConnectionRepository() error = %v", err)
	}
	manager, err := NewManager(repository, &memoryCredentialStore{credentials: make(map[string]Credentials)})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager, metadataPath
}

// memoryCredentialStore is a test-only implementation of CredentialStore.
type memoryCredentialStore struct {
	credentials map[string]Credentials
}

// Load retrieves test credentials by connection ID.
func (s *memoryCredentialStore) Load(_ context.Context, connectionID string) (Credentials, error) {
	credentials, found := s.credentials[connectionID]
	if !found {
		return Credentials{}, ErrCredentialNotFound
	}
	return credentials, nil
}

// Save stores test credentials by connection ID.
func (s *memoryCredentialStore) Save(_ context.Context, connectionID string, credentials Credentials) error {
	s.credentials[connectionID] = credentials
	return nil
}

// Delete removes test credentials by connection ID.
func (s *memoryCredentialStore) Delete(_ context.Context, connectionID string) error {
	delete(s.credentials, connectionID)
	return nil
}

// roundTripFunc adapts an in-memory handler into an HTTP transport for client construction tests.
type roundTripFunc func(*http.Request) *http.Response

// RoundTrip invokes the in-memory request handler.
func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request), nil
}

// testResponse creates an in-memory successful HTTP response.
func testResponse(request *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
