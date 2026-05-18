package main

import (
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/eventlog"
	"github.com/exam-gaurd/server/session"
)

var (
	// DisabledBg / DisabledFg are used to visually grey-out the Start button
	// when no students have joined yet.
	DisabledBg = color.NRGBA{R: 156, G: 163, B: 175, A: 255} // Gray-400
	DisabledFg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

// LockDialogState holds UI widget state for the Lock-all confirmation modal.
type LockDialogState struct {
	Open       bool
	MsgEditor  *widget.Editor
	BtnCancel  *widget.Clickable
	BtnConfirm *widget.Clickable
}

// MsgDialogState holds UI widget state for the Send-Message dialog.
// TargetAll selects a class-wide broadcast; otherwise TargetID identifies the
// specific student.
//
// Per-student picker: studentBtns lazily creates one *widget.Clickable per
// student ID.
//
// TODO Phase 1.5: replace the flat button list with a searchable dropdown.
type MsgDialogState struct {
	Open       bool
	TargetAll  bool
	TargetID   string
	BodyEditor *widget.Editor
	BtnAll     *widget.Clickable
	BtnPer     *widget.Clickable
	BtnCancel  *widget.Clickable
	BtnConfirm *widget.Clickable
	studentBtns map[string]*widget.Clickable
}

// studentPickerBtn lazily creates a *widget.Clickable for the given student ID.
func (md *MsgDialogState) studentPickerBtn(id string) *widget.Clickable {
	if md.studentBtns == nil {
		md.studentBtns = make(map[string]*widget.Clickable)
	}
	if md.studentBtns[id] == nil {
		md.studentBtns[id] = new(widget.Clickable)
	}
	return md.studentBtns[id]
}

type DashboardState struct {
	studentManager    *StudentManager
	imgCache          *ImageCacheManager
	BtnStop           *widget.Clickable
	BtnColMinus       *widget.Clickable
	BtnColPlus        *widget.Clickable
	BtnSortToggle     *widget.Clickable
	BtnSortField      *widget.Clickable
	BtnViewerClose    *widget.Clickable
	BtnStartExam      *widget.Clickable
	BtnStopExam       *widget.Clickable
	BtnLockAll        *widget.Clickable
	BtnUnlock         *widget.Clickable
	BtnSendMsg        *widget.Clickable
	BtnExport         *widget.Clickable
	lockDialog        *LockDialogState
	msgDialog         *MsgDialogState
	server            *Server // injected by main.go for state queries + control broadcasts
	Stop              func()
	columnsCount      int
	viewerOpen        bool
	viewerStudentID   string
	viewerOpenedAtUTC time.Time // set when viewer opens; zero when closed
}

func NewDashboardState(stop func()) *DashboardState {
	return &DashboardState{
		studentManager: NewStudentManager(),
		imgCache:       NewImageCacheManager(),
		BtnStop:        new(widget.Clickable),
		BtnColMinus:    new(widget.Clickable),
		BtnColPlus:     new(widget.Clickable),
		BtnSortToggle:  new(widget.Clickable),
		BtnSortField:   new(widget.Clickable),
		BtnViewerClose: new(widget.Clickable),
		BtnStartExam:   new(widget.Clickable),
		BtnStopExam:    new(widget.Clickable),
		BtnLockAll:     new(widget.Clickable),
		BtnUnlock:      new(widget.Clickable),
		BtnSendMsg:     new(widget.Clickable),
		BtnExport:      new(widget.Clickable),
		lockDialog: &LockDialogState{
			MsgEditor:  new(widget.Editor),
			BtnCancel:  new(widget.Clickable),
			BtnConfirm: new(widget.Clickable),
		},
		msgDialog: &MsgDialogState{
			TargetAll:  true,
			BodyEditor: new(widget.Editor),
			BtnAll:     new(widget.Clickable),
			BtnPer:     new(widget.Clickable),
			BtnCancel:  new(widget.Clickable),
			BtnConfirm: new(widget.Clickable),
		},
		Stop:            stop,
		columnsCount:    3,
		viewerOpen:      false,
		viewerStudentID: "",
	}
}

// SetServer injects the server reference so the dashboard can query session
// state and issue broadcasts.
func (d *DashboardState) SetServer(s *Server) { d.server = s }

// Interface methods for server integration
func (ds *DashboardState) AddStudent(id, name string) {
	ds.studentManager.Add(id, name)
}

func (ds *DashboardState) isExists(id string) bool {
	return ds.studentManager.Exists(id)
}

func (ds *DashboardState) RemoveStudent(id string) {
	ds.studentManager.Remove(id)
	ds.imgCache.Remove(id)
}

func (ds *DashboardState) UpdateImage(id string, img image.Image) {
	ds.studentManager.UpdateImage(id, img)
}

func (ds *DashboardState) UpdateName(id string, name string) {
	ds.studentManager.UpdateName(id, name)
}

func (ds *DashboardState) Layout(gtx layout.Context, th *material.Theme, list *widget.List) layout.Dimensions {
	ds.handleButtonClicks(gtx)

	students := ds.studentManager.GetSorted()

	for _, student := range students {
		if student.Clickable.Clicked(gtx) {
			ds.viewerOpen = true
			ds.viewerStudentID = student.Id
			// Record open event.
			if ds.server != nil {
				if el := ds.server.EventLog(); el != nil {
					viewedAt := time.Now().UTC()
					ds.viewerOpenedAtUTC = viewedAt
					_ = el.Record(eventlog.Event{
						Type:        "instructor_viewed_student",
						StudentID:   student.Id,
						StudentName: student.Name,
						Details: map[string]any{
							"viewed_at_utc": viewedAt.Format(time.RFC3339),
						},
					})
				}
			}
		}
	}

	if ds.viewerOpen {
		viewerStudent := ds.studentManager.GetByID(ds.viewerStudentID)
		if viewerStudent == nil {
			// Student disconnected while viewer was open — record close event.
			ds.recordViewerClose(ds.viewerStudentID, "")
			ds.viewerOpen = false
		} else {
			return LayoutViewer(gtx, th, viewerStudent, ds.imgCache, ds.BtnViewerClose)
		}
	}

	return ds.layoutDashboard(gtx, th, list, students)
}

func (ds *DashboardState) handleButtonClicks(gtx layout.Context) {
	if ds.BtnStop.Clicked(gtx) {
		ds.Stop()
		if ds.viewerOpen {
			viewerStudent := ds.studentManager.GetByID(ds.viewerStudentID)
			name := ""
			if viewerStudent != nil {
				name = viewerStudent.Name
			}
			ds.recordViewerClose(ds.viewerStudentID, name)
		}
		ds.studentManager.Clear()
		ds.imgCache.Clear()
		ds.viewerOpen = false
	}

	if ds.BtnColMinus.Clicked(gtx) && ds.columnsCount > 1 {
		ds.columnsCount--
	}

	if ds.BtnColPlus.Clicked(gtx) && ds.columnsCount < 8 {
		ds.columnsCount++
	}

	if ds.BtnSortToggle.Clicked(gtx) {
		ds.studentManager.ToggleSortDirection()
	}

	if ds.BtnSortField.Clicked(gtx) {
		if ds.studentManager.GetSortField() == "name" {
			ds.studentManager.SetSortField("id")
		} else {
			ds.studentManager.SetSortField("name")
		}
	}

	if ds.BtnViewerClose.Clicked(gtx) {
		if ds.viewerOpen {
			viewerStudent := ds.studentManager.GetByID(ds.viewerStudentID)
			name := ""
			if viewerStudent != nil {
				name = viewerStudent.Name
			}
			ds.recordViewerClose(ds.viewerStudentID, name)
		}
		ds.viewerOpen = false
	}
}

func (ds *DashboardState) layoutDashboard(gtx layout.Context, th *material.Theme, list *widget.List, students []*Student) layout.Dimensions {
	col := ds.columnsCount
	itemCount := (len(students) + col - 1) / col

	dims := layout.Flex{Axis: layout.Vertical}.Layout(
		gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ds.topBar(gtx, th)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTopBar(
				gtx, th,
				ds.studentManager.Count(),
				ds.studentManager.GetSortField(),
				ds.studentManager.IsSortAscending(),
				ds.columnsCount,
				ds.BtnSortField,
				ds.BtnSortToggle,
				ds.BtnColMinus,
				ds.BtnColPlus,
				ds.BtnStop,
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return list.Layout(gtx, itemCount, func(gtx layout.Context, index int) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(
					gtx,
					CreateStudentGrid(gtx, th, students, index, col, ds.imgCache)...,
				)
			})
		}),
	)
	ds.drawLockDialog(gtx, th)
	ds.drawMsgDialog(gtx, th)
	return dims
}

// topBar renders a context-aware row above the main controls depending on
// the current exam session state.
func (d *DashboardState) topBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if d.server == nil {
		return layout.Dimensions{}
	}
	es := d.server.ExamSession()
	state := session.StateIdle
	if es != nil {
		state = es.Machine.State()
	}

	switch state {
	case session.StateWaiting:
		// Start button + count of waiting students.
		if d.BtnStartExam.Clicked(gtx) {
			d.startExam()
		}
		count := d.studentManager.Count()
		startBtn := material.Button(th, d.BtnStartExam, "Start Exam")
		if count == 0 {
			startBtn.Background = DisabledBg
			startBtn.Color = DisabledFg
		}
		label := material.Body1(th, fmt.Sprintf("Waiting for students — %d joined", count))
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(label.Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(startBtn.Layout),
				)
			})
	case session.StateCapturing:
		// Clock + Lock All + Send Message + Stop button.
		if d.BtnLockAll.Clicked(gtx) {
			d.lockDialog.Open = true
			d.lockDialog.MsgEditor.SetText("Please stop typing until further instructions.")
		}
		if d.BtnSendMsg.Clicked(gtx) {
			d.msgDialog.Open = true
			d.msgDialog.TargetAll = true
			d.msgDialog.TargetID = ""
			d.msgDialog.BodyEditor.SetText("")
		}
		if d.BtnStopExam.Clicked(gtx) {
			d.stopExam()
		}
		elapsed := time.Since(es.StartedAt).Truncate(time.Second)
		clock := material.Body1(th, fmt.Sprintf("Elapsed: %s", elapsed))
		lockBtn := material.Button(th, d.BtnLockAll, "Lock All")
		sendMsgBtn := material.Button(th, d.BtnSendMsg, "Send Message")
		stopBtn := material.Button(th, d.BtnStopExam, "Stop Exam")
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(clock.Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(lockBtn.Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(sendMsgBtn.Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(stopBtn.Layout),
				)
			})
	case session.StateLocked:
		// "ROOM LOCKED" label + Unlock button + Stop button.
		if d.BtnUnlock.Clicked(gtx) {
			d.unlockExam()
		}
		if d.BtnStopExam.Clicked(gtx) {
			d.stopExam()
		}
		elapsed := time.Since(es.StartedAt).Truncate(time.Second)
		clock := material.Body1(th, fmt.Sprintf("ROOM LOCKED — Elapsed: %s", elapsed))
		unlockBtn := material.Button(th, d.BtnUnlock, "Unlock")
		stopBtn := material.Button(th, d.BtnStopExam, "Stop Exam")
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(clock.Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(unlockBtn.Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(stopBtn.Layout),
				)
			})
	case session.StateStopped:
		if d.BtnExport.Clicked(gtx) {
			d.exportLog()
		}
		exportBtn := material.Button(th, d.BtnExport, "Export Event Log")
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Body1(th, "Exam ended.").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(exportBtn.Layout),
				)
			})
	default:
		return layout.Dimensions{}
	}
}

func (d *DashboardState) startExam() {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if _, err := es.Machine.Apply(session.EventStartExam); err != nil {
		slog.Warn("startExam: illegal transition", "err", err)
		return
	}
	es.MarkStarted()
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "exam_started"})
		_ = el.SetMeta("started_at_utc", es.StartedAt.Format(time.RFC3339))
	}
	// Broadcast EXAM_START to all currently-connected clients (they are in
	// Waiting state from the registry).
	_ = d.server.BroadcastControlAll(controlframe.TypeExamStart, controlframe.ExamStart{
		ExamName:  es.Name,
		StartedAt: es.StartedAt,
	})
	slog.Info("exam started", "name", es.Name)
}

func (d *DashboardState) stopExam() {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if err := es.Stop(24 * time.Hour); err != nil {
		slog.Warn("stopExam: illegal transition", "err", err)
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "exam_stopped"})
		_ = el.SetMeta("stopped_at_utc", es.StoppedAt.Format(time.RFC3339))
		_ = el.SetMeta("retention_until_utc", es.RetentionUntil.Format(time.RFC3339))
	}
	_ = d.server.BroadcastControlAll(controlframe.TypeExamStop, controlframe.ExamStop{StoppedAt: es.StoppedAt})
	slog.Info("exam stopped", "name", es.Name)
}

// drawLockDialog renders the lock-all confirmation modal overlay when Open is true.
// It must be called last in the parent Layout so it renders on top.
func (d *DashboardState) drawLockDialog(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !d.lockDialog.Open {
		return layout.Dimensions{}
	}
	if d.lockDialog.BtnCancel.Clicked(gtx) {
		d.lockDialog.Open = false
		return layout.Dimensions{}
	}
	if d.lockDialog.BtnConfirm.Clicked(gtx) {
		d.lockDialog.Open = false
		d.lockAll(strings.TrimSpace(d.lockDialog.MsgEditor.Text()))
		return layout.Dimensions{}
	}
	// Render a centred panel with title + editor + buttons stacked on top.
	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if w := gtx.Dp(unit.Dp(480)); gtx.Constraints.Max.X > w {
				gtx.Constraints.Max.X = w
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(material.H6(th, "Lock all student screens?").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
				layout.Rigid(material.Body1(th, "Message shown to students:").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(material.Editor(th, d.lockDialog.MsgEditor, "Message…").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(material.Button(th, d.lockDialog.BtnCancel, "Cancel").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(material.Button(th, d.lockDialog.BtnConfirm, "Lock All").Layout),
					)
				}),
			)
		})
	})
}

// lockAll transitions the session to Locked, records the event, and broadcasts
// the lock-screen control frame to all capturing students.
func (d *DashboardState) lockAll(message string) {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if _, err := es.Machine.Apply(session.EventLock); err != nil {
		slog.Warn("lock: illegal transition", "err", err)
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "lock_applied", Details: map[string]any{"message_to_students": message}})
	}
	_ = d.server.BroadcastControl(controlframe.TypeLockScreen, controlframe.LockScreen{Message: message, LockedBy: "Instructor"})
}

// unlockExam transitions the session back to Capturing, records the event, and
// broadcasts the unlock-screen control frame to all locked students.
func (d *DashboardState) unlockExam() {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if _, err := es.Machine.Apply(session.EventUnlock); err != nil {
		slog.Warn("unlock: illegal transition", "err", err)
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "lock_released"})
	}
	_ = d.server.BroadcastControl(controlframe.TypeUnlockScreen, controlframe.UnlockScreen{})
}

// exportLog writes the current event log to a timestamped CSV file in the
// platform user-config directory under exam-monitor/exports/.
func (d *DashboardState) exportLog() {
	el := d.server.EventLog()
	if el == nil {
		slog.Warn("export: no event log")
		return
	}
	base, _ := os.UserConfigDir()
	exportsDir := filepath.Join(base, "exam-monitor", "exports")
	if err := os.MkdirAll(exportsDir, 0o755); err != nil {
		slog.Warn("export: mkdir failed", "err", err)
		return
	}
	es := d.server.ExamSession()
	slug := "exam"
	if es != nil {
		slug = slugify(es.Name)
	}
	path := filepath.Join(exportsDir, fmt.Sprintf("%s-%s.csv", slug, time.Now().UTC().Format("2006-01-02-150405")))
	if err := el.ExportCSV(path); err != nil {
		slog.Warn("export failed", "path", path, "err", err)
		return
	}
	slog.Info("event log exported", "path", path)
}

// drawMsgDialog renders the Send-Message dialog overlay when Open is true.
// It must be called after drawLockDialog so it renders on top.
func (d *DashboardState) drawMsgDialog(gtx layout.Context, th *material.Theme) layout.Dimensions {
	md := d.msgDialog
	if !md.Open {
		return layout.Dimensions{}
	}
	if md.BtnCancel.Clicked(gtx) {
		md.Open = false
		return layout.Dimensions{}
	}
	if md.BtnAll.Clicked(gtx) {
		md.TargetAll = true
	}
	if md.BtnPer.Clicked(gtx) {
		md.TargetAll = false
	}
	if md.BtnConfirm.Clicked(gtx) {
		md.Open = false
		d.sendMessage(md.TargetAll, md.TargetID, strings.TrimSpace(md.BodyEditor.Text()))
		return layout.Dimensions{}
	}

	// Per-student picker: when !TargetAll, process clicks on individual student buttons.
	if !md.TargetAll {
		for _, st := range d.studentManager.GetSorted() {
			stu := st // capture loop variable
			if md.studentPickerBtn(stu.Id).Clicked(gtx) {
				md.TargetID = stu.Id
			}
		}
	}

	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if w := gtx.Dp(unit.Dp(520)); gtx.Constraints.Max.X > w {
				gtx.Constraints.Max.X = w
			}
			children := []layout.FlexChild{
				layout.Rigid(material.H6(th, "Send message").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(material.Button(th, md.BtnAll, ifThen(md.TargetAll, "● All", "○ All")).Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(material.Button(th, md.BtnPer, ifThen(!md.TargetAll, "● Selected", "○ Selected")).Layout),
					)
				}),
			}

			// Per-student picker row: shown only when a specific student is targeted.
			if !md.TargetAll {
				children = append(children,
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						students := d.studentManager.GetSorted()
						row := make([]layout.FlexChild, 0, len(students)*2)
						for i, st := range students {
							stu := st // capture
							label := stu.Name
							if md.TargetID == stu.Id {
								label = "● " + label
							} else {
								label = "○ " + label
							}
							if i > 0 {
								row = append(row, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
							}
							row = append(row, layout.Rigid(material.Button(th, md.studentPickerBtn(stu.Id), label).Layout))
						}
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, row...)
					}),
				)
			}

			children = append(children,
				layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
				layout.Rigid(material.Editor(th, md.BodyEditor, "Message…").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(material.Button(th, md.BtnCancel, "Cancel").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(material.Button(th, md.BtnConfirm, "Send").Layout),
					)
				}),
			)

			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// sendMessage broadcasts or sends a message to one student, and records the event.
func (d *DashboardState) sendMessage(toAll bool, targetID, body string) {
	if body == "" {
		return
	}
	target := ""
	if !toAll {
		target = targetID
	}
	msg := controlframe.BroadcastMsg{From: "Instructor", Body: body, TargetStudentID: target}
	if toAll {
		// Use BroadcastControlAll so the message reaches every connected
		// client regardless of session state. BroadcastControl filters to
		// Capturing+Locked, which races against unprocessed state-acks right
		// after exam start, dropping messages to clients in transit.
		_ = d.server.BroadcastControlAll(controlframe.TypeBroadcastMsg, msg)
	} else {
		if err := d.server.SendToStudent(target, controlframe.TypeBroadcastMsg, msg); err != nil {
			slog.Warn("send to student failed", "id", target, "err", err)
		}
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{
			Type: "message_broadcast",
			Details: map[string]any{
				"to":   ifThen(toAll, "all", target),
				"body": body,
			},
		})
	}
}

// recordViewerClose records an instructor_viewed_student close event and resets
// viewerOpenedAtUTC. It is a no-op when there is no server/event-log or when
// the viewer was never recorded as opened (zero viewerOpenedAtUTC).
func (d *DashboardState) recordViewerClose(studentID, studentName string) {
	if d.server == nil || d.viewerOpenedAtUTC.IsZero() {
		d.viewerOpenedAtUTC = time.Time{}
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{
			Type:        "instructor_viewed_student",
			StudentID:   studentID,
			StudentName: studentName,
			Details: map[string]any{
				"viewed_at_utc":       d.viewerOpenedAtUTC.Format(time.RFC3339),
				"viewed_until_at_utc": time.Now().UTC().Format(time.RFC3339),
			},
		})
	}
	d.viewerOpenedAtUTC = time.Time{}
}

// ifThen returns a if cond is true, otherwise b.
func ifThen(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
