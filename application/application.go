// Package application coordinates the Faire desktop user interface and confines entered credentials to transient password fields.
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
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/tabview"
	"github.com/gogpu/ui/core/textfield"
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

// connectionEditorMode identifies the connection-management form currently shown to the user.
type connectionEditorMode uint8

const (
	// connectionEditorCreate creates a new direct-token connection.
	connectionEditorCreate connectionEditorMode = iota
	// connectionEditorMetadata updates an existing connection's non-secret metadata.
	connectionEditorMetadata
	// connectionEditorCredentials replaces an existing direct-token credential.
	connectionEditorCredentials
)

// Application owns the desktop interface for selecting and managing saved Faire connections.
// It stores connection metadata, form metadata, and user-visible statuses only; credentials
// exist transiently in a password field while entered and remain in the operating-system store.
type Application struct {
	context     context.Context
	manager     *connections.Manager
	connections []connections.Connection
	ui          *uiapp.App

	status           state.Signal[string]
	managementStatus state.Signal[string]
	selectedTab      state.Signal[int]
	editorLabel      state.Signal[string]
	editorBrandID    state.Signal[string]
	editorMode       connectionEditorMode
	editorConnection connections.Connection
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
	application.ui = uiApplication
	uiApplication.SetRoot(application.root())

	return desktop.Run(gogpuApp, uiApplication)
}

// newApplication creates an Application from previously loaded, non-secret connection metadata.
// The returned Application uses startupStatus to report a credential-store or metadata error safely.
func newApplication(ctx context.Context, manager *connections.Manager, savedConnections []connections.Connection, startupStatus string) *Application {
	return &Application{
		context:          ctx,
		manager:          manager,
		connections:      savedConnections,
		status:           state.NewSignal(startupStatus),
		managementStatus: state.NewSignal("Create a direct-token connection, or select an existing connection to manage it."),
		selectedTab:      state.NewSignal(0),
		editorLabel:      state.NewSignal(""),
		editorBrandID:    state.NewSignal(""),
		editorMode:       connectionEditorCreate,
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
		return manager, savedConnections, "No saved connections yet. Use the Connections tab to add one."
	}

	return manager, savedConnections, "Select a saved Faire brand connection to load its profile."
}

// root builds the tabbed selector and connection-management interface.
// It returns the root widget displayed by the GoGPU UI application.
func (a *Application) root() widget.Widget {
	tabs := tabview.New([]tabview.Tab{
		{Label: "Brands", Content: a.brandSelector()},
		{Label: "Connections", Content: a.connectionManager()},
	}, tabview.SelectedSignalOpt(a.selectedTab))
	return primitives.Box(tabs).Background(widget.RGBA8(250, 250, 250, 255))
}

// brandSelector builds the read-only view used to select a saved connection and load its brand profile.
// It returns a selector widget that contains no credentials.
func (a *Application) brandSelector() widget.Widget {
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

	return primitives.VBox(children...).Padding(32).Gap(16)
}

// connectionManager builds the direct-token connection-management screen.
// It returns a widget that permits metadata edits without reading saved credentials into the UI.
func (a *Application) connectionManager() widget.Widget {
	children := []widget.Widget{
		primitives.Text("Saved connections").FontSize(28).Bold(),
		primitives.Text("Credentials are stored only in the operating-system credential store.").FontSize(16),
		primitives.Text("").ContentSignal(a.managementStatus).FontSize(16),
		a.connectionEditor(),
		primitives.Text("Existing connections").FontSize(20).Bold(),
	}

	if len(a.connections) == 0 {
		children = append(children, primitives.Text("No connections have been saved yet.").FontSize(16))
	}
	for _, savedConnection := range a.connections {
		children = append(children, a.connectionRow(savedConnection))
	}

	return primitives.VBox(children...).Padding(32).Gap(14)
}

// connectionEditor builds the form for the Application's active connection-management operation.
// It returns a form widget with a password field only when a direct token must be supplied.
func (a *Application) connectionEditor() widget.Widget {
	switch a.editorMode {
	case connectionEditorMetadata:
		return a.metadataEditor()
	case connectionEditorCredentials:
		return a.credentialEditor()
	default:
		return a.createConnectionEditor()
	}
}

// createConnectionEditor builds the form for saving a new direct-token connection.
// It returns a form whose token field is cleared immediately after a save attempt.
func (a *Application) createConnectionEditor() widget.Widget {
	accessToken := textfield.New(
		textfield.Placeholder("Faire access token"),
		textfield.InputTypeOpt(textfield.TypePassword),
		textfield.A11yLabel("Faire access token"),
	)

	return primitives.VBox(
		primitives.Text("Add direct-token connection").FontSize(20).Bold(),
		primitives.Text("OAuth connection creation will be added with the Authorization Code Grant flow.").FontSize(14),
		textfield.New(
			textfield.Placeholder("Connection label"),
			textfield.ValueSignal(a.editorLabel),
			textfield.A11yLabel("Connection label"),
		),
		textfield.New(
			textfield.Placeholder("Faire brand ID (optional)"),
			textfield.ValueSignal(a.editorBrandID),
			textfield.A11yLabel("Faire brand ID"),
		),
		accessToken,
		button.New(
			button.TextOpt("Save direct-token connection"),
			button.OnClick(func() {
				token := accessToken.Text()
				accessToken.SetText("")
				a.saveDirectConnection(token)
			}),
		),
	).Padding(20).Gap(12).Background(widget.RGBA8(238, 245, 255, 255)).Rounded(8)
}

// metadataEditor builds the form for updating a saved connection's label and optional brand ID.
// It returns a form that cannot modify or reveal the saved credential bundle.
func (a *Application) metadataEditor() widget.Widget {
	return primitives.VBox(
		primitives.Text("Edit connection metadata").FontSize(20).Bold(),
		primitives.Text("Authentication mode and saved credentials cannot be changed here.").FontSize(14),
		textfield.New(
			textfield.Placeholder("Connection label"),
			textfield.ValueSignal(a.editorLabel),
			textfield.A11yLabel("Connection label"),
		),
		textfield.New(
			textfield.Placeholder("Faire brand ID (optional)"),
			textfield.ValueSignal(a.editorBrandID),
			textfield.A11yLabel("Faire brand ID"),
		),
		primitives.HBox(
			button.New(
				button.TextOpt("Save metadata"),
				button.OnClick(a.saveMetadata),
			),
			button.New(
				button.TextOpt("Cancel"),
				button.OnClick(a.resetEditor),
			),
		).Gap(12),
	).Padding(20).Gap(12).Background(widget.RGBA8(238, 245, 255, 255)).Rounded(8)
}

// credentialEditor builds the form for replacing one saved direct-token credential.
// It returns a form that never pre-populates the existing token.
func (a *Application) credentialEditor() widget.Widget {
	accessToken := textfield.New(
		textfield.Placeholder("New Faire access token"),
		textfield.InputTypeOpt(textfield.TypePassword),
		textfield.A11yLabel("New Faire access token"),
	)

	return primitives.VBox(
		primitives.Text("Replace direct access token").FontSize(20).Bold(),
		primitives.Text("The existing token is not displayed and cannot be recovered from this screen.").FontSize(14),
		accessToken,
		primitives.HBox(
			button.New(
				button.TextOpt("Replace access token"),
				button.OnClick(func() {
					token := accessToken.Text()
					accessToken.SetText("")
					a.replaceAccessToken(token)
				}),
			),
			button.New(
				button.TextOpt("Cancel"),
				button.OnClick(a.resetEditor),
			),
		).Gap(12),
	).Padding(20).Gap(12).Background(widget.RGBA8(238, 245, 255, 255)).Rounded(8)
}

// connectionRow builds management controls for one saved connection.
// It returns a metadata-only row with direct-token credential replacement restricted to direct-token connections.
func (a *Application) connectionRow(connection connections.Connection) widget.Widget {
	actions := []widget.Widget{
		button.New(
			button.TextOpt("Edit metadata"),
			button.OnClick(func() {
				a.beginMetadataEdit(connection)
			}),
		),
		button.New(
			button.TextOpt("Delete"),
			button.OnClick(func() {
				a.confirmDelete(connection)
			}),
		),
	}
	if connection.AuthenticationMode == faire.AuthenticationModeAccessToken {
		actions = append(actions, button.New(
			button.TextOpt("Replace access token"),
			button.OnClick(func() {
				a.beginCredentialReplacement(connection)
			}),
		))
	}

	children := []widget.Widget{
		primitives.Text(connection.Label).FontSize(18).Bold(),
		primitives.Text(connectionDetails(connection)).FontSize(14),
		primitives.HBox(actions...).Gap(12),
	}
	if connection.AuthenticationMode == faire.AuthenticationModeOAuth {
		children = append(children, primitives.Text("OAuth credentials can be reauthorized after the Authorization Code Grant flow is implemented.").FontSize(14))
	}

	return primitives.VBox(children...).Padding(16).Gap(8).Background(widget.RGBA8(255, 255, 255, 255)).Rounded(8)
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

// beginMetadataEdit prepares the metadata editor for connection.
// It preserves the connection's authentication mode and does not access credentials.
func (a *Application) beginMetadataEdit(connection connections.Connection) {
	a.editorConnection = connection
	a.editorLabel.Set(connection.Label)
	a.editorBrandID.Set(string(connection.BrandID))
	a.editorMode = connectionEditorMetadata
	a.selectedTab.Set(1)
	a.refreshRoot()
}

// beginCredentialReplacement prepares the direct-token replacement form for connection.
// It does not access the existing credential, which cannot be shown in the UI.
func (a *Application) beginCredentialReplacement(connection connections.Connection) {
	if connection.AuthenticationMode != faire.AuthenticationModeAccessToken {
		a.managementStatus.Set("Only direct-token connections can replace credentials in this version of the app.")
		return
	}

	a.editorConnection = connection
	a.editorMode = connectionEditorCredentials
	a.selectedTab.Set(1)
	a.refreshRoot()
}

// saveDirectConnection validates and saves a new direct-token connection using the supplied transient token.
// It clears non-secret form fields and refreshes both tabs after a successful save.
func (a *Application) saveDirectConnection(accessToken string) {
	if a.manager == nil {
		a.managementStatus.Set("Saved connections are unavailable. Restart the app after resolving the credential-store issue.")
		return
	}

	label := strings.TrimSpace(a.editorLabel.Get())
	if label == "" {
		a.managementStatus.Set("A connection label is required.")
		return
	}
	if accessToken == "" {
		a.managementStatus.Set("A direct access token is required.")
		return
	}

	connection, err := a.manager.Save(a.context, connections.Connection{
		Label:              label,
		BrandID:            faire.BrandID(strings.TrimSpace(a.editorBrandID.Get())),
		AuthenticationMode: faire.AuthenticationModeAccessToken,
	}, connections.Credentials{AccessToken: accessToken})
	if err != nil {
		a.managementStatus.Set("The direct-token connection could not be saved. Check the credential store and try again.")
		return
	}

	a.editorLabel.Set("")
	a.editorBrandID.Set("")
	a.managementStatus.Set("Saved connection " + connection.Label + ".")
	a.status.Set("Saved " + connection.Label + ". Select it to load its Faire profile.")
	a.refreshConnections()
}

// saveMetadata validates and saves metadata for the connection selected for editing.
// It preserves the saved credential bundle by using Manager.UpdateMetadata.
func (a *Application) saveMetadata() {
	if a.manager == nil {
		a.managementStatus.Set("Saved connections are unavailable. Restart the app after resolving the credential-store issue.")
		return
	}

	label := strings.TrimSpace(a.editorLabel.Get())
	if label == "" {
		a.managementStatus.Set("A connection label is required.")
		return
	}

	connection, err := a.manager.UpdateMetadata(a.context, connections.Connection{
		ID:                 a.editorConnection.ID,
		Label:              label,
		BrandID:            faire.BrandID(strings.TrimSpace(a.editorBrandID.Get())),
		AuthenticationMode: a.editorConnection.AuthenticationMode,
	})
	if err != nil {
		a.managementStatus.Set("Connection metadata could not be saved. Check the application configuration and try again.")
		return
	}

	a.managementStatus.Set("Updated metadata for " + connection.Label + ".")
	a.status.Set("Updated " + connection.Label + ".")
	a.resetEditor()
	a.refreshConnections()
}

// replaceAccessToken saves a new direct token for the connection selected for credential replacement.
// It requires an explicit replacement value and never reads or displays the existing token.
func (a *Application) replaceAccessToken(accessToken string) {
	if a.manager == nil {
		a.managementStatus.Set("Saved connections are unavailable. Restart the app after resolving the credential-store issue.")
		return
	}
	if accessToken == "" {
		a.managementStatus.Set("A new direct access token is required.")
		return
	}

	connection, err := a.manager.Save(a.context, a.editorConnection, connections.Credentials{AccessToken: accessToken})
	if err != nil {
		a.managementStatus.Set("The direct access token could not be replaced. Check the credential store and try again.")
		return
	}

	a.managementStatus.Set("Replaced the access token for " + connection.Label + ".")
	a.status.Set("Updated credentials for " + connection.Label + ".")
	a.resetEditor()
	a.refreshConnections()
}

// confirmDelete shows a modal confirmation before removing connection metadata and its credential-store entry.
// It does nothing when the UI has not been initialized.
func (a *Application) confirmDelete(connection connections.Connection) {
	if a.ui == nil {
		a.managementStatus.Set("The connection cannot be deleted until the desktop UI is ready.")
		return
	}

	confirmation := dialog.Confirm(
		"Delete saved connection?",
		"Delete "+connection.Label+" and remove its saved credentials from the system credential store? This cannot be undone.",
		func() {},
		func() {
			a.deleteConnection(connection)
		},
	)
	confirmation.Show(a.ui.Window().Context())
}

// deleteConnection removes connection metadata and credentials after a user confirms the operation.
// It refreshes both tabs on success so the deleted connection cannot be selected again.
func (a *Application) deleteConnection(connection connections.Connection) {
	if a.manager == nil {
		a.managementStatus.Set("Saved connections are unavailable. Restart the app after resolving the credential-store issue.")
		return
	}

	if err := a.manager.Delete(a.context, connection.ID); err != nil {
		a.managementStatus.Set("The connection could not be deleted. Check the credential store and try again.")
		return
	}

	a.managementStatus.Set("Deleted connection " + connection.Label + ".")
	a.status.Set("Deleted " + connection.Label + ".")
	a.refreshConnections()
}

// refreshConnections reloads saved metadata after a management operation.
// It resets the editor and rebuilds the tabs so selectors and management rows remain current.
func (a *Application) refreshConnections() {
	if a.manager == nil {
		return
	}

	savedConnections, err := a.manager.List(a.context)
	if err != nil {
		a.managementStatus.Set("Saved connection metadata could not be refreshed. Restart the app to reload it.")
		return
	}

	a.connections = savedConnections
	a.selectedTab.Set(1)
	a.refreshRoot()
}

// resetEditor returns the management screen to direct-token connection creation.
// It clears only non-secret form metadata because password fields are local to their forms.
func (a *Application) resetEditor() {
	a.editorMode = connectionEditorCreate
	a.editorConnection = connections.Connection{}
	a.editorLabel.Set("")
	a.editorBrandID.Set("")
}

// refreshRoot replaces the current widget tree after a connection-management state transition.
// It must be called from a UI callback, which keeps the UI mutation on the event thread.
func (a *Application) refreshRoot() {
	if a.ui != nil {
		a.ui.SetRoot(a.root())
	}
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

// connectionDetails formats non-secret connection metadata for the management screen.
// It returns the authentication mode and optional brand ID without exposing any credential fields.
func connectionDetails(connection connections.Connection) string {
	fields := []string{"Authentication: " + string(connection.AuthenticationMode)}
	if connection.BrandID != "" {
		fields = append(fields, "Brand ID: "+string(connection.BrandID))
	}
	return strings.Join(fields, " • ")
}
