package session

import (
	"errors"
	"testing"
	"time"
)

func TestState_String(t *testing.T) {
	for _, tt := range []struct {
		s    State
		want string
	}{
		{StateIdle, "idle"},
		{StateWaiting, "waiting"},
		{StateCapturing, "capturing"},
		{StateLocked, "locked"},
		{StateStopped, "stopped"},
	} {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestStateMachine_LegalTransitions(t *testing.T) {
	tests := []struct {
		name string
		from State
		evt  Event
		want State
	}{
		{"idle → waiting on create", StateIdle, EventCreateExam, StateWaiting},
		{"waiting → capturing on start", StateWaiting, EventStartExam, StateCapturing},
		{"capturing → locked on lock", StateCapturing, EventLock, StateLocked},
		{"locked → capturing on unlock", StateLocked, EventUnlock, StateCapturing},
		{"capturing → stopped on stop", StateCapturing, EventStopExam, StateStopped},
		{"locked → stopped on stop (force-unlock)", StateLocked, EventStopExam, StateStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := NewStateMachine()
			sm.state = tt.from
			got, err := sm.Apply(tt.evt)
			if err != nil {
				t.Fatalf("Apply(%v) from %v: unexpected error %v", tt.evt, tt.from, err)
			}
			if got != tt.want {
				t.Errorf("Apply(%v) from %v: got %v, want %v", tt.evt, tt.from, got, tt.want)
			}
			if sm.State() != tt.want {
				t.Errorf("State() returned %v, want %v", sm.State(), tt.want)
			}
		})
	}
}

func TestStateMachine_RejectsIllegalTransitions(t *testing.T) {
	tests := []struct {
		name string
		from State
		evt  Event
	}{
		{"start without create", StateIdle, EventStartExam},
		{"lock when waiting", StateWaiting, EventLock},
		{"stop when idle", StateIdle, EventStopExam},
		{"create when capturing", StateCapturing, EventCreateExam},
		{"unlock when capturing", StateCapturing, EventUnlock},
		{"stop after already stopped", StateStopped, EventStopExam},
		{"start after stopped", StateStopped, EventStartExam},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := NewStateMachine()
			sm.state = tt.from
			_, err := sm.Apply(tt.evt)
			if err == nil {
				t.Errorf("Apply(%v) from %v should have errored", tt.evt, tt.from)
			}
			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("err = %v; want errors.Is(err, ErrIllegalTransition)", err)
			}
			if sm.State() != tt.from {
				t.Errorf("state should not change on rejection: got %v, want %v", sm.State(), tt.from)
			}
		})
	}
}

func TestStateMachine_IsCapturingIncludesLocked(t *testing.T) {
	sm := NewStateMachine()
	if sm.IsCapturing() {
		t.Error("idle should not be capturing")
	}
	sm.state = StateCapturing
	if !sm.IsCapturing() {
		t.Error("capturing should be capturing")
	}
	sm.state = StateLocked
	if !sm.IsCapturing() {
		t.Error("locked should also be capturing (locked is a substate)")
	}
	sm.state = StateStopped
	if sm.IsCapturing() {
		t.Error("stopped should not be capturing")
	}
}

func TestExamSession_BasicLifecycle(t *testing.T) {
	es := NewExamSession("Biology Final Term 1", 8080, 4*time.Hour)
	if es.Name != "Biology Final Term 1" {
		t.Errorf("Name = %q, want %q", es.Name, "Biology Final Term 1")
	}
	if es.Port != 8080 {
		t.Errorf("Port = %d, want 8080", es.Port)
	}
	if es.Machine.State() != StateWaiting {
		t.Errorf("new ExamSession state = %s, want waiting", es.Machine.State())
	}
	if es.Token.Canonical() == "" {
		t.Error("token should be generated")
	}
	if es.RetentionUntil != (time.Time{}) {
		t.Error("RetentionUntil should be zero before Stop")
	}
}

func TestExamSession_StopSetsRetentionUntil(t *testing.T) {
	es := NewExamSession("Test", 8080, time.Hour)
	if _, err := es.Machine.Apply(EventStartExam); err != nil {
		t.Fatalf("StartExam: %v", err)
	}
	before := time.Now().UTC()
	if err := es.Stop(24 * time.Hour); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if es.Machine.State() != StateStopped {
		t.Errorf("state after Stop = %s, want stopped", es.Machine.State())
	}
	if !es.RetentionUntil.After(before) {
		t.Errorf("RetentionUntil %v not in the future relative to %v", es.RetentionUntil, before)
	}
	if es.StoppedAt == (time.Time{}) {
		t.Error("StoppedAt should be set")
	}
}

func TestExamSession_RegenerateToken(t *testing.T) {
	es := NewExamSession("Test", 8080, time.Hour)
	original := es.Token.Canonical()
	es.RegenerateToken(time.Hour)
	if es.Token.Canonical() == original {
		t.Error("RegenerateToken should produce a different canonical")
	}
}
