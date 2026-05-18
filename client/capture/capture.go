// Package capture is the single seam between the client and platform-native
// screen capture. One implementation per platform; selected at init time via
// build tags and (on Linux) runtime detection of XDG_SESSION_TYPE.
//
// Callers should treat Capturer as the sole way to get a frame:
//
//	cap, err := capture.New()
//	img, err := cap.Capture()
//
// If New returns an error, it's typed (errors defined in errors.go) and tells
// the caller what to surface to the user (e.g. permission denied, no Wayland
// tool, RDP session).
package capture

import "image"

// Capturer captures the local screen as an image.Image. Implementations are
// not required to be safe for concurrent Capture() calls — the client uses one
// goroutine per Capturer, so we don't pay for locking we don't need.
type Capturer interface {
	Capture() (image.Image, error)
	Close() error
	// Diagnostics returns a one-line human-readable string with the backend
	// name and any pertinent platform info (session type, monitor count,
	// permission status). Logged at startup and on the first error.
	Diagnostics() string
}
