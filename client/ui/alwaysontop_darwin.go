//go:build darwin

package ui

// SetAlwaysOnTop is a best-effort no-op on macOS until we wire an
// objc bridge. Phase 1 returns ErrNotSupported and the dashboard tile
// shows the "🪟 not-on-top" badge; users hear about the limitation in
// release notes.
func SetAlwaysOnTop(on bool) error { return ErrNotSupported }
