package session

import (
	"errors"
	"fmt"
	"sync"
)

// State is the server-side exam-session state. Locked is a substate of
// Capturing — IsCapturing() returns true for both. See spec §3.1.
type State int

const (
	StateIdle State = iota
	StateWaiting
	StateCapturing
	StateLocked
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
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

// Event triggers a state transition. Each event has zero or one valid
// (from-state, to-state) edge in the graph.
type Event int

const (
	EventCreateExam Event = iota // idle → waiting
	EventStartExam               // waiting → capturing
	EventLock                    // capturing → locked
	EventUnlock                  // locked → capturing
	EventStopExam                // capturing|locked → stopped
)

// ErrIllegalTransition is returned (wrapped) when Apply is called with an
// event that has no edge from the current state.
var ErrIllegalTransition = errors.New("illegal state transition")

// StateMachine is the authoritative server-side session-state holder. Safe
// for concurrent access via the embedded mutex.
type StateMachine struct {
	mu    sync.RWMutex
	state State
}

func NewStateMachine() *StateMachine {
	return &StateMachine{state: StateIdle}
}

// State returns the current state under a read lock.
func (sm *StateMachine) State() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

// IsCapturing returns true for both Capturing and Locked, since Locked is a
// substate where frames still flow. See spec §3.1.
func (sm *StateMachine) IsCapturing() bool {
	s := sm.State()
	return s == StateCapturing || s == StateLocked
}

// Apply attempts the given event and returns the new state. If the
// transition is not legal from the current state, returns
// ErrIllegalTransition wrapped with context.
func (sm *StateMachine) Apply(evt Event) (State, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	next, ok := transition(sm.state, evt)
	if !ok {
		return sm.state, fmt.Errorf("%w: from %s on %s", ErrIllegalTransition, sm.state, evt)
	}
	sm.state = next
	return sm.state, nil
}

func (e Event) String() string {
	switch e {
	case EventCreateExam:
		return "create_exam"
	case EventStartExam:
		return "start_exam"
	case EventLock:
		return "lock"
	case EventUnlock:
		return "unlock"
	case EventStopExam:
		return "stop_exam"
	default:
		return fmt.Sprintf("unknown_event(%d)", e)
	}
}

// transition encodes the legal (from, evt) → to edges. Returns ok=false
// for any pair not in the table.
func transition(from State, evt Event) (State, bool) {
	type key struct {
		from State
		evt  Event
	}
	table := map[key]State{
		{StateIdle, EventCreateExam}:    StateWaiting,
		{StateWaiting, EventStartExam}:  StateCapturing,
		{StateCapturing, EventLock}:     StateLocked,
		{StateLocked, EventUnlock}:      StateCapturing,
		{StateCapturing, EventStopExam}: StateStopped,
		{StateLocked, EventStopExam}:    StateStopped,
	}
	to, ok := table[key{from, evt}]
	return to, ok
}
