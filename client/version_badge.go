package main

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// versionBadgeColor — slate-400 for a subtle, non-distracting corner label.
var versionBadgeColor = color.NRGBA{R: 148, G: 163, B: 184, A: 255}

// versionBadge renders a small "v0.2.0" label suitable for the bottom-right
// corner of a view. Use it via layout.SE inside the parent's outer Layout
// to anchor it to the bottom-right.
func versionBadge(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(6), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body2(th, Version)
		lbl.TextSize = unit.Sp(10)
		lbl.Color = versionBadgeColor
		return lbl.Layout(gtx)
	})
}
