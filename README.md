# faire-gui

A GoGPU desktop application for managing a Faire brand. It provides a secure saved-connection manager, a brand selector, and read-only brand-profile verification backed by the typed Faire API client.

## Desktop application

The desktop application has **Brands** and **Connections** tabs. The Brands tab lets the user select a saved connection and displays its read-only Faire brand profile. The Connections tab supports direct-token connections:

- create a connection with a label, optional Faire brand ID, and password-masked access-token field;
- edit only non-secret label and brand-ID metadata;
- explicitly replace an access token without displaying or loading the prior token into the UI;
- import a direct token from one environment-variable name the user explicitly enters, such as `API_TOKEN_21C`, without scanning the process environment; and
- delete a connection after a modal confirmation, removing both metadata and its operating-system credential entry.

OAuth connections can have their metadata edited and can be deleted, but OAuth credential creation and reauthorization remain unavailable until the Authorization Code Grant flow is implemented.

Every selected client is created through `connections.Manager`. Credentials remain in macOS Keychain or Windows Credential Manager; a typed or explicitly imported token exists only transiently while it is saved, is never displayed, serialized, or logged, and the app does not change the source environment variable.

GoGPU requires CGO to be disabled. Run the app with:

```sh
CGO_ENABLED=0 go run ./cmd/faire-gui
```

If a machine has graphics-driver trouble, GoGPU can use its software renderer:

```sh
CGO_ENABLED=0 GOGPU_GRAPHICS_API=software go run ./cmd/faire-gui
```

The current target platforms are macOS and Windows.

## Backend structure

- `faire/` — typed Faire External API v2 client.
  - `client.go` — configuration, authenticated HTTP transport, retry handling, and service initialization.
  - `auth_error.go` — credential encoding and typed API errors.
  - `types_*.go` — documented API domain models, request/response models, custom identifier types, and enum types.
  - `services_*.go` — endpoint groups for brands, orders, inventory, prices, products, prepacks, reviews, and retailers.

The client implements every operation in the supplied `faire_api.json` specification. Optional API fields use pointers so PATCH requests can distinguish an omitted field from an explicit zero value or `false`.

## Configuration

The client supports two mutually exclusive authentication modes.

### Direct brand API token

Set one brand's token for the active session:

```sh
export FAIRE_ACCESS_TOKEN="your-brand-token"
```

The client sends this value only in the `X-FAIRE-ACCESS-TOKEN` header. The desktop Connections tab can import one explicitly named environment variable (for example, `API_TOKEN_21C`) into the operating-system credential store; it never scans environment variables automatically. You can also construct a separate `faire.Client` for a selected value:

```go
client, err := faire.NewClient(faire.Config{
    AccessToken: os.Getenv("API_TOKEN_21C"),
})
```

Clients keep their credentials immutable, so switching a selected brand cannot accidentally reuse another brand's token.

### OAuth

OAuth requires both application credentials and an OAuth access token:

```sh
export FAIRE_APP_CREDENTIALS="base64(applicationId:applicationSecret)"
export FAIRE_OAUTH_ACCESS_TOKEN="your-oauth-token"
```

Alternatively, let the client encode the application credentials:

```sh
export FAIRE_APPLICATION_ID="your-application-id"
export FAIRE_APPLICATION_SECRET="your-application-secret"
export FAIRE_OAUTH_ACCESS_TOKEN="your-oauth-token"
```

`FAIRE_BASE_URL` is optional and exists primarily for tests or a future non-production environment. Do not set direct-token and OAuth variables for the same client configuration; the client rejects mixed credentials.

## Saved connections

The `connections` package is the preferred production credential layer on macOS and Windows. It stores only connection labels, Faire brand IDs, and authentication mode in an owner-only metadata file beneath the user configuration directory. It stores the corresponding direct-token or OAuth credential bundle in macOS Keychain or Windows Credential Manager under the `github.com/Fepozopo/faire-gui` service. Tokens and OAuth secrets are never written to the metadata file.

```go
manager, err := connections.NewDefaultManager()
if err != nil {
    // Handle an unavailable credential store or configuration directory.
}

connection, err := manager.Save(context.Background(), connections.Connection{
    Label:              "Brand 21C",
    AuthenticationMode: faire.AuthenticationModeAccessToken,
}, connections.Credentials{
    AccessToken: os.Getenv("API_TOKEN_21C"),
})

client, selected, err := manager.Client(context.Background(), connection.ID, connections.ClientOptions{})
_ = client
_ = selected
```

For OAuth connections, use `faire.AuthenticationModeOAuth` and supply both `AppCredentials` and `OAuthAccessToken` in `connections.Credentials`. The desktop UI lists existing OAuth connections and permits metadata edits or deletion, but it does not accept OAuth credentials until an Authorization Code Grant flow exists. The GUI always builds a selected client through `manager.Client(...)`.

## Usage

```go
client, err := faire.NewClientFromEnvironment()
if err != nil {
    // Handle invalid or incomplete configuration.
}

products, err := client.Products.List(context.Background(), nil)
if err != nil {
    // API failures are returned as *faire.APIError.
}
_ = products
```

Run the backend tests with:

```sh
go test ./...
```
