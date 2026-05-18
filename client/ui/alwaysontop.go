package ui

import "errors"

// ErrNotSupported is returned when the running platform cannot toggle
// always-on-top (e.g. Wayland without compositor support).
var ErrNotSupported = errors.New("always-on-top not supported on this platform")

// AlwaysOnTopHandle is whatever the platform helper needs to identify a
// window. Implementations may keep it nil.
type AlwaysOnTopHandle struct {
	// Reserved for future use; pure-interface placeholder.
}
