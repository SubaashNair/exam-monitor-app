// Package ui holds the client's Phase 1 UI widgets (banner, lock overlay,
// message toast) and the platform-specific always-on-top helpers.
package ui

import (
	"fmt"
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
	// Paint a coloured rectangle and overlay the text. Gio's idiomatic way
	// to paint a solid background behind a layout is paint.FillShape with a
	// clip.Rect; for Phase 1 we keep it minimal with material.H6 on a
	// material.Card-like wrapper.
	title := material.H6(th, fmt.Sprintf("🔴 MONITORING ACTIVE — %s", examName))
	title.Color = DefaultBannerColors.Foreground
	sub := material.Body2(th, fmt.Sprintf("Instructor: %s · %s elapsed", instructor, elapsed.Truncate(time.Second)))
	sub.Color = DefaultBannerColors.Foreground

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return fillRect(gtx, bg)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(title.Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(sub.Layout),
				)
			})
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
