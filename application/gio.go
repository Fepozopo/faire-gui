package application

import (
	"context"
	"fmt"
	"image/color"
	"strings"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
)

const (
	brandsTab = iota
	connectionsTab
)

// DesktopUI owns stable Gio widget state and the non-secret state needed to render the desktop application.
// Its methods run on Gio's frame goroutine, except profile-loading work, which returns only safe text through results.
type DesktopUI struct {
	ctx    context.Context
	cancel context.CancelFunc
	window *app.Window
	theme  *material.Theme

	manager     *connections.Manager
	connections []connections.Connection

	selectedTab int
	editorMode  connectionEditorMode
	editing     connections.Connection

	status           string
	managementStatus string

	labelEditor       widget.Editor
	brandIDEditor     widget.Editor
	environmentEditor widget.Editor
	accessTokenEditor widget.Editor

	brandsList      widget.List
	connectionsList widget.List
	tabButtons      [2]widget.Clickable
	saveButton      widget.Clickable
	importButton    widget.Clickable
	cancelButton    widget.Clickable
	confirmDelete   widget.Clickable
	cancelDelete    widget.Clickable
	modalBlocker    widget.Clickable

	rowControls  map[string]*connectionRowControls
	deleteDialog deleteDialogState
	results      chan profileLoadResult
}

// connectionRowControls owns persistent click state for one saved-connection row.
// Gio requires this state to survive each immediate-mode frame so a pointer gesture keeps its identity.
type connectionRowControls struct {
	selectProfile widget.Clickable
	editMetadata  widget.Clickable
	replaceToken  widget.Clickable
	delete        widget.Clickable
}

// deleteDialogState describes the metadata-only saved connection whose deletion awaits confirmation.
type deleteDialogState struct {
	open       bool
	connection connections.Connection
}

// profileLoadResult transports a credential-safe asynchronous profile-loading result to the UI frame loop.
type profileLoadResult struct {
	status string
}

// Run starts the Faire Gio desktop application.
// It creates the window before app.Main takes control of the process main goroutine, as required by Gio on macOS.
func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	manager, savedConnections, startupStatus := loadSavedConnections(ctx)
	window := new(app.Window)
	window.Option(app.Title(windowTitle), app.Size(unit.Dp(windowWidth), unit.Dp(windowHeight)))
	ui := newDesktopUI(ctx, cancel, window, manager, savedConnections, startupStatus)

	go func() {
		// Gio requires app.Main on the process main goroutine, so the event loop stays in a worker goroutine.
		_ = ui.runWindow()
	}()
	app.Main()
	cancel()
}

// newDesktopUI constructs a DesktopUI from loaded non-secret metadata and configures its persistent Gio controls.
// The returned UI keeps entered token text only in its masked editor until an action immediately clears it.
func newDesktopUI(ctx context.Context, cancel context.CancelFunc, window *app.Window, manager *connections.Manager, savedConnections []connections.Connection, startupStatus string) *DesktopUI {
	ui := &DesktopUI{
		ctx:              ctx,
		cancel:           cancel,
		window:           window,
		theme:            material.NewTheme(),
		manager:          manager,
		connections:      savedConnections,
		status:           startupStatus,
		managementStatus: "Create a direct-token connection, or select an existing connection to manage it.",
		rowControls:      make(map[string]*connectionRowControls),
		results:          make(chan profileLoadResult, 1),
	}
	ui.configureEditors()
	ui.brandsList.Axis = layout.Vertical
	ui.connectionsList.Axis = layout.Vertical
	return ui
}

// configureEditors applies persistent field behavior once, rather than recreating editor state every frame.
// The masked token editor is the only UI state that can contain a direct access token.
func (ui *DesktopUI) configureEditors() {
	ui.labelEditor.SingleLine = true
	ui.brandIDEditor.SingleLine = true
	ui.environmentEditor.SingleLine = true
	ui.accessTokenEditor.SingleLine = true
	ui.accessTokenEditor.Mask = '•'
}

// runWindow handles Gio window events, drains safe background results, and submits complete frames.
// It cancels in-flight profile work when Gio reports that the desktop window has been destroyed.
func (ui *DesktopUI) runWindow() error {
	var ops op.Ops
	for {
		switch event := ui.window.Event().(type) {
		case app.DestroyEvent:
			ui.cancel()
			return event.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, event)
			ui.drainResults()
			ui.Layout(gtx)
			event.Frame(gtx.Ops)
		}
	}
}

// Layout processes current-frame interaction and emits the complete desktop UI.
// In Gio, this function is called for every requested frame; persistent fields on DesktopUI preserve interaction state.
func (ui *DesktopUI) Layout(gtx layout.Context) layout.Dimensions {
	ui.handleTabClicks(gtx)
	return layout.Stack{Alignment: layout.NW}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return fill(gtx, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(20), Right: unit.Dp(24), Bottom: unit.Dp(24), Left: unit.Dp(24)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(ui.layoutTabs),
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if ui.selectedTab == connectionsTab {
							return ui.layoutConnections(gtx)
						}
						return ui.layoutBrands(gtx)
					}),
				)
			})
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.deleteDialog.open {
				return layout.Dimensions{}
			}
			return ui.layoutDeleteModal(gtx)
		}),
	)
}

// handleTabClicks selects a tab from persistent clickable state before laying out the active content.
// Processing clicks before rendering ensures each click affects the same frame that consumes it.
func (ui *DesktopUI) handleTabClicks(gtx layout.Context) {
	if ui.deleteDialog.open {
		return
	}
	for index := range ui.tabButtons {
		if ui.tabButtons[index].Clicked(gtx) {
			ui.selectedTab = index
			ui.window.Invalidate()
		}
	}
}

// drainResults transfers completed safe profile statuses from background work into UI-owned state.
// It never blocks a frame, and no result contains credentials, response bodies, or Gio widget state.
func (ui *DesktopUI) drainResults() {
	for {
		select {
		case result := <-ui.results:
			ui.status = result.status
		default:
			return
		}
	}
}

// selectConnection begins loading one saved connection's profile without blocking the Gio frame loop.
// It publishes only a sanitized message back to the UI and requests a frame after work completes.
func (ui *DesktopUI) selectConnection(connectionID string) {
	if ui.manager == nil {
		ui.status = "Saved connections are unavailable. Restart the app after resolving the credential-store issue."
		ui.window.Invalidate()
		return
	}

	ui.status = "Loading selected Faire brand profile…"
	ui.window.Invalidate()
	go ui.loadProfile(connectionID)
}

// loadProfile creates an isolated client for connectionID and loads its profile in the background.
// It sends a credential-safe result without accessing or mutating UI widget state from the goroutine.
func (ui *DesktopUI) loadProfile(connectionID string) {
	client, connection, err := ui.manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		ui.publishProfileResult(profileLoadErrorMessage(err))
		return
	}

	profile, err := client.Brands.Profile(ui.ctx)
	if err != nil {
		ui.publishProfileResult(profileLoadErrorMessage(err))
		return
	}
	ui.publishProfileResult(profileSummary(connection, profile))
}

// publishProfileResult sends a safe result unless the window has already closed.
// The buffered channel avoids holding the profile goroutine until another frame arrives.
func (ui *DesktopUI) publishProfileResult(status string) {
	select {
	case ui.results <- profileLoadResult{status: status}:
	case <-ui.ctx.Done():
		return
	}
	ui.window.Invalidate()
}

// rowControlsFor returns stable controls for connectionID and creates them only for a newly visible row.
// Stable clickables prevent a list redraw from changing the identity of a user interaction.
func (ui *DesktopUI) rowControlsFor(connectionID string) *connectionRowControls {
	controls, ok := ui.rowControls[connectionID]
	if !ok {
		controls = new(connectionRowControls)
		ui.rowControls[connectionID] = controls
	}
	return controls
}

// beginMetadataEdit prepares the non-secret metadata form for connection.
// It preserves authentication mode and never reads credentials from the credential store.
func (ui *DesktopUI) beginMetadataEdit(connection connections.Connection) {
	ui.editing = connection
	ui.labelEditor.SetText(connection.Label)
	ui.brandIDEditor.SetText(string(connection.BrandID))
	ui.accessTokenEditor.SetText("")
	ui.editorMode = connectionEditorMetadata
	ui.selectedTab = connectionsTab
	ui.window.Invalidate()
}

// beginCredentialReplacement prepares a blank masked field for a direct-token replacement.
// It rejects OAuth rows because the current UI does not implement OAuth reauthorization.
func (ui *DesktopUI) beginCredentialReplacement(connection connections.Connection) {
	if connection.AuthenticationMode != faire.AuthenticationModeAccessToken {
		ui.managementStatus = "Only direct-token connections can replace credentials in this version of the app."
		ui.window.Invalidate()
		return
	}
	ui.editing = connection
	ui.accessTokenEditor.SetText("")
	ui.editorMode = connectionEditorCredentials
	ui.selectedTab = connectionsTab
	ui.window.Invalidate()
}

// beginEnvironmentImport prepares a form that reads exactly one explicitly named environment variable after confirmation.
// It clears previous form values so an imported token cannot be combined with stale connection metadata.
func (ui *DesktopUI) beginEnvironmentImport() {
	ui.resetEditor()
	ui.editorMode = connectionEditorEnvironmentImport
	ui.selectedTab = connectionsTab
	ui.window.Invalidate()
}

// saveDirectConnection reads the transient token, clears its editor before credential-store I/O, and saves a new connection.
// It returns no value; successes and failures are represented by credential-safe UI status text.
func (ui *DesktopUI) saveDirectConnection(successVerb string) {
	accessToken := ui.accessTokenEditor.Text()
	// Clear the visible buffer before validation or I/O so credentials cannot survive a redraw or an error path.
	ui.accessTokenEditor.SetText("")
	if ui.manager == nil {
		ui.managementStatus = "Saved connections are unavailable. Restart the app after resolving the credential-store issue."
		ui.window.Invalidate()
		return
	}

	label := strings.TrimSpace(ui.labelEditor.Text())
	if label == "" {
		ui.managementStatus = "A connection label is required."
		ui.window.Invalidate()
		return
	}
	if accessToken == "" {
		ui.managementStatus = "A direct access token is required."
		ui.window.Invalidate()
		return
	}

	connection, err := ui.manager.Save(ui.ctx, connections.Connection{
		Label:              label,
		BrandID:            faire.BrandID(strings.TrimSpace(ui.brandIDEditor.Text())),
		AuthenticationMode: faire.AuthenticationModeAccessToken,
	}, connections.Credentials{AccessToken: accessToken})
	if err != nil {
		ui.managementStatus = "The direct-token connection could not be saved. Check the credential store and try again."
		ui.window.Invalidate()
		return
	}

	ui.managementStatus = successVerb + " connection " + connection.Label + "."
	ui.status = successVerb + " " + connection.Label + ". Select it to load its Faire profile."
	ui.resetEditor()
	ui.refreshConnections()
}

// importEnvironmentConnection reads a token from the single name entered by the user and immediately clears that field.
// The imported token is copied only into the masked editor long enough for saveDirectConnection to clear and store it.
func (ui *DesktopUI) importEnvironmentConnection() {
	accessToken, err := explicitEnvironmentToken(ui.environmentEditor.Text())
	ui.environmentEditor.SetText("")
	if err != nil {
		ui.managementStatus = "The named environment variable is missing or empty. Enter one exported direct-token variable and try again."
		ui.window.Invalidate()
		return
	}
	ui.accessTokenEditor.SetText(accessToken)
	ui.saveDirectConnection("Imported")
}

// saveMetadata validates and saves non-secret metadata for the selected connection.
// It uses Manager.UpdateMetadata, which preserves the existing credential bundle and authentication mode.
func (ui *DesktopUI) saveMetadata() {
	if ui.manager == nil {
		ui.managementStatus = "Saved connections are unavailable. Restart the app after resolving the credential-store issue."
		ui.window.Invalidate()
		return
	}
	label := strings.TrimSpace(ui.labelEditor.Text())
	if label == "" {
		ui.managementStatus = "A connection label is required."
		ui.window.Invalidate()
		return
	}

	connection, err := ui.manager.UpdateMetadata(ui.ctx, connections.Connection{
		ID:                 ui.editing.ID,
		Label:              label,
		BrandID:            faire.BrandID(strings.TrimSpace(ui.brandIDEditor.Text())),
		AuthenticationMode: ui.editing.AuthenticationMode,
	})
	if err != nil {
		ui.managementStatus = "Connection metadata could not be saved. Check the application configuration and try again."
		ui.window.Invalidate()
		return
	}

	ui.managementStatus = "Updated metadata for " + connection.Label + "."
	ui.status = "Updated " + connection.Label + "."
	ui.resetEditor()
	ui.refreshConnections()
}

// replaceAccessToken writes a new transient direct token for the selected connection and never reads the prior token.
// The token editor is cleared before manager I/O so error paths do not retain a credential in the UI.
func (ui *DesktopUI) replaceAccessToken() {
	accessToken := ui.accessTokenEditor.Text()
	ui.accessTokenEditor.SetText("")
	if ui.manager == nil {
		ui.managementStatus = "Saved connections are unavailable. Restart the app after resolving the credential-store issue."
		ui.window.Invalidate()
		return
	}
	if accessToken == "" {
		ui.managementStatus = "A new direct access token is required."
		ui.window.Invalidate()
		return
	}

	connection, err := ui.manager.Save(ui.ctx, ui.editing, connections.Credentials{AccessToken: accessToken})
	if err != nil {
		ui.managementStatus = "The direct access token could not be replaced. Check the credential store and try again."
		ui.window.Invalidate()
		return
	}

	ui.managementStatus = "Replaced the access token for " + connection.Label + "."
	ui.status = "Updated credentials for " + connection.Label + "."
	ui.resetEditor()
	ui.refreshConnections()
}

// requestDelete opens the confirmation dialog for connection without performing deletion.
// The dialog state intentionally holds metadata only, never credential material.
func (ui *DesktopUI) requestDelete(connection connections.Connection) {
	ui.deleteDialog = deleteDialogState{open: true, connection: connection}
	ui.window.Invalidate()
}

// deleteConnection removes metadata and credentials only after the modal confirm control triggers it.
// It refreshes list data on success so the deleted connection can no longer be selected.
func (ui *DesktopUI) deleteConnection() {
	connection := ui.deleteDialog.connection
	ui.deleteDialog = deleteDialogState{}
	if ui.manager == nil {
		ui.managementStatus = "Saved connections are unavailable. Restart the app after resolving the credential-store issue."
		ui.window.Invalidate()
		return
	}
	if err := ui.manager.Delete(ui.ctx, connection.ID); err != nil {
		ui.managementStatus = "The connection could not be deleted. Check the credential store and try again."
		ui.window.Invalidate()
		return
	}
	ui.managementStatus = "Deleted connection " + connection.Label + "."
	ui.status = "Deleted " + connection.Label + "."
	ui.refreshConnections()
}

// refreshConnections reloads metadata and discards stale per-row click state after a successful operation.
// It retains stable controls for existing IDs so Gio pointer state is preserved across immediate-mode frames.
func (ui *DesktopUI) refreshConnections() {
	if ui.manager == nil {
		return
	}
	savedConnections, err := ui.manager.List(ui.ctx)
	if err != nil {
		ui.managementStatus = "Saved connection metadata could not be refreshed. Restart the app to reload it."
		ui.window.Invalidate()
		return
	}
	ui.connections = savedConnections
	ui.reconcileRowControls()
	ui.selectedTab = connectionsTab
	ui.window.Invalidate()
}

// reconcileRowControls removes click state for deleted connection IDs.
// This prevents an unbounded map while preserving state for all rows that remain visible after a refresh.
func (ui *DesktopUI) reconcileRowControls() {
	active := make(map[string]struct{}, len(ui.connections))
	for _, connection := range ui.connections {
		active[connection.ID] = struct{}{}
	}
	for connectionID := range ui.rowControls {
		if _, ok := active[connectionID]; !ok {
			delete(ui.rowControls, connectionID)
		}
	}
}

// cancelEditor returns to direct-token creation and clears all transient form buffers.
// It does not access the credential store because cancellation must be local and immediate.
func (ui *DesktopUI) cancelEditor() {
	ui.resetEditor()
	ui.window.Invalidate()
}

// resetEditor clears form state and returns to direct-token creation.
// Clearing the masked token buffer here protects every mode transition, including cancel and successful save paths.
func (ui *DesktopUI) resetEditor() {
	ui.editorMode = connectionEditorCreate
	ui.editing = connections.Connection{}
	ui.labelEditor.SetText("")
	ui.brandIDEditor.SetText("")
	ui.environmentEditor.SetText("")
	ui.accessTokenEditor.SetText("")
}

// editorHeading returns the title and explanation for the active connection-management form.
// The text documents why credentials are absent from metadata-only editing and replacement screens.
func (ui *DesktopUI) editorHeading() (string, string) {
	switch ui.editorMode {
	case connectionEditorMetadata:
		return "Edit connection metadata", "Authentication mode and saved credentials cannot be changed here."
	case connectionEditorCredentials:
		return "Replace direct access token", "The existing token is not displayed and cannot be recovered from this screen."
	case connectionEditorEnvironmentImport:
		return "Import direct-token connection", "Enter one environment-variable name to import. The app never scans environment variables automatically."
	default:
		return "Add direct-token connection", "OAuth connection creation will be added with the Authorization Code Grant flow."
	}
}

// fmtDeleteMessage creates the non-secret text displayed by the deletion confirmation modal.
// It includes only the saved label, never credentials or connection-store errors.
func fmtDeleteMessage(connection connections.Connection) string {
	return fmt.Sprintf("Delete %q and remove its saved credentials from the system credential store? This cannot be undone.", connection.Label)
}
