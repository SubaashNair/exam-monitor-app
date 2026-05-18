package controlframe

import (
	"strings"
	"testing"
	"time"
)

func TestExamStart_Roundtrip(t *testing.T) {
	want := ExamStart{ExamName: "Biology Final Term 1", StartedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)}
	body, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got ExamStart
	if err := Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ExamName != want.ExamName || !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLockScreen_Roundtrip(t *testing.T) {
	want := LockScreen{Message: "Please stop typing.", LockedBy: "Ms. Lim"}
	body, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got LockScreen
	if err := Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestBroadcastMsg_TargetAll(t *testing.T) {
	want := BroadcastMsg{From: "Ms. Lim", Body: "Five minutes left.", TargetStudentID: ""}
	body, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got BroadcastMsg
	if err := Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got.IsBroadcastAll() {
		t.Error("empty TargetStudentID should be class-wide broadcast")
	}
}

func TestTokenHandshake_Roundtrip(t *testing.T) {
	want := TokenHandshake{Token: "BIOXQ7394"}
	body, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got TokenHandshake
	if err := Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTokenHandshakeReject_KnownReasons(t *testing.T) {
	for _, r := range []string{ReasonInvalid, ReasonExpired, ReasonRoomMismatch} {
		body, err := Marshal(TokenHandshakeReject{Reason: r})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got TokenHandshakeReject
		if err := Unmarshal(body, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.Reason != r {
			t.Errorf("reason = %q, want %q", got.Reason, r)
		}
	}
}

func TestMarshal_Error(t *testing.T) {
	// channels cannot be JSON-marshaled; Marshal should return a wrapped error.
	_, err := Marshal(make(chan int))
	if err == nil {
		t.Fatal("expected Marshal to return error for un-marshalable type")
	}
	if !strings.Contains(err.Error(), "controlframe marshal") {
		t.Errorf("error missing prefix: %v", err)
	}
}

func TestUnmarshal_Error(t *testing.T) {
	// invalid JSON should yield a wrapped error.
	var v TokenHandshake
	err := Unmarshal([]byte("not json"), &v)
	if err == nil {
		t.Fatal("expected Unmarshal to return error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "controlframe unmarshal") {
		t.Errorf("error missing prefix: %v", err)
	}
}

func TestTypeConstants_AreInExpectedRanges(t *testing.T) {
	// Server → client commands: 10–19
	for _, ty := range []uint16{TypeExamStart, TypeExamStop, TypeLockScreen, TypeUnlockScreen, TypeBroadcastMsg, TypeTokenHandshakeOK, TypeTokenHandshakeReject} {
		if ty < 10 || ty > 19 {
			t.Errorf("server→client type %d out of 10–19", ty)
		}
	}
	// Client → server state acks: 20–29
	for _, ty := range []uint16{TypeStateWaiting, TypeStateCapturing, TypeStateLocked, TypeStateStopped, TypeAckBroadcastMsg, TypeTokenHandshake} {
		if ty < 20 || ty > 29 {
			t.Errorf("client→server type %d out of 20–29", ty)
		}
	}
}
