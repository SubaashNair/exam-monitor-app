package main

import (
	"image/color"
	"log/slog"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/server/session"
)

type HomeState struct {
	ExamNameEditor *widget.Editor
	RoomEditor     *widget.Editor
	BtnStart       *widget.Clickable
	BtnRegenerate  *widget.Clickable
	BtnCopyToken   *widget.Clickable
	BtnAlpha       *widget.Clickable
	BtnBravo       *widget.Clickable
	BtnCharlie     *widget.Clickable
	BtnDelta       *widget.Clickable
	OnClick        func(examName string, room int)

	currentSession *session.ExamSession
	examNameError  string
	roomError      string
	tokenCopied    bool
}

func NewHomeState(start func(examName string, room int)) *HomeState {
	h := &HomeState{
		ExamNameEditor: new(widget.Editor),
		RoomEditor:     new(widget.Editor),
		BtnStart:       new(widget.Clickable),
		BtnRegenerate:  new(widget.Clickable),
		BtnCopyToken:   new(widget.Clickable),
		BtnAlpha:       new(widget.Clickable),
		BtnBravo:       new(widget.Clickable),
		BtnCharlie:     new(widget.Clickable),
		BtnDelta:       new(widget.Clickable),
		OnClick:        start,
	}
	// Room now accepts any string — preset names (Alpha/Bravo/Charlie/Delta)
	// map to fixed ports; arbitrary strings hash deterministically.
	h.ExamNameEditor.Submit = true
	h.RoomEditor.Submit = true
	return h
}

func (h *HomeState) ensureSession() {
	if h.currentSession != nil {
		return
	}
	h.currentSession = session.NewExamSession("", 0, 4*time.Hour)
}

func (h *HomeState) regenerateToken() {
	h.ensureSession()
	h.currentSession.RegenerateToken(4 * time.Hour)
}

func (h *HomeState) validate() bool {
	h.examNameError = ""
	h.roomError = ""
	valid := true
	if strings.TrimSpace(h.ExamNameEditor.Text()) == "" {
		h.examNameError = "Exam name is required."
		valid = false
	}
	if strings.TrimSpace(h.RoomEditor.Text()) == "" {
		h.roomError = "Room is required."
		valid = false
	}
	return valid
}

func (h *HomeState) handleSubmit() {
	if !h.validate() {
		return
	}
	roomName := strings.TrimSpace(h.RoomEditor.Text())
	room := RoomNameToPort(roomName)
	examName := strings.TrimSpace(h.ExamNameEditor.Text())
	h.ensureSession()
	h.currentSession.Name = examName
	h.currentSession.Port = room
	h.OnClick(examName, room)
}

// CurrentSession returns the prepared ExamSession so main.go can install
// it on the Server and open the event log.
func (h *HomeState) CurrentSession() *session.ExamSession { return h.currentSession }

func (h *HomeState) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	h.ensureSession()

	if h.BtnRegenerate.Clicked(gtx) {
		h.regenerateToken()
		h.tokenCopied = false
	}

	if h.BtnStart.Clicked(gtx) {
		h.handleSubmit()
	}

	if h.BtnCopyToken.Clicked(gtx) {
		if err := CopyToClipboard(h.currentSession.Token.String()); err != nil {
			slog.Warn("clipboard copy failed", "err", err)
			h.tokenCopied = false
		} else {
			h.tokenCopied = true
		}
	}

	// Preset room buttons — set the room editor text on click.
	for _, pair := range []struct {
		btn  *widget.Clickable
		name string
	}{
		{h.BtnAlpha, "Alpha"},
		{h.BtnBravo, "Bravo"},
		{h.BtnCharlie, "Charlie"},
		{h.BtnDelta, "Delta"},
	} {
		if pair.btn.Clicked(gtx) {
			h.RoomEditor.SetText(pair.name)
			h.roomError = ""
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(32), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					title := material.H4(th, "Exam Monitor")
					return title.Layout(gtx)
				})
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(
					gtx,
					// Exam name field
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.Body1(th, "Exam Name").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(300)
						gtx.Constraints.Max.X = gtx.Dp(300)
						return TextEditor(th, h.ExamNameEditor, "e.g. Biology Final")(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if h.examNameError == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(th, h.examNameError)
							lbl.Color = color.NRGBA{R: 200, G: 50, B: 50, A: 255}
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
					}),
					// Room field
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.Body1(th, "Room").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(300)
						gtx.Constraints.Max.X = gtx.Dp(300)
						return TextEditor(th, h.RoomEditor, "Alpha, Bravo, or your own room name")(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
					}),
					// Preset room buttons (Alpha / Bravo / Charlie / Delta)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(300)
						gtx.Constraints.Max.X = gtx.Dp(300)
						return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
							layout.Rigid(material.Button(th, h.BtnAlpha, "Alpha").Layout),
							layout.Rigid(material.Button(th, h.BtnBravo, "Bravo").Layout),
							layout.Rigid(material.Button(th, h.BtnCharlie, "Charlie").Layout),
							layout.Rigid(material.Button(th, h.BtnDelta, "Delta").Layout),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if h.roomError == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(th, h.roomError)
							lbl.Color = color.NRGBA{R: 200, G: 50, B: 50, A: 255}
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
					}),
					// Token display + copy + regenerate
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						tokenLabel := material.Body1(th, "Exam token: "+h.currentSession.Token.String())
						tokenLabel.TextSize = unit.Sp(16)
						copyText := "Copy"
						if h.tokenCopied {
							copyText = "Copied ✓"
						}
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(tokenLabel.Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							layout.Rigid(material.Button(th, h.BtnCopyToken, copyText).Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(material.Button(th, h.BtnRegenerate, "regenerate").Layout),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
					}),
					// Open Waiting Room button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(200)
						return material.Button(th, h.BtnStart, "Open Waiting Room").Layout(gtx)
					}),
				)
			})
		}),
	)
}
