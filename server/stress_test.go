package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/session"
)

// validJPEG returns a real JPEG-encoded image (320x180 solid blue) that
// image.Decode will accept. Same content for every client.
func validJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	blue := color.RGBA{R: 30, G: 41, B: 200, A: 255}
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, blue)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

// stressFakeStudentUtil is a minimal StudentUtil that just counts calls.
type stressFakeStudentUtil struct {
	mu          sync.Mutex
	students    map[string]bool
	imageCalls  int64
	nameCalls   int64
	addCalls    int64
	removeCalls int64
}

func newStressFake() *stressFakeStudentUtil {
	return &stressFakeStudentUtil{students: make(map[string]bool)}
}

func (f *stressFakeStudentUtil) AddStudent(id, name string) {
	atomic.AddInt64(&f.addCalls, 1)
	f.mu.Lock()
	f.students[id] = true
	f.mu.Unlock()
}
func (f *stressFakeStudentUtil) RemoveStudent(id string) {
	atomic.AddInt64(&f.removeCalls, 1)
	f.mu.Lock()
	delete(f.students, id)
	f.mu.Unlock()
}
func (f *stressFakeStudentUtil) UpdateImage(id string, img image.Image) {
	atomic.AddInt64(&f.imageCalls, 1)
}
func (f *stressFakeStudentUtil) UpdateName(id string, name string) {
	atomic.AddInt64(&f.nameCalls, 1)
}
func (f *stressFakeStudentUtil) isExists(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.students[id]
}

// TestStressSameMachineWireProtocol — spin up the real Server.Start() on a
// high port, then spawn N concurrent fake clients on 127.0.0.1 that each
// complete the wire protocol end-to-end: TCP dial → TokenHandshake →
// receive TokenHandshakeOK → send NAME → loop sending PICTURE frames.
//
// What this proves:
//   - The TCP listener actually opens on the chosen port (no bind error).
//   - Multiple concurrent clients can complete the token handshake.
//   - Frames flow from clients to the server's StudentUtil.UpdateImage.
//
// What this DOESN'T touch:
//   - mDNS discovery
//   - UDP broadcast discovery
//   - The Gio UI
//
// If this test passes but real same-machine GUI testing fails, the bug is
// in discovery/UI, not in the wire protocol.
func TestStressSameMachineWireProtocol(t *testing.T) {
	const numClients = 5
	const framesPerClient = 20
	const port = 24680 // arbitrary high port

	// Set up server
	srv := NewServer()
	fake := newStressFake()
	srv.studentUtil = fake

	// Create an exam session and advance it to StateCapturing so PICTURE
	// frames are accepted by the v0.1.3 case-2 handler.
	es := session.NewExamSession("stress-test", port, time.Hour) // already in Waiting
	if _, err := es.Machine.Apply(session.EventStartExam); err != nil {
		t.Fatalf("could not advance to Capturing: %v", err)
	}
	es.MarkStarted()
	srv.SetExamSession(es)

	token := es.Token.Canonical()
	t.Logf("server token: %s, port: %d", token, port)

	// Start the server (opens listener)
	srv.Start(port)
	defer srv.Stop()

	// Give the listener a moment to actually bind
	time.Sleep(200 * time.Millisecond)

	// Sanity check — can we even reach the listener?
	probeConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("BASELINE: server TCP listener never bound on 127.0.0.1:%d: %v", port, err)
	}
	probeConn.Close()
	t.Logf("BASELINE: TCP listener confirmed bound on 127.0.0.1:%d", port)

	// Concurrent fake clients
	var (
		dialOK         atomic.Int64
		handshakeOK    atomic.Int64
		framesSent     atomic.Int64
		clientErrors   atomic.Int64
	)
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			studentID := fmt.Sprintf("stress-%d", id)

			// TCP dial
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
			if err != nil {
				t.Logf("client %d dial failed: %v", id, err)
				clientErrors.Add(1)
				return
			}
			defer conn.Close()
			dialOK.Add(1)

			// Token handshake
			handshakeBody, _ := json.Marshal(controlframe.TokenHandshake{Token: token})
			if err := sendStressFrame(conn, controlframe.TypeTokenHandshake, handshakeBody); err != nil {
				t.Logf("client %d handshake send failed: %v", id, err)
				clientErrors.Add(1)
				return
			}

			// Read handshake response
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			hdr := make([]byte, 8)
			if _, err := io.ReadFull(conn, hdr); err != nil {
				t.Logf("client %d response read failed: %v", id, err)
				clientErrors.Add(1)
				return
			}
			respType := binary.BigEndian.Uint16(hdr[2:4])
			respLen := binary.BigEndian.Uint32(hdr[4:8])
			respBody := make([]byte, respLen)
			io.ReadFull(conn, respBody)
			if respType != controlframe.TypeTokenHandshakeOK {
				t.Logf("client %d got non-OK response type=%d body=%s", id, respType, respBody)
				clientErrors.Add(1)
				return
			}
			handshakeOK.Add(1)
			conn.SetReadDeadline(time.Time{})

			// Send NAME frame (type 0)
			if err := sendStressFrame(conn, 0, []byte(studentID+"###Stress "+fmt.Sprintf("%d", id))); err != nil {
				t.Logf("client %d NAME failed: %v", id, err)
				clientErrors.Add(1)
				return
			}

			// Send PICTURE frames (type 2) — real JPEG so image.Decode succeeds
			realJPEG := validJPEG()
			for k := 0; k < framesPerClient; k++ {
				if err := sendStressFrame(conn, 2, realJPEG); err != nil {
					t.Logf("client %d frame %d send failed: %v", id, k, err)
					clientErrors.Add(1)
					return
				}
				framesSent.Add(1)
				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Give the server a brief moment to process the last frames
	time.Sleep(300 * time.Millisecond)

	imgCalls := atomic.LoadInt64(&fake.imageCalls)
	addCalls := atomic.LoadInt64(&fake.addCalls)

	t.Logf("=== STRESS TEST RESULTS ===")
	t.Logf("clients dialed OK:          %d / %d", dialOK.Load(), numClients)
	t.Logf("handshakes OK:              %d / %d", handshakeOK.Load(), numClients)
	t.Logf("AddStudent calls (server):  %d", addCalls)
	t.Logf("frames sent by clients:     %d (expected %d)", framesSent.Load(), numClients*framesPerClient)
	t.Logf("frames accepted by server:  %d (UpdateImage calls)", imgCalls)
	t.Logf("client errors:              %d", clientErrors.Load())

	// Pass criteria
	if dialOK.Load() < int64(numClients) {
		t.Errorf("only %d of %d clients dialed successfully", dialOK.Load(), numClients)
	}
	if handshakeOK.Load() < int64(numClients) {
		t.Errorf("only %d of %d handshakes succeeded", handshakeOK.Load(), numClients)
	}
	if imgCalls == 0 {
		t.Errorf("server processed 0 PICTURE frames")
	}
	// Note: server may not have processed every frame yet (network buffering),
	// but it should have processed AT LEAST half.
	if imgCalls < int64(numClients*framesPerClient/2) {
		t.Errorf("server only processed %d frames, expected at least %d", imgCalls, numClients*framesPerClient/2)
	}
}

// sendStressFrame builds + writes an HE-wire frame to conn.
func sendStressFrame(conn net.Conn, typ uint16, body []byte) error {
	buf := make([]byte, 8+len(body))
	copy(buf, "HE")
	binary.BigEndian.PutUint16(buf[2:4], typ)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(body)))
	copy(buf[8:], body)

	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := conn.Write(buf)
	return err
}
