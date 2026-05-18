package eventlog

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ExportCSV writes the entire event log to a CSV file at the given path.
// Header columns: timestamp_utc, event_type, student_id, student_name, details.
// `details` is the events.details_json column verbatim (compact JSON).
func (el *EventLog) ExportCSV(path string) error {
	events, err := el.All()
	if err != nil {
		return fmt.Errorf("read events: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"timestamp_utc", "event_type", "student_id", "student_name", "details"}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, e := range events {
		details, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("marshal details for %s: %w", e.Type, err)
		}
		if err := w.Write([]string{
			e.Timestamp.UTC().Format(time.RFC3339Nano),
			e.Type,
			e.StudentID,
			e.StudentName,
			string(details),
		}); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	return nil
}
