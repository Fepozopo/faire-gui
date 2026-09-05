package application

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// orderStatusBadgeColors defines the background and foreground colors that identify
// an order status in the Orders table.
type orderStatusBadgeColors struct {
	background color.NRGBA
	text       color.NRGBA
}

var (
	orderStatusBadgeTextColor       = color.NRGBA{R: 60, G: 60, B: 60, A: 255}
	orderStatusBadgeLightTextColor  = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	orderStatusBadgeNeutralColor    = color.NRGBA{R: 241, G: 241, B: 241, A: 255}
	orderStatusBadgeNewColor        = color.NRGBA{R: 71, G: 71, B: 71, A: 255}
	orderStatusBadgeProcessingColor = color.NRGBA{R: 246, G: 239, B: 219, A: 255}
	orderStatusBadgeDeliveredColor  = color.NRGBA{R: 226, G: 240, B: 230, A: 255}
	orderStatusBadgeCanceledColor   = color.NRGBA{R: 242, G: 228, B: 225, A: 255}
	orderStatusBadgeBackorderColor  = color.NRGBA{R: 230, G: 230, B: 230, A: 255}
)

// orderStatusBadgeCell preserves the fixed Status-column width while left-aligning
// a compact badge whose background only covers the rendered status text.
func orderStatusBadgeCell(gtx layout.Context, theme *material.Theme, status string) layout.Dimensions {
	return layout.Stack{Alignment: layout.W}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return orderStatusBadge(gtx, theme, status)
		}),
	)
}

// orderStatusBadge draws a two-line-or-shorter status label on its semantic fill.
// Recording the text before painting lets the fill use the label's natural size,
// rather than the full fixed width of the Status table column.
func orderStatusBadge(gtx layout.Context, theme *material.Theme, status string) layout.Dimensions {
	colors := orderStatusBadgeColorsFor(status)
	macro := op.Record(gtx.Ops)
	style := material.Body1(theme, status)
	style.MaxLines = 2
	style.Color = colors.text
	dimensions := layout.Inset{Top: unit.Dp(4), Right: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(8)}.Layout(gtx, style.Layout)
	call := macro.Stop()

	radius := gtx.Dp(unit.Dp(4))
	paint.FillShape(gtx.Ops, colors.background, clip.UniformRRect(image.Rectangle{Max: dimensions.Size}, radius).Op(gtx.Ops))
	call.Add(gtx.Ops)
	return dimensions
}

// orderStatusBadgeColorsFor maps table status labels to their visual treatments.
// Unknown or missing API states remain visibly tagged with the neutral treatment
// so future statuses do not appear unstyled.
func orderStatusBadgeColorsFor(status string) orderStatusBadgeColors {
	switch status {
	case "New":
		return orderStatusBadgeColors{background: orderStatusBadgeNewColor, text: orderStatusBadgeLightTextColor}
	case "Processing":
		return orderStatusBadgeColors{background: orderStatusBadgeProcessingColor, text: orderStatusBadgeTextColor}
	case "Pre-transit", "In transit":
		return orderStatusBadgeColors{background: orderStatusBadgeNeutralColor, text: orderStatusBadgeTextColor}
	case "Delivered":
		return orderStatusBadgeColors{background: orderStatusBadgeDeliveredColor, text: orderStatusBadgeTextColor}
	case "Canceled":
		return orderStatusBadgeColors{background: orderStatusBadgeCanceledColor, text: orderStatusBadgeTextColor}
	case "Backordered":
		return orderStatusBadgeColors{background: orderStatusBadgeBackorderColor, text: orderStatusBadgeTextColor}
	case "Pending retailer confirmation":
		return orderStatusBadgeColors{background: orderStatusBadgeBackorderColor, text: orderStatusBadgeTextColor}
	default:
		return orderStatusBadgeColors{background: orderStatusBadgeNeutralColor, text: orderStatusBadgeTextColor}
	}
}
