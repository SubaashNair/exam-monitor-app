package capture

import "errors"

// Typed errors returned by Capturer.Capture() and New(). Surfaced to the user
// by the client's UI layer.
var (
	// ErrPermissionDenied: macOS Screen Recording permission is missing.
	// The capture API returns black frames silently; we detect this by
	// sampling pixels after the first frame.
	ErrPermissionDenied = errors.New("capture: permission denied")

	// ErrNoWaylandTool: running under Wayland but no supported screenshot
	// tool was found on $PATH (we try grim, gnome-screenshot, spectacle).
	ErrNoWaylandTool = errors.New("capture: no Wayland screenshot tool found")

	// ErrRDPSession: detected an RDP / remote session where capture
	// commonly returns black frames. Not strictly an error — we still try
	// to capture — but reported so the user knows.
	ErrRDPSession = errors.New("capture: RDP / remote session detected")

	// ErrNoDisplay: no display reachable (no DISPLAY/WAYLAND_DISPLAY,
	// or no monitors enumerable). Capture cannot proceed.
	ErrNoDisplay = errors.New("capture: no display available")
)
