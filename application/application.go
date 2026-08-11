package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	_ "github.com/gogpu/gg/gpu"
	"github.com/gogpu/gogpu"
	uiapp "github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
)

const (
	windowTitle  = "Faire GUI"
	windowWidth  = 960
	windowHeight = 640
)

// Application owns the read-only desktop interface for selecting saved Faire connections.
// It stores connection metadata and user-visible status only; credentials remain in the
// operating-system credential store and are loaded transiently by connections.Manager.
type Application struct {
	context     context.Context
	manager     *connections.Manager
	connections []connections.Connection
	status      state.Signal[string]
}

// Run starts the Faire desktop application. It returns an error only when the GoGPU
// window or rendering runtime cannot be initialized or run.
func Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager, savedConnections, startupStatus := loadSavedConnections(ctx)
	application := newApplication(ctx, manager, savedConnections, startupStatus)

	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(windowTitle).
		WithSize(windowWidth, windowHeight))
	uiApplication := uiapp.New(
		uiapp.WithWindowProvider(gogpuApp),
		uiapp.WithPlatformProvider(gogpuApp),
		uiapp.WithEventSource(gogpuApp.EventSource()),
	)
	uiApplication.SetRoot(application.root())

	return desktop.Run(gogpuApp, uiApplication)
}

// newApplication creates an Application from previously loaded, non-secret connection metadata.
// The returned Application uses startupStatus to report a credential-store or metadata error safely.
func newApplication(ctx context.Context, manager *connections.Manager, savedConnections []connections.Connection, startupStatus string) *Application {
	return &Application{
		context:     ctx,
		manager:     manager,
		connections: savedConnections,
		status:      state.NewSignal(startupStatus),
	}
}

// loadSavedConnections initializes the default connection manager and loads its metadata.
// It returns a nil manager and a safe status message if the credential store or metadata file is unavailable.
func loadSavedConnections(ctx context.Context) (*connections.Manager, []connections.Connection, string) {
	manager, err := connections.NewDefaultManager()
	if err != nil {
		return nil, nil, "Saved connections are unavailable. Check that your system credential store is available, then restart the app."
	}

	savedConnections, err := manager.List(ctx)
	if err != nil {
		return nil, nil, "Saved connection metadata could not be loaded. Check the application configuration and restart the app."
	}

	if len(savedConnections) == 0 {
		return manager, savedConnections, "No saved connections yet. Connection creation will be added in a future update."
	}

	return manager, savedConnections, "Select a saved Faire brand connection to load its profile."
}

// root builds the static connection-selector layout and binds its status label to Application.status.
// It returns the root widget displayed by the GoGPU UI application.
func (a *Application) root() widget.Widget {
	children := []widget.Widget{
		primitives.Text("Faire").FontSize(32).Bold(),
		primitives.Text("Choose a saved brand connection to verify its Faire profile.").FontSize(16),
	}

	for _, savedConnection := range a.connections {
		connection := savedConnection
		children = append(children, button.New(
			button.TextOpt(connection.Label),
			button.OnClick(func() {
				a.selectConnection(connection.ID)
			}),
		))
	}

	children = append(children,
		primitives.Text("Status").FontSize(18).Bold(),
		primitives.Text("").ContentSignal(a.status).FontSize(16),
	)

	return primitives.VBox(children...).
		Padding(32).
		Gap(16).
		Background(widget.RGBA8(250, 250, 250, 255))
}

// selectConnection begins loading the selected connection's read-only brand profile.
// It returns immediately so network I/O cannot block the desktop event loop.
func (a *Application) selectConnection(connectionID string) {
	if a.manager == nil {
		a.status.Set("Saved connections are unavailable. Restart the app after resolving the credential-store issue.")
		return
	}

	a.status.Set("Loading selected Faire brand profile…")
	go a.loadProfile(connectionID)
}

// loadProfile creates an isolated client for connectionID and loads its brand profile.
// It updates Application.status with a credential-safe success or failure message.
func (a *Application) loadProfile(connectionID string) {
	client, connection, err := a.manager.Client(a.context, connectionID, connections.ClientOptions{})
	if err != nil {
		a.status.Set(profileLoadErrorMessage(err))
		return
	}

	profile, err := client.Brands.Profile(a.context)
	if err != nil {
		a.status.Set(profileLoadErrorMessage(err))
		return
	}

	a.status.Set(profileSummary(connection, profile))
}

// profileLoadErrorMessage converts an internal profile-loading error into a safe status message.
// It returns a message that identifies actionable HTTP failure classes without displaying response bodies or credentials.
func profileLoadErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Profile loading was canceled."
	}

	var apiError *faire.APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then try again."
		default:
			return fmt.Sprintf("Faire could not load this brand profile (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}

	return "The saved connection could not be opened. Check the system credential store and try again."
}

// profileSummary formats the selected connection and brand profile for display.
// It returns a non-secret summary that omits absent optional fields.
func profileSummary(connection connections.Connection, profile *faire.BrandProfile) string {
	name := connection.Label
	if profile != nil && profile.Name != nil && *profile.Name != "" {
		name = *profile.Name
	}

	fields := []string{"Connected to " + name}
	if profile != nil && profile.BrandID != nil {
		fields = append(fields, "Brand ID: "+string(*profile.BrandID))
	} else if connection.BrandID != "" {
		fields = append(fields, "Brand ID: "+string(connection.BrandID))
	}
	if profile != nil && profile.Currency != nil && *profile.Currency != "" {
		fields = append(fields, "Currency: "+*profile.Currency)
	}
	if profile != nil && profile.Locale != nil && *profile.Locale != "" {
		fields = append(fields, "Locale: "+*profile.Locale)
	}

	return strings.Join(fields, " • ")
}
