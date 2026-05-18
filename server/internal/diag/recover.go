package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync/atomic"
)

const crashMarkerFile = "last-crash.txt"

// defaultLogger holds the process-global Logger so package-level helpers like
// RecoverPanic can be used in places where threading a Logger through is
// awkward (top of main, goroutines fired from view callbacks, etc).
var defaultLogger atomic.Pointer[Logger]

// SetDefault registers a Logger as the package-global. Pass nil to clear.
func SetDefault(l *Logger) {
	defaultLogger.Store(l)
}

// Default returns the registered package-global Logger, or nil if none.
func Default() *Logger {
	return defaultLogger.Load()
}

// RecoverPanic is a package-level convenience wrapper for use in `defer
// diag.RecoverPanic("name")` when a Logger reference isn't conveniently in
// scope. Falls back to stderr if no default Logger has been registered.
func RecoverPanic(name string) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	if l := Default(); l != nil {
		l.recordPanic(name, r, stack)
	} else {
		fmt.Fprintf(os.Stderr, "panic in %s: %v\n%s\n", name, r, stack)
	}
	panic(r)
}

// RecoverPanic captures any in-flight panic in the calling goroutine, logs it
// with structured fields, writes a single-file crash marker the next launch
// can surface, then re-panics so the OS crash reporter still sees it.
func (l *Logger) RecoverPanic(name string) {
	r := recover()
	if r == nil {
		return
	}
	l.recordPanic(name, r, debug.Stack())
	panic(r)
}

func (l *Logger) recordPanic(name string, recovered any, stack []byte) {
	l.Error("panic recovered",
		"site", name,
		"recovered", fmt.Sprintf("%v", recovered),
	)
	// Stack traces are multi-line and unwieldy in slog text format — write
	// them separately to the marker file alongside a header.
	body := fmt.Sprintf("Panic in %s\nRecovered: %v\n\n%s\n", name, recovered, stack)
	_ = os.WriteFile(l.crashMarkerPath(), []byte(body), filePermission)
}

func (l *Logger) crashMarkerPath() string {
	return filepath.Join(l.dir, crashMarkerFile)
}

// LastCrash returns the content of the crash marker if one is present from a
// previous run.
func (l *Logger) LastCrash() (string, bool) {
	data, err := os.ReadFile(l.crashMarkerPath())
	if err != nil {
		return "", false
	}
	return string(data), true
}

// ClearLastCrash removes the marker. Safe to call when no marker exists.
func (l *Logger) ClearLastCrash() error {
	err := os.Remove(l.crashMarkerPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
