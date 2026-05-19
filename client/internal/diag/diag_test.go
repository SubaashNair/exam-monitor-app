package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLogger(t *testing.T) *Logger {
	t.Helper()
	dir := t.TempDir()
	l, err := newAt(dir, "client-test")
	if err != nil {
		t.Fatalf("newAt: %v", err)
	}
	// On Windows, t.TempDir() cleanup fails ("file is being used by another
	// process") if the Logger's file handle is still open. Close it before
	// the cleanup phase runs.
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func TestRecoverPanic_WritesMarker(t *testing.T) {
	l := newTestLogger(t)

	func() {
		defer func() {
			// Outer recover catches the re-panic so the test process survives.
			if r := recover(); r == nil {
				t.Fatal("expected RecoverPanic to re-panic, got nil")
			}
		}()
		defer l.RecoverPanic("test_site")
		panic("boom")
	}()

	content, ok := l.LastCrash()
	if !ok {
		t.Fatalf("LastCrash returned false; marker should exist at %s", l.crashMarkerPath())
	}
	if !strings.Contains(content, "test_site") {
		t.Errorf("marker missing site name; got: %q", content)
	}
	if !strings.Contains(content, "boom") {
		t.Errorf("marker missing recovered value; got: %q", content)
	}
}

func TestLastCrash_AbsentReturnsFalse(t *testing.T) {
	l := newTestLogger(t)
	content, ok := l.LastCrash()
	if ok {
		t.Errorf("expected ok=false, got ok=true content=%q", content)
	}
}

func TestClearLastCrash_RemovesFile(t *testing.T) {
	l := newTestLogger(t)

	func() {
		defer func() { _ = recover() }()
		defer l.RecoverPanic("setup")
		panic("setup panic")
	}()

	if _, ok := l.LastCrash(); !ok {
		t.Fatal("marker should exist after setup panic")
	}

	if err := l.ClearLastCrash(); err != nil {
		t.Fatalf("ClearLastCrash: %v", err)
	}

	if _, ok := l.LastCrash(); ok {
		t.Error("expected marker to be gone after ClearLastCrash")
	}
}

func TestRotate_AboveThresholdMovesOldLog(t *testing.T) {
	dir := t.TempDir()
	component := "rotate-test"
	logPath := filepath.Join(dir, component+".log")

	// Pre-create a 2 MB log file (over the rotation threshold).
	bigContent := strings.Repeat("X", 2*1024*1024)
	if err := os.WriteFile(logPath, []byte(bigContent), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	l, err := newAt(dir, component)
	if err != nil {
		t.Fatalf("newAt: %v", err)
	}
	defer l.Close()

	rotated := logPath + ".1"
	if _, err := os.Stat(rotated); err != nil {
		t.Fatalf("rotated file should exist at %s: %v", rotated, err)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("fresh log should exist: %v", err)
	}
	if info.Size() >= int64(len(bigContent)) {
		t.Errorf("fresh log should be smaller than seeded content; size=%d", info.Size())
	}
}

func TestRotate_BelowThresholdKeepsFile(t *testing.T) {
	dir := t.TempDir()
	component := "rotate-skip"
	logPath := filepath.Join(dir, component+".log")

	small := []byte("tiny\n")
	if err := os.WriteFile(logPath, small, 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	l, err := newAt(dir, component)
	if err != nil {
		t.Fatalf("newAt: %v", err)
	}
	defer l.Close()

	if _, err := os.Stat(logPath + ".1"); !os.IsNotExist(err) {
		t.Errorf("did not expect rotation; .1 file present (err=%v)", err)
	}
}

func TestNewAt_CreatesDir(t *testing.T) {
	parent := t.TempDir()
	nested := filepath.Join(parent, "does-not-exist", "yet")
	l, err := newAt(nested, "client")
	if err != nil {
		t.Fatalf("newAt: %v", err)
	}
	defer l.Close()

	if _, err := os.Stat(nested); err != nil {
		t.Errorf("expected diag to create %s: %v", nested, err)
	}
}

func TestDefault_SetAndGet(t *testing.T) {
	prior := Default()
	t.Cleanup(func() { SetDefault(prior) })

	if got := Default(); got != prior {
		t.Fatalf("baseline mismatch: got %v want %v", got, prior)
	}

	l := newTestLogger(t)
	SetDefault(l)
	if Default() != l {
		t.Errorf("Default() did not return the logger we set")
	}

	SetDefault(nil)
	if Default() != nil {
		t.Errorf("Default() should be nil after SetDefault(nil)")
	}
}

func TestPackageRecoverPanic_UsesDefault(t *testing.T) {
	l := newTestLogger(t)
	prior := Default()
	SetDefault(l)
	t.Cleanup(func() { SetDefault(prior) })

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected re-panic from package-level RecoverPanic")
			}
		}()
		defer RecoverPanic("pkg_site")
		panic("via-package")
	}()

	content, ok := l.LastCrash()
	if !ok {
		t.Fatal("marker should exist after package-level RecoverPanic")
	}
	if !strings.Contains(content, "pkg_site") || !strings.Contains(content, "via-package") {
		t.Errorf("marker missing expected content: %q", content)
	}
}

func TestPackageRecoverPanic_NoDefaultFallsBackToStderr(t *testing.T) {
	prior := Default()
	SetDefault(nil)
	t.Cleanup(func() { SetDefault(prior) })

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected re-panic from package-level RecoverPanic without default")
			}
		}()
		defer RecoverPanic("no_default_site")
		panic("no-default")
	}()
}

func TestPackageRecoverPanic_NoPanicIsNoop(t *testing.T) {
	// Should not panic when there's nothing in flight.
	defer RecoverPanic("nothing")
}

func TestLogger_LogsAreReadableInFile(t *testing.T) {
	l := newTestLogger(t)
	l.Info("hello world", "key", "value")
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(l.dir, "client-test.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("log content missing message; got %q", data)
	}
	if !strings.Contains(string(data), "value") {
		t.Errorf("log content missing structured field; got %q", data)
	}
}
