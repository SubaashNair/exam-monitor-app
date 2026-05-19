// Package diag is a small diagnostics layer: a slog-based file logger with
// best-effort rotation, plus panic recovery that leaves a marker file the next
// launch can surface to the user.
package diag

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	rotateThreshold = 1 * 1024 * 1024 // 1 MB
	dirPermission   = 0o755
	filePermission  = 0o644
)

// Logger wraps slog with a file destination and a marker-file path used by
// RecoverPanic / LastCrash.
type Logger struct {
	*slog.Logger
	dir       string
	component string
	file      *os.File
	mu        sync.Mutex
}

// New opens (or creates) a Logger under the user's config dir:
//
//	macOS:   $HOME/Library/Application Support/exam-monitor/<component>.log
//	Linux:   $XDG_CONFIG_HOME/exam-monitor/<component>.log (fallback ~/.config)
//	Windows: %AppData%\exam-monitor\<component>.log
//
// component is a short identifier ("client", "server"). Errors here are
// non-fatal in spirit — callers should still launch the app, but log to stderr
// only when diag fails to come up.
func New(component string) (*Logger, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("user config dir: %w", err)
	}
	return newAt(filepath.Join(base, "exam-monitor"), component)
}

func newAt(dir, component string) (*Logger, error) {
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	logPath := filepath.Join(dir, component+".log")
	if err := rotateIfLarge(logPath); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePermission)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", logPath, err)
	}

	// Tee to stderr for WARN+ so terminal users still see problems. When
	// EXAM_MONITOR_DEBUG=1, drop the stderr threshold to DEBUG for live
	// troubleshooting from the terminal.
	stderrLevel := slog.LevelWarn
	if os.Getenv("EXAM_MONITOR_DEBUG") == "1" {
		stderrLevel = slog.LevelDebug
	}
	writer := io.MultiWriter(f, &levelFilteredStderr{minLevel: stderrLevel})
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return &Logger{
		Logger:    slog.New(handler).With("component", component),
		dir:       dir,
		component: component,
		file:      f,
	}, nil
}

// Close flushes and closes the underlying file. Safe to call multiple times.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func rotateIfLarge(logPath string) error {
	info, err := os.Stat(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", logPath, err)
	}
	if info.Size() < rotateThreshold {
		return nil
	}
	rotated := logPath + ".1"
	_ = os.Remove(rotated)
	return os.Rename(logPath, rotated)
}

// levelFilteredStderr is a minimal writer that only forwards lines whose slog
// level is >= minLevel. slog's text format prefixes records with `level=INFO`
// etc., so we can grep for that.
type levelFilteredStderr struct {
	minLevel slog.Level
}

func (w *levelFilteredStderr) Write(p []byte) (int, error) {
	if containsLevelAtLeast(p, w.minLevel) {
		return os.Stderr.Write(p)
	}
	return len(p), nil
}

func containsLevelAtLeast(line []byte, min slog.Level) bool {
	// slog text handler writes `level=DEBUG`, `level=INFO`, `level=WARN`,
	// `level=ERROR`. Honor all four so EXAM_MONITOR_DEBUG=1 actually surfaces
	// DEBUG records on stderr.
	for _, tag := range []string{"level=ERROR", "level=WARN", "level=INFO", "level=DEBUG"} {
		if bytesContains(line, tag) && levelOf(tag) >= min {
			return true
		}
	}
	return false
}

func bytesContains(haystack []byte, needle string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return len(needle) == 0
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func levelOf(tag string) slog.Level {
	switch tag {
	case "level=ERROR":
		return slog.LevelError
	case "level=WARN":
		return slog.LevelWarn
	case "level=INFO":
		return slog.LevelInfo
	case "level=DEBUG":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}
