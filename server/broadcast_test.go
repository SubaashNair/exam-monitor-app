package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/session"
)

func TestBroadcastControl_SendsToAllRegistered(t *testing.T) {
	srv := NewServer()
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	// Set up two pipe-based connections to act as student sockets.
	s1, c1 := net.Pipe()
	s2, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = s1.Close()
		_ = c1.Close()
		_ = s2.Close()
		_ = c2.Close()
	})

	srv.registerConn("S001", &registeredConn{writer: s1})
	srv.registerConn("S002", &registeredConn{writer: s2})

	// Mark both as capturing so the broadcast picks them up.
	srv.setStudentState("S001", session.StateCapturing)
	srv.setStudentState("S002", session.StateCapturing)

	done := make(chan struct{}, 2)
	for _, reader := range []net.Conn{c1, c2} {
		go func(r net.Conn) {
			hdr := make([]byte, 8)
			r.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := r.Read(hdr); err == nil {
				typ := binary.BigEndian.Uint16(hdr[2:4])
				length := binary.BigEndian.Uint32(hdr[4:8])
				body := make([]byte, length)
				_, _ = r.Read(body)
				if typ == controlframe.TypeLockScreen {
					var ls controlframe.LockScreen
					if json.Unmarshal(body, &ls) == nil && ls.Message == "stop" {
						done <- struct{}{}
					}
				}
			}
		}(reader)
	}

	if err := srv.BroadcastControl(controlframe.TypeLockScreen, controlframe.LockScreen{Message: "stop", LockedBy: "Ms. Lim"}); err != nil {
		t.Fatalf("BroadcastControl: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-done:
			// got it
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/2 receivers got the lock frame", i)
		}
	}
}

func TestBroadcastControl_SkipsNonCapturing(t *testing.T) {
	srv := NewServer()
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	s1, c1 := net.Pipe()
	t.Cleanup(func() {
		_ = s1.Close()
		_ = c1.Close()
	})
	srv.registerConn("S001", &registeredConn{writer: s1})
	srv.setStudentState("S001", session.StateWaiting) // still waiting; broadcast should NOT reach

	if err := srv.BroadcastControl(controlframe.TypeLockScreen, controlframe.LockScreen{Message: "x"}); err != nil {
		t.Fatalf("BroadcastControl: %v", err)
	}

	c1.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := c1.Read(make([]byte, 8)); err == nil {
		t.Error("waiting student should not receive lock frame")
	}
}

func TestSendToStudent_DeliversOnlyToTarget(t *testing.T) {
	srv := NewServer()
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	s1, c1 := net.Pipe()
	s2, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = s1.Close()
		_ = c1.Close()
		_ = s2.Close()
		_ = c2.Close()
	})
	srv.registerConn("S001", &registeredConn{writer: s1})
	srv.registerConn("S002", &registeredConn{writer: s2})
	srv.setStudentState("S001", session.StateCapturing)
	srv.setStudentState("S002", session.StateCapturing)

	// Run SendToStudent in a goroutine so the synchronous net.Pipe() write
	// does not block: c1 must be reading concurrently for the write to complete.
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- srv.SendToStudent("S001", controlframe.TypeBroadcastMsg, controlframe.BroadcastMsg{Body: "hi"})
	}()

	// Read the full frame from c1 (header + body) so the pipe's Write unblocks.
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(c1, hdr); err != nil {
		t.Fatalf("expected delivery to S001 (header): %v", err)
	}
	length := binary.BigEndian.Uint32(hdr[4:8])
	body := make([]byte, length)
	if _, err := io.ReadFull(c1, body); err != nil {
		t.Fatalf("expected delivery to S001 (body): %v", err)
	}

	// Confirm SendToStudent itself returned without error.
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("SendToStudent: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendToStudent goroutine did not complete")
	}

	c2.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := c2.Read(make([]byte, 8)); err == nil {
		t.Error("S002 should not have received the targeted message")
	}
}
