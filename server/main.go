package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/server/eventlog"
	"github.com/exam-gaurd/server/internal/diag"
)

type AppState struct {
	currentScreen string
	mu            sync.Mutex
}

func NewAppState() AppState {
	return AppState{
		currentScreen: "home",
	}
}

func (state *AppState) swtichScreen(screen string) {
	state.mu.Lock()
	state.currentScreen = screen
	state.mu.Unlock()
}

func main() {
	logger, err := diag.New("server")
	if err != nil {
		log.Printf("diag: %v (falling back to stderr-only logging)", err)
	} else {
		diag.SetDefault(logger)
		slog.SetDefault(logger.Logger)
		defer logger.Close()
		if _, ok := logger.LastCrash(); ok {
			logger.Warn("previous session crashed", "marker", "last-crash.txt")
		}
		logger.Info("server starting",
			"os", runtime.GOOS,
			"arch", runtime.GOARCH,
			"go_version", runtime.Version(),
		)
	}

	defer diag.RecoverPanic("server_main")

	examsRoot := ""
	if logger != nil {
		base, _ := os.UserConfigDir()
		examsRoot = filepath.Join(base, "exam-monitor", "exams")
		if err := os.MkdirAll(examsRoot, 0o755); err != nil {
			slog.Warn("could not create exams dir", "dir", examsRoot, "err", err)
		}
		reaperCtx, reaperCancel := context.WithCancel(context.Background())
		defer reaperCancel()
		eventlog.RunReaper(reaperCtx, examsRoot, time.Hour)
		slog.Info("retention reaper started", "root", examsRoot, "interval", time.Hour)
	}

	go func() {
		defer diag.RecoverPanic("server_window_goroutine")
		w := new(app.Window)

		w.Option(app.Title("Exam Monitor"))
		w.Option(app.Size(unit.Dp(1000), unit.Dp(700)))

		if err := run(w, examsRoot); err != nil {
			log.Fatal(err)
			os.Exit(0)
		}
	}()

	app.Main()
}

func run(w *app.Window, examsRoot string) error {
	var ops op.Ops
	th := material.NewTheme()
	state := NewAppState()
	server := NewServer()
	server.examsRoot = examsRoot

	var home *HomeState
	home = NewHomeState(func(examName string, room int) {
		state.swtichScreen("dashboard")

		es := home.CurrentSession() // already has Name, Port, Token

		// Open the event log file for this exam.
		if examsRoot != "" {
			slug := slugify(examName)
			examDir := filepath.Join(examsRoot, time.Now().UTC().Format("2006-01-02")+"_"+slug)
			if err := os.MkdirAll(filepath.Join(examDir, "finals"), 0o755); err == nil {
				if el, err := eventlog.Open(filepath.Join(examDir, "events.sqlite")); err == nil {
					_ = el.SetMeta("exam_name", examName)
					_ = el.SetMeta("room_port", fmt.Sprintf("%d", room))
					_ = el.SetMeta("token", es.Token.Canonical())
					_ = el.SetMeta("created_at_utc", time.Now().UTC().Format(time.RFC3339))
					_ = el.Record(eventlog.Event{Type: "exam_created", Details: map[string]any{"token": es.Token.Canonical(), "port": room}})
					server.SetEventLog(el)
				} else {
					slog.Warn("eventlog open failed", "dir", examDir, "err", err)
				}
			} else {
				slog.Warn("mkdir exam dir failed", "dir", examDir, "err", err)
			}
		}

		server.SetExamSession(es)
		server.Start(room)
	})

	dashboard := NewDashboardState(func() {
		state.swtichScreen("home")
		server.Stop()
	})

	server.studentUtil = dashboard
	dashboard.SetServer(server)

	var list widget.List
	list.Axis = layout.Vertical

	updateNotice := NewUpdateNotice()
	updateNotice.CheckAsync(w.Invalidate)

	invalidateTicker := time.NewTicker(time.Second / 4)
	go func() {
		for range invalidateTicker.C {
			w.Invalidate()
		}
	}()

	for {
		event := w.Event()
		switch typ := event.(type) {
		case app.FrameEvent:
			gtx := app.NewContext(&ops, typ)
			switch state.currentScreen {
			case "home":
				layout.Flex{
					Axis: layout.Vertical,
				}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return updateNotice.Layout(gtx, th)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return home.Layout(gtx, th)
					}),
				)
			default:
				layout.Flex{
					Axis: layout.Vertical,
				}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return updateNotice.Layout(gtx, th)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return dashboard.Layout(gtx, th, &list)
					}),
				)
			}

			typ.Frame(gtx.Ops)
		case app.DestroyEvent:
			os.Exit(0)
		}
	}
}
