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

func TestLateFrameRejected_WhenServerNotCapturing(t *testing.T) {
	srv, conn := newConnectedServer(t)
	tok := srv.examSession.Token.Canonical()
	finishHandshake(t, conn, tok)

	// Server is in Waiting (never started). Send NAME, then a PICTURE.
	if _, err := conn.Write(packFrame(0, []byte("S001###Aisha"))); err != nil {
		t.Fatalf("write NAME: %v", err)
	}
	frame := tinyJPEG(t)
	if _, err := conn.Write(packFrame(2, frame)); err != nil {
		t.Fatalf("write PICTURE: %v", err)
	}

	// Wait for the server to process. The student util's UpdateImage should
	// NOT have been called.
	time.Sleep(200 * time.Millisecond)
	su := srv.studentUtil.(*fakeStudentUtil)
	if su.imageCalls != 0 {
		t.Errorf("UpdateImage called %d times; want 0 when server is not Capturing", su.imageCalls)
	}
}
