//go:build windows

package ui

// SetAlwaysOnTop on Windows would call user32.SetWindowPos with
// HWND_TOPMOST. Phase 1 returns ErrNotSupported; revisit when we wire a
// helper. The dashboard tile already shows a not-on-top warning when this
// returns ErrNotSupported.
func SetAlwaysOnTop(on bool) error { return ErrNotSupported }
