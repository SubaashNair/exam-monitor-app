package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/session"
)

// TestSendMessage_ReachesFreshlyJoinedClient — FR-3 spec acceptance test.
//
// Simulates the race where:
//  1. Server has started an exam (state=Capturing).
//  2. A new client connects and is added to s.conns BUT its state-ack
//     hasn't been received yet, so rc.state is still Waiting.
//  3. Instructor clicks Send Message.
//
// Before v0.1.1 this dropped the message because BroadcastControl filtered
// to state in {Capturing, Locked}. v0.1.1 switched sendMessage to
// BroadcastControlAll. This test pins that behaviour so a future refactor
// can't silently regress it.
func TestSendMessage_ReachesFreshlyJoinedClient(t *testing.T) {
	// 1. Build the in-memory pair (server side of the connection plus a
	//    captured client-side pipe so we can read what the server writes).
	srvConn, cliConn := net.Pipe()
	defer srvConn.Close()
	defer cliConn.Close()

	srv := NewServer()
	// Pretend the student handshake completed but state-ack has not.
	srv.connsMu.Lock()
	srv.conns["S001"] = &registeredConn{
		writer: srvConn,
		state:  session.StateWaiting, // <- the key condition: NOT Capturing yet
	}
	srv.connsMu.Unlock()

	// 2. Broadcast a BroadcastMsg via BroadcastControlAll (the path
	//    sendMessage uses post-v0.1.1). net.Pipe is synchronous, so the
	//    write blocks until the reader consumes the data; run it in a
	//    goroutine so the read below can proceed concurrently.
	msg := controlframe.BroadcastMsg{From: "Instructor", Body: "five-minute warning"}
	broadcastErrCh := make(chan error, 1)
	go func() {
		broadcastErrCh <- srv.BroadcastControlAll(controlframe.TypeBroadcastMsg, msg)
	}()

	// 3. Read the frame from the client side of the pipe with a deadline.
	cliConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(cliConn, hdr); err != nil {
		t.Fatalf("read header: %v", err)
	}
	if !bytes.Equal(hdr[:2], []byte("HE")) {
		t.Fatalf("bad magic: %q", hdr[:2])
	}
	typ := binary.BigEndian.Uint16(hdr[2:4])
	if typ != controlframe.TypeBroadcastMsg {
		t.Fatalf("typ = %d, want TypeBroadcastMsg(%d)", typ, controlframe.TypeBroadcastMsg)
	}
	bodyLen := binary.BigEndian.Uint32(hdr[4:8])
	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(cliConn, body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	var got controlframe.BroadcastMsg
	if err := controlframe.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.Body != "five-minute warning" {
		t.Errorf("body = %q, want 'five-minute warning'", got.Body)
	}

	// Ensure the broadcast goroutine completed without error.
	if err := <-broadcastErrCh; err != nil {
		t.Errorf("BroadcastControlAll returned error: %v", err)
	}
}
