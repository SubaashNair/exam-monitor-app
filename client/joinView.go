package main

import (
	"fmt"
	"image/color"
	"net"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type JoinView struct {
	IdEditor       *widget.Editor
	NameEditor     *widget.Editor
	RoomPicker     *RoomDropdown
	ServerIPEditor *widget.Editor
	TokenEditor    *widget.Editor
	BtnStart       *widget.Clickable
	BtnTestConn    *widget.Clickable
	OnClick        func(sid, name string, room int, roomName, serverIP, examToken string)

	idError    string
	nameError  string
	roomError  string
	ipError    string
	tokenError string

	submitAttempted bool

	// Connection diagnostic state
	testMu         sync.Mutex
	testResult     string     // human-readable last test outcome
	testResultOK   bool       // determines colour
	testInProgress bool       // grays out the button while a dial is running
	invalidate     func()     // window invalidate callback (set via SetInvalidate)
}

func NewJoinView(start func(sid, name string, room int, roomName, serverIP, examToken string)) *JoinView {
	joinView := JoinView{
		IdEditor:       new(widget.Editor),
		NameEditor:     new(widget.Editor),
		RoomPicker:     NewRoomDropdown(),
		ServerIPEditor: new(widget.Editor),
		TokenEditor:    new(widget.Editor),
		BtnStart:       new(widget.Clickable),
		BtnTestConn:    new(widget.Clickable),
		OnClick:        start,
	}

	joinView.IdEditor.Submit = true
	joinView.NameEditor.Submit = true
	joinView.ServerIPEditor.Submit = true
	joinView.TokenEditor.Submit = true

	if data, err := LoadFormData(); err == nil && data != nil {
		joinView.IdEditor.SetText(data.StudentID)
		joinView.NameEditor.SetText(data.Name)
		joinView.RoomPicker.SetValue(data.Room)
		joinView.ServerIPEditor.SetText(data.ServerIP)
	}

	return &joinView
}

func (h *JoinView) validate() bool {
	h.idError = ""
	h.nameError = ""
	h.roomError = ""
	h.ipError = ""
	h.tokenError = ""

	valid := true

	if strings.TrimSpace(h.IdEditor.Text()) == "" {
		h.idError = "Student ID is required."
		valid = false
	}

	if strings.TrimSpace(h.NameEditor.Text()) == "" {
		h.nameError = "Name is required."
		valid = false
	}

	if strings.TrimSpace(h.RoomPicker.Value()) == "" {
		h.roomError = "Room is required."
		valid = false
	}

	// Manual server IP is optional, but if provided must parse.
	ipText := strings.TrimSpace(h.ServerIPEditor.Text())
	if ipText != "" {
		if _, err := parseManualIP(ipText); err != nil {
			h.ipError = "Server IP must be a valid IPv4/IPv6 address."
			valid = false
		}
	}

	if strings.TrimSpace(h.TokenEditor.Text()) == "" {
		h.tokenError = "Exam token is required."
		valid = false
	}

	return valid
}

func (h *JoinView) isValid() bool {
	if strings.TrimSpace(h.IdEditor.Text()) == "" {
		return false
	}
	if strings.TrimSpace(h.NameEditor.Text()) == "" {
		return false
	}
	if strings.TrimSpace(h.RoomPicker.Value()) == "" {
		return false
	}
	if strings.TrimSpace(h.TokenEditor.Text()) == "" {
		return false
	}
	return true
}

// SetInvalidate registers the window invalidate callback so async test
// results trigger a redraw. main.go calls this after window construction.
func (h *JoinView) SetInvalidate(fn func()) { h.invalidate = fn }

// runConnectivityTest dials Server IP + room-derived port with a 3s timeout
// and updates testResult under mutex. Runs in a goroutine so the UI thread
// is never blocked.
func (h *JoinView) runConnectivityTest() {
	ipText := strings.TrimSpace(h.ServerIPEditor.Text())
	roomText := strings.TrimSpace(h.RoomPicker.Value())

	if ipText == "" {
		h.testMu.Lock()
		h.testResult = "Enter a Server IP to test (auto-discovery can't be tested from here)."
		h.testResultOK = false
		h.testInProgress = false
		h.testMu.Unlock()
		if h.invalidate != nil {
			h.invalidate()
		}
		return
	}
	if _, err := parseManualIP(ipText); err != nil {
		h.testMu.Lock()
		h.testResult = fmt.Sprintf("Invalid Server IP: %s", err.Error())
		h.testResultOK = false
		h.testInProgress = false
		h.testMu.Unlock()
		if h.invalidate != nil {
			h.invalidate()
		}
		return
	}
	if roomText == "" {
		h.testMu.Lock()
		h.testResult = "Pick a Room first so we know which port to test."
		h.testResultOK = false
		h.testInProgress = false
		h.testMu.Unlock()
		if h.invalidate != nil {
			h.invalidate()
		}
		return
	}

	port := RoomNameToPort(roomText)
	target := net.JoinHostPort(ipText, fmt.Sprintf("%d", port))

	h.testMu.Lock()
	h.testInProgress = true
	h.testResult = fmt.Sprintf("Testing %s …", target)
	h.testResultOK = false
	h.testMu.Unlock()
	if h.invalidate != nil {
		h.invalidate()
	}

	go func() {
		conn, err := net.DialTimeout("tcp", target, 3*time.Second)

		h.testMu.Lock()
		defer h.testMu.Unlock()
		h.testInProgress = false
		if err != nil {
			h.testResult = fmt.Sprintf("✗ %s unreachable: %s", target, err.Error())
			h.testResultOK = false
		} else {
			_ = conn.Close()
			h.testResult = fmt.Sprintf("✓ Reached %s — server is accepting connections.", target)
			h.testResultOK = true
		}
		if h.invalidate != nil {
			h.invalidate()
		}
	}()
}

func (h *JoinView) handleSubmit() {
	h.submitAttempted = true
	if h.validate() {
		roomName := strings.TrimSpace(h.RoomPicker.Value())
		room := RoomNameToPort(roomName)
		studentID := strings.TrimSpace(h.IdEditor.Text())
		name := strings.TrimSpace(h.NameEditor.Text())
		serverIP := strings.TrimSpace(h.ServerIPEditor.Text())
		examToken := strings.TrimSpace(h.TokenEditor.Text())

		// Token is NOT persisted across sessions (spec §6.3).
		SaveFormData(studentID, name, roomName, serverIP)

		h.OnClick(studentID, name, room, roomName, serverIP, examToken)
	}
}

func (h *JoinView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if h.BtnStart.Clicked(gtx) {
		h.handleSubmit()
	}
	if h.BtnTestConn.Clicked(gtx) {
		h.runConnectivityTest()
	}

	h.testMu.Lock()
	testResult := h.testResult
	testResultOK := h.testResultOK
	testInProgress := h.testInProgress
	h.testMu.Unlock()

	var idErr, nameErr, roomErr, ipErr, tokenErr string
	if h.submitAttempted {
		idErr = h.idError
		nameErr = h.nameError
		roomErr = h.roomError
		ipErr = h.ipError
		tokenErr = h.tokenError
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return MaxWidthContainer(gtx, 480, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(
				gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						subtitle := material.Body1(th, "Please enter your details to join:")
						subtitle.TextSize = unit.Sp(14)
						return subtitle.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(24)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return FormRow(gtx, "Student ID", th, func(gtx layout.Context) layout.Dimensions {
						return TextEditorWithError(th, h.IdEditor, "Enter student id", idErr)(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return FormRow(gtx, "Name", th, func(gtx layout.Context) layout.Dimensions {
						return TextEditorWithError(th, h.NameEditor, "Enter your name", nameErr)(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return FormRow(gtx, "Room", th, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return h.RoomPicker.Layout(gtx, th)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if roomErr == "" {
									return layout.Dimensions{}
								}
								return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(th, roomErr)
									lbl.Color = ErrorColor
									return lbl.Layout(gtx)
								})
							}),
						)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return FormRow(gtx, "Server IP (optional)", th, func(gtx layout.Context) layout.Dimensions {
						return TextEditorWithError(th, h.ServerIPEditor, "Leave blank for auto-discovery", ipErr)(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							btn := material.Button(th, h.BtnTestConn, "Test connection")
							btn.TextSize = unit.Sp(13)
							if testInProgress {
								btn.Background = DisabledBg
								btn.Color = DisabledFg
							}
							return btn.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							if testResult == "" {
								return layout.Dimensions{}
							}
							lbl := material.Body2(th, testResult)
							if testResultOK {
								lbl.Color = color.NRGBA{R: 22, G: 163, B: 74, A: 255} // emerald
							} else {
								lbl.Color = ErrorColor
							}
							return lbl.Layout(gtx)
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return FormRow(gtx, "Exam token", th, func(gtx layout.Context) layout.Dimensions {
						return TextEditorWithError(th, h.TokenEditor, "e.g. BIO-XQ7-394", tokenErr)(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(24)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, h.BtnStart, "Start")

						if !h.isValid() {
							btn.Background = DisabledBg
							btn.Color = DisabledFg
						}

						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(200))
						return btn.Layout(gtx)
					})
				}),
			)
		})
	})
}
