//go:build linux

package ui

// SetAlwaysOnTop on Linux X11 would set _NET_WM_STATE_ABOVE. Phase 1
// returns ErrNotSupported; Wayland compositors usually refuse anyway.
func SetAlwaysOnTop(on bool) error { return ErrNotSupported }
