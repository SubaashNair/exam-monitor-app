// Package controlframe defines the new wire types (10–29) that extend the
// existing HE-header binary protocol with bidirectional control frames.
// Existing types 0 (NAME), 1 (MESSAGE), 2 (PICTURE) remain in server.go.
// All control-frame payloads are JSON.
package controlframe

import (
	"encoding/json"
	"fmt"
	"time"
)

// Server → client command types (10–19).
const (
	TypeExamStart            uint16 = 10
	TypeExamStop             uint16 = 11
	TypeLockScreen           uint16 = 12
	TypeUnlockScreen         uint16 = 13
	TypeBroadcastMsg         uint16 = 14
	TypeTokenHandshakeOK     uint16 = 15
	TypeTokenHandshakeReject uint16 = 16
)

// Client → server state-ack and handshake types (20–29).
const (
	TypeStateWaiting    uint16 = 20
	TypeStateCapturing  uint16 = 21
	TypeStateLocked     uint16 = 22
	TypeStateStopped    uint16 = 23
	TypeAckBroadcastMsg uint16 = 24
	TypeTokenHandshake  uint16 = 25
)

// Reason constants for TokenHandshakeReject.
const (
	ReasonInvalid      = "invalid"
	ReasonExpired      = "expired"
	ReasonRoomMismatch = "room_mismatch"
)

// ExamStart (type 10) — server tells client capture should begin.
type ExamStart struct {
	ExamName  string    `json:"exam_name"`
	StartedAt time.Time `json:"started_at_utc"`
}

// ExamStop (type 11) — server tells client capture should end.
type ExamStop struct {
	StoppedAt time.Time `json:"stopped_at_utc"`
}

// LockScreen (type 12) — server tells client to render the lock overlay.
type LockScreen struct {
	Message  string `json:"message_to_students"`
	LockedBy string `json:"locked_by"`
}

// UnlockScreen (type 13) — server tells client to release lock overlay.
type UnlockScreen struct{}

// BroadcastMsg (type 14) — instructor message. Empty TargetStudentID means
// class-wide (delivered to every Capturing client).
type BroadcastMsg struct {
	From            string `json:"from"`
	Body            string `json:"body"`
	TargetStudentID string `json:"target_student_id,omitempty"`
	MessageID       string `json:"message_id,omitempty"` // optional, paired with Ack
}

// IsBroadcastAll returns true when this message has no specific target.
func (b BroadcastMsg) IsBroadcastAll() bool { return b.TargetStudentID == "" }

// TokenHandshakeOK (type 15) — server's affirmative reply to a client's
// TokenHandshake.
type TokenHandshakeOK struct {
	ExamName string `json:"exam_name"`
	Started  bool   `json:"started"` // true if the exam is already in Capturing state
}

// TokenHandshakeReject (type 16) — server's negative reply. Reason is one
// of ReasonInvalid / ReasonExpired / ReasonRoomMismatch.
type TokenHandshakeReject struct {
	Reason string `json:"reason"`
}

// StateWaiting / StateCapturing / StateLocked / StateStopped (types 20–23)
// — client reports its current state. All have empty bodies; we still
// JSON-encode `{}` for forward compatibility.
type StateWaiting struct{}
type StateCapturing struct{}
type StateLocked struct{}
type StateStopped struct{}

// AckBroadcastMsg (type 24) — client confirms display of a message.
type AckBroadcastMsg struct {
	MessageID string `json:"message_id"`
}

// TokenHandshake (type 25) — client sends the token to authenticate. Server
// replies with TokenHandshakeOK or TokenHandshakeReject.
type TokenHandshake struct {
	Token string `json:"token"`
}

// Marshal encodes any control-frame payload as JSON.
func Marshal(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("controlframe marshal %T: %w", v, err)
	}
	return body, nil
}

// Unmarshal decodes a JSON payload into the given pointer.
func Unmarshal(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("controlframe unmarshal %T: %w", v, err)
	}
	return nil
}
