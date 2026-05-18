package eventlog

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestLog(t *testing.T) *EventLog {
	t.Helper()
	dir := t.TempDir()
	el, err := Open(filepath.Join(dir, "events.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = el.Close() })
	return el
}

func TestOpen_CreatesSchema(t *testing.T) {
	el := newTestLog(t)
	if err := el.Record(Event{
		Timestamp: time.Now().UTC(),
		Type:      "exam_created",
		Details:   map[string]any{"token": "BIOXQ7394", "port": 8080},
	}); err != nil {
		t.Fatalf("Record on fresh DB: %v", err)
	}
}

func TestRecord_RoundtripJSON(t *testing.T) {
	el := newTestLog(t)
	want := Event{
		Timestamp:   time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC),
		Type:        "student_joined",
		StudentID:   "S001",
		StudentName: "Aisha Rahman",
		Details:     map[string]any{"remote_addr": "192.168.1.50", "late_join": false},
	}
	if err := el.Record(want); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(All()) = %d, want 1", len(got))
	}
	if got[0].Type != want.Type || got[0].StudentID != want.StudentID || got[0].StudentName != want.StudentName {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
	if got[0].Details["remote_addr"] != "192.168.1.50" {
		t.Errorf("details lost JSON round-trip: %+v", got[0].Details)
	}
	if got[0].Details["late_join"] != false {
		t.Errorf("bool field lost JSON round-trip: %+v", got[0].Details)
	}
}

func TestAll_OrderedByTimestamp(t *testing.T) {
	el := newTestLog(t)
	base := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{3 * time.Second, 0, 1 * time.Second, 2 * time.Second} {
		if err := el.Record(Event{Timestamp: base.Add(offset), Type: "exam_started"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	got, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len(All()) = %d, want 4", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.Before(got[i-1].Timestamp) {
			t.Errorf("All() not ordered by timestamp at index %d: %v before %v", i, got[i].Timestamp, got[i-1].Timestamp)
		}
	}
}

func TestMeta_RoundtripExamData(t *testing.T) {
	el := newTestLog(t)
	if err := el.SetMeta("exam_name", "Biology Final"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := el.SetMeta("retention_until_utc", "2026-05-16T11:00:00Z"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	got, ok := el.GetMeta("exam_name")
	if !ok || got != "Biology Final" {
		t.Errorf("GetMeta(exam_name) = %q, ok=%v; want \"Biology Final\", true", got, ok)
	}
	if _, ok := el.GetMeta("missing_key"); ok {
		t.Error("GetMeta on missing key should return ok=false")
	}
}
