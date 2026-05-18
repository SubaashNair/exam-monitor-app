// Package ui holds the client's Phase 1 UI widgets (banner, lock overlay,
// message toast) and the platform-specific always-on-top helpers.
package ui

import (
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// BannerColors are the palette for the monitoring banner.
type BannerColors struct {
	Background color.NRGBA
	Foreground color.NRGBA
}

var DefaultBannerColors = BannerColors{
	Background: color.NRGBA{R: 220, G: 38, B: 38, A: 255}, // red-600
	Foreground: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
}

// Banner renders the persistent monitoring banner. examName/instructor may
// be empty; elapsed is the duration since EXAM_START.
func Banner(gtx layout.Context, th *material.Theme, examName, instructor string, elapsed time.Duration) layout.Dimensions {
	bg := DefaultBannerColors.Background

	titleText := "🔴 MONITORING ACTIVE"
	if examName != "" {
		titleText = titleText + " — " + examName
	}
	title := material.H6(th, titleText)
	title.Color = DefaultBannerColors.Foreground

	elapsedText := elapsed.Truncate(time.Second).String() + " elapsed"
	subText := elapsedText
	if instructor != "" {
		subText = "Instructor: " + instructor + " · " + elapsedText
	}
	sub := material.Body2(th, subText)
	sub.Color = DefaultBannerColors.Foreground

	// Build the content first so the Stack sizes itself to the content's
	// natural height (Stacked) rather than the parent's max-Y. Expanded then
	// paints the background within that bounded area.
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			// Force the content to span the full available width so the
			// background paints across the row rather than just behind the
			// text glyphs.
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(title.Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(sub.Layout),
				)
			})
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return fillRect(gtx, bg)
		}),
	)
}

// fillRect fills the gtx's max-constraints rectangle with c.
func fillRect(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
