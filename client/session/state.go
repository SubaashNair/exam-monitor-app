// Package session is the client-side session state machine. Mirrors the
// server's authoritative state machine but only tracks the four states a
// client knows about. Implements the four mismatch recoveries from spec §6.5.
package session

import (
	"errors"
	"fmt"
	"sync"
)

type State int

const (
	StateWaiting State = iota
	StateCapturing
	StateLocked
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateWaiting:
		return "waiting"
	case StateCapturing:
		return "capturing"
	case StateLocked:
		return "locked"
	case StateStopped:
		return "stopped"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

type Event int

const (
	EventExamStart Event = iota
	EventExamStop
	EventLock
	EventUnlock
)

func (e Event) String() string {
	switch e {
	case EventExamStart:
		return "exam_start"
	case EventExamStop:
		return "exam_stop"
	case EventLock:
		return "lock"
	case EventUnlock:
		return "unlock"
	default:
		return fmt.Sprintf("unknown_event(%d)", e)
	}
}

var ErrIllegalTransition = errors.New("illegal state transition")

type StateMachine struct {
	mu           sync.RWMutex
	state        State
	lastRecovery bool // true if the most recent Apply applied a recovery
}

func NewStateMachine() *StateMachine {
	return &StateMachine{state: StateWaiting}
}

func (sm *StateMachine) State() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *StateMachine) IsCapturing() bool {
	s := sm.State()
	return s == StateCapturing || s == StateLocked
}

func (sm *StateMachine) LastRecovery() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastRecovery
}

// Apply handles the event. The four spec §6.5 recoveries are baked in:
//
//  1. LOCK from Waiting → recover to Capturing first, then apply Lock.
//  2. STOP from Locked  → force-unlock as part of the Stop transition (already
//     legal as Locked → Stopped in the table).
//  3. STOP from Waiting → accept the stop (no capture ever happened; client
//     just lands in Stopped).
//  4. UNLOCK from Capturing → no-op (already unlocked); return current state.
//
// Anything else is rejected with ErrIllegalTransition.
func (sm *StateMachine) Apply(evt Event) (State, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.lastRecovery = false

	switch {
	case sm.state == StateStopped:
		return sm.state, fmt.Errorf("%w: from stopped on %s", ErrIllegalTransition, evt)
	case evt == EventLock && sm.state == StateWaiting:
		sm.state = StateLocked
		sm.lastRecovery = true
		return sm.state, nil
	case evt == EventUnlock && sm.state == StateCapturing:
		// no-op recovery; we're already unlocked
		sm.lastRecovery = true
		return sm.state, nil
	case evt == EventExamStop && sm.state == StateWaiting:
		// recovery: a Waiting client receiving Stop just goes to Stopped.
		sm.state = StateStopped
		sm.lastRecovery = true
		return sm.state, nil
	}

	type key struct {
		from State
		evt  Event
	}
	table := map[key]State{
		{StateWaiting, EventExamStart}:  StateCapturing,
		{StateCapturing, EventLock}:     StateLocked,
		{StateLocked, EventUnlock}:      StateCapturing,
		{StateCapturing, EventExamStop}: StateStopped,
		{StateLocked, EventExamStop}:    StateStopped,
	}
	if next, ok := table[key{sm.state, evt}]; ok {
		sm.state = next
		return sm.state, nil
	}
	return sm.state, fmt.Errorf("%w: from %s on %s", ErrIllegalTransition, sm.state, evt)
}
