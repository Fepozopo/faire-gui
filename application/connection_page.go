package application

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
)

// layoutConnections renders the management form and all metadata-only saved-connection rows in one scrollable list.
// It keeps form controls and row controls persistent so keyboard focus and pointer state remain stable between frames.
func (ui *DesktopUI) layoutConnections(gtx layout.Context) layout.Dimensions {
	rowOffset := 2
	return ui.connectionsList.Layout(gtx, len(ui.connections)+rowOffset, func(gtx layout.Context, index int) layout.Dimensions {
		switch index {
		case 0:
			return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.H3(ui.theme, "Saved connections").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					layout.Rigid(bodyText(ui.theme, "Credentials are stored only in the operating-system credential store.", mutedTextColor)),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					layout.Rigid(statusText(ui.theme, ui.managementStatus)),
				)
			})
		case 1:
			return layout.Inset{Bottom: unit.Dp(18)}.Layout(gtx, ui.layoutConnectionEditor)
		default:
			connection := ui.connections[index-rowOffset]
			return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.layoutConnectionRow(gtx, connection)
			})
		}
	})
}

// layoutConnectionEditor renders the appropriate persistent editor fields and action controls for the selected mode.
// It processes a button click before drawing so a completed action updates the form in the same Gio frame.
func (ui *DesktopUI) layoutConnectionEditor(gtx layout.Context) layout.Dimensions {
	headline, explanation := ui.editorHeading()
	if ui.saveButton.Clicked(gtx) {
		switch ui.editorMode {
		case connectionEditorMetadata:
			ui.saveMetadata()
		case connectionEditorCredentials:
			ui.replaceAccessToken()
		case connectionEditorEnvironmentImport:
			ui.importEnvironmentConnection()
		default:
			ui.saveDirectConnection("Saved")
		}
	}
	if ui.importButton.Clicked(gtx) {
		ui.beginEnvironmentImport()
	}
	if ui.cancelButton.Clicked(gtx) {
		ui.cancelEditor()
	}

	return roundedPanel(gtx, formBackground, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(material.H5(ui.theme, headline).Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
				layout.Rigid(bodyText(ui.theme, explanation, mutedTextColor)),
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
			}

			switch ui.editorMode {
			case connectionEditorMetadata:
				children = append(children, ui.metadataEditorFields()...)
			case connectionEditorCredentials:
				children = append(children, ui.credentialEditorFields()...)
			case connectionEditorEnvironmentImport:
				children = append(children, ui.environmentImportEditorFields()...)
			default:
				children = append(children, ui.createEditorFields()...)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// createEditorFields returns the direct-token creation fields and their action buttons.
// The access-token editor is masked and is cleared by saveDirectConnection before any validation or storage call.
func (ui *DesktopUI) createEditorFields() []layout.FlexChild {
	return []layout.FlexChild{
		fieldSpacer(),
		layout.Rigid(ui.labelField),
		fieldSpacer(),
		layout.Rigid(ui.brandIDField),
		fieldSpacer(),
		layout.Rigid(ui.accessTokenField),
		fieldSpacer(),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(primaryButton(ui.theme, &ui.saveButton, "Save direct-token connection")),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(primaryButton(ui.theme, &ui.importButton, "Import from environment variable")),
			)
		}),
	}
}

// metadataEditorFields returns only metadata fields, keeping credential state inaccessible during metadata editing.
func (ui *DesktopUI) metadataEditorFields() []layout.FlexChild {
	return []layout.FlexChild{
		fieldSpacer(),
		layout.Rigid(ui.labelField),
		fieldSpacer(),
		layout.Rigid(ui.brandIDField),
		fieldSpacer(),
		layout.Rigid(ui.saveCancelButtons("Save metadata")),
	}
}

// credentialEditorFields returns a blank, masked replacement-token field and cancellation controls.
// It intentionally excludes label and brand metadata to make the secret-replacement intent unambiguous.
func (ui *DesktopUI) credentialEditorFields() []layout.FlexChild {
	return []layout.FlexChild{
		fieldSpacer(),
		layout.Rigid(ui.accessTokenField),
		fieldSpacer(),
		layout.Rigid(ui.saveCancelButtons("Replace access token")),
	}
}

// environmentImportEditorFields returns fields for importing exactly one user-named token variable.
// It does not list or inspect the rest of the process environment.
func (ui *DesktopUI) environmentImportEditorFields() []layout.FlexChild {
	return []layout.FlexChild{
		fieldSpacer(),
		layout.Rigid(ui.labelField),
		fieldSpacer(),
		layout.Rigid(ui.brandIDField),
		fieldSpacer(),
		layout.Rigid(ui.environmentField),
		fieldSpacer(),
		layout.Rigid(ui.saveCancelButtons("Import direct-token connection")),
	}
}

// saveCancelButtons renders persistent dark filled controls for saving or discarding transient form state.
func (ui *DesktopUI) saveCancelButtons(primaryLabel string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(primaryButton(ui.theme, &ui.saveButton, primaryLabel)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.cancelButton, "Cancel")),
		)
	}
}

// labelField renders the persistent label editor inside a visible input panel.
func (ui *DesktopUI) labelField(gtx layout.Context) layout.Dimensions {
	return inputField(gtx, ui.theme, &ui.labelEditor, "Connection label")
}

// brandIDField renders the optional non-secret Faire brand ID editor.
func (ui *DesktopUI) brandIDField(gtx layout.Context) layout.Dimensions {
	return inputField(gtx, ui.theme, &ui.brandIDEditor, "Faire brand ID (optional)")
}

// environmentField renders the explicit environment-variable name rather than inspecting the full environment.
func (ui *DesktopUI) environmentField(gtx layout.Context) layout.Dimensions {
	return inputField(gtx, ui.theme, &ui.environmentEditor, "Environment variable name, for example API_TOKEN_21C")
}

// accessTokenField renders the sole token-holding editor, which has been configured with a bullet mask.
func (ui *DesktopUI) accessTokenField(gtx layout.Context) layout.Dimensions {
	hint := "Faire access token"
	if ui.editorMode == connectionEditorCredentials {
		hint = "New Faire access token"
	}
	return inputField(gtx, ui.theme, &ui.accessTokenEditor, hint)
}

// layoutConnectionRow renders non-secret metadata and stable actions for one connection.
// OAuth rows omit the direct-token replacement control because their credentials must be reauthorized through a future flow.
func (ui *DesktopUI) layoutConnectionRow(gtx layout.Context, connection connections.Connection) layout.Dimensions {
	controls := ui.rowControlsFor(connection.ID)
	if controls.editMetadata.Clicked(gtx) {
		ui.beginMetadataEdit(connection)
	}
	if controls.delete.Clicked(gtx) {
		ui.requestDelete(connection)
	}
	if connection.AuthenticationMode == faire.AuthenticationModeAccessToken && controls.replaceToken.Clicked(gtx) {
		ui.beginCredentialReplacement(connection)
	}

	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(material.H5(ui.theme, connection.Label).Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(bodyText(ui.theme, connectionDetails(connection), mutedTextColor)),
				layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(primaryButton(ui.theme, &controls.editMetadata, "Edit metadata")),
						layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
						layout.Rigid(ui.deleteButton(controls)),
						layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if connection.AuthenticationMode != faire.AuthenticationModeAccessToken {
								return layout.Dimensions{}
							}
							return primaryButton(ui.theme, &controls.replaceToken, "Replace access token")(gtx)
						}),
					)
				}),
			}
			if connection.AuthenticationMode == faire.AuthenticationModeOAuth {
				children = append(children,
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(bodyText(ui.theme, "OAuth credentials can be reauthorized after the Authorization Code Grant flow is implemented.", mutedTextColor)),
				)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// deleteButton renders the destructive red action while preserving the shared filled-button dimensions.
func (ui *DesktopUI) deleteButton(controls *connectionRowControls) layout.Widget {
	return dangerButton(ui.theme, &controls.delete, "Delete")
}

// layoutDeleteModal draws a full-window scrim before the dialog card so pointer input cannot reach controls behind it.
// Confirm and cancel are persistent clickables and are the only paths that close or execute the pending deletion.
func (ui *DesktopUI) layoutDeleteModal(gtx layout.Context) layout.Dimensions {
	if ui.cancelDelete.Clicked(gtx) {
		ui.deleteDialog = deleteDialogState{}
		ui.window.Invalidate()
	}
	if ui.confirmDelete.Clicked(gtx) {
		ui.deleteConnection()
	}
	// Register the scrim after the normal UI but before the dialog. Gio routes input in operation order,
	// so this blocks background controls while leaving the later dialog controls interactive.
	fullSize := ui.modalBlocker.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, modalScrimColor)
	})
	layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(500)))
			gtx.Constraints.Min.X = 0
			return roundedPanel(gtx, cardBackground, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(24), Left: unit.Dp(24)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(material.H4(ui.theme, "Delete saved connection?").Layout),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						layout.Rigid(bodyText(ui.theme, fmtDeleteMessage(ui.deleteDialog.connection), mutedTextColor)),
						layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
								layout.Rigid(primaryButton(ui.theme, &ui.cancelDelete, "Cancel")),
								layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
								layout.Rigid(dangerButton(ui.theme, &ui.confirmDelete, "Delete")),
							)
						}),
					)
				})
			})
		}),
	)
	return fullSize
}
