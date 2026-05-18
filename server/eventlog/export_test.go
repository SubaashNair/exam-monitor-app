package eventlog

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportCSV_HeaderAndOrdering(t *testing.T) {
	el := newTestLog(t)
	base := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	must(el.Record(Event{Timestamp: base.Add(2 * time.Second), Type: "exam_started"}))
	must(el.Record(Event{Timestamp: base, Type: "exam_created", Details: map[string]any{"token": "BIOXQ7394", "port": 8080}}))
	must(el.Record(Event{Timestamp: base.Add(1 * time.Second), Type: "student_joined", StudentID: "S001", StudentName: "Aisha Rahman"}))

	out := filepath.Join(t.TempDir(), "log.csv")
	if err := el.ExportCSV(out); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	r := csv.NewReader(strings.NewReader(string(data)))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (header + 3), got %d", len(rows))
	}
	wantHeader := []string{"timestamp_utc", "event_type", "student_id", "student_name", "details"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, rows[0][i], h)
		}
	}
	// Ordering: exam_created, student_joined, exam_started.
	if rows[1][1] != "exam_created" || rows[2][1] != "student_joined" || rows[3][1] != "exam_started" {
		t.Errorf("rows not ordered: %v / %v / %v", rows[1][1], rows[2][1], rows[3][1])
	}
	if !strings.Contains(rows[1][4], `"token":"BIOXQ7394"`) {
		t.Errorf("details cell missing token JSON: %q", rows[1][4])
	}
}

func TestExportCSV_EmptyLogStillWritesHeader(t *testing.T) {
	el := newTestLog(t)
	out := filepath.Join(t.TempDir(), "empty.csv")
	if err := el.ExportCSV(out); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if !strings.HasPrefix(string(data), "timestamp_utc,event_type,student_id,student_name,details\n") {
		t.Errorf("empty log should still produce header row; got %q", string(data))
	}
}
