// Package eventlog is the per-exam SQLite event log. One directory per
// exam, with events.sqlite + finals/<student-id>.jpg under it.
// See spec §7.
package eventlog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers as "sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp_utc   TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    student_id      TEXT,
    student_name    TEXT,
    details_json    TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_events_ts ON events (timestamp_utc);
CREATE INDEX IF NOT EXISTS idx_events_student ON events (student_id);

CREATE TABLE IF NOT EXISTS exam_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// Event is one row in the event log.
type Event struct {
	Timestamp   time.Time
	Type        string
	StudentID   string
	StudentName string
	Details     map[string]any
}

// EventLog wraps a SQLite handle. Safe for concurrent Record/All calls via
// the embedded mutex; the DB itself is also goroutine-safe under
// database/sql but the mutex prevents two writers from interleaving
// SetMeta + Record in confusing orders.
type EventLog struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens (creating if needed) the SQLite file at path and ensures the
// schema is present. Callers should defer Close().
func Open(path string) (*EventLog, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &EventLog{db: db}, nil
}

// Close releases the SQLite handle.
func (el *EventLog) Close() error {
	el.mu.Lock()
	defer el.mu.Unlock()
	return el.db.Close()
}

// Record appends a single event. Empty Details is stored as `{}`.
func (el *EventLog) Record(e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	body, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details: %w", err)
	}
	el.mu.Lock()
	defer el.mu.Unlock()
	_, err = el.db.Exec(
		`INSERT INTO events (timestamp_utc, event_type, student_id, student_name, details_json) VALUES (?, ?, ?, ?, ?)`,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		e.Type,
		nullableString(e.StudentID),
		nullableString(e.StudentName),
		string(body),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// All returns every event ordered by timestamp ascending.
func (el *EventLog) All() ([]Event, error) {
	el.mu.Lock()
	defer el.mu.Unlock()
	rows, err := el.db.Query(`SELECT timestamp_utc, event_type, COALESCE(student_id, ''), COALESCE(student_name, ''), details_json FROM events ORDER BY timestamp_utc ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var (
			ts, typ, sid, sname, details string
		)
		if err := rows.Scan(&ts, &typ, &sid, &sname, &details); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		parsedTS, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp %q: %w", ts, err)
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(details), &d); err != nil {
			return nil, fmt.Errorf("unmarshal details %q: %w", details, err)
		}
		out = append(out, Event{
			Timestamp:   parsedTS,
			Type:        typ,
			StudentID:   sid,
			StudentName: sname,
			Details:     d,
		})
	}
	return out, rows.Err()
}

// SetMeta writes (or overwrites) a key in the exam_meta table.
func (el *EventLog) SetMeta(key, value string) error {
	el.mu.Lock()
	defer el.mu.Unlock()
	_, err := el.db.Exec(
		`INSERT INTO exam_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set meta %s: %w", key, err)
	}
	return nil
}

// GetMeta reads a key from exam_meta. Returns ok=false if absent.
func (el *EventLog) GetMeta(key string) (string, bool) {
	el.mu.Lock()
	defer el.mu.Unlock()
	var v string
	err := el.db.QueryRow(`SELECT value FROM exam_meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return v, true
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
