package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/session"
)

// packFrame builds an HE-header frame for tests. Uses the same wire format
// the client uses (HE + uint16 type + uint32 length + payload).
func packFrame(typ uint16, body []byte) []byte {
	buf := make([]byte, 8+len(body))
	copy(buf, "HE")
	binary.BigEndian.PutUint16(buf[2:4], typ)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(body)))
	copy(buf[8:], body)
	return buf
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func newConnectedServer(t *testing.T) (*Server, *net.TCPConn) {
	t.Helper()
	srv := NewServer()
	srv.isRunning.Store(true)
	t.Cleanup(func() { srv.isRunning.Store(false) })
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	// Listen on a random port for the test.
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	addr := listener.Addr().(*net.TCPAddr)
	dialerDone := make(chan *net.TCPConn, 1)
	go func() {
		conn, _ := net.DialTCP("tcp", nil, addr)
		dialerDone <- conn
	}()
	accepted, err := listener.AcceptTCP()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	t.Cleanup(func() { _ = accepted.Close() })

	go srv.handleStudent(accepted)

	c := <-dialerDone
	if c == nil {
		t.Fatal("dial failed")
	}
	t.Cleanup(func() { _ = c.Close() })
	return srv, c
}

type fakeStudentUtil struct {
	mu         sync.Mutex
	exists     map[string]bool
	imageCalls int
}

func (f *fakeStudentUtil) AddStudent(id, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists[id] = true
}
func (f *fakeStudentUtil) RemoveStudent(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.exists, id)
}
func (f *fakeStudentUtil) UpdateImage(_ string, _ image.Image) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imageCalls++
}
func (f *fakeStudentUtil) UpdateName(_ string, _ string) {}
func (f *fakeStudentUtil) isExists(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exists[id]
}

func TestHandshake_AcceptsValidToken(t *testing.T) {
	srv, conn := newConnectedServer(t)
	tok := srv.examSession.Token.Canonical()

	body := mustMarshal(t, controlframe.TokenHandshake{Token: tok})
	if _, err := conn.Write(packFrame(controlframe.TypeTokenHandshake, body)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	// Server should reply with TokenHandshakeOK.
	hdr := make([]byte, 8)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(hdr); err != nil {
		t.Fatalf("read reply header: %v", err)
	}
	typ := binary.BigEndian.Uint16(hdr[2:4])
	if typ != controlframe.TypeTokenHandshakeOK {
		t.Errorf("reply type = %d, want %d (OK)", typ, controlframe.TypeTokenHandshakeOK)
	}
}

func TestHandshake_RejectsInvalidToken(t *testing.T) {
	_, conn := newConnectedServer(t)

	body := mustMarshal(t, controlframe.TokenHandshake{Token: "WRONG-TOKEN-001"})
	if _, err := conn.Write(packFrame(controlframe.TypeTokenHandshake, body)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	hdr := make([]byte, 8)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(hdr); err != nil {
		t.Fatalf("read reply header: %v", err)
	}
	typ := binary.BigEndian.Uint16(hdr[2:4])
	if typ != controlframe.TypeTokenHandshakeReject {
		t.Errorf("reply type = %d, want %d (Reject)", typ, controlframe.TypeTokenHandshakeReject)
	}

	// Read body and verify reason.
	length := binary.BigEndian.Uint32(hdr[4:8])
	bodyOut := make([]byte, length)
	if _, err := conn.Read(bodyOut); err != nil {
		t.Fatalf("read reject body: %v", err)
	}
	var reject controlframe.TokenHandshakeReject
	if err := controlframe.Unmarshal(bodyOut, &reject); err != nil {
		t.Fatalf("unmarshal reject: %v", err)
	}
	if reject.Reason != controlframe.ReasonInvalid {
		t.Errorf("reason = %q, want %q", reject.Reason, controlframe.ReasonInvalid)
	}
}

func TestHandshake_RejectsBeforeAnyOtherFrame(t *testing.T) {
	_, conn := newConnectedServer(t)

	// Try to send NAME first. Server should close us without processing it.
	if _, err := conn.Write(packFrame(0, []byte("S001###Aisha"))); err != nil {
		// Write may succeed; that's OK — we're verifying the server's
		// behaviour, not the write.
		_ = err
	}

	// Server should send a TokenHandshakeReject and close. The exact bytes
	// we get back depend on timing; the key assertion is that no further
	// progress is allowed.
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	hdr := make([]byte, 8)
	n, _ := conn.Read(hdr)
	if n == 0 {
		// Server closed without reply — also acceptable.
		return
	}
	typ := binary.BigEndian.Uint16(hdr[2:4])
	if typ == 0 {
		t.Errorf("server accepted NAME before handshake (type=%d); should reject or close", typ)
	}
}

// Use bytes.NewReader once if the test file would otherwise build with unused imports.
var _ = bytes.NewReader
