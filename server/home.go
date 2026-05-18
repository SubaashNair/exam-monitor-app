package main

import (
	"image/color"
	"strconv"
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
	OnClick        func(examName string, room int)

	currentSession *session.ExamSession
	examNameError  string
	roomError      string
}

func NewHomeState(start func(examName string, room int)) *HomeState {
	h := &HomeState{
		ExamNameEditor: new(widget.Editor),
		RoomEditor:     new(widget.Editor),
		BtnStart:       new(widget.Clickable),
		BtnRegenerate:  new(widget.Clickable),
		OnClick:        start,
	}
	h.RoomEditor.Filter = "0123456789"
	h.RoomEditor.MaxLen = 6
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
	roomText := strings.TrimSpace(h.RoomEditor.Text())
	if roomText == "" {
		h.roomError = "Room is required."
		valid = false
	} else if room, err := strconv.Atoi(roomText); err != nil || room <= 0 {
		h.roomError = "Room must be a positive number."
		valid = false
	}
	return valid
}

func (h *HomeState) handleSubmit() {
	if !h.validate() {
		return
	}
	room, _ := strconv.Atoi(strings.TrimSpace(h.RoomEditor.Text()))
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
	}

	if h.BtnStart.Clicked(gtx) {
		h.handleSubmit()
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
						return TextEditor(th, h.RoomEditor, "Enter room number")(gtx)
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
					// Token display + regenerate
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						tokenLabel := material.Body1(th, "Exam token: "+h.currentSession.Token.String())
						tokenLabel.TextSize = unit.Sp(16)
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(tokenLabel.Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
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
