package eventlog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ReapStats summarises one reaper pass.
type ReapStats struct {
	Reaped  int // dirs deleted
	Skipped int // dirs kept (not yet expired, or missing meta)
	Errors  int // dirs that failed to evaluate or delete
}

// ReapOnce inspects every exam directory under root and deletes the ones
// whose retention_until_utc has passed. Directories without that meta key
// are kept (cautious default; an exam still in progress won't have it).
//
// Per spec §7.4 — also run once at startup so an exam whose retention
// expired while the server was down is cleaned up.
func ReapOnce(root string) (ReapStats, error) {
	var stats ReapStats
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, fmt.Errorf("read %s: %w", root, err)
	}
	now := time.Now().UTC()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		examDir := filepath.Join(root, e.Name())
		sqlitePath := filepath.Join(examDir, "events.sqlite")
		until, ok := readRetentionUntil(sqlitePath)
		if !ok {
			stats.Skipped++
			continue
		}
		if now.Before(until) {
			stats.Skipped++
			continue
		}
		if err := os.RemoveAll(examDir); err != nil {
			slog.Warn("reaper: failed to delete expired exam", "dir", examDir, "err", err)
			stats.Errors++
			continue
		}
		stats.Reaped++
		slog.Info("reaper: exam purged", "dir", e.Name())
	}
	return stats, nil
}

// readRetentionUntil opens the SQLite file, reads
// exam_meta.retention_until_utc, returns (time, true) when present and
// parseable. All other paths return ok=false; the caller treats absent as
// "keep, cautious default."
func readRetentionUntil(path string) (time.Time, bool) {
	if _, err := os.Stat(path); err != nil {
		return time.Time{}, false
	}
	el, err := Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer el.Close()
	raw, ok := el.GetMeta("retention_until_utc")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// RunReaper starts a goroutine that runs ReapOnce once immediately and
// then on every tick of the interval, until ctx is cancelled.
func RunReaper(ctx context.Context, root string, interval time.Duration) {
	go func() {
		if _, err := ReapOnce(root); err != nil {
			slog.Warn("reaper: startup pass failed", "err", err)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := ReapOnce(root); err != nil {
					slog.Warn("reaper: tick failed", "err", err)
				}
			}
		}
	}()
}
