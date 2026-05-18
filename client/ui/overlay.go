package ui

import (
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// LockOverlay owns a separate Gio window. Show/Hide are safe to call from
// any goroutine; the window itself runs on its own goroutine.
type LockOverlay struct {
	visible atomic.Bool
	message atomic.Value // string
	by      atomic.Value // string
	window  atomic.Pointer[app.Window]
	th      *material.Theme
}

// NewLockOverlay creates a LockOverlay that will render using th.
func NewLockOverlay(th *material.Theme) *LockOverlay {
	o := &LockOverlay{th: th}
	o.message.Store("")
	o.by.Store("")
	return o
}

// Show makes the overlay visible with the given lock message. Idempotent —
// if already showing the message is updated but no new window is opened.
func (o *LockOverlay) Show(message, by string) {
	o.message.Store(message)
	o.by.Store(by)
	if o.visible.Swap(true) {
		// Already visible; just refresh the stored strings.
		if w := o.window.Load(); w != nil {
			w.Invalidate()
		}
		return
	}
	go o.run()
}

// Hide closes the overlay window. Idempotent.
func (o *LockOverlay) Hide() {
	if !o.visible.Swap(false) {
		return
	}
	if w := o.window.Load(); w != nil {
		// Perform ActionClose wakes the event loop and triggers DestroyEvent.
		w.Perform(system.ActionClose)
	}
}

func (o *LockOverlay) run() {
	w := new(app.Window)
	// Configure: title, fullscreen mode, minimum size fallback.
	// app.Fullscreen is a WindowMode constant; .Option() returns an app.Option.
	w.Option(
		app.Title("Exam Paused"),
		app.Fullscreen.Option(),
		app.MinSize(unit.Dp(1920), unit.Dp(1080)),
	)
	_ = SetAlwaysOnTop(true) // best-effort; ErrNotSupported is ignored
	o.window.Store(w)
	defer o.window.Store(nil)

	var ops op.Ops
	for o.visible.Load() {
		ev := w.Event()
		switch ev := ev.(type) {
		case app.DestroyEvent:
			o.visible.Store(false)
			_ = SetAlwaysOnTop(false)
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, ev)
			o.draw(gtx)
			ev.Frame(gtx.Ops)
		}
	}
	// visible flag was cleared by Hide(); close the window gracefully.
	w.Perform(system.ActionClose)
	// Drain events until DestroyEvent so the goroutine exits cleanly.
	for {
		ev := w.Event()
		if _, ok := ev.(app.DestroyEvent); ok {
			break
		}
	}
	_ = SetAlwaysOnTop(false)
}

func (o *LockOverlay) draw(gtx layout.Context) layout.Dimensions {
	msg, _ := o.message.Load().(string)
	by, _ := o.by.Load().(string)

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H1(o.th, "EXAM PAUSED").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(24)}.Layout),
			layout.Rigid(material.H4(o.th, msg).Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(48)}.Layout),
			layout.Rigid(material.Body1(o.th, "Locked by: "+by).Layout),
		)
	})
}
