package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/eventlog"
	"github.com/exam-gaurd/server/session"
)

// makeServer boots a real Server on a free localhost port, prepares an
// ExamSession in Waiting, attaches a fake event log to a temp dir, and
// returns (server, port, eventLog, exam-session).
func makeServer(t *testing.T) (*Server, int, *eventlog.EventLog, *session.ExamSession) {
	t.Helper()
	dir := t.TempDir()
	el, err := eventlog.Open(filepath.Join(dir, "events.sqlite"))
	if err != nil {
		t.Fatalf("eventlog open: %v", err)
	}
	t.Cleanup(func() { _ = el.Close() })

	su := &fakeStudentUtil{exists: map[string]bool{}}
	srv := NewServer()
	srv.studentUtil = su
	srv.SetEventLog(el)
	es := session.NewExamSession("E2E Test", 0, time.Hour)
	srv.SetExamSession(es)

	// Use a random port.
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	srv.isRunning.Store(true)
	go func() {
		for srv.isRunning.Load() {
			conn, err := listener.AcceptTCP()
			if err != nil {
				return
			}
			conn.SetKeepAlive(true)
			conn.SetNoDelay(true)
			go srv.handleStudent(conn)
		}
	}()
	t.Cleanup(func() {
		srv.isRunning.Store(false)
		_ = listener.Close()
	})

	return srv, port, el, es
}

func writeFrame(t *testing.T, conn net.Conn, typ uint16, body []byte) {
	t.Helper()
	frame := make([]byte, 8+len(body))
	copy(frame, "HE")
	binary.BigEndian.PutUint16(frame[2:4], typ)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(body)))
	copy(frame[8:], body)
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame type %d: %v", typ, err)
	}
}

func readFrame(t *testing.T, conn net.Conn, deadline time.Duration) (typ uint16, body []byte, ok bool) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(deadline))
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return 0, nil, false
	}
	typ = binary.BigEndian.Uint16(hdr[2:4])
	length := binary.BigEndian.Uint32(hdr[4:8])
	body = make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(conn, body); err != nil {
			return 0, nil, false
		}
	}
	return typ, body, true
}

func clientHandshake(t *testing.T, conn net.Conn, token string) {
	t.Helper()
	body, _ := json.Marshal(controlframe.TokenHandshake{Token: token})
	writeFrame(t, conn, controlframe.TypeTokenHandshake, body)
	typ, _, ok := readFrame(t, conn, 2*time.Second)
	if !ok {
		t.Fatal("handshake: no reply")
	}
	if typ != controlframe.TypeTokenHandshakeOK {
		t.Fatalf("handshake reply type = %d, want OK", typ)
	}
}

func tinyJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{byte(x * 64), byte(y * 64), 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func TestE2E_WaitingToCapturingToStopped(t *testing.T) {
	srv, port, el, es := makeServer(t)

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	clientHandshake(t, conn, es.Token.Canonical())

	// Send NAME and STATE_WAITING; verify no frames captured.
	writeFrame(t, conn, 0, []byte("S001###Aisha"))
	writeFrame(t, conn, controlframe.TypeStateWaiting, []byte("{}"))

	// Drain any incoming frames during waiting (server may broadcast nothing).
	go drain(conn) // best-effort sink

	// Send a PICTURE while still in Waiting — should be rejected.
	writeFrame(t, conn, 2, tinyJPEGBytes(t))
	time.Sleep(200 * time.Millisecond)
	if su := srv.studentUtil.(*fakeStudentUtil); imageCallCount(su) != 0 {
		t.Errorf("UpdateImage called %d times during Waiting; want 0", imageCallCount(su))
	}

	// Teacher clicks Start.
	if _, err := es.Machine.Apply(session.EventStartExam); err != nil {
		t.Fatalf("apply ExamStart: %v", err)
	}
	es.MarkStarted()
	_ = el.Record(eventlog.Event{Type: "exam_started"})
	_ = srv.BroadcastControlAll(controlframe.TypeExamStart, controlframe.ExamStart{ExamName: es.Name, StartedAt: es.StartedAt})

	// Give the server's connection-side loop a beat to absorb broadcast.
	time.Sleep(100 * time.Millisecond)

	// Client mirrors state — send STATE_CAPTURING and one PICTURE.
	writeFrame(t, conn, controlframe.TypeStateCapturing, []byte("{}"))
	writeFrame(t, conn, 2, tinyJPEGBytes(t))
	time.Sleep(200 * time.Millisecond)
	if su := srv.studentUtil.(*fakeStudentUtil); imageCallCount(su) < 1 {
		t.Errorf("UpdateImage called %d times during Capturing; want >=1", imageCallCount(su))
	}

	// Teacher clicks Stop.
	if err := es.Stop(24 * time.Hour); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	_ = el.Record(eventlog.Event{Type: "exam_stopped"})
	_ = el.SetMeta("retention_until_utc", es.RetentionUntil.Format(time.RFC3339))
	_ = srv.BroadcastControlAll(controlframe.TypeExamStop, controlframe.ExamStop{StoppedAt: es.StoppedAt})

	// Send STATE_STOPPED then attempt one more PICTURE — should not be
	// counted (server is Stopped).
	writeFrame(t, conn, controlframe.TypeStateStopped, []byte("{}"))
	su := srv.studentUtil.(*fakeStudentUtil)
	imagesBeforeLate := imageCallCount(su)
	writeFrame(t, conn, 2, tinyJPEGBytes(t))
	time.Sleep(200 * time.Millisecond)
	if got := imageCallCount(su); got != imagesBeforeLate {
		t.Errorf("UpdateImage incremented after Stop; want unchanged, got %d -> %d", imagesBeforeLate, got)
	}

	// Event log should contain exam_started + exam_stopped at minimum.
	events, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if !hasEvent(events, "exam_started") {
		t.Error("event log missing exam_started")
	}
	if !hasEvent(events, "exam_stopped") {
		t.Error("event log missing exam_stopped")
	}
	if until, ok := el.GetMeta("retention_until_utc"); !ok || until == "" {
		t.Errorf("retention_until_utc not set; got %q ok=%v", until, ok)
	}
}

func drain(conn net.Conn) {
	buf := make([]byte, 4096)
	for {
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		if _, err := conn.Read(buf); err != nil {
			return
		}
	}
}

func hasEvent(events []eventlog.Event, typ string) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// imageCallCount safely reads imageCalls under the fake's mutex.
func imageCallCount(su *fakeStudentUtil) int {
	su.mu.Lock()
	defer su.mu.Unlock()
	return su.imageCalls
}
