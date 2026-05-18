// Package ui holds the client's Phase 1 UI widgets (banner, lock overlay,
// message toast) and the platform-specific always-on-top helpers.
package ui

import (
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
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

// Banner renders the monitoring banner. examName/instructor may be empty;
// elapsed is the duration since EXAM_START.
//
// Implementation note: this uses op.Record to measure the content's intrinsic
// height BEFORE painting the background. Using layout.Stack with Expanded
// fillRect (the prior approach) sized the background to gtx.Constraints.Max,
// which on Gio v0.8 with a Vertical Flex Rigid parent leaks the parent's
// remaining-height max into the banner — making the red rectangle fill the
// rest of the window. Recording the content first, painting bg at exactly the
// recorded dimensions, then replaying the content guarantees the banner is
// natural-sized regardless of parent constraints.
func Banner(gtx layout.Context, th *material.Theme, examName, instructor string, elapsed time.Duration) layout.Dimensions {
	bg := DefaultBannerColors.Background

	titleText := "🔴 EXAM IN PROGRESS"
	if examName != "" {
		titleText = titleText + " — " + examName
	}
	title := material.H6(th, titleText)
	title.Color = DefaultBannerColors.Foreground

	elapsedText := elapsed.Truncate(time.Second).String() + " elapsed"
	subText := elapsedText
	if instructor != "" {
		subText = instructor + " · " + elapsedText
	}
	sub := material.Body2(th, subText)
	sub.Color = DefaultBannerColors.Foreground

	// Step 1: record the content layout into a deferred macro so we can
	// measure its dimensions without committing the draw ops yet.
	macro := op.Record(gtx.Ops)
	contentDims := layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X // span full width
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(title.Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
			layout.Rigid(sub.Layout),
		)
	})
	contentCall := macro.Stop()

	// Step 2: paint the background at exactly the content's measured size.
	paint.FillShape(gtx.Ops, bg, clip.Rect{Max: contentDims.Size}.Op())

	// Step 3: now replay the content draws on top of the background.
	contentCall.Add(gtx.Ops)

	return contentDims
}
