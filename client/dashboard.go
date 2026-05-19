package main

import (
	"fmt"
	"image"
	"image/color"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/client/session"
	"github.com/exam-gaurd/client/ui"
)

type DashboardState struct {
	client    *Client
	BtnStop   *widget.Clickable
	BtnRetry  *widget.Clickable
	BtnCancel *widget.Clickable
	Stop      func()
	UpdateUI  func()
	errorMsg  string

	// UI helpers wired in by main.go (Task 28)
	overlay *ui.LockOverlay
	toast   *ui.ToastState

	// Exam state — written by the network reader goroutine (via OnExamStart /
	// OnExamStop / SetRoomName) and read by the UI goroutine in Layout. On
	// relaxed-memory-order CPUs (Apple Silicon, modern ARM) plain field
	// writes are not visible across goroutines without explicit
	// synchronization; mu protects all five fields below.
	mu          sync.RWMutex
	examName    string
	roomName    string
	examStart   time.Time
	examEnded   bool
	examEndedAt time.Time
}

var (
	statusTextPrimary   = color.NRGBA{R: 30, G: 41, B: 59, A: 255}    // slate-800
	statusTextSecondary = color.NRGBA{R: 100, G: 116, B: 139, A: 255} // slate-500
	statusOK            = color.NRGBA{R: 22, G: 163, B: 74, A: 255}   // green-600
	statusDividerColor  = color.NRGBA{R: 226, G: 232, B: 240, A: 255} // slate-200
)

func NewDashboardState(stop func(), updateUI func()) *DashboardState {
	client := NewClient()
	ds := &DashboardState{
		client:    client,
		BtnStop:   new(widget.Clickable),
		BtnRetry:  new(widget.Clickable),
		BtnCancel: new(widget.Clickable),
		Stop:      stop,
		UpdateUI:  updateUI,
	}

	client.SetCallbacks(
		func() {
			ds.errorMsg = ""
			ds.UpdateUI()
		},
		func(err error) {
			ds.errorMsg = err.Error()
			ds.UpdateUI()
		},
	)

	return ds
}

// SetUIHelpers stores the overlay and toast helpers created in main.go.
func (d *DashboardState) SetUIHelpers(o *ui.LockOverlay, t *ui.ToastState) {
	d.overlay = o
	d.toast = t
}

// OnExamStart records the exam name and the start wall-clock time.
func (d *DashboardState) OnExamStart(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.examName = name
	d.examStart = time.Now()
	d.examEnded = false
	d.examEndedAt = time.Time{}
}

// SetRoomName records the room name the student joined (e.g. "Alpha" or a
// custom string), shown in the status card.
func (d *DashboardState) SetRoomName(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.roomName = name
}

// OnExamStop flags the exam as ended so Layout can render the stopped view
// regardless of how the session state machine resolved the transition. This
// is the dashboard's own source of truth — a defensive flag in case Apply()
// silently rejected the EventExamStop transition or the new state isn't
// visible to the UI goroutine yet.
func (d *DashboardState) OnExamStop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.examEnded = true
	d.examEndedAt = time.Now()
}

// examInfo returns a consistent snapshot of all mu-protected fields so
// callers can render without holding the lock across draw operations.
func (d *DashboardState) examInfo() (name, room string, start time.Time, ended bool, endedAt time.Time) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.examName, d.roomName, d.examStart, d.examEnded, d.examEndedAt
}

// Layout branches on the current session state.
func (d *DashboardState) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if d.BtnStop.Clicked(gtx) {
		d.Stop()
		d.client.Stop()
	}
	if d.BtnRetry.Clicked(gtx) {
		d.errorMsg = ""
	}
	if d.BtnCancel.Clicked(gtx) {
		d.Stop()
		d.client.Stop()
	}

	// The dashboard's own examEnded flag takes precedence over the session
	// state machine. If we received TypeExamStop OR detected a disconnect
	// after an exam was running, render the ended view regardless of what
	// the state machine reports.
	if _, _, _, ended, _ := d.examInfo(); ended {
		return d.layoutEnded(gtx, th)
	}

	state := d.client.SessionState().State()

	// If the client lost its TCP connection, show a Reconnecting notice
	// instead of the (now stale) banner. Applies in both the lobby
	// (StateWaiting) and during-exam (StateCapturing/Locked) states; only
	// StateStopped is carved out because layoutEnded handles that path.
	if !d.client.IsConnected() && state != session.StateStopped {
		return d.layoutReconnecting(gtx, th)
	}

	switch state {
	case session.StateWaiting:
		return d.layoutWaiting(gtx, th)
	case session.StateCapturing, session.StateLocked:
		return d.layoutCapturing(gtx, th)
	case session.StateStopped:
		return d.layoutStopped(gtx, th)
	default:
		return layout.Dimensions{}
	}
}

// layoutEnded renders the post-exam confirmation, shown after the instructor
// stops the exam OR the connection dropped after an active exam started.
func (d *DashboardState) layoutEnded(gtx layout.Context, th *material.Theme) layout.Dimensions {
	_, _, _, _, endedAt := d.examInfo()
	endedText := "Your exam has ended."
	if !endedAt.IsZero() {
		endedText = "Your exam ended at " + endedAt.Format("15:04:05") + "."
	}
	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H4(th, "✓ Exam ended").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(material.Body1(th, endedText).Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(material.Body2(th, "You may close this window.").Layout),
		)
	})
}

// layoutReconnecting renders the "we lost the instructor" notice. Shown when
// the session state machine still thinks we're Capturing/Locked but the TCP
// connection has dropped.
func (d *DashboardState) layoutReconnecting(gtx layout.Context, th *material.Theme) layout.Dimensions {
	disconnAt := d.client.DisconnectedAt()
	elapsed := time.Duration(0)
	if !disconnAt.IsZero() {
		if since := time.Since(disconnAt); since >= 0 && since < 10*time.Minute {
			elapsed = since
		}
	}

	// Schedule the next render in 500ms so the elapsed counter ticks.
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(500 * time.Millisecond)})

	headline := "⚠ Connection lost"
	subText := "Trying to reconnect to your instructor…"
	if elapsed > 0 {
		subText = fmt.Sprintf("Trying to reconnect to your instructor… (%ds)", int(elapsed.Seconds()))
	}

	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H6(th, headline).Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(material.Body1(th, subText).Layout),
		)
	})
}

func (d *DashboardState) layoutWaiting(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// FR-13: capture is already running once we've joined. Show a green
	// "Connected — sharing active" banner instead of the old passive
	// "waiting for instructor" message. The status card below confirms
	// frames are flowing.
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.PreExamBanner(gtx, th)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return d.layoutStatusCard(gtx, th)
		}),
	)
}

func (d *DashboardState) layoutCapturing(gtx layout.Context, th *material.Theme) layout.Dimensions {
	examName, _, examStart, _, _ := d.examInfo()
	// Guard against rendering the banner before OnExamStart has populated
	// examStart. time.Since(time.Time{}) saturates to ~292 years on amd64
	// because time.Duration is an int64 nanosecond count, which would render
	// as "2562047h47m16s elapsed".
	elapsed := time.Duration(0)
	if !examStart.IsZero() {
		if since := time.Since(examStart); since >= 0 && since < 24*time.Hour {
			elapsed = since
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.Banner(gtx, th, examName, "", elapsed)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return d.layoutStatusCard(gtx, th)
		}),
	)
}

// layoutStatusCard renders the live status block: a STATUS section with
// connection indicators, a divider, and an instructor-messages area that
// becomes the active toast region when a message arrives.
func (d *DashboardState) layoutStatusCard(gtx layout.Context, th *material.Theme) layout.Dimensions {
	_, roomName, _, _, _ := d.examInfo()
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// STATUS heading
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, "STATUS")
				lbl.Color = statusTextSecondary
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			// Checkmark rows
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return statusRow(gtx, th, "✓", statusOK, "Screen capture active")
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				connectedText := "Connected"
				if roomName != "" {
					connectedText = "Connected (" + roomName + " room)"
				}
				return statusRow(gtx, th, "✓", statusOK, connectedText)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				framesText := fmt.Sprintf("%d frames sent", d.client.FramesSent())
				return statusRow(gtx, th, "●", statusOK, framesText)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			// Divider
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return statusDivider(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			// INSTRUCTOR MESSAGES heading
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, "💬 INSTRUCTOR MESSAGES")
				lbl.Color = statusTextSecondary
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			// Toast content (or placeholder text)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if d.toast != nil && d.toast.IsVisible() {
					return d.toast.Layout(gtx, th)
				}
				lbl := material.Body2(th, "(no messages yet)")
				lbl.Color = statusTextSecondary
				return lbl.Layout(gtx)
			}),
		)
	})
}

// statusRow renders a single 'glyph + label' row in the status card.
func statusRow(gtx layout.Context, th *material.Theme, glyph string, glyphColor color.NRGBA, text string) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, glyph)
			lbl.Color = glyphColor
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, text)
			lbl.Color = statusTextPrimary
			return lbl.Layout(gtx)
		}),
	)
}

// statusDivider draws a thin horizontal divider line in the status card.
func statusDivider(gtx layout.Context) layout.Dimensions {
	height := gtx.Dp(unit.Dp(1))
	width := gtx.Constraints.Max.X
	rect := image.Rect(0, 0, width, height)
	paint.FillShape(gtx.Ops, statusDividerColor, clip.Rect(rect).Op())
	return layout.Dimensions{Size: image.Pt(width, height)}
}

func (d *DashboardState) layoutStopped(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H6(th, "✓ Exam ended.").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(material.Body1(th, "Your screen is no longer being captured.").Layout),
		)
	})
}

// layoutBody is the original dashboard body: searching/connecting indicator and
// connected stats. It is shown as the main content area while capturing is active.
func (d *DashboardState) layoutBody(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if d.client.isConnected.Load() {
		return d.layoutConnected(gtx, th)
	}
	return d.layoutSearching(gtx, th)
}

func (d *DashboardState) layoutSearching(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return MaxWidthContainer(gtx, 480, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(
				gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						loader := material.Loader(th)
						return loader.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						msg := material.Body1(th, "Looking for the server…")
						return msg.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						hint := material.Body2(th, "This can take up to 10s.")
						hint.Color = DisabledFg
						hint.TextSize = unit.Sp(12)
						return hint.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(24)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if d.errorMsg != "" {
						return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return NewBorderWithColor(ErrorColor).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										errLabel := material.Body2(th, "Could not connect. Please try again.")
										errLabel.Color = ErrorColor
										return errLabel.Layout(gtx)
									})
								})
							})
						})
					}
					return layout.Dimensions{}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, d.BtnCancel, "Cancel")
						btn.Background = DisabledBg
						btn.Color = DisabledFg
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(200))
						return btn.Layout(gtx)
					})
				}),
			)
		})
	})
}

func (d *DashboardState) layoutConnected(gtx layout.Context, th *material.Theme) layout.Dimensions {
	lastSent := d.client.GetLastSentTime()
	var timeSinceStr string
	if !lastSent.IsZero() {
		since := time.Since(lastSent)
		if since < time.Second {
			timeSinceStr = "just now"
		} else if since < time.Minute {
			timeSinceStr = fmt.Sprintf("%ds ago", int(since.Seconds()))
		} else {
			timeSinceStr = fmt.Sprintf("%dm ago", int(since.Minutes()))
		}
	} else {
		timeSinceStr = "pending"
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return MaxWidthContainer(gtx, 480, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(
				gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						status := material.H6(th, "✓  Screen Sharing Started")
						status.Color = th.Palette.ContrastBg
						return status.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(24)}.Layout(gtx)
				}),

				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lastSentLabel := material.Body2(th, fmt.Sprintf("Last sent: %s", timeSinceStr))
						lastSentLabel.Color = DisabledFg
						lastSentLabel.TextSize = unit.Sp(12)
						return lastSentLabel.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(32)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, d.BtnStop, "Stop")
						btn.Background = ErrorColor
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(200))
						return btn.Layout(gtx)
					})
				}),
			)
		})
	})
}
