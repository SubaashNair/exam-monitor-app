package main

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/server/session"
)

// ConnState — three-tier classification used by the per-tile connection
// dot. v0.2.0 spec FR-2: green when streaming, yellow during the 3-8s
// grace window, red after.
type ConnState int

const (
	ConnStateLive  ConnState = iota // last frame within 3s
	ConnStateStale                  // 3-8s: in the reconnect grace window
	ConnStateLost                   // >= 8s: connection considered dropped
)

const (
	connStaleThreshold = 3 * time.Second
	connLostThreshold  = 8 * time.Second
)

// classifyConnection returns the ConnState for a given last-frame Timestamp.
// A zero Timestamp (no frame ever) is treated as Lost.
func classifyConnection(lastFrame time.Time) ConnState {
	if lastFrame.IsZero() {
		return ConnStateLost
	}
	since := time.Since(lastFrame)
	switch {
	case since < connStaleThreshold:
		return ConnStateLive
	case since < connLostThreshold:
		return ConnStateStale
	default:
		return ConnStateLost
	}
}

var (
	dotLive  = color.NRGBA{R: 16, G: 185, B: 129, A: 255}  // emerald-500
	dotStale = color.NRGBA{R: 234, G: 179, B: 8, A: 255}   // yellow-500
	dotLost  = color.NRGBA{R: 220, G: 38, B: 38, A: 255}   // red-600
)

// Card colors
var (
	cardBackground  = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	cardBorder      = color.NRGBA{R: 226, G: 232, B: 240, A: 255} // Soft gray border
	cardShadow      = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
	cardHover       = color.NRGBA{R: 248, G: 250, B: 252, A: 255}
	textPrimary     = color.NRGBA{R: 30, G: 41, B: 59, A: 255}    // Slate-800
	textSecondary   = color.NRGBA{R: 100, G: 116, B: 139, A: 255} // Slate-500
	placeholderBg   = color.NRGBA{R: 241, G: 245, B: 249, A: 255} // Slate-100
	placeholderText = color.NRGBA{R: 148, G: 163, B: 184, A: 255} // Slate-400
)

// stateBadge returns a one-character glyph and background colour for the
// per-tile state badge. The glyph provides accessibility for colour-blind
// users; the colour gives the quick at-a-glance read.
func stateBadge(state session.State, online bool) (glyph string, bg color.NRGBA) {
	if !online {
		return "!", color.NRGBA{R: 220, G: 38, B: 38, A: 255} // red-600
	}
	switch state {
	case session.StateWaiting:
		return "W", color.NRGBA{R: 16, G: 185, B: 129, A: 255} // emerald-500
	case session.StateCapturing:
		return "●", color.NRGBA{R: 16, G: 185, B: 129, A: 255}
	case session.StateLocked:
		return "🔒", color.NRGBA{R: 31, G: 41, B: 55, A: 255} // gray-800
	default:
		return "?", color.NRGBA{R: 234, G: 179, B: 8, A: 255} // yellow-500
	}
}

func StudentCard(gtx layout.Context, th *material.Theme, student *Student, width int, imgCache *ImageCacheManager) layout.Dimensions {
	return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.Clickable(gtx, student.Clickable, func(gtx layout.Context) layout.Dimensions {
			// Draw shadow
			shadowRect := clip.RRect{
				Rect: image.Rect(2, 2, gtx.Constraints.Max.X, gtx.Constraints.Max.Y),
				NE:   8, NW: 8, SE: 8, SW: 8,
			}
			paint.FillShape(gtx.Ops, cardShadow, shadowRect.Op(gtx.Ops))

			// Draw card background with rounded corners
			cardRect := clip.RRect{
				Rect: image.Rect(0, 0, gtx.Constraints.Max.X-2, gtx.Constraints.Max.Y-2),
				NE:   8, NW: 8, SE: 8, SW: 8,
			}
			paint.FillShape(gtx.Ops, cardBackground, cardRect.Op(gtx.Ops))

			// Draw border
			return widget.Border{
				Color:        cardBorder,
				Width:        unit.Dp(1),
				CornerRadius: unit.Dp(8),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(
						gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutStudentImage(gtx, th, student, width, imgCache)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutStudentInfo(gtx, th, student)
						}),
					)
				})
			})
		})
	})
}

func layoutStudentImage(gtx layout.Context, th *material.Theme, student *Student, width int, imgCache *ImageCacheManager) layout.Dimensions {
	// Lock the image area to a 16:9 box regardless of source aspect ratio.
	// Both the placeholder and the rendered image return this same size, so
	// the surrounding card stays a consistent shape across the join
	// → capturing → stopped lifecycle (no more "card grows / white space
	// appears" when frames first arrive).
	imgHeight := width * 9 / 16

	// Stack layers the image (or placeholder) beneath the state badge.
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			// Always fill the 16:9 area with the placeholder colour first;
			// any letterbox bars from Fit-Contain will pick up this colour.
			boxRect := image.Rect(0, 0, width, imgHeight)
			paint.FillShape(gtx.Ops, placeholderBg, clip.RRect{
				Rect: boxRect,
				NE:   6, NW: 6, SE: 6, SW: 6,
			}.Op(gtx.Ops))

			if student.Image == nil {
				return layout.Dimensions{Size: image.Pt(width, imgHeight)}
			}

			imgOp := imgCache.GetImageOp(student)
			imgSize := student.Image.Bounds().Size()

			// Fit-Contain: pick the smaller of the two scale factors so the
			// image fits entirely inside the 16:9 box without cropping.
			// Letterbox bars (top/bottom or left/right) appear when the
			// source aspect ratio differs from 16:9.
			scaleX := float32(width) / float32(imgSize.X)
			scaleY := float32(imgHeight) / float32(imgSize.Y)
			scale := scaleX
			if scaleY < scaleX {
				scale = scaleY
			}
			renderedX := int(float32(imgSize.X) * scale)
			renderedY := int(float32(imgSize.Y) * scale)
			offsetX := (width - renderedX) / 2
			offsetY := (imgHeight - renderedY) / 2

			// Clip everything we draw next to the rounded 16:9 box.
			defer clip.RRect{
				Rect: boxRect,
				NE:   6, NW: 6, SE: 6, SW: 6,
			}.Push(gtx.Ops).Pop()

			// Translate to centre the image; draw at the computed scale.
			defer op.Offset(image.Pt(offsetX, offsetY)).Push(gtx.Ops).Pop()
			widget.Image{Src: imgOp, Scale: scale}.Layout(gtx)

			return layout.Dimensions{Size: image.Pt(width, imgHeight)}
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// Render the state badge in the top-right corner.
			glyph, bg := stateBadge(student.State, true)
			return layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						badgeSize := gtx.Dp(unit.Dp(20))
						defer clip.RRect{
							Rect: image.Rect(0, 0, badgeSize, badgeSize),
							NE:   4, NW: 4, SE: 4, SW: 4,
						}.Push(gtx.Ops).Pop()
						paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(badgeSize, badgeSize)}.Op())
						lbl := material.Caption(th, glyph)
						lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						lbl.TextSize = unit.Sp(10)
						return layout.Center.Layout(gtx, lbl.Layout)
					})
			})
		}),
	)
}

// connectionDot renders a small filled circle reflecting the three-tier
// connection state: green (live), yellow (stale/grace window), red (lost).
// Sized at 10 dp so it sits comfortably next to the name.
func connectionDot(gtx layout.Context, st ConnState) layout.Dimensions {
	size := gtx.Dp(unit.Dp(10))
	var col color.NRGBA
	switch st {
	case ConnStateLive:
		col = dotLive
	case ConnStateStale:
		col = dotStale
	default:
		col = dotLost
	}
	defer clip.Ellipse{Max: image.Pt(size, size)}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(size, size)}
}

func layoutStudentInfo(gtx layout.Context, th *material.Theme, student *Student) layout.Dimensions {
	// Classify the connection state based on time since the last accepted
	// PICTURE frame. The Student struct refreshes Timestamp via UpdateImage,
	// so this reflects "frames flowing in the last N seconds" rather than
	// just "TCP socket is open."
	state := classifyConnection(student.Timestamp)
	return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(
			gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return connectionDot(gtx, state)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := material.Body1(th, student.Name)
						label.Color = textPrimary
						label.MaxLines = 1
						label.TextSize = unit.Sp(14)
						return label.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					label := material.Body2(th, "ID: "+student.Id)
					label.Color = textSecondary
					label.MaxLines = 1
					label.TextSize = unit.Sp(12)
					return label.Layout(gtx)
				})
			}),
		)
	})
}

func CreateStudentGrid(gtx layout.Context, th *material.Theme, students []*Student, rowIndex int, col int, imgCache *ImageCacheManager) []layout.FlexChild {
	var row []layout.FlexChild
	start := rowIndex * col
	end := start + col
	if end > len(students) {
		end = len(students)
	}
	width := (gtx.Constraints.Max.X - 2*col*8) / col

	for i := start; i < end; i++ {
		item := students[i]
		row = append(row, layout.Flexed(1.0/float32(col), func(gtx layout.Context) layout.Dimensions {
			return StudentCard(gtx, th, item, width, imgCache)
		}))
	}

	// Fill empty slots
	for len(row) < col {
		row = append(row, layout.Flexed(1.0/float32(col), func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{}
		}))
	}

	return row
}
