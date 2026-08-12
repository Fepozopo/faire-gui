package application

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
)

var (
	cardBackground   = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	formBackground   = color.NRGBA{R: 238, G: 245, B: 255, A: 255}
	mutedTextColor   = color.NRGBA{R: 80, G: 80, B: 80, A: 255}
	dangerColor      = color.NRGBA{R: 176, G: 39, B: 39, A: 255}
	modalScrimColor  = color.NRGBA{R: 0, G: 0, B: 0, A: 110}
	selectedTabColor = color.NRGBA{R: 63, G: 81, B: 181, A: 255}
)

// layoutTabs draws a conventional top tab strip for Brands and Connections.
// The Clickables remain on DesktopUI so Gio preserves their interaction state across full redraws.
func (ui *DesktopUI) layoutTabs(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.H2(ui.theme, "Faire").Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{Alignment: layout.S}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					// The divider establishes a shared visual baseline beneath both tabs.
					return bottomRule(gtx, color.NRGBA{R: 210, G: 210, B: 210, A: 255})
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.layoutTabButton(gtx, brandsTab, "Brands")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(24)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.layoutTabButton(gtx, connectionsTab, "Connections")
						}),
					)
				}),
			)
		}),
	)
}

// layoutTabButton renders a text tab with an active underline rather than a raised button.
// It keeps the original persistent Clickable so tab selection continues to work with Gio's event model.
func (ui *DesktopUI) layoutTabButton(gtx layout.Context, index int, label string) layout.Dimensions {
	return ui.tabButtons[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.S}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(4), Bottom: unit.Dp(12), Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					style := material.Label(ui.theme, unit.Sp(16), label)
					if index == ui.selectedTab {
						style.Color = selectedTabColor
					} else {
						style.Color = mutedTextColor
					}
					return style.Layout(gtx)
				})
			}),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if index == ui.selectedTab {
					return bottomRule(gtx, selectedTabColor)
				}
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
		)
	})
}

// layoutBrands renders a vertically scrollable selector of saved connections and the current safe profile status.
// Each row uses stable controls keyed by connection ID so scrolling does not change pointer interaction identity.
func (ui *DesktopUI) layoutBrands(gtx layout.Context) layout.Dimensions {
	itemCount := len(ui.connections) + 1
	if len(ui.connections) == 0 {
		itemCount++
	}
	return ui.brandsList.Layout(gtx, itemCount, func(gtx layout.Context, index int) layout.Dimensions {
		if index == 0 {
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.H3(ui.theme, "Choose a saved brand connection").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					layout.Rigid(bodyText(ui.theme, "Select a connection to verify its read-only Faire brand profile.", mutedTextColor)),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(statusText(ui.theme, ui.status)),
				)
			})
		}
		if index == 1 && len(ui.connections) == 0 {
			return bodyText(ui.theme, "No connections have been saved yet. Use the Connections tab to add one.", mutedTextColor)(gtx)
		}
		connectionIndex := index - 1
		if len(ui.connections) == 0 {
			connectionIndex--
		}
		connection := ui.connections[connectionIndex]
		controls := ui.rowControlsFor(connection.ID)
		if controls.selectProfile.Clicked(gtx) {
			ui.selectConnection(connection.ID)
		}
		return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return card(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(material.H5(ui.theme, connection.Label).Layout),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(bodyText(ui.theme, connectionDetails(connection), mutedTextColor)),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						layout.Rigid(material.Button(ui.theme, &controls.selectProfile, "Load brand profile").Layout),
					)
				})
			})
		})
	})
}

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
				layout.Rigid(material.Button(ui.theme, &ui.saveButton, "Save direct-token connection").Layout),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(material.Button(ui.theme, &ui.importButton, "Import from environment variable").Layout),
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

// saveCancelButtons renders the persistent primary action alongside a control that discards transient form state.
func (ui *DesktopUI) saveCancelButtons(primaryLabel string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(material.Button(ui.theme, &ui.saveButton, primaryLabel).Layout),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Rigid(material.Button(ui.theme, &ui.cancelButton, "Cancel").Layout),
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
						layout.Rigid(material.Button(ui.theme, &controls.editMetadata, "Edit metadata").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
						layout.Rigid(ui.deleteButton(controls)),
						layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if connection.AuthenticationMode != faire.AuthenticationModeAccessToken {
								return layout.Dimensions{}
							}
							return material.Button(ui.theme, &controls.replaceToken, "Replace access token").Layout(gtx)
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

// deleteButton renders the destructive action with an explicit red background to differentiate it from metadata edits.
func (ui *DesktopUI) deleteButton(controls *connectionRowControls) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		button := material.Button(ui.theme, &controls.delete, "Delete")
		button.Background = dangerColor
		return button.Layout(gtx)
	}
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
							confirm := material.Button(ui.theme, &ui.confirmDelete, "Delete")
							confirm.Background = dangerColor
							return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
								layout.Rigid(material.Button(ui.theme, &ui.cancelDelete, "Cancel").Layout),
								layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
								layout.Rigid(confirm.Layout),
							)
						}),
					)
				})
			})
		}),
	)
	return fullSize
}

// inputField draws an editor on a white surface with enough padding for a practical touch and mouse target.
// The editor must be a persistent DesktopUI field because Gio stores its cursor, selection, and typed text in widget.Editor.
func inputField(gtx layout.Context, theme *material.Theme, editor *widget.Editor, hint string) layout.Dimensions {
	return roundedPanel(gtx, cardBackground, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, material.Editor(theme, editor, hint).Layout)
	})
}

// fieldSpacer returns the consistent vertical separation between form controls.
func fieldSpacer() layout.FlexChild {
	return layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout)
}

// bodyText creates a wrapped material body label with an explicit color.
// Status and explanatory labels share this helper so only safe plain text reaches the renderer.
func bodyText(theme *material.Theme, text string, textColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		style := material.Body1(theme, text)
		style.Color = textColor
		return style.Layout(gtx)
	}
}

// statusText renders a status with its label only when there is status content to show.
func statusText(theme *material.Theme, status string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if status == "" {
			return layout.Dimensions{}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(material.H6(theme, "Status").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
			layout.Rigid(bodyText(theme, status, mutedTextColor)),
		)
	}
}

// card draws the standard white rounded surface for saved-connection rows.
func card(gtx layout.Context, child layout.Widget) layout.Dimensions {
	return roundedPanel(gtx, cardBackground, child)
}

// roundedPanel paints a rounded background behind child without relying on absolute screen coordinates.
// Gio calculates the panel dimensions from its child, which lets the interface reflow when the window resizes.
func roundedPanel(gtx layout.Context, background color.NRGBA, child layout.Widget) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			radius := gtx.Dp(unit.Dp(8))
			paint.FillShape(gtx.Ops, background, clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, radius).Op(gtx.Ops))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		child,
	)
}

// fill paints the full available layout area with a solid color.
func fill(gtx layout.Context, background color.NRGBA) layout.Dimensions {
	paint.FillShape(gtx.Ops, background, clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Op())
	return layout.Dimensions{Size: gtx.Constraints.Min}
}

// bottomRule paints a two-device-independent-pixel line along the bottom of its available area.
// The tab strip uses it once for the shared divider and once for the selected-tab indicator.
func bottomRule(gtx layout.Context, lineColor color.NRGBA) layout.Dimensions {
	lineHeight := gtx.Dp(unit.Dp(2))
	lineHeight = min(lineHeight, gtx.Constraints.Min.Y)
	offset := op.Offset(image.Pt(0, gtx.Constraints.Min.Y-lineHeight)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, lineColor, clip.Rect(image.Rect(0, 0, gtx.Constraints.Min.X, lineHeight)).Op())
	offset.Pop()
	return layout.Dimensions{Size: gtx.Constraints.Min}
}
