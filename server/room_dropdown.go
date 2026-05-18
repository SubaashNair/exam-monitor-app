package main

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// RoomPresets are the named-room choices shown in the dropdown. Each maps
// to a fixed network port via RoomNameToPort (see util.go).
var RoomPresets = []string{"Alpha", "Bravo", "Charlie", "Delta"}

// RoomDropdown — see client/room_dropdown.go for the shared design notes.
// Server and client implementations are kept identical so the two forms
// behave the same way; if you change one, update the other in lockstep.
type RoomDropdown struct {
	Editor *widget.Editor

	BtnToggle  *widget.Clickable
	BtnPresets []*widget.Clickable
	BtnCustom  *widget.Clickable

	open     bool
	custom   bool
	selected string
}

func NewRoomDropdown() *RoomDropdown {
	d := &RoomDropdown{
		Editor:    new(widget.Editor),
		BtnToggle: new(widget.Clickable),
		BtnCustom: new(widget.Clickable),
	}
	d.Editor.SingleLine = true
	d.BtnPresets = make([]*widget.Clickable, len(RoomPresets))
	for i := range RoomPresets {
		d.BtnPresets[i] = new(widget.Clickable)
	}
	return d
}

func (d *RoomDropdown) Value() string {
	if d.custom {
		return strings.TrimSpace(d.Editor.Text())
	}
	return d.selected
}

func (d *RoomDropdown) SetValue(s string) {
	s = strings.TrimSpace(s)
	for _, p := range RoomPresets {
		if strings.EqualFold(s, p) {
			d.selected = p
			d.custom = false
			return
		}
	}
	if s != "" {
		d.selected = ""
		d.custom = true
		d.Editor.SetText(s)
	}
}

func (d *RoomDropdown) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if d.BtnToggle.Clicked(gtx) {
		d.open = !d.open
	}
	for i, btn := range d.BtnPresets {
		if btn.Clicked(gtx) {
			d.selected = RoomPresets[i]
			d.custom = false
			d.open = false
		}
	}
	if d.BtnCustom.Clicked(gtx) {
		d.custom = true
		d.selected = ""
		d.open = false
	}

	label := "Select a room…"
	switch {
	case d.custom:
		label = "Custom…"
	case d.selected != "":
		label = d.selected
	}
	label = label + "  ▾"

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, d.BtnToggle, label).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !d.open {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.Button(th, d.BtnPresets[0], "Alpha").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(material.Button(th, d.BtnPresets[1], "Bravo").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(material.Button(th, d.BtnPresets[2], "Charlie").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(material.Button(th, d.BtnPresets[3], "Delta").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(material.Button(th, d.BtnCustom, "Custom…").Layout),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !d.custom {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return material.Editor(th, d.Editor, "type your room name").Layout(gtx)
			})
		}),
	)
}
