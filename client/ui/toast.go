package ui

import (
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const toastDuration = 5 * time.Second

// ToastState holds the currently-displayed message, if any. Safe for
// concurrent Set() (called from the network goroutine) and Layout() (called
// from the UI goroutine).
type ToastState struct {
	mu        sync.Mutex
	from      string
	body      string
	expiresAt time.Time
	dismiss   widget.Clickable
}

func NewToastState() *ToastState { return &ToastState{} }

// Set records a new message. Resets the 5s timer.
func (t *ToastState) Set(from, body string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.from = from
	t.body = body
	t.expiresAt = time.Now().Add(toastDuration)
}

// IsVisible reports whether Layout should render.
func (t *ToastState) IsVisible() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.expiresAt.IsZero() && time.Now().Before(t.expiresAt)
}

// Layout renders the toast if visible; otherwise zero dimensions.
// Schedules a redraw at expiresAt so the toast disappears at exactly
// 5s after Set() — without needing other UI activity to trigger a
// redraw.
func (t *ToastState) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	t.mu.Lock()
	expiresAt := t.expiresAt
	from, body := t.from, t.body
	t.mu.Unlock()

	if expiresAt.IsZero() || !time.Now().Before(expiresAt) {
		return layout.Dimensions{}
	}

	// Schedule the next render at exactly the expiry time so the toast
	// disappears without needing another UI event to trigger a redraw.
	gtx.Execute(op.InvalidateCmd{At: expiresAt})

	if t.dismiss.Clicked(gtx) {
		t.mu.Lock()
		t.expiresAt = time.Time{}
		t.mu.Unlock()
		return layout.Dimensions{}
	}

	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.Body1(th, "💬 "+from+": "+body).Layout),
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(material.Button(th, &t.dismiss, "Dismiss").Layout),
		)
	})
}
