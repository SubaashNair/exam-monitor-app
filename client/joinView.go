package main

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type JoinView struct {
	IdEditor       *widget.Editor
	NameEditor     *widget.Editor
	RoomEditor     *widget.Editor
	ServerIPEditor *widget.Editor
	TokenEditor    *widget.Editor
	BtnStart       *widget.Clickable
	BtnAlpha       *widget.Clickable
	BtnBravo       *widget.Clickable
	BtnCharlie     *widget.Clickable
	BtnDelta       *widget.Clickable
	OnClick        func(sid, name string, room int, serverIP, examToken string)

	idError    string
	nameError  string
	roomError  string
	ipError    string
	tokenError string

	submitAttempted bool
}

func NewJoinView(start func(sid, name string, room int, serverIP, examToken string)) *JoinView {
	joinView := JoinView{
		IdEditor:       new(widget.Editor),
		NameEditor:     new(widget.Editor),
		RoomEditor:     new(widget.Editor),
		ServerIPEditor: new(widget.Editor),
		TokenEditor:    new(widget.Editor),
		BtnStart:       new(widget.Clickable),
		BtnAlpha:       new(widget.Clickable),
		BtnBravo:       new(widget.Clickable),
		BtnCharlie:     new(widget.Clickable),
		BtnDelta:       new(widget.Clickable),
		OnClick:        start,
	}

	// Room now accepts any string (presets + custom). RoomNameToPort handles
	// the mapping to a network port consistently with the server.

	joinView.IdEditor.Submit = true
	joinView.NameEditor.Submit = true
	joinView.RoomEditor.Submit = true
	joinView.ServerIPEditor.Submit = true
	joinView.TokenEditor.Submit = true

	if data, err := LoadFormData(); err == nil && data != nil {
		joinView.IdEditor.SetText(data.StudentID)
		joinView.NameEditor.SetText(data.Name)
		joinView.RoomEditor.SetText(data.Room)
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

	if strings.TrimSpace(h.RoomEditor.Text()) == "" {
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
	if strings.TrimSpace(h.RoomEditor.Text()) == "" {
		return false
	}
	if strings.TrimSpace(h.TokenEditor.Text()) == "" {
		return false
	}
	return true
}

func (h *JoinView) handleSubmit() {
	h.submitAttempted = true
	if h.validate() {
		roomName := strings.TrimSpace(h.RoomEditor.Text())
		room := RoomNameToPort(roomName)
		studentID := strings.TrimSpace(h.IdEditor.Text())
		name := strings.TrimSpace(h.NameEditor.Text())
		serverIP := strings.TrimSpace(h.ServerIPEditor.Text())
		examToken := strings.TrimSpace(h.TokenEditor.Text())

		// Token is NOT persisted across sessions (spec §6.3).
		SaveFormData(studentID, name, roomName, serverIP)

		h.OnClick(studentID, name, room, serverIP, examToken)
	}
}

func (h *JoinView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if h.BtnStart.Clicked(gtx) {
		h.handleSubmit()
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
						return TextEditorWithError(th, h.RoomEditor, "Alpha, Bravo, or your own room name", roomErr)(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
				}),
				// Preset room buttons (Alpha / Bravo / Charlie / Delta)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(material.Button(th, h.BtnAlpha, "Alpha").Layout),
						layout.Rigid(material.Button(th, h.BtnBravo, "Bravo").Layout),
						layout.Rigid(material.Button(th, h.BtnCharlie, "Charlie").Layout),
						layout.Rigid(material.Button(th, h.BtnDelta, "Delta").Layout),
					)
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
