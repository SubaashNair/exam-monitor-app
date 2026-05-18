package eventlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedExam(t *testing.T, root, slug string, retentionUntil time.Time) string {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(filepath.Join(dir, "finals"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	el, err := Open(filepath.Join(dir, "events.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !retentionUntil.IsZero() {
		if err := el.SetMeta("retention_until_utc", retentionUntil.UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("SetMeta: %v", err)
		}
	}
	_ = el.Close()
	return dir
}

func TestReapOnce_DeletesExpiredKeepsFresh(t *testing.T) {
	root := t.TempDir()
	pastDir := seedExam(t, root, "2026-04-01_old", time.Now().UTC().Add(-1*time.Hour))
	futureDir := seedExam(t, root, "2026-05-15_fresh", time.Now().UTC().Add(1*time.Hour))
	missingMetaDir := seedExam(t, root, "2026-05-15_no_meta", time.Time{})

	stats, err := ReapOnce(root)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if stats.Reaped != 1 {
		t.Errorf("Reaped = %d, want 1", stats.Reaped)
	}
	if stats.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (future + missing-meta)", stats.Skipped)
	}

	if _, err := os.Stat(pastDir); !os.IsNotExist(err) {
		t.Errorf("past exam dir should be deleted; stat err = %v", err)
	}
	if _, err := os.Stat(futureDir); err != nil {
		t.Errorf("future exam dir should remain: %v", err)
	}
	if _, err := os.Stat(missingMetaDir); err != nil {
		t.Errorf("missing-meta dir should remain (cautious default): %v", err)
	}
}

func TestReapOnce_IgnoresFilesAtRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	stats, err := ReapOnce(root)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if stats.Reaped != 0 || stats.Errors != 0 {
		t.Errorf("stats = %+v; want zero reaped/errors when only files present", stats)
	}
}

func TestReapOnce_NonexistentRootIsNoError(t *testing.T) {
	stats, err := ReapOnce("/tmp/does_not_exist_exam_root_xyz")
	if err != nil {
		t.Fatalf("ReapOnce on missing root should not error: %v", err)
	}
	if stats.Reaped != 0 || stats.Errors != 0 || stats.Skipped != 0 {
		t.Errorf("stats = %+v; want all zeros for missing root", stats)
	}
}

func TestReapOnce_BadRetentionValueIsSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "bad_meta_exam")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	el, err := Open(filepath.Join(dir, "events.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Store a non-parseable timestamp.
	if err := el.SetMeta("retention_until_utc", "not-a-date"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	_ = el.Close()

	stats, err := ReapOnce(root)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	// Bad timestamp treated as missing → keep (cautious default).
	if stats.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 for unparseable retention value", stats.Skipped)
	}
	if stats.Reaped != 0 || stats.Errors != 0 {
		t.Errorf("stats = %+v; want only Skipped=1", stats)
	}
}

func TestReapOnce_NoSQLiteFileIsSkipped(t *testing.T) {
	root := t.TempDir()
	// Dir without events.sqlite — should be treated as missing meta → keep.
	if err := os.MkdirAll(filepath.Join(root, "exam_no_sqlite"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stats, err := ReapOnce(root)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if stats.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 for dir without events.sqlite", stats.Skipped)
	}
}

func TestRunReaper_StartsAndCancels(t *testing.T) {
	root := t.TempDir()
	// Seed one expired exam so the startup pass does real work.
	seedExam(t, root, "expired", time.Now().UTC().Add(-1*time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	// Use a very long interval so the ticker never fires in this test.
	RunReaper(ctx, root, 24*time.Hour)

	// Give the goroutine a moment to run the startup pass.
	time.Sleep(200 * time.Millisecond)
	cancel() // signal shutdown

	// The expired exam should have been cleaned up by the startup pass.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected root to be empty after startup reap; got %d entries", len(entries))
	}
}
