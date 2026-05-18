package main

import (
	"image/color"
	"log/slog"
	"sync"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/server/updater"
)

// UpdateNotice — see client/updater_notice.go for the shared design.
// Server and client implementations are kept identical so a single change
// touches both lockstep.
type UpdateNotice struct {
	mu         sync.Mutex
	available  bool
	tag        string
	dismissed  bool
	BtnOpen    *widget.Clickable
	BtnDismiss *widget.Clickable
}

func NewUpdateNotice() *UpdateNotice {
	return &UpdateNotice{
		BtnOpen:    new(widget.Clickable),
		BtnDismiss: new(widget.Clickable),
	}
}

func (n *UpdateNotice) CheckAsync(invalidate func()) {
	go func() {
		rel, err := updater.FetchLatest()
		if err != nil {
			slog.Debug("updater: fetch failed", "err", err)
			return
		}
		if !updater.IsNewer(Version, rel.TagName) {
			slog.Debug("updater: already up to date", "running", Version, "latest", rel.TagName)
			return
		}
		n.mu.Lock()
		n.available = true
		n.tag = rel.TagName
		n.mu.Unlock()
		if invalidate != nil {
			invalidate()
		}
	}()
}

func (n *UpdateNotice) Available() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.available && !n.dismissed
}

func (n *UpdateNotice) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !n.Available() {
		return layout.Dimensions{}
	}
	if n.BtnOpen.Clicked(gtx) {
		if err := updater.OpenBrowser(updater.ReleasesURL); err != nil {
			slog.Warn("updater: open browser failed", "err", err)
		}
	}
	if n.BtnDismiss.Clicked(gtx) {
		n.mu.Lock()
		n.dismissed = true
		n.mu.Unlock()
		return layout.Dimensions{}
	}

	n.mu.Lock()
	tag := n.tag
	n.mu.Unlock()

	bg := color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	fg := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	subFg := color.NRGBA{R: 191, G: 219, B: 254, A: 255}

	// op.Record pattern (see client/updater_notice.go for design notes):
	// measure content first, then paint background sized exactly to the
	// measurement so the banner never fills the parent's remaining space.
	macro := op.Record(gtx.Ops)
	contentDims := layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, "↑ Update available: "+tag)
				lbl.Color = fg
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(th, n.BtnOpen, "Download")
				btn.Background = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				btn.Color = bg
				return btn.Layout(gtx)
			}),
			layout.Flexed(1, layout.Spacer{}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(th, n.BtnDismiss, "Dismiss")
				btn.Background = bg
				btn.Color = subFg
				return btn.Layout(gtx)
			}),
		)
	})
	contentCall := macro.Stop()

	paint.FillShape(gtx.Ops, bg, clip.Rect{Max: contentDims.Size}.Op())
	contentCall.Add(gtx.Ops)

	return contentDims
}
