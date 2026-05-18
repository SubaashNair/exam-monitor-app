# Exam Monitor

Real-time classroom exam monitoring. Students run a desktop client that captures their screen and streams JPEG frames to a teacher's desktop server over the local network. The teacher sees every student in a live grid, can start and stop exams, lock all screens, send messages, and export an audit log.

Built in Go with the [Gio](https://gioui.org/) UI toolkit. Runs natively on macOS, Linux (X11 and Wayland), and Windows.

## Features

**Discovery & connection**
- Three-tier resolver: manual IP → mDNS (`_examguard._tcp`) → UDP broadcast — first hit wins.
- Per-student reconnection grace period (5 s) so transient drops don't churn the dashboard.

**Live monitoring**
- Pure-Go screen capture via `kbinani/screenshot` — no CGO compositor, no platform-specific drivers at compile time.
- JPEG quality 60, frames resized to 720 px wide, one frame every 500 ms (~2 FPS).
- 8-byte framing protocol: `"HE"` magic + `uint16` type + `uint32` length, big-endian. 5 MB max frame size.

**Classroom workflow**
- Teacher creates an exam with a token; students join with their ID, name, room, and the token.
- Session state machine — students can only capture while the exam is `Capturing` or `Locked`.
- Start / Stop / Lock-all / Unlock / Send-message controls in the teacher dashboard.
- SQLite event log records every instructor action (start, stop, lock, unlock, message, view-student).
- CSV export of the event log on demand.
- Automatic retention reaper deletes exam directories older than 24 h.

**Reliability**
- Panic recovery and crash markers at every goroutine boundary.
- Structured logging via `slog` to `<UserConfigDir>/exam-monitor/<component>.log`, rotated at 1 MB.
- macOS: first-frame blank detection catches denied screen-recording TCC permission instead of streaming silent black frames.
- Linux Wayland: runtime detection of `grim`, `gnome-screenshot`, or `spectacle`.
- Windows: DXGI capture with multi-monitor support via the `EXAM_MONITOR_DISPLAY` environment variable.

## Repository layout

Two independent Go modules (no `go.work` file):

| Path | Module | Purpose |
|---|---|---|
| `client/` | `github.com/exam-gaurd/client` | Student app — captures screen, streams frames. |
| `server/` | `github.com/exam-gaurd/server` | Teacher app — receives frames, runs the dashboard. |
| `scripts/` | — | `bundle-macos.sh` builds `.app` bundles with the correct TCC + Bonjour keys. |
| `docs/` | — | Design specs and implementation plans. |

> **Note:** the Go module paths use `exam-gaurd` (misspelled). Changing it requires updating both `go.mod` files and every import; nothing currently imports across modules, so it's cosmetic — but worth knowing before grepping.

## Building

### Prerequisites

**macOS:** Go 1.21+ (`brew install go`).

**Linux X11:**

```bash
sudo apt install libvulkan-dev libxkbcommon-x11-dev libx11-xcb-dev \
    libx11-dev libxext-dev libxdamage-dev
```

**Linux Wayland:** install one of `grim`, `gnome-screenshot`, or `spectacle` at runtime — no compile-time dependency.

**Windows cross-compile (from Linux):** MinGW (`x86_64-w64-mingw32-gcc`).

### Native build

```bash
# Teacher app
cd server && go build -o server . && ./server

# Student app
cd client && go build -o client . && ./client
```

### macOS app bundles

The bundler script generates the `Info.plist` with `NSScreenCaptureDescription`, `NSLocalNetworkUsageDescription`, and `NSBonjourServices=_examguard._tcp`, then ad-hoc codesigns the result:

```bash
./scripts/bundle-macos.sh client   # → dist/ExamMonitorStudent.app
./scripts/bundle-macos.sh server   # → dist/ExamMonitorTeacher.app
```

### Cross-compile Windows client (from Linux)

```bash
cd client
x86_64-w64-mingw32-windres app.rc -O coff -o app-res.o
GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
    go build -ldflags "-H=windowsgui -extldflags=-Wl,app-res.o" -o examgaurd.exe .
```

## Running

1. **Teacher:** launch `./server`, type the exam name, room, and token, click **Create Exam**.
2. **Each student:** launch `./client`, fill in student ID, name, room, and the token. Leave **Server IP** blank to auto-discover; click **Join**.
3. **Teacher:** click **Start Exam** — student tiles begin streaming.
4. During the exam, use **Lock All** (with an optional message), **Send Message**, **Unlock**, **Stop Exam**, and **Export Event Log** as needed.

Exam data is written under `<UserConfigDir>/exam-monitor/exams/<YYYY-MM-DD>_<slug>/`; CSV exports under `<UserConfigDir>/exam-monitor/exports/`.

## Testing

Each module is tested independently with race detection and coverage:

```bash
cd client && go test -race -cover ./...
cd server && go test -race -cover ./...
```

Pure-logic packages (`session`, `controlframe`, `eventlog`, `internal/diag`, `capture`) sit at 80–100 % coverage. UI glue (`client`, `server` top-level, `client/ui`) is not unit-tested because Gio rendering needs a live display; an end-to-end loopback test in `server/e2e_test.go` exercises the full join → start → lock → message → stop flow without one.

## Roadmap

Already shipped:

- Token-based join handshake.
- Session state machine and control-frame wire protocol.
- SQLite event log, CSV export, and 24 h retention reaper.
- Lock-all overlay, instructor-to-student messages, viewer audit log.

Planned (post-current):

- QR-code pairing for projector → student handoff.
- Per-platform `SetAlwaysOnTop` for the lock overlay (currently stubbed).
- Authenticode-signed Windows binaries; Developer-ID / notarised macOS bundles.
- Evidence signals: foreground window title, monitor count, idle time, RDP detection.
- TLS for the wire protocol.
- Federated multi-classroom view.

## Status

This project does not yet ship signed binaries or installers. Build from source per the steps above.
