package main

import (
	"testing"

	"github.com/exam-gaurd/client/session"
)

func TestCaptureGate_NoCaptureInWaiting(t *testing.T) {
	c := NewClient()
	// Session is Waiting by default.
	if c.sessionState.State() != session.StateWaiting {
		t.Fatalf("initial state = %v, want waiting", c.sessionState.State())
	}
	// Capturer should not be initialised until EXAM_START.
	if c.capturer != nil {
		t.Errorf("capturer should be nil in Waiting state, got %T", c.capturer)
	}
}

func TestCaptureGate_IsCapturingAfterExamStart(t *testing.T) {
	c := NewClient()
	if _, err := c.sessionState.Apply(session.EventExamStart); err != nil {
		t.Fatalf("apply ExamStart: %v", err)
	}
	if !c.sessionState.IsCapturing() {
		t.Error("IsCapturing should be true after EXAM_START")
	}
}
