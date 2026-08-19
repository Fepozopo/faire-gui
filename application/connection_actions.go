package application

import (
	"context"
	"fmt"
	"strings"

	"gioui.org/layout"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
)

// drainResults transfers completed safe profile statuses into UI-owned shell state.
// Connection-scoped Orders data actions are drained separately so their cross-feature dependency remains explicit.
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

// drainOrdersDataActionEvents applies safe connection-scoped cache-action feedback on the frame goroutine.
// Completion clears the connection-switch guard only for its matching action and never replaces another connection's table.
func (ui *DesktopUI) drainOrdersDataActionEvents() {
	for {
		select {
		case event := <-ui.orders.dataActionResults:
			ui.status = event.Status
			if event.Done && event.ConnectionID == ui.orders.dataActionConnectionID {
				ui.orders.dataActionConnectionID = ""
				if event.ConnectionID == ui.activeConnectionID {
					ui.orders.view.state.Loading = false
					ui.startOrdersLoad(ordersLoadLocalOnly)
				}
			}
		default:
			return
		}
	}
}

// selectConnection begins loading one saved connection's profile without blocking the Gio frame loop.
// It returns the Brands list to its status area, publishes only a sanitized message, and requests a frame after work completes.
func (ui *DesktopUI) selectConnection(connectionID string) {
	// Profile feedback is displayed at the top of the list, so reset a deep row position before changing the status.
	ui.brandsList.Position = layout.Position{}
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

// publishProfileResult sends a safe profile status unless the window has already closed.
// The buffered channel avoids holding the profile goroutine until another frame arrives.
func (ui *DesktopUI) publishProfileResult(status string) {
	ui.publishProfileLoadResult(profileLoadResult{status: status})
}

// publishProfileLoadResult publishes a credential-safe Brand Profile result unless application shutdown has begun.
func (ui *DesktopUI) publishProfileLoadResult(result profileLoadResult) {
	select {
	case ui.results <- result:
	case <-ui.ctx.Done():
		return
	}
	ui.invalidate()
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

// beginMetadataEdit prepares the non-secret metadata form for connection and scrolls the management list to its form.
// It preserves authentication mode, never reads credentials from the credential store, and makes the edit feedback visible immediately.
func (ui *DesktopUI) beginMetadataEdit(connection connections.Connection) {
	ui.editing = connection
	ui.labelEditor.SetText(connection.Label)
	ui.brandIDEditor.SetText(string(connection.BrandID))
	ui.accessTokenEditor.SetText("")
	ui.editorMode = connectionEditorMetadata
	ui.selectedTab = connectionsTab
	// The editor is at the top of the list, so discard the previous row scroll position before Gio draws the edit form.
	ui.connectionsList.Position = layout.Position{}
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

// deleteConnection removes metadata, credentials, and that connection's private local Orders cache after modal confirmation.
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
	if ui.orders.store != nil {
		go ui.deleteConnectionCache(connection.ID, connection.Label, ui.orders.store)
	}
	ui.managementStatus = "Deleted connection " + connection.Label + "."
	ui.status = "Deleted " + connection.Label + "."
	if ui.activeConnectionID == connection.ID {
		// A deleted connection cannot remain active because later requests must never resolve a removed credential entry.
		ui.activeConnectionID = ""
		ui.activeConnectionLabel = ""
		ui.resetOrdersState()
		ui.orders.view.searchActive = false
		ui.orders.view.search.SetText("")
	}
	ui.refreshConnections()
}

// connectionCleanupResult carries a credential-safe result from background connection-cache cleanup.
type connectionCleanupResult struct {
	label  string
	status string
}

// deleteConnectionCache removes only a deleted connection's local Orders data outside the Gio frame loop.
func (ui *DesktopUI) deleteConnectionCache(connectionID, label string, store interface {
	DeleteConnectionData(context.Context, string) error
}) {
	if err := store.DeleteConnectionData(ui.ctx, connectionID); err != nil {
		select {
		case ui.connectionCleanupResults <- connectionCleanupResult{label: label, status: "Deleted connection " + label + ", but its local order data could not be removed."}:
			ui.invalidate()
		case <-ui.ctx.Done():
		}
	}
}

// drainConnectionCleanupResults applies safe background cache-cleanup failure statuses on the Gio frame loop.
func (ui *DesktopUI) drainConnectionCleanupResults() {
	for {
		select {
		case result := <-ui.connectionCleanupResults:
			ui.managementStatus = result.status
			ui.status = "Connection deleted; local order data removal needs attention."
		default:
			return
		}
	}
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
