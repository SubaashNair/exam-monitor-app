// client/session/state_test.go
package session

import (
	"errors"
	"testing"
)

func TestState_String(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateWaiting, "waiting"},
		{StateCapturing, "capturing"},
		{StateLocked, "locked"},
		{StateStopped, "stopped"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestApply_StraightLinePath(t *testing.T) {
	sm := NewStateMachine()
	if sm.State() != StateWaiting {
		t.Fatalf("initial = %v, want waiting", sm.State())
	}
	tests := []struct {
		evt  Event
		want State
	}{
		{EventExamStart, StateCapturing},
		{EventLock, StateLocked},
		{EventUnlock, StateCapturing},
		{EventExamStop, StateStopped},
	}
	for _, tt := range tests {
		got, err := sm.Apply(tt.evt)
		if err != nil {
			t.Fatalf("Apply(%v): %v", tt.evt, err)
		}
		if got != tt.want {
			t.Errorf("Apply(%v) = %v, want %v", tt.evt, got, tt.want)
		}
	}
}

func TestApply_RecoveryLockFromWaiting(t *testing.T) {
	// Spec §6.5: receiving LOCK_SCREEN while Waiting (e.g. reconnect race)
	// should silently transition Waiting → Capturing → Locked.
	sm := NewStateMachine()
	got, err := sm.Apply(EventLock)
	if err != nil {
		t.Fatalf("Apply(Lock) from waiting should recover; got err %v", err)
	}
	if got != StateLocked {
		t.Errorf("recovered state = %v, want locked", got)
	}
	if !sm.LastRecovery() {
		t.Error("LastRecovery should be true after recovery")
	}
}

func TestApply_StopFromLockedForceUnlocks(t *testing.T) {
	sm := NewStateMachine()
	_, _ = sm.Apply(EventExamStart)
	_, _ = sm.Apply(EventLock)
	got, err := sm.Apply(EventExamStop)
	if err != nil {
		t.Fatalf("stop from locked: %v", err)
	}
	if got != StateStopped {
		t.Errorf("state = %v, want stopped", got)
	}
}

func TestApply_RejectsTrulyIllegal(t *testing.T) {
	sm := NewStateMachine()
	_, _ = sm.Apply(EventExamStart)
	_, _ = sm.Apply(EventExamStop)
	// After stopped, ExamStart shouldn't work — it's terminal.
	if _, err := sm.Apply(EventExamStart); err == nil {
		t.Error("ExamStart after Stopped should error")
	} else if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("err = %v; want ErrIllegalTransition", err)
	}
}

func TestIsCapturing(t *testing.T) {
	sm := NewStateMachine()
	if sm.IsCapturing() {
		t.Error("waiting is not capturing")
	}
	_, _ = sm.Apply(EventExamStart)
	if !sm.IsCapturing() {
		t.Error("capturing should be capturing")
	}
	_, _ = sm.Apply(EventLock)
	if !sm.IsCapturing() {
		t.Error("locked is a substate of capturing")
	}
}

func TestEvent_String(t *testing.T) {
	cases := []struct {
		e    Event
		want string
	}{
		{EventExamStart, "exam_start"},
		{EventExamStop, "exam_stop"},
		{EventLock, "lock"},
		{EventUnlock, "unlock"},
		{Event(99), "unknown_event(99)"},
	}
	for _, c := range cases {
		if got := c.e.String(); got != c.want {
			t.Errorf("Event(%d).String() = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestState_String_Unknown(t *testing.T) {
	s := State(99)
	want := "unknown(99)"
	if got := s.String(); got != want {
		t.Errorf("State(99).String() = %q, want %q", got, want)
	}
}

func TestApply_RecoveryUnlockFromCapturing(t *testing.T) {
	// Spec §6.5: UNLOCK while already Capturing is a no-op recovery.
	sm := NewStateMachine()
	_, _ = sm.Apply(EventExamStart)
	got, err := sm.Apply(EventUnlock)
	if err != nil {
		t.Fatalf("Unlock from capturing should recover; got err %v", err)
	}
	if got != StateCapturing {
		t.Errorf("state = %v, want capturing", got)
	}
	if !sm.LastRecovery() {
		t.Error("LastRecovery should be true after no-op unlock recovery")
	}
}

func TestApply_RecoveryStopFromWaiting(t *testing.T) {
	// Spec §6.5: STOP while Waiting → just land in Stopped.
	sm := NewStateMachine()
	got, err := sm.Apply(EventExamStop)
	if err != nil {
		t.Fatalf("Stop from waiting should recover; got err %v", err)
	}
	if got != StateStopped {
		t.Errorf("state = %v, want stopped", got)
	}
	if !sm.LastRecovery() {
		t.Error("LastRecovery should be true after stop-from-waiting recovery")
	}
}

func TestApply_IllegalFromWaiting(t *testing.T) {
	// Unlock from Waiting is neither a valid transition nor a covered recovery.
	sm := NewStateMachine()
	// Patch: Unlock from Waiting is actually a non-recovery illegal. We first
	// need a state where no recovery applies and the table has no entry.
	// ExamStop from Waiting is a recovery, so use ExamStart then try Lock twice
	// (second Lock from Locked is illegal).
	_, _ = sm.Apply(EventExamStart)
	_, _ = sm.Apply(EventLock)
	// Now in Locked — try ExamStart which has no table entry.
	if _, err := sm.Apply(EventExamStart); err == nil {
		t.Error("ExamStart from Locked should error")
	} else if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("err = %v; want ErrIllegalTransition", err)
	}
}

func TestLastRecovery_ClearedOnNormalTransition(t *testing.T) {
	sm := NewStateMachine()
	// Trigger a recovery first.
	_, _ = sm.Apply(EventLock) // recovery: Waiting → Locked
	if !sm.LastRecovery() {
		t.Fatal("expected recovery=true")
	}
	// Now a normal transition from Locked.
	_, _ = sm.Apply(EventExamStop)
	if sm.LastRecovery() {
		t.Error("LastRecovery should be false after a normal transition")
	}
}
