package application

import (
	"image"
	"image/color"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var (
	cardBackground     = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	selectionBarColor  = color.NRGBA{R: 226, G: 231, B: 240, A: 255}
	formBackground     = selectionBarColor
	mutedTextColor     = color.NRGBA{R: 80, G: 80, B: 80, A: 255}
	dangerColor        = color.NRGBA{R: 176, G: 39, B: 39, A: 255}
	modalScrimColor    = color.NRGBA{R: 0, G: 0, B: 0, A: 110}
	panelBorderColor   = color.NRGBA{R: 221, G: 221, B: 221, A: 255}
	activityColor      = color.NRGBA{R: 235, G: 187, B: 58, A: 255}
	primaryButtonColor = color.NRGBA{R: 48, G: 48, B: 48, A: 255}
	primaryButtonText  = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

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
// Its background fills the child's measured dimensions so responsive content keeps a consistent surface.
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

// outlinedPanel renders child on a rounded surface enclosed by a one-device-independent-pixel border.
// The border and fill colors are supplied by the caller, while the returned dimensions include the border.
func outlinedPanel(gtx layout.Context, background, border color.NRGBA, child layout.Widget) layout.Dimensions {
	return roundedPanel(gtx, border, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(1), Right: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return roundedPanel(gtx, background, child)
		})
	})
}

// primaryButton creates the dark filled action treatment used for the application's non-destructive controls.
// The returned widget preserves the supplied clickable's interaction state and lays out its label with white text.
func primaryButton(theme *material.Theme, button *widget.Clickable, label string) layout.Widget {
	return filledButton(theme, button, label, primaryButtonColor)
}

// dangerButton creates the red filled treatment reserved for destructive Delete actions.
// The returned widget matches primary-button sizing while retaining a visually distinct destructive color.
func dangerButton(theme *material.Theme, button *widget.Clickable, label string) layout.Widget {
	return filledButton(theme, button, label, dangerColor)
}

// filledButton creates a consistently sized filled button using the supplied background color.
// theme supplies typography, button owns interaction state, label is visible text, background paints the button, and the returned widget shows a pointer cursor over its complete hit area.
func filledButton(theme *material.Theme, button *widget.Clickable, label string, background color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		style := material.Button(theme, button, label)
		style.Background = background
		style.Color = primaryButtonText
		style.CornerRadius = unit.Dp(4)
		style.Inset = layout.Inset{Top: unit.Dp(10), Right: unit.Dp(16), Bottom: unit.Dp(10), Left: unit.Dp(16)}
		return pointerCursor(gtx, style.Layout)
	}
}

// pointerCursor adds the link-style pointer cursor to exactly the measured area of child.
// gtx provides the active Gio operations, child renders the interactive area, and the returned dimensions match child without expanding its hit target.
func pointerCursor(gtx layout.Context, child layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dimensions := child(gtx)
	call := macro.Stop()

	// Replay the child inside its measured clip so the pointer never leaks into adjacent non-interactive UI.
	defer clip.Rect(image.Rectangle{Max: dimensions.Size}).Push(gtx.Ops).Pop()
	pointer.CursorPointer.Add(gtx.Ops)
	call.Add(gtx.Ops)
	return dimensions
}

// clickableWithPointer lays out button with child and gives its measured hit target a link-style pointer cursor.
// gtx supplies the active frame, button owns persistent interaction state, child draws its content, and the returned dimensions match the clickable target.
func clickableWithPointer(gtx layout.Context, button *widget.Clickable, child layout.Widget) layout.Dimensions {
	return pointerCursor(gtx, func(gtx layout.Context) layout.Dimensions {
		return button.Layout(gtx, child)
	})
}

// linkLabel renders text as a navigation link that underlines only while its persistent clickable target is hovered.
// gtx supplies the current frame, theme controls text shaping, button owns interaction state, text is the visible label, and the returned dimensions preserve the caller's column width.
func linkLabel(gtx layout.Context, theme *material.Theme, button *widget.Clickable, text string) layout.Dimensions {
	return clickableWithPointer(gtx, button, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.W}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				// Measure the label naturally so the underline communicates the text link rather than the entire table column.
				gtx.Constraints.Min.X = 0
				style := material.Body1(theme, text)
				style.MaxLines = 2
				style.Color = color.NRGBA{R: 0, G: 91, B: 160, A: 255}
				dimensions := style.Layout(gtx)
				if button.Hovered() {
					// Keep the normal link label compact while making its destination affordance explicit on hover.
					underlineHeight := max(gtx.Dp(unit.Dp(1)), 1)
					paint.FillShape(gtx.Ops, style.Color, clip.Rect(image.Rect(0, max(dimensions.Size.Y-underlineHeight, 0), dimensions.Size.X, dimensions.Size.Y)).Op())
				}
				return dimensions
			}),
		)
	})
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
