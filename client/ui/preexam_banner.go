package ui

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// preExamColors — green equivalent of the red MONITORING banner.
var (
	preExamBg = color.NRGBA{R: 22, G: 163, B: 74, A: 255}   // green-600
	preExamFg = color.NRGBA{R: 255, G: 255, B: 255, A: 255} // white
)

// PreExamBanner renders the pre-exam "Connected — sharing active" banner
// shown after Join but before Start Exam. Uses the same op.Record measure
// -then-paint pattern as the red Banner widget so it never fills the
// parent's remaining space.
func PreExamBanner(gtx layout.Context, th *material.Theme) layout.Dimensions {
	title := material.H6(th, "🟢 CONNECTED")
	title.Color = preExamFg
	sub := material.Body2(th, "Your screen is being shared. Waiting for instructor to start the exam.")
	sub.Color = preExamFg

	macro := op.Record(gtx.Ops)
	contentDims := layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(title.Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
			layout.Rigid(sub.Layout),
		)
	})
	contentCall := macro.Stop()

	paint.FillShape(gtx.Ops, preExamBg, clip.Rect{Max: contentDims.Size}.Op())
	contentCall.Add(gtx.Ops)

	return contentDims
}
