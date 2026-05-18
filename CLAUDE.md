# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

Real-time exam-monitoring tool. Students run [`client/`](client/) (Gio UI desktop app) which periodically captures the screen and streams JPEGs to a teacher running [`server/`](server/) (Gio UI desktop app). The server is discovered on the LAN via **manual IP entry → mDNS (`_examguard._tcp`) → UDP broadcast** in that order; the student connects over TCP.

Two **independent Go modules**, no workspace file:
- [`client/go.mod`](client/go.mod) → `github.com/exam-gaurd/client` (Go 1.24)
- [`server/go.mod`](server/go.mod) → `github.com/exam-gaurd/server` (Go 1.21, toolchain 1.24)

Note the module path typo `exam-gaurd` (not `guard`). Do not "fix" it without also updating both go.mod files and all imports — currently nothing imports across modules, so it's only cosmetic.

[`exam-client-mac/`](exam-client-mac/) is a **legacy prebuilt macOS .app bundle** with no source and an Info.plist missing `NSScreenCaptureDescription` (which is exactly why the bundled binary returns black frames on modern macOS). Use the **new** [`scripts/bundle-macos.sh`](scripts/bundle-macos.sh) instead — it generates a fresh `.app` from source with the correct TCC + LocalNetwork keys.

## Build & run

Both binaries are committed to the repo root of each module (`client/client`, `server/server`); rebuild from inside each module dir — there is no top-level Makefile.

```bash
# Server (teacher)
cd server && go build -o server . && ./server

# Client (student)
cd client && go build -o client . && ./client

# macOS .app bundle for the client (correct Info.plist with TCC keys, ad-hoc codesigned)
./scripts/bundle-macos.sh client   # → dist/ExamMonitorStudent.app

# macOS .app bundle for the server
./scripts/bundle-macos.sh server   # → dist/ExamMonitorTeacher.app

# Cross-compile Windows client (from Linux, requires MinGW)
cd client
x86_64-w64-mingw32-windres app.rc -O coff -o app-res.o
GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags "-H=windowsgui -extldflags=-Wl,app-res.o" -o examgaurd.exe .
```

Linux X11 build needs `libvulkan-dev libxkbcommon-x11-dev libx11-xcb-dev libx11-dev libxext-dev libxdamage-dev`. Linux Wayland users additionally need **one** of `grim` / `gnome-screenshot` / `spectacle` installed at runtime (the client picks one via `XDG_SESSION_TYPE` detection and `exec.LookPath`). macOS needs `brew install go`. Full prereqs in [README.md](README.md).

**Cross-compiling Gio apps from macOS to Linux/Windows requires platform C toolchains** (Gio uses CGO for Metal/Vulkan/D3D11). The `./capture/...` and `./internal/diag/...` subpackages have no Gio dependency and cross-compile cleanly (`GOOS=linux go build ./capture/...` works for syntax checks).

## Tests

Run tests per module:

```bash
cd client && go test -race -cover ./...   # diag 82.7%, capture 56.2%, top-level 4.6% (UI)
cd server && go test -race -cover ./...   # diag 82.7%, top-level 1.0% (UI)
```

Coverage on the top-level packages is intentionally low — they're mostly Gio rendering glue (not unit-testable without a display) and main wiring. The pure-logic packages (`internal/diag`, `capture`, and `discovery.go` / `header_test.go` at the top level) are well-covered. When adding logic that needs tests, put it in a subpackage or a pure helper so it's testable.

## ⚠️ README vs. actual code — they disagree

[README.md](README.md) describes an aspirational **compositor-based architecture** (CGO + XShm/PipeWire/DXGI/CGDisplayStream, MJPEG with dirty rectangles, 6 FPS, JPEG quality 45, wire types `0x01` keyframe / `0x02` dirty tiles). That work was **reverted** — see `git log --oneline` (the top ~6 commits are `Revert ...`).

The current `main` actually does:
- **Screen capture:** pure-Go `github.com/kbinani/screenshot` (no CGO compositor) in [client/client.go:164](client/client.go#L164).
- **Encoding:** full-frame JPEG, quality **60**, resized to 720px wide via `nfnt/resize` ([client/client.go:175](client/client.go#L175)). No dirty rectangles, no keyframes.
- **Rate:** one frame every **500 ms** (`UPDATE_INTERVAL` in [client/client.go:16](client/client.go#L16)) — i.e. ~2 FPS, not 6.
- **Wire types** ([client/client.go:18-21](client/client.go#L18) / [server/server.go:135](server/server.go#L135)):
  - `0` = NAME (`"<studentId>###<studentName>"`)
  - `1` = MESSAGE (text, server just `println`s it)
  - `2` = PICTURE (JPEG bytes)

  Server's `switch dataType` uses `0`, `1`, `default:` (treats anything non-0/1 as a picture) — so the README's `0x01`/`0x02` framing **does not apply**. When in doubt, trust the code.

The wire header itself **does** match the README: 8 bytes, `"HE"` magic + `uint16` type + `uint32` length, big-endian ([client/client.go:227](client/client.go#L227), [server/server.go:202](server/server.go#L202)). Max frame size enforced at 5 MB ([server/server.go:120](server/server.go#L120)).

## Architecture (what spans multiple files)

### Discovery + connection lifecycle

The client uses a three-tier resolver chain ([client/discovery.go](client/discovery.go)) — first hit wins:

1. **Manual IP** — typed into the JoinView "Server IP (optional)" field, parsed by `parseManualIP` (accepts `1.2.3.4`, `1.2.3.4:port`, `[::1]:port`). Persisted via `FormData.ServerIP` in [client/persistence.go](client/persistence.go).
2. **mDNS** — `_examguard._tcp.local.` via `github.com/grandcat/zeroconf`. Server registers via [server/mdns.go](server/mdns.go) on `Server.Start`, unregisters on `Stop`. macOS bundles must declare `NSBonjourServices=_examguard._tcp` and `NSLocalNetworkUsageDescription` (handled by [scripts/Info.plist.tmpl](scripts/Info.plist.tmpl)).
3. **UDP broadcast** — preserved for backward compatibility. Server `broadcastHost` UDP-broadcasts the literal bytes `"server"` to `255.255.255.255:<port>` every second ([server/server.go:185](server/server.go#L185)). Client listens on UDP `0.0.0.0:<port>` and uses the source IP.

After a successful connect, the IP is cached in `client.cachedServerIP` for fast reconnect on transient drops. The cache is **separate** from `manualServerIP` — the manual one persists across reconnect cycles; the cache is cleared on error.

After resolution, the client dials TCP, sends a NAME frame, then loops: `capture.Capturer.Capture()` → JPEG resize (720px wide, quality 60) → send PICTURE → sleep 500ms. Exponential retry with cap at 8s on connect failure.

### Reconnection grace period (subtle — read before changing student lifecycle)

The server tracks active connections in `Server.activeConns map[studentID]int64` (timestamp). On disconnect, `scheduleStudentRemoval` waits 5 s, then only removes the student if the stored timestamp still matches the disconnected connection's timestamp. A reconnect within 5 s overwrites the timestamp and the removal is suppressed. See [server/server.go:77-100](server/server.go#L77).

Why this matters: naive "remove on disconnect" causes the student's tile to flicker/duplicate during transient network drops. Don't simplify this without preserving the timestamp check.

### UI layer (both apps)

Both `main.go` files run the same Gio pattern: spawn a `app.Window` on a goroutine, run an event loop, switch between two screens via a string in `AppState.currentScreen`:
- **Client:** `"join"` → `"dashboard"` ([client/main.go:78](client/main.go#L78)).
- **Server:** `"home"` (room selection) → `"dashboard"` (grid of student tiles) ([server/main.go:78](server/main.go#L78)).

The server also runs a 250 ms `time.Ticker` that calls `w.Invalidate()` to drive repaints from the network goroutine ([server/main.go:71](server/main.go#L71)) — this is how new frames show up on screen without per-frame UI signaling. The client invalidates on demand via the `updateUI` callback passed into `client.Start`.

### `capture.Capturer` interface (decoupling client logic ↔ platform capture)

[client/capture/](client/capture/) is the single seam between `client.go` and the OS-native screenshot APIs. `capture.New()` is build-tagged per platform:

- **macOS** ([client/capture/macos.go](client/capture/macos.go)): kbinani/screenshot. **First-frame blank detection** in `isAllBlack` ([client/capture/blank.go](client/capture/blank.go)) — samples 16 pixels; if all are black on the first non-error frame, the macOS TCC permission is denied and we return `ErrPermissionDenied`. Without this, macOS streams black frames silently because CGDisplayCreateImage doesn't error when permission is missing.
- **Linux** ([client/capture/linux.go](client/capture/linux.go)): runtime detect via `XDG_SESSION_TYPE`. X11 → kbinani/screenshot. Wayland → tool shell-out (`grim` for wlroots, `gnome-screenshot` for GNOME, `spectacle` for KDE — first available wins, picked by [client/capture/linux_backend.go](client/capture/linux_backend.go)). No CGO PipeWire — keeps the build simple.
- **Windows** ([client/capture/windows.go](client/capture/windows.go)): kbinani/screenshot DXGI path. **Multi-monitor:** reads `EXAM_MONITOR_DISPLAY` env var (default 0, clamped to count). **RDP detection:** sniffs `SESSIONNAME` env — RDP sessions commonly return black; logged but not blocked.

`Capturer.Diagnostics()` returns a one-line string logged on first init via slog (e.g. `backend=macos displays=1 state=ok`). All errors are typed (see [client/capture/errors.go](client/capture/errors.go)) so the UI can show targeted messages instead of generic stack traces.

### `diag` package — panic recovery + crash markers

[client/internal/diag/](client/internal/diag/) and [server/internal/diag/](server/internal/diag/) are **duplicates** (no `go.work`; each module has its own copy). Provide:

- A `slog.TextHandler`-backed `Logger` writing to `<UserConfigDir>/exam-monitor/<component>.log`, rotated at 1 MB to `.log.1`. Tees WARN+ to stderr.
- `RecoverPanic(name string)` as both a method and a package-level helper (uses `Default()`). Logs the panic at `level=ERROR` with the recovered value, writes a single-file crash marker (`last-crash.txt`) under the same dir, then re-panics so the OS crash reporter still sees it.
- `LastCrash() (string, bool)` to read the marker on next launch.

Installed at every goroutine boundary that could panic: [client/main.go](client/main.go) (`client_main`, `client_window_goroutine`), [client/client.go](client/client.go) (`client_run_goroutine`), [server/main.go](server/main.go) (`server_main`, `server_window_goroutine`), [server/server.go](server/server.go) (`broadcast_host`, `tcp_accept_loop`, `handle_student`).

### Server `StudentUtil` interface (decoupling network ↔ UI)

`server.go` does not import the dashboard directly. It defines a small `StudentUtil` interface ([server/server.go:31](server/server.go#L31)) — `AddStudent`, `RemoveStudent`, `UpdateImage`, `UpdateName`, `isExists` — and `main.go` wires `server.studentUtil = dashboard` after both are constructed. When extending, implement against this interface from the UI side; don't reach into the server for student state.

### Image cache

[server/image_cache.go](server/image_cache.go) wraps Gio's `paint.ImageOp` per student with a timestamp; uploading the same `image.Image` pointer repeatedly is avoided by comparing `uintptr` identity. Touch this carefully — Gio re-uploads textures on identity change, so swapping the pointer too eagerly hurts FPS.

## Conventions specific to this repo

- **Logging** is mostly `println` or `log.Fatal` — there's no structured logger. Don't introduce one unless asked.
- **Concurrency:** `atomic.Bool` for run/connected flags; `sync.Mutex` around `activeConns` and `AppState.currentScreen`. The pattern is "atomic for hot booleans, mutex for maps/strings."
- **Error handling:** the network loops swallow most errors and just `break` to retry — they don't wrap. Wrapping is the global rule for new code, but matching the surrounding style here is reasonable for low-level retry loops.
- **String-based screen state** (`"join"`, `"dashboard"`, `"home"`) is the existing pattern. If you change it to typed constants, change both apps and watch the `default:` branch in `main.go` event switch.

## Things that look broken but aren't

- `swtichScreen` (typo for `switchScreen`) appears in both `main.go` files. Renaming is fine but it's not a bug.
- The protocol constant `MESSAGE = 1` in the client has no server-side handler beyond `println` — the chat/message channel was added but no UI wires it yet.
- `client/dashboard.go`'s `dashboard.client.Start(...)` doesn't return an error — the client logs internally. Errors surface via the `onError` callback set with `SetCallbacks`.
