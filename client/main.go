package main

import (
	"image/color"
	"log"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/exam-gaurd/client/internal/diag"
	"github.com/exam-gaurd/client/ui"
)

type AppState struct {
	currentScreen string
	mu            sync.Mutex
}

var AppPalette = material.Palette{
	Bg:         color.NRGBA{R: 255, G: 255, B: 255, A: 255}, // White
	Fg:         color.NRGBA{R: 17, G: 24, B: 39, A: 255},    // gray-900
	ContrastBg: color.NRGBA{R: 37, G: 99, B: 235, A: 255},   // blue-600 (Primary)
	ContrastFg: color.NRGBA{R: 255, G: 255, B: 255, A: 255}, // White
}

var ErrorColor = color.NRGBA{R: 220, G: 38, B: 38, A: 255} // red-600

var DisabledBg = color.NRGBA{R: 229, G: 231, B: 235, A: 255} // gray-200
var DisabledFg = color.NRGBA{R: 156, G: 163, B: 175, A: 255} // gray-400

func NewAppState() AppState {
	return AppState{
		currentScreen: "join",
	}
}

func (state *AppState) swtichScreen(screen string) {
	state.mu.Lock()
	state.currentScreen = screen
	state.mu.Unlock()
}

func main() {
	logger, err := diag.New("client")
	if err != nil {
		log.Printf("diag: %v (falling back to stderr-only logging)", err)
	} else {
		diag.SetDefault(logger)
		slog.SetDefault(logger.Logger)
		defer logger.Close()
		if crash, ok := logger.LastCrash(); ok {
			logger.Warn("previous session crashed", "marker", "last-crash.txt")
			_ = crash // surfaced to UI later; for now just leave the marker in place
		}
		logger.Info("client starting",
			"os", runtime.GOOS,
			"arch", runtime.GOARCH,
			"go_version", runtime.Version(),
		)
	}

	defer diag.RecoverPanic("client_main")

	go func() {
		defer diag.RecoverPanic("client_window_goroutine")
		w := new(app.Window)
		w.Option(app.Title("Exam Guard Client"))
		w.Option(app.Size(unit.Dp(400), unit.Dp(600)))
		if err := run(w); err != nil {
			log.Fatal(err)
			os.Exit(0)
		}
	}()

	app.Main()
}

func run(w *app.Window) error {
	var ops op.Ops
	th := material.NewTheme()
	th.Palette = AppPalette
	state := NewAppState()

	dashboard := NewDashboardState(func() {
		state.swtichScreen("join")
	}, func() {
		w.Invalidate()
	})

	overlay := ui.NewLockOverlay(th)
	toast := ui.NewToastState()
	dashboard.SetUIHelpers(overlay, toast)

	dashboard.client.SetSessionCallbacks(
		func(examName string) {
			dashboard.OnExamStart(examName)
			w.Invalidate()
		},
		func() {
			dashboard.OnExamStop()
			w.Invalidate()
		},
		func(msg, by string) {
			overlay.Show(msg, by)
			w.Invalidate()
		},
		func() {
			overlay.Hide()
			w.Invalidate()
		},
		func(from, body string) {
			toast.Set(from, body)
			w.Invalidate()
		},
	)

	joinView := NewJoinView(func(sid, name string, room int, serverIP, examToken string) {
		state.swtichScreen("dashboard")
		dashboard.client.SetManualServerIP(serverIP)
		dashboard.client.SetExamToken(examToken)
		dashboard.client.Start(sid, name, room, func() {
			w.Invalidate()
		})
	})

	for {
		event := w.Event()
		switch typ := event.(type) {
		case app.FrameEvent:
			gtx := app.NewContext(&ops, typ)
			switch state.currentScreen {
			case "join":
				layout.Flex{
					Axis: layout.Vertical,
				}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return joinView.Layout(gtx, th)
					}),
				)
			default:
				layout.Flex{
					Axis: layout.Vertical,
				}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return dashboard.Layout(gtx, th)
					}),
				)
			}

			typ.Frame(gtx.Ops)
		case app.DestroyEvent:
			os.Exit(0)
		}
	}

}
