package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/session"
)

// finishHandshake performs the client side of the token handshake against
// the given socket. After this returns, the connection is ready for NAME
// + PICTURE / state-ack frames.
func finishHandshake(t *testing.T, conn net.Conn, token string) {
	t.Helper()
	body := mustMarshal(t, controlframe.TokenHandshake{Token: token})
	if _, err := conn.Write(packFrame(controlframe.TypeTokenHandshake, body)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	hdr := make([]byte, 8)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(hdr); err != nil {
		t.Fatalf("read handshake reply: %v", err)
	}
	length := binary.BigEndian.Uint32(hdr[4:8])
	if length > 0 {
		_, _ = conn.Read(make([]byte, length))
	}
	// drop the deadline
	conn.SetReadDeadline(time.Time{})
}

func TestStateAck_UpdatesRegisteredState(t *testing.T) {
	srv, conn := newConnectedServer(t)
	tok := srv.examSession.Token.Canonical()
	finishHandshake(t, conn, tok)

	// Send NAME first so the server registers under "S001".
	nameBody := []byte("S001###Aisha")
	if _, err := conn.Write(packFrame(0, nameBody)); err != nil {
		t.Fatalf("write NAME: %v", err)
	}
	// Send STATE_CAPTURING ack.
	if _, err := conn.Write(packFrame(controlframe.TypeStateCapturing, []byte("{}"))); err != nil {
		t.Fatalf("write state ack: %v", err)
	}

	// Give the server a moment to process.
	time.Sleep(100 * time.Millisecond)
	srv.connsMu.RLock()
	rc, ok := srv.conns["S001"]
	srv.connsMu.RUnlock()
	if !ok {
		t.Fatalf("S001 not registered")
	}
	if rc.state != session.StateCapturing {
		t.Errorf("state = %v, want capturing", rc.state)
	}
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{0, 128, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// TestFrameAcceptedDuringWaiting_RejectedAfterStop pins the FR-13 contract:
// frames are accepted during StateWaiting (lobby) and StateCapturing, but
// rejected once the exam session reaches StateStopped.
func TestFrameAcceptedDuringWaiting_RejectedAfterStop(t *testing.T) {
	srv, conn := newConnectedServer(t)
	tok := srv.examSession.Token.Canonical()
	finishHandshake(t, conn, tok)

	// Send NAME first so the server registers the student.
	if _, err := conn.Write(packFrame(0, []byte("S001###Aisha"))); err != nil {
		t.Fatalf("write NAME: %v", err)
	}

	// FR-13: send a PICTURE while server is still in Waiting state.
	// UpdateImage SHOULD be called — frames are no longer gated on Capturing.
	frame := tinyJPEG(t)
	if _, err := conn.Write(packFrame(2, frame)); err != nil {
		t.Fatalf("write PICTURE (waiting): %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	su := srv.studentUtil.(*fakeStudentUtil)
	su.mu.Lock()
	callsAfterWaiting := su.imageCalls
	su.mu.Unlock()
	if callsAfterWaiting < 1 {
		t.Errorf("UpdateImage called %d times during Waiting; want >=1 (FR-13)", callsAfterWaiting)
	}

	// Transition server to Stopped.
	if _, err := srv.examSession.Machine.Apply(session.EventStartExam); err != nil {
		t.Fatalf("apply ExamStart: %v", err)
	}
	if _, err := srv.examSession.Machine.Apply(session.EventStopExam); err != nil {
		t.Fatalf("apply ExamStop: %v", err)
	}

	// Send another PICTURE after Stop — should be rejected.
	su.mu.Lock()
	callsBeforeStop := su.imageCalls
	su.mu.Unlock()
	if _, err := conn.Write(packFrame(2, frame)); err != nil {
		t.Fatalf("write PICTURE (stopped): %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	su.mu.Lock()
	callsAfterStop := su.imageCalls
	su.mu.Unlock()
	if callsAfterStop != callsBeforeStop {
		t.Errorf("UpdateImage incremented after Stop: before=%d after=%d; want unchanged", callsBeforeStop, callsAfterStop)
	}
}
