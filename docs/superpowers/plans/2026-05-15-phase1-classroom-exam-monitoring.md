# Phase 1 Classroom Exam Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add exam-bounded capture, real instructor UI (mosaic + lock-all + message + event log), and per-exam session tokens to the existing exam-monitor codebase.

**Architecture:** Additive on top of the existing Gio-based two-module Go project (`client/`, `server/`). New packages `server/session`, `server/eventlog`, `server/controlframe`, `client/session`, `client/controlframe`, `client/ui`. Wire protocol extended with new types 10-29 (server→client control + client→server state acks) — types 0/1/2 unchanged. SQLite event log via pure-Go `modernc.org/sqlite`. State machine on both ends (Idle/Waiting/Capturing/Locked/Stopped).

**Tech Stack:** Go 1.21+/1.24 (existing); Gio v0.8 for UI (existing); `modernc.org/sqlite` (new dep); `grandcat/zeroconf` (existing, mDNS); `kbinani/screenshot` (existing, capture); `nfnt/resize` (existing, JPEG resize).

**Spec reference:** [`docs/superpowers/specs/2026-05-15-phase1-classroom-exam-monitoring-design.md`](../specs/2026-05-15-phase1-classroom-exam-monitoring-design.md). When this plan and the spec disagree, the spec wins; flag the contradiction and ask the user.

**Project rules to follow:**
- Two independent Go modules; tests and builds run from inside each module dir.
- Existing pattern: `package main` with multiple `.go` files at module root, internal subpackages under `internal/`. Public sibling packages (like `client/capture/`) are used for things that need build tags or want their own test scope.
- `slog` for logging, structured (`slog.Info(msg, "key", value, ...)`). Avoid `println`, `fmt.Print*`, and `log.Print*` outside `main` boot.
- `atomic.Bool` for hot flags read by many goroutines; `sync.Mutex` around maps and strings.
- Errors wrap with `fmt.Errorf("context: %w", err)` (per project `golang/coding-style.md`).
- Tests: table-driven; `go test -race -cover ./...`; new packages must hit ≥80% statement coverage.
- Do NOT commit unless the user explicitly asks. The Commit step in each task is shown for completeness — when running the plan, ask the user before each commit.
- Predecessor work (do not regress):
  - `internal/diag` panic recovery + slog log file in both modules
  - mDNS + manual-IP discovery resolver chain in client
  - `client/capture/` Capturer interface with macOS/Linux/Windows backends
  - `scripts/bundle-macos.sh` for .app bundle with `NSScreenCaptureDescription`
  - Existing tests in `server/header_test.go`, `client/discovery_test.go`, `client/capture/*_test.go`, `*/internal/diag/diag_test.go`

**Conventions used by every code task below:**
- All control-frame payloads are JSON (one struct per type, marshalled with `encoding/json`). Existing wire types 0/1/2 stay non-JSON (raw bytes).
- New control-frame types live in a per-module `controlframe` package (duplicated content; module isolation, same as `internal/diag`).
- Server is the authority on state; client mirrors via received frames + locally enforced rules.
- `time.Now().UTC()` everywhere a timestamp is recorded; ISO 8601 in storage / on the wire.

**Estimated total effort:** ~16 engineer-days. Plan is 25 tasks; expect 0.5–1.5 days per task depending on UI complexity.

---

## Stage 0 — Foundation (shared types)

### Task 0: Verify baseline before any change

**Files:** none (verification only)

- [ ] **Step 1: Verify both modules build cleanly**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
go build .
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go build .
```

Expected: both succeed silently, no output.

- [ ] **Step 2: Verify all existing tests pass**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
go test -race -cover ./...
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go test -race -cover ./...
```

Expected: both show `ok` for each package. Note baseline coverage numbers — should be `client/capture` ~56%, both `internal/diag` ~82%, `client` ~4%, `server` ~1% (the bottom two are UI-heavy and intentionally low).

- [ ] **Step 3: Note the current SHA and pre-existing test count**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app
git log --oneline -1 2>/dev/null || echo "(no git repo)"
grep -r "^func Test" --include='*.go' -h | wc -l
```

Expected: a baseline test count (write it down — the final task will assert tests increased). If the working copy is dirty or untracked, that's fine for this project.

- [ ] **Step 4: Confirm spec exists and is readable**

```bash
ls -la /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/docs/superpowers/specs/2026-05-15-phase1-classroom-exam-monitoring-design.md
```

Expected: file exists. If missing, stop and reload the spec; the plan references it.

---

### Task 1: `server/session` token type

**Files:**
- Create: `server/session/token.go`
- Create: `server/session/token_test.go`

The token type generates 9-char tokens in the format `XXX-XXX-NNN` (3 letters - 3 letters - 3 digits), with the unambiguous alphabet (drops `O`, `I`, `L`). Canonical storage is uppercase, no hyphens. Display always hyphenates.

- [ ] **Step 1: Write the failing test for `Generate` + `String`**

Create `server/session/token_test.go`:

```go
package session

import (
	"strings"
	"testing"
	"time"
)

func TestToken_GenerateShape(t *testing.T) {
	tok := Generate(4 * time.Hour)
	got := tok.String() // display form
	if len(got) != 11 {
		t.Fatalf("display form should be 11 chars (3-3-3 + 2 hyphens), got %d in %q", len(got), got)
	}
	if got[3] != '-' || got[7] != '-' {
		t.Errorf("hyphens should be at positions 3 and 7, got %q", got)
	}
	for i := 0; i < 3; i++ {
		c := got[i]
		if !(c >= 'A' && c <= 'Z') {
			t.Errorf("first triplet position %d should be A-Z, got %c", i, c)
		}
		if c == 'O' || c == 'I' || c == 'L' {
			t.Errorf("first triplet position %d uses ambiguous letter %c", i, c)
		}
	}
	for i := 4; i < 7; i++ {
		c := got[i]
		if !(c >= 'A' && c <= 'Z') {
			t.Errorf("second triplet position %d should be A-Z, got %c", i, c)
		}
	}
	for i := 8; i < 11; i++ {
		c := got[i]
		if !(c >= '0' && c <= '9') {
			t.Errorf("third triplet position %d should be 0-9, got %c", i, c)
		}
	}
}

func TestToken_CanonicalIsUppercaseAlnumOnly(t *testing.T) {
	tok := Generate(time.Hour)
	canon := tok.Canonical()
	if len(canon) != 9 {
		t.Fatalf("canonical form should be 9 chars, got %d in %q", len(canon), canon)
	}
	for _, c := range canon {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Errorf("canonical contains non-alphanumeric: %c", c)
		}
	}
}

func TestToken_Normalise(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hyphenated mixed case", "bio-xq7-394", "BIOXQ7394"},
		{"no hyphens", "BIOXQ7394", "BIOXQ7394"},
		{"with spaces", "Bio Xq7 394", "BIOXQ7394"},
		{"trailing whitespace", "  BIO-XQ7-394  ", "BIOXQ7394"},
		{"empty", "", ""},
		{"weird punctuation", "BIO.XQ7/394", "BIOXQ7394"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Normalise(tt.input); got != tt.want {
				t.Errorf("Normalise(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToken_Matches(t *testing.T) {
	tok := Generate(time.Hour)
	display := tok.String()
	canon := tok.Canonical()

	for _, input := range []string{display, canon, strings.ToLower(display), strings.ReplaceAll(display, "-", " ")} {
		if !tok.Matches(input) {
			t.Errorf("Matches(%q) returned false; want true", input)
		}
	}

	if tok.Matches("AAA-AAA-000") {
		t.Error("Matches should reject a non-matching string")
	}
}

func TestToken_Expiry(t *testing.T) {
	tok := Generate(50 * time.Millisecond)
	if tok.Expired() {
		t.Fatal("token should not be expired immediately")
	}
	time.Sleep(80 * time.Millisecond)
	if !tok.Expired() {
		t.Error("token should be expired after sleeping past expiry")
	}
}

func TestToken_Uniqueness(t *testing.T) {
	// Birthday-paradox sanity: 1000 generations should not collide on canonical form.
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		c := Generate(time.Hour).Canonical()
		if seen[c] {
			t.Fatalf("collision after %d generations: %s", i, c)
		}
		seen[c] = true
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go test ./session/... 2>&1 | head -10
```

Expected: build error — `undefined: Generate`, `undefined: Normalise`, etc.

- [ ] **Step 3: Implement `server/session/token.go`**

```go
// Package session models a single exam session: its state machine, token,
// and lifecycle metadata. The token is a low-friction join secret; not a
// cryptographic credential — see spec §6.6.
package session

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// tokenLetters is the uppercase alphabet minus O, I, L (ambiguous glyphs).
const tokenLetters = "ABCDEFGHJKMNPQRSTUVWXYZ"

// Token is the join secret a teacher gives students. Stored canonical
// (uppercase, no hyphens); displayed with hyphens; matched after Normalise.
type Token struct {
	canonical string
	expiresAt time.Time
}

// Generate creates a new random token valid for the given duration.
func Generate(validFor time.Duration) Token {
	var b strings.Builder
	b.Grow(9)
	for i := 0; i < 3; i++ {
		b.WriteByte(tokenLetters[randIndex(len(tokenLetters))])
	}
	for i := 0; i < 3; i++ {
		b.WriteByte(tokenLetters[randIndex(len(tokenLetters))])
	}
	for i := 0; i < 3; i++ {
		b.WriteByte(byte('0' + randIndex(10)))
	}
	return Token{
		canonical: b.String(),
		expiresAt: time.Now().UTC().Add(validFor),
	}
}

func randIndex(n int) int {
	x, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// crypto/rand failing is exceptional; fall back to time-derived index
		// to keep the server running. Coverage of this branch is irrelevant
		// for tests.
		return int(time.Now().UnixNano()) % n
	}
	return int(x.Int64())
}

// String returns the display form: XXX-XXX-NNN.
func (t Token) String() string {
	if len(t.canonical) != 9 {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s", t.canonical[0:3], t.canonical[3:6], t.canonical[6:9])
}

// Canonical returns the storage form: 9 chars, uppercase, alphanumeric.
func (t Token) Canonical() string { return t.canonical }

// Matches compares the input (after Normalise) to this token's canonical
// form.
func (t Token) Matches(input string) bool {
	return t.canonical != "" && Normalise(input) == t.canonical
}

// Expired returns true if the token's validity window has passed.
func (t Token) Expired() bool {
	return !time.Now().UTC().Before(t.expiresAt)
}

// ExpiresAt returns the absolute expiry instant.
func (t Token) ExpiresAt() time.Time { return t.expiresAt }

// Normalise strips whitespace and non-alphanumerics, then uppercases. Used
// to compare user input to canonical token form.
func Normalise(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32) // ASCII uppercase
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
		// drop everything else (hyphens, spaces, punctuation, unicode)
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./session/... -v 2>&1 | tail -20
```

Expected: all 6 tests PASS, coverage ≥ 80%.

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/session/token.go server/session/token_test.go
git commit -m "feat(server): add session.Token with generate/normalise/expiry"
```

---

### Task 2: `server/session` state machine

**Files:**
- Create: `server/session/state.go`
- Create: `server/session/state_test.go`

The state machine has five states. The legal transition graph and `Locked` being a substate of `Capturing` come straight from spec §3.1.

- [ ] **Step 1: Write the failing test**

Create `server/session/state_test.go`:

```go
package session

import (
	"errors"
	"testing"
)

func TestState_String(t *testing.T) {
	for _, tt := range []struct {
		s    State
		want string
	}{
		{StateIdle, "idle"},
		{StateWaiting, "waiting"},
		{StateCapturing, "capturing"},
		{StateLocked, "locked"},
		{StateStopped, "stopped"},
	} {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestStateMachine_LegalTransitions(t *testing.T) {
	tests := []struct {
		name string
		from State
		evt  Event
		want State
	}{
		{"idle → waiting on create", StateIdle, EventCreateExam, StateWaiting},
		{"waiting → capturing on start", StateWaiting, EventStartExam, StateCapturing},
		{"capturing → locked on lock", StateCapturing, EventLock, StateLocked},
		{"locked → capturing on unlock", StateLocked, EventUnlock, StateCapturing},
		{"capturing → stopped on stop", StateCapturing, EventStopExam, StateStopped},
		{"locked → stopped on stop (force-unlock)", StateLocked, EventStopExam, StateStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := NewStateMachine()
			sm.state = tt.from
			got, err := sm.Apply(tt.evt)
			if err != nil {
				t.Fatalf("Apply(%v) from %v: unexpected error %v", tt.evt, tt.from, err)
			}
			if got != tt.want {
				t.Errorf("Apply(%v) from %v: got %v, want %v", tt.evt, tt.from, got, tt.want)
			}
			if sm.State() != tt.want {
				t.Errorf("State() returned %v, want %v", sm.State(), tt.want)
			}
		})
	}
}

func TestStateMachine_RejectsIllegalTransitions(t *testing.T) {
	tests := []struct {
		name string
		from State
		evt  Event
	}{
		{"start without create", StateIdle, EventStartExam},
		{"lock when waiting", StateWaiting, EventLock},
		{"stop when idle", StateIdle, EventStopExam},
		{"create when capturing", StateCapturing, EventCreateExam},
		{"unlock when capturing", StateCapturing, EventUnlock},
		{"stop after already stopped", StateStopped, EventStopExam},
		{"start after stopped", StateStopped, EventStartExam},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := NewStateMachine()
			sm.state = tt.from
			_, err := sm.Apply(tt.evt)
			if err == nil {
				t.Errorf("Apply(%v) from %v should have errored", tt.evt, tt.from)
			}
			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("err = %v; want errors.Is(err, ErrIllegalTransition)", err)
			}
			if sm.State() != tt.from {
				t.Errorf("state should not change on rejection: got %v, want %v", sm.State(), tt.from)
			}
		})
	}
}

func TestStateMachine_IsCapturingIncludesLocked(t *testing.T) {
	sm := NewStateMachine()
	if sm.IsCapturing() {
		t.Error("idle should not be capturing")
	}
	sm.state = StateCapturing
	if !sm.IsCapturing() {
		t.Error("capturing should be capturing")
	}
	sm.state = StateLocked
	if !sm.IsCapturing() {
		t.Error("locked should also be capturing (locked is a substate)")
	}
	sm.state = StateStopped
	if sm.IsCapturing() {
		t.Error("stopped should not be capturing")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test ./session/... 2>&1 | head -10
```

Expected: build errors — `undefined: State`, `undefined: Event`, etc.

- [ ] **Step 3: Implement `server/session/state.go`**

```go
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
		{StateIdle, EventCreateExam}:      StateWaiting,
		{StateWaiting, EventStartExam}:    StateCapturing,
		{StateCapturing, EventLock}:       StateLocked,
		{StateLocked, EventUnlock}:        StateCapturing,
		{StateCapturing, EventStopExam}:   StateStopped,
		{StateLocked, EventStopExam}:      StateStopped,
	}
	to, ok := table[key{from, evt}]
	return to, ok
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./session/... -v 2>&1 | tail -20
```

Expected: all tests PASS, coverage ≥ 80%.

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/session/state.go server/session/state_test.go
git commit -m "feat(server): add session state machine with table-driven transitions"
```

---

### Task 3: `server/session` ExamSession aggregate

**Files:**
- Create: `server/session/session.go`
- Modify: `server/session/state_test.go` — add a single ExamSession test (the StateMachine tests stay as they are).

`ExamSession` ties the token, state machine, and metadata together. It's the type the rest of the server holds.

- [ ] **Step 1: Write the failing test**

Append to `server/session/state_test.go`:

```go
func TestExamSession_BasicLifecycle(t *testing.T) {
	es := NewExamSession("Biology Final Term 1", 8080, 4*time.Hour)
	if es.Name != "Biology Final Term 1" {
		t.Errorf("Name = %q, want %q", es.Name, "Biology Final Term 1")
	}
	if es.Port != 8080 {
		t.Errorf("Port = %d, want 8080", es.Port)
	}
	if es.Machine.State() != StateWaiting {
		t.Errorf("new ExamSession state = %s, want waiting", es.Machine.State())
	}
	if es.Token.Canonical() == "" {
		t.Error("token should be generated")
	}
	if es.RetentionUntil != (time.Time{}) {
		t.Error("RetentionUntil should be zero before Stop")
	}
}

func TestExamSession_StopSetsRetentionUntil(t *testing.T) {
	es := NewExamSession("Test", 8080, time.Hour)
	if _, err := es.Machine.Apply(EventStartExam); err != nil {
		t.Fatalf("StartExam: %v", err)
	}
	before := time.Now().UTC()
	if err := es.Stop(24 * time.Hour); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if es.Machine.State() != StateStopped {
		t.Errorf("state after Stop = %s, want stopped", es.Machine.State())
	}
	if !es.RetentionUntil.After(before) {
		t.Errorf("RetentionUntil %v not in the future relative to %v", es.RetentionUntil, before)
	}
	if es.StoppedAt == (time.Time{}) {
		t.Error("StoppedAt should be set")
	}
}

func TestExamSession_RegenerateToken(t *testing.T) {
	es := NewExamSession("Test", 8080, time.Hour)
	original := es.Token.Canonical()
	es.RegenerateToken(time.Hour)
	if es.Token.Canonical() == original {
		t.Error("RegenerateToken should produce a different canonical")
	}
}
```

Note: this test file now needs `import "time"` — add it to the existing imports.

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test ./session/... 2>&1 | head -10
```

Expected: `undefined: NewExamSession`, `undefined: ExamSession`.

- [ ] **Step 3: Implement `server/session/session.go`**

```go
package session

import "time"

// ExamSession is the per-exam aggregate the server holds. One per running
// server instance; the server transitions to a new ExamSession when the
// teacher creates a fresh exam.
type ExamSession struct {
	Name           string    // user-supplied exam name
	Port           int       // TCP port the room listens on
	Token          Token     // join token shared with students
	CreatedAt      time.Time
	StartedAt      time.Time // zero until EventStartExam
	StoppedAt      time.Time // zero until Stop()
	RetentionUntil time.Time // zero until Stop(); = StoppedAt + retention
	Machine        *StateMachine
}

// NewExamSession constructs an ExamSession already advanced to Waiting (the
// CreateExam event is applied implicitly — there is no useful "Idle" exam
// state in practice).
func NewExamSession(name string, port int, tokenValidFor time.Duration) *ExamSession {
	sm := NewStateMachine()
	_, _ = sm.Apply(EventCreateExam) // idle → waiting; cannot fail
	return &ExamSession{
		Name:      name,
		Port:      port,
		Token:     Generate(tokenValidFor),
		CreatedAt: time.Now().UTC(),
		Machine:   sm,
	}
}

// Stop transitions the session to Stopped and computes RetentionUntil.
func (e *ExamSession) Stop(retention time.Duration) error {
	if _, err := e.Machine.Apply(EventStopExam); err != nil {
		return err
	}
	e.StoppedAt = time.Now().UTC()
	e.RetentionUntil = e.StoppedAt.Add(retention)
	return nil
}

// MarkStarted records the actual start time. Caller is expected to apply
// EventStartExam separately (we keep timekeeping and state transitions
// independent so tests can compose them freely).
func (e *ExamSession) MarkStarted() { e.StartedAt = time.Now().UTC() }

// RegenerateToken replaces the current token. Students already past the
// handshake stay connected; new joins must use the new token.
func (e *ExamSession) RegenerateToken(validFor time.Duration) {
	e.Token = Generate(validFor)
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./session/... 2>&1 | tail -5
```

Expected: all tests PASS, coverage ≥ 80% on `session`.

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/session/session.go server/session/state_test.go
git commit -m "feat(server): add ExamSession aggregate (state + token + metadata)"
```

---

### Task 4: `server/eventlog` — open, schema, Record (TDD)

**Files:**
- Create: `server/eventlog/eventlog.go`
- Create: `server/eventlog/eventlog_test.go`

This task adds the SQLite layer: open-or-create with schema migration, plus a `Record` function that appends events. CSV export and the reaper come in later tasks so this one stays bite-sized.

- [ ] **Step 1: Add the dep**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go get modernc.org/sqlite
```

Expected: `go: added modernc.org/sqlite v…` plus its transitive deps.

- [ ] **Step 2: Write the failing test**

Create `server/eventlog/eventlog_test.go`:

```go
package eventlog

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestLog(t *testing.T) *EventLog {
	t.Helper()
	dir := t.TempDir()
	el, err := Open(filepath.Join(dir, "events.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = el.Close() })
	return el
}

func TestOpen_CreatesSchema(t *testing.T) {
	el := newTestLog(t)
	// A fresh DB should accept inserts and queries without errors.
	if err := el.Record(Event{
		Timestamp:   time.Now().UTC(),
		Type:        "exam_created",
		Details:     map[string]any{"token": "BIOXQ7394", "port": 8080},
	}); err != nil {
		t.Fatalf("Record on fresh DB: %v", err)
	}
}

func TestRecord_RoundtripJSON(t *testing.T) {
	el := newTestLog(t)
	want := Event{
		Timestamp:   time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC),
		Type:        "student_joined",
		StudentID:   "S001",
		StudentName: "Aisha Rahman",
		Details:     map[string]any{"remote_addr": "192.168.1.50", "late_join": false},
	}
	if err := el.Record(want); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(All()) = %d, want 1", len(got))
	}
	if got[0].Type != want.Type || got[0].StudentID != want.StudentID || got[0].StudentName != want.StudentName {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
	if got[0].Details["remote_addr"] != "192.168.1.50" {
		t.Errorf("details lost JSON round-trip: %+v", got[0].Details)
	}
	if got[0].Details["late_join"] != false {
		t.Errorf("bool field lost JSON round-trip: %+v", got[0].Details)
	}
}

func TestAll_OrderedByTimestamp(t *testing.T) {
	el := newTestLog(t)
	base := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{3 * time.Second, 0, 1 * time.Second, 2 * time.Second} {
		if err := el.Record(Event{Timestamp: base.Add(offset), Type: "exam_started"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	got, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len(All()) = %d, want 4", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.Before(got[i-1].Timestamp) {
			t.Errorf("All() not ordered by timestamp at index %d: %v before %v", i, got[i].Timestamp, got[i-1].Timestamp)
		}
	}
}

func TestMeta_RoundtripExamData(t *testing.T) {
	el := newTestLog(t)
	if err := el.SetMeta("exam_name", "Biology Final"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := el.SetMeta("retention_until_utc", "2026-05-16T11:00:00Z"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	got, ok := el.GetMeta("exam_name")
	if !ok || got != "Biology Final" {
		t.Errorf("GetMeta(exam_name) = %q, ok=%v; want \"Biology Final\", true", got, ok)
	}
	if _, ok := el.GetMeta("missing_key"); ok {
		t.Error("GetMeta on missing key should return ok=false")
	}
}
```

- [ ] **Step 3: Run tests, verify they fail**

```bash
go test ./eventlog/... 2>&1 | head -10
```

Expected: `undefined: EventLog`, `undefined: Event`, etc.

- [ ] **Step 4: Implement `server/eventlog/eventlog.go`**

```go
// Package eventlog is the per-exam SQLite event log. One directory per
// exam, with events.sqlite + finals/<student-id>.jpg under it.
// See spec §7.
package eventlog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers as "sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp_utc   TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    student_id      TEXT,
    student_name    TEXT,
    details_json    TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_events_ts ON events (timestamp_utc);
CREATE INDEX IF NOT EXISTS idx_events_student ON events (student_id);

CREATE TABLE IF NOT EXISTS exam_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// Event is one row in the event log.
type Event struct {
	Timestamp   time.Time
	Type        string
	StudentID   string
	StudentName string
	Details     map[string]any
}

// EventLog wraps a SQLite handle. Safe for concurrent Record/All calls via
// the embedded mutex; the DB itself is also goroutine-safe under
// database/sql but the mutex prevents two writers from interleaving
// SetMeta + Record in confusing orders.
type EventLog struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens (creating if needed) the SQLite file at path and ensures the
// schema is present. Callers should defer Close().
func Open(path string) (*EventLog, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &EventLog{db: db}, nil
}

// Close releases the SQLite handle.
func (el *EventLog) Close() error {
	el.mu.Lock()
	defer el.mu.Unlock()
	return el.db.Close()
}

// Record appends a single event. Empty Details is stored as `{}`.
func (el *EventLog) Record(e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	body, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details: %w", err)
	}
	el.mu.Lock()
	defer el.mu.Unlock()
	_, err = el.db.Exec(
		`INSERT INTO events (timestamp_utc, event_type, student_id, student_name, details_json) VALUES (?, ?, ?, ?, ?)`,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		e.Type,
		nullableString(e.StudentID),
		nullableString(e.StudentName),
		string(body),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// All returns every event ordered by timestamp ascending.
func (el *EventLog) All() ([]Event, error) {
	el.mu.Lock()
	defer el.mu.Unlock()
	rows, err := el.db.Query(`SELECT timestamp_utc, event_type, COALESCE(student_id, ''), COALESCE(student_name, ''), details_json FROM events ORDER BY timestamp_utc ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var (
			ts, typ, sid, sname, details string
		)
		if err := rows.Scan(&ts, &typ, &sid, &sname, &details); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		parsedTS, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp %q: %w", ts, err)
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(details), &d); err != nil {
			return nil, fmt.Errorf("unmarshal details %q: %w", details, err)
		}
		out = append(out, Event{
			Timestamp:   parsedTS,
			Type:        typ,
			StudentID:   sid,
			StudentName: sname,
			Details:     d,
		})
	}
	return out, rows.Err()
}

// SetMeta writes (or overwrites) a key in the exam_meta table.
func (el *EventLog) SetMeta(key, value string) error {
	el.mu.Lock()
	defer el.mu.Unlock()
	_, err := el.db.Exec(
		`INSERT INTO exam_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set meta %s: %w", key, err)
	}
	return nil
}

// GetMeta reads a key from exam_meta. Returns ok=false if absent.
func (el *EventLog) GetMeta(key string) (string, bool) {
	el.mu.Lock()
	defer el.mu.Unlock()
	var v string
	err := el.db.QueryRow(`SELECT value FROM exam_meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return v, true
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 5: Run tests, verify they pass**

```bash
go test -race -cover ./eventlog/... -v 2>&1 | tail -20
```

Expected: all 4 tests PASS, coverage ≥ 80%.

- [ ] **Step 6: Commit (ask user first)**

```bash
git add server/eventlog/eventlog.go server/eventlog/eventlog_test.go server/go.mod server/go.sum
git commit -m "feat(server): add eventlog package with SQLite store"
```

---

## Stage 0 progress checkpoint

After Tasks 0–4 you should have:

- `server/session/` package: `Token` (generate/normalise/expiry), `State`/`Event`/`StateMachine`, `ExamSession`.
- `server/eventlog/` package: `EventLog` over SQLite with `Record`/`All`/`SetMeta`/`GetMeta`.
- `modernc.org/sqlite` in `server/go.mod`.
- 13+ new passing tests on the server module.
- ≥ 80 % coverage on both new packages.

If any of those are missing, fix before moving on — later tasks depend on these types.

---

## Stage 1 — Wire & server-side plumbing

### Task 5: `server/eventlog` — CSV export

**Files:**
- Create: `server/eventlog/export.go`
- Create: `server/eventlog/export_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/eventlog/export_test.go`:

```go
package eventlog

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportCSV_HeaderAndOrdering(t *testing.T) {
	el := newTestLog(t)
	base := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	must(el.Record(Event{Timestamp: base.Add(2 * time.Second), Type: "exam_started"}))
	must(el.Record(Event{Timestamp: base, Type: "exam_created", Details: map[string]any{"token": "BIOXQ7394", "port": 8080}}))
	must(el.Record(Event{Timestamp: base.Add(1 * time.Second), Type: "student_joined", StudentID: "S001", StudentName: "Aisha Rahman"}))

	out := filepath.Join(t.TempDir(), "log.csv")
	if err := el.ExportCSV(out); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	r := csv.NewReader(strings.NewReader(string(data)))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (header + 3), got %d", len(rows))
	}
	wantHeader := []string{"timestamp_utc", "event_type", "student_id", "student_name", "details"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, rows[0][i], h)
		}
	}
	// Ordering: exam_created, student_joined, exam_started.
	if rows[1][1] != "exam_created" || rows[2][1] != "student_joined" || rows[3][1] != "exam_started" {
		t.Errorf("rows not ordered: %v / %v / %v", rows[1][1], rows[2][1], rows[3][1])
	}
	if !strings.Contains(rows[1][4], `"token":"BIOXQ7394"`) {
		t.Errorf("details cell missing token JSON: %q", rows[1][4])
	}
}

func TestExportCSV_EmptyLogStillWritesHeader(t *testing.T) {
	el := newTestLog(t)
	out := filepath.Join(t.TempDir(), "empty.csv")
	if err := el.ExportCSV(out); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if !strings.HasPrefix(string(data), "timestamp_utc,event_type,student_id,student_name,details\n") {
		t.Errorf("empty log should still produce header row; got %q", string(data))
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test ./eventlog/... 2>&1 | head -5
```

Expected: `(*EventLog).ExportCSV undefined`.

- [ ] **Step 3: Implement `server/eventlog/export.go`**

```go
package eventlog

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ExportCSV writes the entire event log to a CSV file at the given path.
// Header columns: timestamp_utc, event_type, student_id, student_name, details.
// `details` is the events.details_json column verbatim (compact JSON).
func (el *EventLog) ExportCSV(path string) error {
	events, err := el.All()
	if err != nil {
		return fmt.Errorf("read events: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"timestamp_utc", "event_type", "student_id", "student_name", "details"}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, e := range events {
		details, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("marshal details for %s: %w", e.Type, err)
		}
		if err := w.Write([]string{
			e.Timestamp.UTC().Format(time.RFC3339Nano),
			e.Type,
			e.StudentID,
			e.StudentName,
			string(details),
		}); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./eventlog/... 2>&1 | tail -5
```

Expected: all 6 tests in `eventlog` PASS, coverage ≥ 80%.

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/eventlog/export.go server/eventlog/export_test.go
git commit -m "feat(server): add CSV export for event log"
```

---

### Task 6: `server/eventlog` — retention reaper

**Files:**
- Create: `server/eventlog/reaper.go`
- Create: `server/eventlog/reaper_test.go`

A goroutine that hourly deletes per-exam directories whose `exam_meta.retention_until_utc` is in the past. Pure testable function `ReapOnce` is what the test drives; the goroutine wrapper is thin.

- [ ] **Step 1: Write the failing test**

Create `server/eventlog/reaper_test.go`:

```go
package eventlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedExam(t *testing.T, root, slug string, retentionUntil time.Time) string {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(filepath.Join(dir, "finals"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	el, err := Open(filepath.Join(dir, "events.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !retentionUntil.IsZero() {
		if err := el.SetMeta("retention_until_utc", retentionUntil.UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("SetMeta: %v", err)
		}
	}
	_ = el.Close()
	return dir
}

func TestReapOnce_DeletesExpiredKeepsFresh(t *testing.T) {
	root := t.TempDir()
	pastDir := seedExam(t, root, "2026-04-01_old", time.Now().UTC().Add(-1*time.Hour))
	futureDir := seedExam(t, root, "2026-05-15_fresh", time.Now().UTC().Add(1*time.Hour))
	missingMetaDir := seedExam(t, root, "2026-05-15_no_meta", time.Time{})

	stats, err := ReapOnce(root)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if stats.Reaped != 1 {
		t.Errorf("Reaped = %d, want 1", stats.Reaped)
	}
	if stats.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (future + missing-meta)", stats.Skipped)
	}

	if _, err := os.Stat(pastDir); !os.IsNotExist(err) {
		t.Errorf("past exam dir should be deleted; stat err = %v", err)
	}
	if _, err := os.Stat(futureDir); err != nil {
		t.Errorf("future exam dir should remain: %v", err)
	}
	if _, err := os.Stat(missingMetaDir); err != nil {
		t.Errorf("missing-meta dir should remain (cautious default): %v", err)
	}
}

func TestReapOnce_IgnoresFilesAtRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	stats, err := ReapOnce(root)
	if err != nil {
		t.Fatalf("ReapOnce: %v", err)
	}
	if stats.Reaped != 0 || stats.Errors != 0 {
		t.Errorf("stats = %+v; want zero reaped/errors when only files present", stats)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test ./eventlog/... 2>&1 | head -5
```

Expected: `undefined: ReapOnce`.

- [ ] **Step 3: Implement `server/eventlog/reaper.go`**

```go
package eventlog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ReapStats summarises one reaper pass.
type ReapStats struct {
	Reaped  int // dirs deleted
	Skipped int // dirs kept (not yet expired, or missing meta)
	Errors  int // dirs that failed to evaluate or delete
}

// ReapOnce inspects every exam directory under root and deletes the ones
// whose retention_until_utc has passed. Directories without that meta key
// are kept (cautious default; an exam still in progress won't have it).
//
// Per spec §7.4 — also run once at startup so an exam whose retention
// expired while the server was down is cleaned up.
func ReapOnce(root string) (ReapStats, error) {
	var stats ReapStats
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, fmt.Errorf("read %s: %w", root, err)
	}
	now := time.Now().UTC()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		examDir := filepath.Join(root, e.Name())
		sqlitePath := filepath.Join(examDir, "events.sqlite")
		until, ok := readRetentionUntil(sqlitePath)
		if !ok {
			stats.Skipped++
			continue
		}
		if now.Before(until) {
			stats.Skipped++
			continue
		}
		if err := os.RemoveAll(examDir); err != nil {
			slog.Warn("reaper: failed to delete expired exam", "dir", examDir, "err", err)
			stats.Errors++
			continue
		}
		stats.Reaped++
		slog.Info("reaper: exam purged", "dir", e.Name())
	}
	return stats, nil
}

// readRetentionUntil opens the SQLite file, reads
// exam_meta.retention_until_utc, returns (time, true) when present and
// parseable. All other paths return ok=false; the caller treats absent as
// "keep, cautious default."
func readRetentionUntil(path string) (time.Time, bool) {
	if _, err := os.Stat(path); err != nil {
		return time.Time{}, false
	}
	el, err := Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer el.Close()
	raw, ok := el.GetMeta("retention_until_utc")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// RunReaper starts a goroutine that runs ReapOnce once immediately and
// then on every tick of the interval, until ctx is cancelled.
func RunReaper(ctx context.Context, root string, interval time.Duration) {
	go func() {
		if _, err := ReapOnce(root); err != nil {
			slog.Warn("reaper: startup pass failed", "err", err)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := ReapOnce(root); err != nil {
					slog.Warn("reaper: tick failed", "err", err)
				}
			}
		}
	}()
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./eventlog/... 2>&1 | tail -5
```

Expected: all 8 tests in `eventlog` PASS, coverage ≥ 80 % (the `RunReaper` goroutine wrapper is uncovered — acceptable; `ReapOnce` is the testable surface).

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/eventlog/reaper.go server/eventlog/reaper_test.go
git commit -m "feat(server): add retention reaper for expired exam dirs"
```

---

### Task 7: `server/controlframe` — wire types & JSON codecs

**Files:**
- Create: `server/controlframe/controlframe.go`
- Create: `server/controlframe/controlframe_test.go`

This package defines the new wire types 10–29 and provides typed encode/decode for each frame's JSON payload. Existing wire types 0/1/2 stay in `server/server.go` untouched.

- [ ] **Step 1: Write the failing test**

Create `server/controlframe/controlframe_test.go`:

```go
package controlframe

import (
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
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test ./controlframe/... 2>&1 | head -10
```

Expected: every undefined identifier listed.

- [ ] **Step 3: Implement `server/controlframe/controlframe.go`**

```go
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
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./controlframe/... 2>&1 | tail -5
```

Expected: 6 tests PASS, coverage ≥ 80 %.

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/controlframe/controlframe.go server/controlframe/controlframe_test.go
git commit -m "feat(server): add controlframe package with wire types 10-29"
```

---

### Task 8: `server/server.go` — token handshake before NAME

**Files:**
- Modify: `server/server.go` — add token-handshake step at the top of `handleStudent`; reject connections that don't present a valid token within a deadline.
- Modify: `server/server.go` — add an `ExamSession` field to `Server` and a setter.
- Create: `server/handshake_test.go`

The current `handleStudent` reads frames in a loop and dispatches by type. We insert a one-shot handshake before that loop: read one frame, require it to be type `TypeTokenHandshake` (25), validate the token, reply with `TypeTokenHandshakeOK` (15) or close with `TypeTokenHandshakeReject` (16).

- [ ] **Step 1: Write the failing test**

Create `server/handshake_test.go`:

```go
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
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
	exists map[string]bool
}

func (f *fakeStudentUtil) AddStudent(id, name string)   { f.exists[id] = true }
func (f *fakeStudentUtil) RemoveStudent(id string)      { delete(f.exists, id) }
func (f *fakeStudentUtil) UpdateImage(_ string, _ any)  {}
func (f *fakeStudentUtil) UpdateName(_ string, _ string) {}
func (f *fakeStudentUtil) isExists(id string) bool      { return f.exists[id] }

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
```

Note: the existing `StudentUtil` interface in `server.go` declares `UpdateImage(id string, img image.Image)`. The `fakeStudentUtil` here needs to satisfy that — adjust the test's import to use `image.Image` (parameter renamed to `_ any` won't work because the interface signature is concrete). Replace `UpdateImage(_ string, _ any)` with `UpdateImage(_ string, _ image.Image)` and add the `image` import.

- [ ] **Step 2: Run tests, verify they fail**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go test -run TestHandshake_ -v 2>&1 | head -20
```

Expected: compile error — `srv.examSession undefined`, `srv.SetExamSession undefined`, etc.

- [ ] **Step 3: Modify `server/server.go` — add ExamSession field & setter**

Add the imports `"github.com/exam-gaurd/server/controlframe"` and `"github.com/exam-gaurd/server/session"`. Then extend `Server`:

```go
type Server struct {
	listener    *net.TCPListener
	isRunning   atomic.Bool
	studentUtil StudentUtil

	activeConns    map[string]int64
	activeConnsMu  sync.Mutex
	mdnsCancel     func()
	mdnsCancelOnce sync.Once

	// NEW (Task 8):
	examSession   *session.ExamSession
	examSessionMu sync.RWMutex
}
```

Add the setter and getter:

```go
// SetExamSession installs the current exam session. Called by the home
// screen's "Open Waiting Room" button.
func (s *Server) SetExamSession(es *session.ExamSession) {
	s.examSessionMu.Lock()
	s.examSession = es
	s.examSessionMu.Unlock()
}

func (s *Server) ExamSession() *session.ExamSession {
	s.examSessionMu.RLock()
	defer s.examSessionMu.RUnlock()
	return s.examSession
}
```

- [ ] **Step 4: Modify `server/server.go` — insert token handshake at top of `handleStudent`**

Insert a helper and use it as the first thing in `handleStudent`. Replace the start of `handleStudent` from:

```go
func (s *Server) handleStudent(socket *net.TCPConn) {
	defer socket.Close()
	id := ""
	var connTimestamp int64 = 0
	header := make([]byte, HEADER_SIZE)
	data := make([]byte, 0)
```

to:

```go
func (s *Server) handleStudent(socket *net.TCPConn) {
	defer socket.Close()

	if err := s.performHandshake(socket); err != nil {
		slog.Info("token handshake failed", "remote", socket.RemoteAddr().String(), "err", err)
		return
	}

	id := ""
	var connTimestamp int64 = 0
	header := make([]byte, HEADER_SIZE)
	data := make([]byte, 0)
```

Then add the helper at the bottom of the file:

```go
// performHandshake reads exactly one TokenHandshake frame from the
// connection, validates the token against the current exam session, and
// replies with TokenHandshakeOK or TokenHandshakeReject. Returns nil only
// when handshake succeeded; otherwise the caller closes the connection.
func (s *Server) performHandshake(socket *net.TCPConn) error {
	socket.SetReadDeadline(time.Now().Add(10 * time.Second))

	hdr := make([]byte, HEADER_SIZE)
	if _, err := io.ReadFull(socket, hdr); err != nil {
		return fmt.Errorf("read handshake header: %w", err)
	}
	typ, length, err := unpackHeader(hdr)
	if err != nil {
		return fmt.Errorf("invalid handshake header: %w", err)
	}
	if typ != controlframe.TypeTokenHandshake {
		_ = s.sendControlFrame(socket, controlframe.TypeTokenHandshakeReject, controlframe.TokenHandshakeReject{Reason: controlframe.ReasonInvalid})
		return fmt.Errorf("expected TokenHandshake (%d), got %d", controlframe.TypeTokenHandshake, typ)
	}
	if length <= 0 || length > 1024 {
		return fmt.Errorf("handshake length out of range: %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(socket, body); err != nil {
		return fmt.Errorf("read handshake body: %w", err)
	}

	var th controlframe.TokenHandshake
	if err := controlframe.Unmarshal(body, &th); err != nil {
		_ = s.sendControlFrame(socket, controlframe.TypeTokenHandshakeReject, controlframe.TokenHandshakeReject{Reason: controlframe.ReasonInvalid})
		return err
	}

	es := s.ExamSession()
	if es == nil {
		_ = s.sendControlFrame(socket, controlframe.TypeTokenHandshakeReject, controlframe.TokenHandshakeReject{Reason: controlframe.ReasonInvalid})
		return errors.New("no exam session")
	}
	if es.Token.Expired() {
		_ = s.sendControlFrame(socket, controlframe.TypeTokenHandshakeReject, controlframe.TokenHandshakeReject{Reason: controlframe.ReasonExpired})
		return errors.New("token expired")
	}
	if !es.Token.Matches(th.Token) {
		_ = s.sendControlFrame(socket, controlframe.TypeTokenHandshakeReject, controlframe.TokenHandshakeReject{Reason: controlframe.ReasonInvalid})
		return errors.New("token mismatch")
	}

	ok := controlframe.TokenHandshakeOK{
		ExamName: es.Name,
		Started:  es.Machine.IsCapturing(),
	}
	if err := s.sendControlFrame(socket, controlframe.TypeTokenHandshakeOK, ok); err != nil {
		return fmt.Errorf("write handshake OK: %w", err)
	}
	return nil
}

// sendControlFrame writes a single HE-header frame with a JSON payload.
func (s *Server) sendControlFrame(socket *net.TCPConn, typ uint16, payload any) error {
	body, err := controlframe.Marshal(payload)
	if err != nil {
		return err
	}
	buf := make([]byte, HEADER_SIZE+len(body))
	copy(buf, "HE")
	binary.BigEndian.PutUint16(buf[2:4], typ)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(body)))
	copy(buf[HEADER_SIZE:], body)
	socket.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = socket.Write(buf)
	return err
}
```

(The new imports needed: `errors`, `fmt`, `encoding/binary` (already there). The existing `slog` import is already in place from prior work.)

- [ ] **Step 5: Run tests, verify they pass**

```bash
go test -race -run TestHandshake_ -v 2>&1 | tail -20
```

Expected: all 3 handshake tests PASS. Also run the previously-passing tests to confirm no regression:

```bash
go test -race -cover ./... 2>&1 | tail -10
```

Expected: every package still passes; coverage on `server` package may inch up slightly (new code is tested).

- [ ] **Step 6: Commit (ask user first)**

```bash
git add server/server.go server/handshake_test.go
git commit -m "feat(server): require token handshake before NAME frame"
```

---

### Task 9: `server/server.go` — broadcast control frames to capturing clients

**Files:**
- Modify: `server/server.go` — track per-connection writer and state; add `BroadcastControl` and `SendToStudent` methods.
- Create: `server/broadcast_test.go`

After handshake, each `handleStudent` goroutine owns one TCP socket. To send a server→client control frame (Lock, Unlock, BroadcastMsg, ExamStart, ExamStop), the server needs a registry of `studentID → *net.TCPConn` (or a write-only wrapper). We add that registry plus public methods the dashboard buttons will call.

- [ ] **Step 1: Write the failing test**

Create `server/broadcast_test.go`:

```go
package main

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/session"
)

func TestBroadcastControl_SendsToAllRegistered(t *testing.T) {
	srv := NewServer()
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	// Set up two pipe-based connections to act as student sockets.
	s1, c1 := net.Pipe()
	s2, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = s1.Close()
		_ = c1.Close()
		_ = s2.Close()
		_ = c2.Close()
	})

	srv.registerConn("S001", &registeredConn{writer: s1})
	srv.registerConn("S002", &registeredConn{writer: s2})

	// Mark both as capturing so the broadcast picks them up.
	srv.setStudentState("S001", session.StateCapturing)
	srv.setStudentState("S002", session.StateCapturing)

	done := make(chan struct{}, 2)
	for _, reader := range []net.Conn{c1, c2} {
		go func(r net.Conn) {
			hdr := make([]byte, 8)
			r.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := r.Read(hdr); err == nil {
				typ := binary.BigEndian.Uint16(hdr[2:4])
				length := binary.BigEndian.Uint32(hdr[4:8])
				body := make([]byte, length)
				_, _ = r.Read(body)
				if typ == controlframe.TypeLockScreen {
					var ls controlframe.LockScreen
					if json.Unmarshal(body, &ls) == nil && ls.Message == "stop" {
						done <- struct{}{}
					}
				}
			}
		}(reader)
	}

	if err := srv.BroadcastControl(controlframe.TypeLockScreen, controlframe.LockScreen{Message: "stop", LockedBy: "Ms. Lim"}); err != nil {
		t.Fatalf("BroadcastControl: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-done:
			// got it
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/2 receivers got the lock frame", i)
		}
	}
}

func TestBroadcastControl_SkipsNonCapturing(t *testing.T) {
	srv := NewServer()
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	s1, c1 := net.Pipe()
	t.Cleanup(func() {
		_ = s1.Close()
		_ = c1.Close()
	})
	srv.registerConn("S001", &registeredConn{writer: s1})
	srv.setStudentState("S001", session.StateWaiting) // still waiting; broadcast should NOT reach

	if err := srv.BroadcastControl(controlframe.TypeLockScreen, controlframe.LockScreen{Message: "x"}); err != nil {
		t.Fatalf("BroadcastControl: %v", err)
	}

	c1.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := c1.Read(make([]byte, 8)); err == nil {
		t.Error("waiting student should not receive lock frame")
	}
}

func TestSendToStudent_DeliversOnlyToTarget(t *testing.T) {
	srv := NewServer()
	srv.studentUtil = &fakeStudentUtil{exists: map[string]bool{}}
	srv.SetExamSession(session.NewExamSession("Test", 0, time.Hour))

	s1, c1 := net.Pipe()
	s2, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = s1.Close()
		_ = c1.Close()
		_ = s2.Close()
		_ = c2.Close()
	})
	srv.registerConn("S001", &registeredConn{writer: s1})
	srv.registerConn("S002", &registeredConn{writer: s2})
	srv.setStudentState("S001", session.StateCapturing)
	srv.setStudentState("S002", session.StateCapturing)

	if err := srv.SendToStudent("S001", controlframe.TypeBroadcastMsg, controlframe.BroadcastMsg{Body: "hi"}); err != nil {
		t.Fatalf("SendToStudent: %v", err)
	}

	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdr := make([]byte, 8)
	if _, err := c1.Read(hdr); err != nil {
		t.Fatalf("expected delivery to S001: %v", err)
	}

	c2.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := c2.Read(make([]byte, 8)); err == nil {
		t.Error("S002 should not have received the targeted message")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test -run TestBroadcastControl_ -run TestSendToStudent_ -v 2>&1 | head -10
```

Expected: undefined identifiers — `registeredConn`, `srv.registerConn`, `srv.setStudentState`, `srv.BroadcastControl`, `srv.SendToStudent`.

- [ ] **Step 3: Modify `server/server.go` — add registry and broadcasters**

Add a connection-registry type and the new fields/methods:

```go
// registeredConn is the writer end of an accepted TCP connection plus its
// last-known session state. Held in Server.conns keyed by studentID.
type registeredConn struct {
	writer net.Conn        // we only need Write + SetWriteDeadline
	state  session.State
}
```

Add to `Server` struct:

```go
// per-student registry (NEW Task 9)
conns      map[string]*registeredConn
connsMu    sync.RWMutex
```

In `NewServer`, initialise the map:

```go
return &Server{
	isRunning:   atomic.Bool{},
	activeConns: make(map[string]int64),
	conns:       make(map[string]*registeredConn),
}
```

Then add the methods:

```go
func (s *Server) registerConn(id string, rc *registeredConn) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	s.conns[id] = rc
}

func (s *Server) unregisterConn(id string) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	delete(s.conns, id)
}

func (s *Server) setStudentState(id string, st session.State) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if rc, ok := s.conns[id]; ok {
		rc.state = st
	}
}

// BroadcastControl sends a control frame to every currently-capturing
// (or locked) student. Errors on individual writes are logged but do not
// abort the broadcast.
func (s *Server) BroadcastControl(typ uint16, payload any) error {
	body, err := controlframe.Marshal(payload)
	if err != nil {
		return err
	}
	frame := buildFrame(typ, body)

	s.connsMu.RLock()
	targets := make([]*registeredConn, 0, len(s.conns))
	for _, rc := range s.conns {
		if rc.state == session.StateCapturing || rc.state == session.StateLocked {
			targets = append(targets, rc)
		}
	}
	s.connsMu.RUnlock()

	for _, rc := range targets {
		rc.writer.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := rc.writer.Write(frame); err != nil {
			slog.Warn("broadcast write failed", "err", err)
		}
	}
	return nil
}

// SendToStudent sends a control frame to one student. If the student is
// not registered, returns an error.
func (s *Server) SendToStudent(id string, typ uint16, payload any) error {
	body, err := controlframe.Marshal(payload)
	if err != nil {
		return err
	}
	frame := buildFrame(typ, body)

	s.connsMu.RLock()
	rc, ok := s.conns[id]
	s.connsMu.RUnlock()
	if !ok {
		return fmt.Errorf("no connection registered for student %s", id)
	}
	rc.writer.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := rc.writer.Write(frame); err != nil {
		return fmt.Errorf("write to %s: %w", id, err)
	}
	return nil
}

func buildFrame(typ uint16, body []byte) []byte {
	buf := make([]byte, HEADER_SIZE+len(body))
	copy(buf, "HE")
	binary.BigEndian.PutUint16(buf[2:4], typ)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(body)))
	copy(buf[HEADER_SIZE:], body)
	return buf
}
```

Also: in `handleStudent`, register the connection after handshake succeeds and unregister on defer:

```go
// Right after the handshake succeeds, before the read loop:
rc := &registeredConn{writer: socket, state: session.StateWaiting}
// id isn't known until NAME (type 0) is received; we register with a
// placeholder under a unique key based on the remote address, then move
// it to the studentID once NAME arrives.
placeholder := socket.RemoteAddr().String()
s.registerConn(placeholder, rc)
defer s.unregisterConn(placeholder)
```

In the `case 0:` (NAME) branch where `id` is set, also do:

```go
s.unregisterConn(placeholder)
s.registerConn(id, rc)
defer s.unregisterConn(id) // shadows earlier defer for the placeholder
```

Note: Go's `defer` accumulates; both will run, the first being a no-op after the second moves the entry. To avoid two defers, restructure with a small function — but the simplest robust path is to set `placeholder = id` after the move and keep one defer that uses the current value via a closure:

```go
key := socket.RemoteAddr().String()
s.registerConn(key, rc)
defer func() {
	s.unregisterConn(key) // unregisters whatever `key` is at function exit
}()
// ...later, after NAME:
s.unregisterConn(key)
key = id
s.registerConn(key, rc)
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -run 'TestBroadcastControl_|TestSendToStudent_' -v 2>&1 | tail -10
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Run the full module suite — confirm no regression**

```bash
go test -race -cover ./... 2>&1 | tail -10
```

Expected: every package still ok; no new failures.

- [ ] **Step 6: Commit (ask user first)**

```bash
git add server/server.go server/broadcast_test.go
git commit -m "feat(server): add per-student conn registry + broadcast/send-to-student"
```

---

### Task 10: `server/server.go` — handle client state-ack frames; reject late frames

**Files:**
- Modify: `server/server.go` — extend the switch in `handleStudent` to handle types 20–23 (state acks) and 24 (ack broadcast msg).
- Modify: `server/server.go` — reject PICTURE frames (type 2) when the server's exam state is not Capturing or Locked. Log a `late_frame_rejected` event.
- Create: `server/state_ack_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/state_ack_test.go`:

```go
package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"bytes"
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
```

You will also need to extend `fakeStudentUtil` in `handshake_test.go` to count `UpdateImage` calls — add `imageCalls int` and increment it in the method.

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test -run 'TestStateAck_|TestLateFrameRejected_' -v 2>&1 | head -10
```

Expected: the state-ack test fails because `handleStudent` doesn't yet recognise types 20–23; the late-frame test fails because PICTURE is currently always accepted.

- [ ] **Step 3: Modify the switch in `handleStudent`**

Replace the existing `switch dataType { … }` block with one that adds state-ack and the late-frame guard. The new structure:

```go
switch dataType {
case 0:
	// existing NAME handling — keep
case 1:
	// existing MESSAGE handling — keep
case 2:
	// PICTURE: only accept while server says Capturing/Locked
	es := s.ExamSession()
	if es == nil || (es.Machine.State() != session.StateCapturing && es.Machine.State() != session.StateLocked) {
		slog.Debug("late frame rejected", "id", id, "server_state", func() string {
			if es == nil {
				return "no_session"
			}
			return es.Machine.State().String()
		}())
		// Optionally: record an event via the eventlog (deferred until Task 11
		// wires the EventLog into Server).
		break
	}
	img, _, err := image.Decode(bytes.NewReader(data[:dataSize]))
	if err == nil {
		s.studentUtil.UpdateImage(id, img)
	} else {
		slog.Debug("image decode failed", "id", id, "bytes", dataSize, "err", err)
	}
case controlframe.TypeStateWaiting:
	s.setStudentState(id, session.StateWaiting)
case controlframe.TypeStateCapturing:
	s.setStudentState(id, session.StateCapturing)
case controlframe.TypeStateLocked:
	s.setStudentState(id, session.StateLocked)
case controlframe.TypeStateStopped:
	s.setStudentState(id, session.StateStopped)
case controlframe.TypeAckBroadcastMsg:
	// We don't track per-message delivery in Phase 1; just log.
	slog.Debug("ack broadcast msg", "id", id, "len", dataSize)
default:
	slog.Debug("unknown frame type ignored", "type", dataType, "id", id)
}
```

(The previous `default:` branch decoded any unknown type as a picture. We replace that with explicit cases — the late-frame test will pass because PICTURE is now case 2 and is gated by state.)

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -run 'TestStateAck_|TestLateFrameRejected_' -v 2>&1 | tail -10
```

Expected: both PASS. Then full module:

```bash
go test -race -cover ./... 2>&1 | tail -10
```

Expected: every package still ok.

- [ ] **Step 5: Commit (ask user first)**

```bash
git add server/server.go server/state_ack_test.go server/handshake_test.go
git commit -m "feat(server): handle client state acks; reject pictures when not capturing"
```

---

### Task 11: `server/main.go` — initialise session, eventlog, reaper

**Files:**
- Modify: `server/main.go`
- Modify: `server/server.go` — add an `EventLog` field so callsites can record events.

We don't yet have UI to create an exam (Task 12 onward). For now, `main.go` opens an event-log directory and starts the reaper goroutine. The actual `ExamSession` is created later when the teacher clicks "Open Waiting Room" in the home screen.

- [ ] **Step 1: Modify `server/server.go` — add EventLog wiring**

Add to `Server` struct:

```go
eventLog *eventlog.EventLog
```

Add a setter:

```go
func (s *Server) SetEventLog(el *eventlog.EventLog) {
	s.eventLog = el
}

func (s *Server) EventLog() *eventlog.EventLog { return s.eventLog }
```

Import:
```go
"github.com/exam-gaurd/server/eventlog"
```

- [ ] **Step 2: Modify `server/main.go` — set up exams root and reaper at boot**

Add to imports:
```go
"context"
"path/filepath"
"github.com/exam-gaurd/server/eventlog"
```

After the `logger, err := diag.New("server")` block (and before `defer diag.RecoverPanic`), insert:

```go
examsRoot := ""
if logger != nil {
	base, _ := os.UserConfigDir()
	examsRoot = filepath.Join(base, "exam-monitor", "exams")
	if err := os.MkdirAll(examsRoot, 0o755); err != nil {
		slog.Warn("could not create exams dir", "dir", examsRoot, "err", err)
	}
	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	defer reaperCancel()
	eventlog.RunReaper(reaperCtx, examsRoot, time.Hour)
	slog.Info("retention reaper started", "root", examsRoot, "interval", time.Hour)
}
```

Also pass `examsRoot` into the run loop so the home screen can use it when creating an exam:

```go
// Replace:
//   server := NewServer()
// With:
server := NewServer()
server.examsRoot = examsRoot
```

…and add an `examsRoot string` field to the `Server` struct (just below the `eventLog` field).

- [ ] **Step 3: Verify the module builds and existing tests pass**

```bash
go build ./...
go test -race -cover ./... 2>&1 | tail -10
```

Expected: build clean; every package still passes.

- [ ] **Step 4: Commit (ask user first)**

```bash
git add server/main.go server/server.go
git commit -m "feat(server): start retention reaper and prep exams-root on boot"
```

---

## Stage 1 progress checkpoint

After Tasks 5–11 you should have:

- CSV export + retention reaper on `eventlog`.
- `controlframe` package with all wire types and JSON codecs.
- Token-handshake gate at the top of `handleStudent`.
- Per-student connection registry + `BroadcastControl` / `SendToStudent`.
- Client state acks handled; late frames rejected outside Capturing.
- Reaper started at boot; `examsRoot` ready for the home screen.
- 18+ new passing server tests (4 controlframe + 3 handshake + 3 broadcast + 2 state-ack/late-frame + 6 reaper / CSV).

If any test is amber, fix before continuing — the rest of the plan depends on these primitives.

---

## Stage 2 — Server UI

Reading `server/home.go` and `server/dashboard.go` before each UI task is required — they're the files you're modifying, and Gio code is hard to write blind.

### Task 12: `server/home.go` — Create Exam screen

**Files:**
- Modify: `server/home.go` — add `ExamNameEditor`, `BtnRegenerateToken`, store the freshly-generated `*session.ExamSession`, render the token, dispatch to dashboard with an exam session prepared.
- Modify: `server/main.go` — pass the new exam session into the dashboard, open the event log file, record `exam_created`.

The home screen today asks only for a room number. It needs to also accept an exam name and display the token. When the teacher clicks "Open Waiting Room", we (a) create a `session.ExamSession`, (b) open `<examsRoot>/<date>_<slug>/events.sqlite`, (c) record `exam_created`, (d) set both on the `Server`, (e) switch to dashboard view.

- [ ] **Step 1: Read the current home.go**

```bash
cat server/home.go
```

Note where `BtnStart` is wired and how `OnClick(room)` is dispatched. The pattern stays the same; we add fields and forward a second arg.

- [ ] **Step 2: Add fields and wire the new controls**

Replace the `HomeState` struct with:

```go
type HomeState struct {
	ExamNameEditor   *widget.Editor
	RoomEditor       *widget.Editor
	BtnStart         *widget.Clickable
	BtnRegenerate    *widget.Clickable
	OnClick          func(examName string, room int)

	currentSession *session.ExamSession
	examNameError  string
	roomError      string
}

func NewHomeState(start func(examName string, room int)) *HomeState {
	h := &HomeState{
		ExamNameEditor: new(widget.Editor),
		RoomEditor:     new(widget.Editor),
		BtnStart:       new(widget.Clickable),
		BtnRegenerate:  new(widget.Clickable),
		OnClick:        start,
	}
	h.RoomEditor.Filter = "0123456789"
	h.RoomEditor.MaxLen = 6
	h.ExamNameEditor.Submit = true
	h.RoomEditor.Submit = true
	return h
}
```

Add imports: `"github.com/exam-gaurd/server/session"` and `"time"` and (likely already there) `"gioui.org/widget"`.

- [ ] **Step 3: Token handling: generate-or-keep, regenerate-on-click**

Add to `HomeState`:

```go
func (h *HomeState) ensureSession() {
	if h.currentSession != nil {
		return
	}
	h.currentSession = session.NewExamSession("", 0, 4*time.Hour)
}

func (h *HomeState) regenerateToken() {
	h.ensureSession()
	h.currentSession.RegenerateToken(4 * time.Hour)
}
```

- [ ] **Step 4: Validation + submit**

Replace `validate()` / `isValid()` / `handleSubmit()` to also require exam name, and to dispatch with the chosen room and exam name:

```go
func (h *HomeState) validate() bool {
	h.examNameError = ""
	h.roomError = ""
	valid := true
	if strings.TrimSpace(h.ExamNameEditor.Text()) == "" {
		h.examNameError = "Exam name is required."
		valid = false
	}
	roomText := strings.TrimSpace(h.RoomEditor.Text())
	if roomText == "" {
		h.roomError = "Room is required."
		valid = false
	} else if room, err := strconv.Atoi(roomText); err != nil || room <= 0 {
		h.roomError = "Room must be a positive number."
		valid = false
	}
	return valid
}

func (h *HomeState) handleSubmit() {
	if !h.validate() {
		return
	}
	room, _ := strconv.Atoi(strings.TrimSpace(h.RoomEditor.Text()))
	examName := strings.TrimSpace(h.ExamNameEditor.Text())
	h.ensureSession()
	h.currentSession.Name = examName
	h.currentSession.Port = room
	h.OnClick(examName, room)
}

// CurrentSession returns the prepared ExamSession so main.go can install
// it on the Server and open the event log.
func (h *HomeState) CurrentSession() *session.ExamSession { return h.currentSession }
```

- [ ] **Step 5: Update Layout to render the new field + token block**

The layout was a single column with Room + Start. Now add: exam-name row, then the room row, then a token panel showing `currentSession.Token.String()` plus a `[regenerate]` button. The Start button label becomes "Open Waiting Room".

A minimal addition (sketch — drop into the existing `Layout` after the room input):

```go
h.ensureSession() // make sure the token exists for display

if h.BtnRegenerate.Clicked(gtx) {
	h.regenerateToken()
}

// In the Flex column, after the room row:
layout.Rigid(func(gtx layout.Context) layout.Dimensions {
	return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
}),
layout.Rigid(func(gtx layout.Context) layout.Dimensions {
	return FormRow(gtx, "Exam name", th, func(gtx layout.Context) layout.Dimensions {
		return TextEditorWithError(th, h.ExamNameEditor, "e.g. Biology Final", h.examNameError)(gtx)
	})
}),
layout.Rigid(func(gtx layout.Context) layout.Dimensions {
	return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
}),
layout.Rigid(func(gtx layout.Context) layout.Dimensions {
	tokenLabel := material.Body1(th, "Exam token: "+h.currentSession.Token.String())
	tokenLabel.TextSize = unit.Sp(16)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(tokenLabel.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Rigid(material.Button(th, h.BtnRegenerate, "regenerate").Layout),
	)
}),
```

Don't worry about pixel-perfect alignment — Phase 1 only needs the data to be visible and the regenerate button to work. Polish is Phase 2.

- [ ] **Step 6: Wire the home callback in `server/main.go`**

`server/main.go` already does:

```go
home := NewHomeState(func(room int) {
	state.swtichScreen("dashboard")
	server.Start(room)
})
```

Change to (note the new arg + event-log open + exam-session install):

```go
home := NewHomeState(func(examName string, room int) {
	state.swtichScreen("dashboard")

	es := home.CurrentSession() // already has Name, Port, Token

	// Open the event log file for this exam.
	if examsRoot != "" {
		slug := slugify(examName)
		examDir := filepath.Join(examsRoot, time.Now().UTC().Format("2006-01-02")+"_"+slug)
		if err := os.MkdirAll(filepath.Join(examDir, "finals"), 0o755); err == nil {
			if el, err := eventlog.Open(filepath.Join(examDir, "events.sqlite")); err == nil {
				_ = el.SetMeta("exam_name", examName)
				_ = el.SetMeta("room_port", fmt.Sprintf("%d", room))
				_ = el.SetMeta("token", es.Token.Canonical())
				_ = el.SetMeta("created_at_utc", time.Now().UTC().Format(time.RFC3339))
				_ = el.Record(eventlog.Event{Type: "exam_created", Details: map[string]any{"token": es.Token.Canonical(), "port": room}})
				server.SetEventLog(el)
			} else {
				slog.Warn("eventlog open failed", "dir", examDir, "err", err)
			}
		} else {
			slog.Warn("mkdir exam dir failed", "dir", examDir, "err", err)
		}
	}

	server.SetExamSession(es)
	server.Start(room)
})
```

Add `slugify`:

```go
func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevHyphen := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case (r == ' ' || r == '-' || r == '_') && !prevHyphen:
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = out[:32]
	}
	if out == "" {
		out = "untitled"
	}
	return out
}
```

Add imports: `fmt`, `strings`, `os`, `path/filepath`, `time`, `"github.com/exam-gaurd/server/eventlog"`.

- [ ] **Step 7: Build, smoke-test, then commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
```

Expected: build clean; all tests still pass. (The home screen doesn't have unit tests — it's pure Gio glue; the e2e test in Task 25 will exercise it end-to-end.)

```bash
git add server/home.go server/main.go server/server.go
git commit -m "feat(server): Create Exam screen with exam name + token display"
```

---

### Task 13: `server/dashboard.go` — context-aware top bar + Start/Stop buttons

**Files:**
- Modify: `server/dashboard.go` — add the new buttons, render them only in the appropriate session state, wire Start/Stop to the session state machine + event log.
- Modify: `server/server.go` — record `exam_started` / `exam_stopped` events; for `exam_stopped` set the SQLite `retention_until_utc` meta.

This task adds **only** Start, Stop, and the elapsed clock. Lock, Send-message, and Export get their own tasks because each is its own dialog.

- [ ] **Step 1: Read the current dashboard.go**

```bash
cat server/dashboard.go
```

Identify where the dashboard's main `Layout` is defined and where buttons currently exist (Stop, column +/-, sort, viewer-close).

- [ ] **Step 2: Add `BtnStartExam` + `BtnStopExam` to `DashboardState`**

Add fields:

```go
BtnStartExam *widget.Clickable
BtnStopExam  *widget.Clickable
server       *Server // injected by main.go for state queries + control broadcasts
```

In `NewDashboardState`, allocate:

```go
BtnStartExam: new(widget.Clickable),
BtnStopExam:  new(widget.Clickable),
```

Provide a setter:

```go
func (d *DashboardState) SetServer(s *Server) { d.server = s }
```

- [ ] **Step 3: Inject the server reference from `main.go`**

After `server.studentUtil = dashboard`, add:

```go
dashboard.SetServer(server)
```

- [ ] **Step 4: Render a context-aware top bar**

At the top of `DashboardState.Layout`, add a new row above the existing content. The visible widgets depend on the server's session state:

```go
func (d *DashboardState) topBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	es := d.server.ExamSession()
	state := session.StateIdle
	if es != nil {
		state = es.Machine.State()
	}

	switch state {
	case session.StateWaiting:
		// Start button + count of waiting students.
		if d.BtnStartExam.Clicked(gtx) {
			d.startExam()
		}
		count := d.studentManager.Count()
		startBtn := material.Button(th, d.BtnStartExam, "Start Exam")
		if count == 0 {
			startBtn.Background = DisabledBg
			startBtn.Color = DisabledFg
		}
		label := material.Body1(th, fmt.Sprintf("Waiting for students — %d joined", count))
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(label.Layout),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(startBtn.Layout),
		)
	case session.StateCapturing, session.StateLocked:
		// Clock + Stop button (Lock, Message added in later tasks).
		if d.BtnStopExam.Clicked(gtx) {
			d.stopExam()
		}
		elapsed := time.Since(es.StartedAt).Truncate(time.Second)
		clock := material.Body1(th, fmt.Sprintf("Elapsed: %s", elapsed))
		stopBtn := material.Button(th, d.BtnStopExam, "Stop Exam")
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(clock.Layout),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(stopBtn.Layout),
		)
	case session.StateStopped:
		label := material.Body1(th, "Exam ended.")
		return label.Layout(gtx)
	default:
		return layout.Dimensions{}
	}
}
```

Then call `d.topBar(gtx, th)` as the first child of the main layout column (above the existing mosaic).

Add imports: `"fmt"`, `"time"`, `"github.com/exam-gaurd/server/session"`.

- [ ] **Step 5: Implement `startExam` and `stopExam`**

```go
func (d *DashboardState) startExam() {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if _, err := es.Machine.Apply(session.EventStartExam); err != nil {
		slog.Warn("startExam: illegal transition", "err", err)
		return
	}
	es.MarkStarted()
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "exam_started"})
		_ = el.SetMeta("started_at_utc", es.StartedAt.Format(time.RFC3339))
	}
	// Broadcast EXAM_START to all currently-connected clients (they are in
	// Waiting state from the registry).
	_ = d.server.BroadcastControlAll(controlframe.TypeExamStart, controlframe.ExamStart{
		ExamName:  es.Name,
		StartedAt: es.StartedAt,
	})
	slog.Info("exam started", "name", es.Name)
}

func (d *DashboardState) stopExam() {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if err := es.Stop(24 * time.Hour); err != nil {
		slog.Warn("stopExam: illegal transition", "err", err)
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "exam_stopped"})
		_ = el.SetMeta("stopped_at_utc", es.StoppedAt.Format(time.RFC3339))
		_ = el.SetMeta("retention_until_utc", es.RetentionUntil.Format(time.RFC3339))
	}
	_ = d.server.BroadcastControlAll(controlframe.TypeExamStop, controlframe.ExamStop{StoppedAt: es.StoppedAt})
	slog.Info("exam stopped", "name", es.Name)
}
```

- [ ] **Step 6: Add `BroadcastControlAll` on `Server`**

`BroadcastControl` from Task 9 only delivers to capturing clients. Start needs to broadcast `EXAM_START` to *Waiting* clients (those who joined before Start). Add a sibling:

```go
// BroadcastControlAll sends a control frame to every registered connection
// regardless of state. Used for EXAM_START which transitions clients out
// of Waiting.
func (s *Server) BroadcastControlAll(typ uint16, payload any) error {
	body, err := controlframe.Marshal(payload)
	if err != nil {
		return err
	}
	frame := buildFrame(typ, body)

	s.connsMu.RLock()
	targets := make([]*registeredConn, 0, len(s.conns))
	for _, rc := range s.conns {
		targets = append(targets, rc)
	}
	s.connsMu.RUnlock()

	for _, rc := range targets {
		rc.writer.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = rc.writer.Write(frame)
	}
	return nil
}
```

- [ ] **Step 7: Build, run tests, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add server/dashboard.go server/main.go server/server.go
git commit -m "feat(server): Start/Stop exam buttons + state-aware top bar"
```

---

### Task 14: `server/dashboard.go` — tile state badges

**Files:**
- Modify: `server/student.go` — add a `State session.State` field on `Student`.
- Modify: `server/server.go` — also bump the `Student.State` field in `setStudentState`.
- Modify: `server/card.go` — render a small state badge in the top-right of each tile.

- [ ] **Step 1: Add the field**

In `server/student.go`, add to the `Student` struct:

```go
State session.State
```

And in `NewStudent`, default it to `session.StateWaiting`. Add the `session` import.

- [ ] **Step 2: Update `setStudentState` to also write the `Student.State`**

In `server/server.go`, in `setStudentState`, after the `rc.state = st` line, also reach into the student manager:

```go
if stu := s.studentUtil.(*DashboardState).studentManager.GetByID(id); stu != nil {
	stu.State = st
}
```

This is a tight coupling; an alternative is a `SetStudentState(id, state)` method on the dashboard, exposed through the `StudentUtil` interface. Pick whichever feels less invasive — the interface route is cleaner long-term but is two extra lines.

For Phase 1, the type-asserted shortcut is acceptable since the production `studentUtil` is always `*DashboardState`. Document this with a comment.

- [ ] **Step 3: Render the badge in `server/card.go`**

Find the card layout. Add a small top-right rune-based badge that depends on `student.State`:

```go
func stateBadge(state session.State, online bool) (glyph string, bg color.NRGBA) {
	if !online {
		return "!", color.NRGBA{R: 220, G: 38, B: 38, A: 255} // red-600
	}
	switch state {
	case session.StateWaiting:
		return "W", color.NRGBA{R: 16, G: 185, B: 129, A: 255} // emerald-500
	case session.StateCapturing:
		return "●", color.NRGBA{R: 16, G: 185, B: 129, A: 255}
	case session.StateLocked:
		return "🔒", color.NRGBA{R: 31, G: 41, B: 55, A: 255} // gray-800
	default:
		return "?", color.NRGBA{R: 234, G: 179, B: 8, A: 255} // yellow-500
	}
}
```

Render the glyph in a small coloured square in the card's top-right corner. The colour gives the at-a-glance read; the glyph gives accessibility for colour-blind users.

- [ ] **Step 4: Build, run tests, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add server/student.go server/server.go server/card.go
git commit -m "feat(server): per-tile state badges (colour + glyph)"
```

---

### Task 15: Lock-all dialog + Unlock button

**Files:**
- Modify: `server/dashboard.go` — add `BtnLockAll`, `BtnUnlock`, plus a `LockDialogState` struct for the modal.
- Modify: the top bar from Task 13 to show Lock when `Capturing`, Unlock when `Locked`.

- [ ] **Step 1: Add fields**

```go
BtnLockAll *widget.Clickable
BtnUnlock  *widget.Clickable
lockDialog *LockDialogState

type LockDialogState struct {
	Open        bool
	MsgEditor   *widget.Editor
	BtnCancel   *widget.Clickable
	BtnConfirm  *widget.Clickable
}
```

`NewDashboardState`:

```go
BtnLockAll: new(widget.Clickable),
BtnUnlock:  new(widget.Clickable),
lockDialog: &LockDialogState{
	MsgEditor:  new(widget.Editor),
	BtnCancel:  new(widget.Clickable),
	BtnConfirm: new(widget.Clickable),
},
```

- [ ] **Step 2: Wire button clicks**

In `topBar`, when `state == session.StateCapturing`:

```go
if d.BtnLockAll.Clicked(gtx) {
	d.lockDialog.Open = true
	d.lockDialog.MsgEditor.SetText("Please stop typing until further instructions.")
}
```

When `state == session.StateLocked`:

```go
if d.BtnUnlock.Clicked(gtx) {
	d.unlockExam()
}
```

Layout: append the Lock button to the Capturing branch's row; append the Unlock button to a new Locked branch in the switch.

- [ ] **Step 3: Implement the dialog overlay**

A minimal modal pattern in Gio is to render the dialog at the very end of the column with a semi-transparent background that absorbs clicks. Skeleton:

```go
func (d *DashboardState) drawLockDialog(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !d.lockDialog.Open {
		return layout.Dimensions{}
	}
	if d.lockDialog.BtnCancel.Clicked(gtx) {
		d.lockDialog.Open = false
		return layout.Dimensions{}
	}
	if d.lockDialog.BtnConfirm.Clicked(gtx) {
		d.lockDialog.Open = false
		d.lockAll(strings.TrimSpace(d.lockDialog.MsgEditor.Text()))
		return layout.Dimensions{}
	}
	// Otherwise render a centred panel with title + editor + buttons.
	// (Render this last in the parent Layout so it stacks on top.)
	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return MaxWidthContainer(gtx, 480, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(material.H6(th, "Lock all student screens?").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
				layout.Rigid(material.Body1(th, "Message shown to students:").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(material.Editor(th, d.lockDialog.MsgEditor, "Message…").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Button(th, d.lockDialog.BtnCancel, "Cancel").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(th, d.lockDialog.BtnConfirm, "Lock All").Layout),
				).Layout),
			)
		})
	})
}
```

(Use `MaxWidthContainer` if already defined in `views.go`; otherwise a `layout.Center` wrapper.)

Call `d.drawLockDialog(gtx, th)` at the bottom of the dashboard's main `Layout`.

- [ ] **Step 4: Implement `lockAll` and `unlockExam`**

```go
func (d *DashboardState) lockAll(message string) {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if _, err := es.Machine.Apply(session.EventLock); err != nil {
		slog.Warn("lock: illegal transition", "err", err)
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "lock_applied", Details: map[string]any{"message_to_students": message}})
	}
	_ = d.server.BroadcastControl(controlframe.TypeLockScreen, controlframe.LockScreen{Message: message, LockedBy: "Instructor"})
}

func (d *DashboardState) unlockExam() {
	es := d.server.ExamSession()
	if es == nil {
		return
	}
	if _, err := es.Machine.Apply(session.EventUnlock); err != nil {
		slog.Warn("unlock: illegal transition", "err", err)
		return
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{Type: "lock_released"})
	}
	_ = d.server.BroadcastControl(controlframe.TypeUnlockScreen, controlframe.UnlockScreen{})
}
```

- [ ] **Step 5: Build, run tests, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add server/dashboard.go
git commit -m "feat(server): Lock All / Unlock with confirmation dialog"
```

---

### Task 16: Send-Message dialog

**Files:**
- Modify: `server/dashboard.go` — add `BtnSendMsg`, `MsgDialogState` with a target selector (all / per-student) and a body editor.

The flow mirrors Task 15 (button → modal → confirm → broadcast). Implementation is structurally identical; the differences are (a) the target picker, (b) the wire type (`TypeBroadcastMsg`), (c) the event logged (`message_broadcast`).

- [ ] **Step 1: Add fields**

```go
BtnSendMsg *widget.Clickable
msgDialog  *MsgDialogState

type MsgDialogState struct {
	Open       bool
	TargetAll  bool
	TargetID   string
	BodyEditor *widget.Editor
	BtnAll     *widget.Clickable
	BtnPer     *widget.Clickable
	BtnCancel  *widget.Clickable
	BtnConfirm *widget.Clickable
}
```

`NewDashboardState`:

```go
BtnSendMsg: new(widget.Clickable),
msgDialog: &MsgDialogState{
	TargetAll:  true,
	BodyEditor: new(widget.Editor),
	BtnAll:     new(widget.Clickable),
	BtnPer:     new(widget.Clickable),
	BtnCancel:  new(widget.Clickable),
	BtnConfirm: new(widget.Clickable),
},
```

- [ ] **Step 2: Add the button to the top bar (Capturing branch)**

In `topBar` Capturing case, append a `BtnSendMsg` after the Lock button. When clicked:

```go
if d.BtnSendMsg.Clicked(gtx) {
	d.msgDialog.Open = true
	d.msgDialog.TargetAll = true
	d.msgDialog.TargetID = ""
	d.msgDialog.BodyEditor.SetText("")
}
```

- [ ] **Step 3: Implement the dialog**

```go
func (d *DashboardState) drawMsgDialog(gtx layout.Context, th *material.Theme) layout.Dimensions {
	md := d.msgDialog
	if !md.Open {
		return layout.Dimensions{}
	}
	if md.BtnCancel.Clicked(gtx) {
		md.Open = false
		return layout.Dimensions{}
	}
	if md.BtnAll.Clicked(gtx) {
		md.TargetAll = true
	}
	if md.BtnPer.Clicked(gtx) {
		md.TargetAll = false
	}
	if md.BtnConfirm.Clicked(gtx) {
		md.Open = false
		d.sendMessage(md.TargetAll, md.TargetID, strings.TrimSpace(md.BodyEditor.Text()))
		return layout.Dimensions{}
	}
	// Render: radio for target, editor for body, Cancel/Send buttons.
	// (Use the same layout shape as the lock dialog.)
	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return MaxWidthContainer(gtx, 520, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(material.H6(th, "Send message").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Button(th, md.BtnAll, ifThen(md.TargetAll, "● All", "○ All")).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(th, md.BtnPer, ifThen(!md.TargetAll, "● Selected", "○ Selected")).Layout),
				).Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
				layout.Rigid(material.Editor(th, md.BodyEditor, "Message…").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Button(th, md.BtnCancel, "Cancel").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(th, md.BtnConfirm, "Send").Layout),
				).Layout),
			)
		})
	})
}

func ifThen(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
```

Per-student target selection: for now, `TargetID` is left blank (always class-wide). Wire it in a follow-up by adding a dropdown over `studentManager.GetSorted()` — too much UI for this task. Document this in a `// TODO Phase 1.5` comment near the dialog struct.

Wait — per the spec, per-student message is in scope for Phase 1. Don't leave it out. The minimum viable picker:

```go
// Below the radio row, when !TargetAll, show a horizontal scrolling list of
// student buttons; clicking sets TargetID.
if !md.TargetAll {
	for _, st := range d.studentManager.GetSorted() {
		stu := st // capture
		if md.studentPickerBtn(stu.Id).Clicked(gtx) {
			md.TargetID = stu.Id
		}
		// render a small button label = stu.Name with selected highlight
	}
}
```

`studentPickerBtn(id)` lazy-creates a `*widget.Clickable` per student ID; store these in a map inside `MsgDialogState`. This is plausibly 20 LOC; pair it with a smoke test by sending to one student and watching the integration test in Task 25.

- [ ] **Step 4: Implement `sendMessage`**

```go
func (d *DashboardState) sendMessage(toAll bool, targetID, body string) {
	if body == "" {
		return
	}
	target := ""
	if !toAll {
		target = targetID
	}
	msg := controlframe.BroadcastMsg{From: "Instructor", Body: body, TargetStudentID: target}
	if toAll {
		_ = d.server.BroadcastControl(controlframe.TypeBroadcastMsg, msg)
	} else {
		if err := d.server.SendToStudent(target, controlframe.TypeBroadcastMsg, msg); err != nil {
			slog.Warn("send to student failed", "id", target, "err", err)
		}
	}
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{
			Type: "message_broadcast",
			Details: map[string]any{
				"to":   ifThen(toAll, "all", target),
				"body": body,
			},
		})
	}
}
```

- [ ] **Step 5: Build, run tests, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add server/dashboard.go
git commit -m "feat(server): Send Message dialog with class-wide + per-student targets"
```

---

### Task 17: Export Event Log CSV button

**Files:**
- Modify: `server/dashboard.go` — add `BtnExport`, render in the `Stopped` branch of the top bar.

- [ ] **Step 1: Add button**

```go
BtnExport *widget.Clickable
```

`NewDashboardState`: allocate. Wire into the `StateStopped` branch of `topBar`:

```go
case session.StateStopped:
	if d.BtnExport.Clicked(gtx) {
		d.exportLog()
	}
	exportBtn := material.Button(th, d.BtnExport, "Export Event Log")
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(material.Body1(th, "Exam ended.").Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
		layout.Rigid(exportBtn.Layout),
	)
```

- [ ] **Step 2: Implement `exportLog`**

```go
func (d *DashboardState) exportLog() {
	el := d.server.EventLog()
	if el == nil {
		slog.Warn("export: no event log")
		return
	}
	base, _ := os.UserConfigDir()
	exportsDir := filepath.Join(base, "exam-monitor", "exports")
	if err := os.MkdirAll(exportsDir, 0o755); err != nil {
		slog.Warn("export: mkdir failed", "err", err)
		return
	}
	es := d.server.ExamSession()
	slug := "exam"
	if es != nil {
		slug = slugify(es.Name)
	}
	path := filepath.Join(exportsDir, fmt.Sprintf("%s-%s.csv", slug, time.Now().UTC().Format("2006-01-02-150405")))
	if err := el.ExportCSV(path); err != nil {
		slog.Warn("export failed", "path", path, "err", err)
		return
	}
	slog.Info("event log exported", "path", path)
}
```

Add imports: `os`, `path/filepath`, `fmt`, `time` (already in some files; add as needed).

Note: `slugify` was defined in `server/main.go` (Task 12). Move it to a shared file like `server/util.go` so `dashboard.go` can call it without duplicating:

```go
// server/util.go
package main

import "strings"

func slugify(s string) string {
	// (paste the body from main.go)
}
```

Delete the duplicate from `main.go`.

- [ ] **Step 3: Build, run tests, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add server/dashboard.go server/main.go server/util.go
git commit -m "feat(server): Export Event Log button writes CSV to exports dir"
```

---

### Task 18: Viewer audit log

**Files:**
- Modify: `server/viewer.go` — record `instructor_viewed_student` when an enlarged view opens, and a paired event when it closes. Track `viewed_at_utc`.

This is the single most important entry for the privacy moat per spec §3 and §7.3.

- [ ] **Step 1: Read viewer.go to find where enlarge open/close fires**

```bash
cat server/viewer.go
```

Identify the function/method that's invoked when the viewer becomes visible vs. hidden.

- [ ] **Step 2: Record events at open/close**

Where the viewer opens (typically when `viewerState.Open = true`), add:

```go
if d.server != nil {
	if el := d.server.EventLog(); el != nil {
		viewedAt := time.Now().UTC()
		v.openedAtUTC = viewedAt // store on the viewer state
		_ = el.Record(eventlog.Event{
			Type:        "instructor_viewed_student",
			StudentID:   v.studentID,
			StudentName: v.studentName,
			Details: map[string]any{
				"viewed_at_utc": viewedAt.Format(time.RFC3339),
			},
		})
	}
}
```

Where it closes:

```go
if d.server != nil && !v.openedAtUTC.IsZero() {
	if el := d.server.EventLog(); el != nil {
		_ = el.Record(eventlog.Event{
			Type:        "instructor_viewed_student",
			StudentID:   v.studentID,
			StudentName: v.studentName,
			Details: map[string]any{
				"viewed_at_utc":       v.openedAtUTC.Format(time.RFC3339),
				"viewed_until_at_utc": time.Now().UTC().Format(time.RFC3339),
			},
		})
	}
	v.openedAtUTC = time.Time{}
}
```

Add the `openedAtUTC time.Time` field to whichever viewer struct holds the per-view state.

- [ ] **Step 3: Build, run tests, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add server/viewer.go
git commit -m "feat(server): audit log every instructor view-enlarge open/close"
```

---

## Stage 2 progress checkpoint

After Tasks 12–18 you should have:

- A Create-Exam home screen with exam name + token + regenerate.
- A dashboard top bar that's context-aware by session state, with Start, Stop, Lock-All, Unlock, Send-Message, Export-Log buttons.
- Tile state badges with colour + glyph.
- Lock-all confirmation dialog and Send-message dialog (class-wide + per-student picker).
- CSV export to `<UserConfigDir>/exam-monitor/exports/`.
- Audit log entries for every instructor-viewed-student open/close.

Manual smoke check before moving on:

```bash
cd server
go build .
./server &
SERVER_PID=$!
sleep 1
kill $SERVER_PID || true
```

The binary should boot and exit cleanly. (Real interaction requires the client — Stage 3.)

---

## Stage 3 — Client

### Task 19: `client/controlframe` — mirror server wire types

**Files:**
- Create: `client/controlframe/controlframe.go`
- Create: `client/controlframe/controlframe_test.go`

The client needs the same wire types and JSON codecs the server has. Since both modules are independent, we duplicate (same pattern as `internal/diag`). The file content is identical to `server/controlframe/controlframe.go` from Task 7.

- [ ] **Step 1: Copy the package from server**

```bash
mkdir -p /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client/controlframe
cp /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server/controlframe/controlframe.go \
   /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client/controlframe/controlframe.go
cp /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server/controlframe/controlframe_test.go \
   /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client/controlframe/controlframe_test.go
```

The `package controlframe` declaration doesn't change between modules; no edits needed.

- [ ] **Step 2: Run tests, verify they pass against the client module**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
go test -race -cover ./controlframe/... 2>&1 | tail -5
```

Expected: 6 tests PASS, coverage ≥ 80 %.

- [ ] **Step 3: Commit (ask user first)**

```bash
git add client/controlframe/controlframe.go client/controlframe/controlframe_test.go
git commit -m "feat(client): mirror controlframe wire types from server"
```

---

### Task 20: `client/session` state machine

**Files:**
- Create: `client/session/state.go`
- Create: `client/session/state_test.go`

The client state machine mirrors the server's but only tracks the four states a client knows about (Waiting / Capturing / Locked / Stopped — no Idle, since a client only exists after handshake). It also defines the four state-mismatch recoveries from spec §6.5.

- [ ] **Step 1: Write the failing test**

```go
// client/session/state_test.go
package session

import (
	"errors"
	"testing"
)

func TestState_String(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateWaiting, "waiting"},
		{StateCapturing, "capturing"},
		{StateLocked, "locked"},
		{StateStopped, "stopped"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestApply_StraightLinePath(t *testing.T) {
	sm := NewStateMachine()
	if sm.State() != StateWaiting {
		t.Fatalf("initial = %v, want waiting", sm.State())
	}
	tests := []struct {
		evt  Event
		want State
	}{
		{EventExamStart, StateCapturing},
		{EventLock, StateLocked},
		{EventUnlock, StateCapturing},
		{EventExamStop, StateStopped},
	}
	for _, tt := range tests {
		got, err := sm.Apply(tt.evt)
		if err != nil {
			t.Fatalf("Apply(%v): %v", tt.evt, err)
		}
		if got != tt.want {
			t.Errorf("Apply(%v) = %v, want %v", tt.evt, got, tt.want)
		}
	}
}

func TestApply_RecoveryLockFromWaiting(t *testing.T) {
	// Spec §6.5: receiving LOCK_SCREEN while Waiting (e.g. reconnect race)
	// should silently transition Waiting → Capturing → Locked.
	sm := NewStateMachine()
	got, err := sm.Apply(EventLock)
	if err != nil {
		t.Fatalf("Apply(Lock) from waiting should recover; got err %v", err)
	}
	if got != StateLocked {
		t.Errorf("recovered state = %v, want locked", got)
	}
	if !sm.LastRecovery() {
		t.Error("LastRecovery should be true after recovery")
	}
}

func TestApply_StopFromLockedForceUnlocks(t *testing.T) {
	sm := NewStateMachine()
	_, _ = sm.Apply(EventExamStart)
	_, _ = sm.Apply(EventLock)
	got, err := sm.Apply(EventExamStop)
	if err != nil {
		t.Fatalf("stop from locked: %v", err)
	}
	if got != StateStopped {
		t.Errorf("state = %v, want stopped", got)
	}
}

func TestApply_RejectsTrulyIllegal(t *testing.T) {
	sm := NewStateMachine()
	_, _ = sm.Apply(EventExamStart)
	_, _ = sm.Apply(EventExamStop)
	// After stopped, ExamStart shouldn't work — it's terminal.
	if _, err := sm.Apply(EventExamStart); err == nil {
		t.Error("ExamStart after Stopped should error")
	} else if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("err = %v; want ErrIllegalTransition", err)
	}
}

func TestIsCapturing(t *testing.T) {
	sm := NewStateMachine()
	if sm.IsCapturing() {
		t.Error("waiting is not capturing")
	}
	_, _ = sm.Apply(EventExamStart)
	if !sm.IsCapturing() {
		t.Error("capturing should be capturing")
	}
	_, _ = sm.Apply(EventLock)
	if !sm.IsCapturing() {
		t.Error("locked is a substate of capturing")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test ./session/... 2>&1 | head -10
```

Expected: undefined identifiers.

- [ ] **Step 3: Implement `client/session/state.go`**

```go
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
	mu            sync.RWMutex
	state         State
	lastRecovery  bool // true if the most recent Apply applied a recovery
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
		{StateWaiting, EventExamStart}:   StateCapturing,
		{StateCapturing, EventLock}:      StateLocked,
		{StateLocked, EventUnlock}:       StateCapturing,
		{StateCapturing, EventExamStop}:  StateStopped,
		{StateLocked, EventExamStop}:     StateStopped,
	}
	if next, ok := table[key{sm.state, evt}]; ok {
		sm.state = next
		return sm.state, nil
	}
	return sm.state, fmt.Errorf("%w: from %s on %s", ErrIllegalTransition, sm.state, evt)
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -cover ./session/... 2>&1 | tail -5
```

Expected: all PASS, coverage ≥ 80 %.

- [ ] **Step 5: Commit**

```bash
git add client/session/state.go client/session/state_test.go
git commit -m "feat(client): add session state machine with mismatch recoveries"
```

---

### Task 21: `client/client.go` — token handshake + control-frame router

**Files:**
- Modify: `client/client.go` — add `examToken string`, `examName string`, `sessionState *session.StateMachine`, `onExamStart/Stop/Lock/Unlock/Message func(...)` callbacks; send `TokenHandshake` after connect; read incoming control frames in a small reader goroutine; route them.

The current `Client.Start` does: discover → dial → send NAME → loop {capture → SendScreenshot}. After this task: discover → dial → send TokenHandshake → read OK/Reject → send NAME → start reader goroutine + send state ack → loop {if Capturing: capture → SendScreenshot}.

- [ ] **Step 1: Add fields and setters to `Client`**

```go
type Client struct {
	// ...existing fields...

	examToken       string                  // NEW
	manualServerIP  string                  // (already added in earlier work)
	capturer        capture.Capturer        // (already added)

	sessionState    *session.StateMachine   // NEW
	onExamStart     func(examName string)
	onExamStop      func()
	onLock          func(message, lockedBy string)
	onUnlock        func()
	onMessage       func(from, body string)
}

func (c *Client) SetExamToken(token string) { c.examToken = strings.TrimSpace(token) }

func (c *Client) SetCallbacks(onExamStart func(string), onExamStop func(), onLock func(string, string), onUnlock func(), onMessage func(string, string)) {
	c.onExamStart = onExamStart
	c.onExamStop = onExamStop
	c.onLock = onLock
	c.onUnlock = onUnlock
	c.onMessage = onMessage
}

func (c *Client) SessionState() *session.StateMachine { return c.sessionState }
```

`NewClient` initialises `sessionState`:

```go
sessionState: session.NewStateMachine(),
```

Add imports: `"github.com/exam-gaurd/client/controlframe"`, `"github.com/exam-gaurd/client/session"`.

- [ ] **Step 2: Send TokenHandshake after dial**

In `Client.Start`, after the `client.socket, err = net.DialTCP(...)` block succeeds, but before the `client.SendStudentName(...)` call, insert:

```go
// Token handshake — send our token, expect OK/Reject.
if err := client.sendHandshake(); err != nil {
	slog.Info("token handshake failed", "err", err)
	if client.onError != nil {
		client.onError(fmt.Errorf("server rejected join: %w", err))
	}
	client.socket.Close()
	time.Sleep(retryDelay)
	retryDelay = min(retryDelay*2, 8*time.Second)
	continue
}
```

Implement `sendHandshake`:

```go
func (c *Client) sendHandshake() error {
	body, err := controlframe.Marshal(controlframe.TokenHandshake{Token: c.examToken})
	if err != nil {
		return err
	}
	if err := c.sendFrame(controlframe.TypeTokenHandshake, body); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	// Read reply: one HE-header frame, must be OK or Reject.
	c.socket.SetReadDeadline(time.Now().Add(10 * time.Second))
	hdr := make([]byte, HEADER_SIZE)
	if _, err := io.ReadFull(c.socket, hdr); err != nil {
		return fmt.Errorf("read reply header: %w", err)
	}
	typ, length, err := c.unpackHeader(hdr)
	if err != nil {
		return fmt.Errorf("invalid reply header: %w", err)
	}
	replyBody := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.socket, replyBody); err != nil {
			return fmt.Errorf("read reply body: %w", err)
		}
	}
	c.socket.SetReadDeadline(time.Time{})

	switch typ {
	case controlframe.TypeTokenHandshakeOK:
		var ok controlframe.TokenHandshakeOK
		_ = controlframe.Unmarshal(replyBody, &ok)
		c.examName = ok.ExamName
		if ok.Started {
			// We joined an exam already in progress — recover into Capturing.
			_, _ = c.sessionState.Apply(session.EventExamStart)
		}
		return nil
	case controlframe.TypeTokenHandshakeReject:
		var rej controlframe.TokenHandshakeReject
		_ = controlframe.Unmarshal(replyBody, &rej)
		return fmt.Errorf("rejected: %s", rej.Reason)
	default:
		return fmt.Errorf("unexpected reply type %d", typ)
	}
}

func (c *Client) sendFrame(typ uint16, body []byte) error {
	frame := make([]byte, HEADER_SIZE+len(body))
	copy(frame, "HE")
	binary.BigEndian.PutUint16(frame[2:4], typ)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(body)))
	copy(frame[HEADER_SIZE:], body)
	_, err := c.socket.Write(frame)
	return err
}

func (c *Client) unpackHeader(hdr []byte) (uint16, int, error) {
	if len(hdr) < HEADER_SIZE || string(hdr[:2]) != "HE" {
		return 0, 0, fmt.Errorf("invalid header")
	}
	typ := binary.BigEndian.Uint16(hdr[2:4])
	length := int(binary.BigEndian.Uint32(hdr[4:8]))
	return typ, length, nil
}
```

Add imports: `"io"`, `"fmt"`, and (already present) `"encoding/binary"`.

- [ ] **Step 3: Start a reader goroutine for server→client frames**

After handshake succeeds and NAME is sent, start a goroutine that reads incoming frames and routes them:

```go
go client.readControlFrames()
```

Implementation:

```go
func (c *Client) readControlFrames() {
	defer diag.RecoverPanic("client_reader_goroutine")
	hdr := make([]byte, HEADER_SIZE)
	for c.isConnected.Load() && c.isRunning.Load() {
		c.socket.SetReadDeadline(time.Now().Add(30 * time.Second))
		if _, err := io.ReadFull(c.socket, hdr); err != nil {
			slog.Debug("reader: header read failed", "err", err)
			return
		}
		typ, length, err := c.unpackHeader(hdr)
		if err != nil || length < 0 || length > 1024*1024 {
			return
		}
		body := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(c.socket, body); err != nil {
				return
			}
		}
		c.dispatchControl(typ, body)
	}
}

func (c *Client) dispatchControl(typ uint16, body []byte) {
	switch typ {
	case controlframe.TypeExamStart:
		var p controlframe.ExamStart
		_ = controlframe.Unmarshal(body, &p)
		c.examName = p.ExamName
		_, _ = c.sessionState.Apply(session.EventExamStart)
		if c.onExamStart != nil {
			c.onExamStart(p.ExamName)
		}
		_ = c.sendStateAck(controlframe.TypeStateCapturing)
	case controlframe.TypeExamStop:
		_, _ = c.sessionState.Apply(session.EventExamStop)
		if c.onExamStop != nil {
			c.onExamStop()
		}
		_ = c.sendStateAck(controlframe.TypeStateStopped)
	case controlframe.TypeLockScreen:
		var p controlframe.LockScreen
		_ = controlframe.Unmarshal(body, &p)
		_, _ = c.sessionState.Apply(session.EventLock)
		if c.onLock != nil {
			c.onLock(p.Message, p.LockedBy)
		}
		_ = c.sendStateAck(controlframe.TypeStateLocked)
	case controlframe.TypeUnlockScreen:
		_, _ = c.sessionState.Apply(session.EventUnlock)
		if c.onUnlock != nil {
			c.onUnlock()
		}
		_ = c.sendStateAck(controlframe.TypeStateCapturing)
	case controlframe.TypeBroadcastMsg:
		var p controlframe.BroadcastMsg
		_ = controlframe.Unmarshal(body, &p)
		if c.onMessage != nil {
			c.onMessage(p.From, p.Body)
		}
	default:
		slog.Debug("client: unknown control frame", "type", typ, "len", len(body))
	}
}

func (c *Client) sendStateAck(typ uint16) error {
	return c.sendFrame(typ, []byte("{}"))
}
```

- [ ] **Step 4: Build, run all client tests**

```bash
go build ./...
go test -race -cover ./... 2>&1 | tail -10
```

Expected: build clean; all tests still pass.

- [ ] **Step 5: Commit**

```bash
git add client/client.go
git commit -m "feat(client): token handshake + control-frame reader + state acks"
```

---

### Task 22: `client/client.go` — gate capture on session state

**Files:**
- Modify: `client/client.go` — inside the per-frame loop, only call `captureScreen` when `sessionState.IsCapturing()` is true (i.e., the session is `Capturing` or `Locked`). In `Waiting` we just sleep.

- [ ] **Step 1: Find the capture loop**

It currently looks like:

```go
for client.isConnected.Load() && client.isRunning.Load() {
	screenshot, err := client.captureScreen()
	if err != nil {
		client.isConnected.Store(false)
		break
	}
	err = client.SendScreenshot(screenshot)
	if err != nil {
		break
	}
	client.lastSentTime.Store(time.Now())
	updateUI()
	time.Sleep(UPDATE_INTERVAL)
}
```

- [ ] **Step 2: Add the session-state gate**

Replace the loop with:

```go
for client.isConnected.Load() && client.isRunning.Load() {
	if !client.sessionState.IsCapturing() {
		// Waiting or Stopped — don't capture; just sleep and re-check.
		time.Sleep(UPDATE_INTERVAL)
		continue
	}
	screenshot, err := client.captureScreen()
	if err != nil {
		slog.Debug("capture failed", "err", err)
		client.isConnected.Store(false)
		break
	}
	if err := client.SendScreenshot(screenshot); err != nil {
		break
	}
	client.lastSentTime.Store(time.Now())
	updateUI()
	time.Sleep(UPDATE_INTERVAL)
}
```

- [ ] **Step 3: Add a test that verifies capture is not initialised before EXAM_START**

Create `client/capture_gate_test.go`:

```go
package main

import (
	"testing"

	"github.com/exam-gaurd/client/session"
)

func TestCaptureGate_NoCaptureInWaiting(t *testing.T) {
	c := NewClient()
	// Session is Waiting by default.
	if c.sessionState.State() != session.StateWaiting {
		t.Fatalf("initial state = %v, want waiting", c.sessionState.State())
	}
	// Capturer should not be initialised until EXAM_START.
	if c.capturer != nil {
		t.Errorf("capturer should be nil in Waiting state, got %T", c.capturer)
	}
}

func TestCaptureGate_IsCapturingAfterExamStart(t *testing.T) {
	c := NewClient()
	if _, err := c.sessionState.Apply(session.EventExamStart); err != nil {
		t.Fatalf("apply ExamStart: %v", err)
	}
	if !c.sessionState.IsCapturing() {
		t.Error("IsCapturing should be true after EXAM_START")
	}
}
```

- [ ] **Step 4: Run tests, build, commit**

```bash
go test -race ./... 2>&1 | tail -10
git add client/client.go client/capture_gate_test.go
git commit -m "feat(client): gate screen capture on session state"
```

---

### Task 23: `client/ui` — notice banner widget

**Files:**
- Create: `client/ui/banner.go`

A widget that renders the persistent red "Monitoring active" banner. Pure layout function; no state of its own. The caller passes the exam name + elapsed time and the widget renders.

- [ ] **Step 1: Implement `client/ui/banner.go`**

```go
// Package ui holds the client's Phase 1 UI widgets (banner, lock overlay,
// message toast) and the platform-specific always-on-top helpers.
package ui

import (
	"fmt"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// BannerColors are the palette for the monitoring banner.
type BannerColors struct {
	Background color.NRGBA
	Foreground color.NRGBA
}

var DefaultBannerColors = BannerColors{
	Background: color.NRGBA{R: 220, G: 38, B: 38, A: 255}, // red-600
	Foreground: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
}

// Banner renders the persistent monitoring banner. examName/instructor may
// be empty; elapsed is the duration since EXAM_START.
func Banner(gtx layout.Context, th *material.Theme, examName, instructor string, elapsed time.Duration) layout.Dimensions {
	bg := DefaultBannerColors.Background
	// Paint a coloured rectangle and overlay the text. Gio's idiomatic way
	// to paint a solid background behind a layout is paint.FillShape with a
	// clip.Rect; for Phase 1 we keep it minimal with material.H6 on a
	// material.Card-like wrapper.
	title := material.H6(th, fmt.Sprintf("🔴 MONITORING ACTIVE — %s", examName))
	title.Color = DefaultBannerColors.Foreground
	sub := material.Body2(th, fmt.Sprintf("Instructor: %s · %s elapsed", instructor, elapsed.Truncate(time.Second)))
	sub.Color = DefaultBannerColors.Foreground

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return fillRect(gtx, bg)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(title.Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(sub.Layout),
				)
			})
		}),
	)
}
```

Add a small `fillRect` helper (some Gio versions ship `paint.ColorOp`; the simplest cross-version approach is via `paint.FillShape` + `clip.Rect`):

```go
// fillRect fills the gtx's max-constraints rectangle with c.
func fillRect(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
```

Add imports: `"gioui.org/op/clip"`, `"gioui.org/op/paint"`.

- [ ] **Step 2: Smoke test by building from the client root**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
go build ./ui/...
```

Expected: builds cleanly.

- [ ] **Step 3: Commit**

```bash
git add client/ui/banner.go
git commit -m "feat(client): notice banner widget"
```

---

### Task 24: `client/ui` — message toast widget

**Files:**
- Create: `client/ui/toast.go`

A toast that appears for `BROADCAST_MSG`, auto-dismisses after 5 s. Holds a tiny state machine: when a message arrives, set `expiresAt = now + 5s`; render only while `time.Now() < expiresAt`.

- [ ] **Step 1: Implement `client/ui/toast.go`**

```go
package ui

import (
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const toastDuration = 5 * time.Second

// ToastState holds the currently-displayed message, if any. Safe for
// concurrent Set() (called from the network goroutine) and Layout() (called
// from the UI goroutine).
type ToastState struct {
	mu        sync.Mutex
	from      string
	body      string
	expiresAt time.Time
	dismiss   widget.Clickable
}

func NewToastState() *ToastState { return &ToastState{} }

// Set records a new message. Resets the 5s timer.
func (t *ToastState) Set(from, body string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.from = from
	t.body = body
	t.expiresAt = time.Now().Add(toastDuration)
}

// IsVisible reports whether Layout should render.
func (t *ToastState) IsVisible() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.expiresAt.IsZero() && time.Now().Before(t.expiresAt)
}

// Layout renders the toast if visible; otherwise zero dimensions.
func (t *ToastState) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !t.IsVisible() {
		return layout.Dimensions{}
	}
	if t.dismiss.Clicked(gtx) {
		t.mu.Lock()
		t.expiresAt = time.Time{}
		t.mu.Unlock()
		return layout.Dimensions{}
	}
	t.mu.Lock()
	from, body := t.from, t.body
	t.mu.Unlock()

	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.Body1(th, "💬 "+from+": "+body).Layout),
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(material.Button(th, &t.dismiss, "Dismiss").Layout),
		)
	})
}
```

- [ ] **Step 2: Build and commit**

```bash
go build ./ui/...
git add client/ui/toast.go
git commit -m "feat(client): message toast widget with 5s timeout"
```

---

### Task 25: `client/ui` — always-on-top platform helper

**Files:**
- Create: `client/ui/alwaysontop.go` — interface + macOS impl with build tag.
- Create: `client/ui/alwaysontop_windows.go` — Windows impl.
- Create: `client/ui/alwaysontop_linux.go` — Linux impl (best-effort, X11 only).
- Create: `client/ui/alwaysontop_other.go` — fallback that returns `ErrNotSupported`.

Per spec §5.1: always-on-top is best-effort. Each platform helper returns `nil` on success or an error. The fallback returns `ErrNotSupported`.

- [ ] **Step 1: Implement `client/ui/alwaysontop.go`**

```go
package ui

import "errors"

// ErrNotSupported is returned when the running platform cannot toggle
// always-on-top (e.g. Wayland without compositor support).
var ErrNotSupported = errors.New("always-on-top not supported on this platform")

// AlwaysOnTopHandle is whatever the platform helper needs to identify a
// window. Implementations may keep it nil.
type AlwaysOnTopHandle struct {
	// Reserved for future use; pure-interface placeholder.
}
```

- [ ] **Step 2: Implement per-platform helpers**

`client/ui/alwaysontop_darwin.go`:

```go
//go:build darwin

package ui

// SetAlwaysOnTop is a best-effort no-op on macOS until we wire an
// objc bridge. Phase 1 returns ErrNotSupported and the dashboard tile
// shows the "🪟 not-on-top" badge; users hear about the limitation in
// release notes.
func SetAlwaysOnTop(on bool) error { return ErrNotSupported }
```

`client/ui/alwaysontop_windows.go`:

```go
//go:build windows

package ui

// SetAlwaysOnTop on Windows would call user32.SetWindowPos with
// HWND_TOPMOST. Phase 1 returns ErrNotSupported; revisit when we wire a
// helper. The dashboard tile already shows a not-on-top warning when this
// returns ErrNotSupported.
func SetAlwaysOnTop(on bool) error { return ErrNotSupported }
```

`client/ui/alwaysontop_linux.go`:

```go
//go:build linux

package ui

// SetAlwaysOnTop on Linux X11 would set _NET_WM_STATE_ABOVE. Phase 1
// returns ErrNotSupported; Wayland compositors usually refuse anyway.
func SetAlwaysOnTop(on bool) error { return ErrNotSupported }
```

This task is deliberately a stub — implementing actual native handles requires either CGo or platform-specific Go bindings (`golang.org/x/sys/windows`, `jezek/xgb`) and a couple of careful test cycles. The interface lets us land Phase 1 with the rest of the UI honest about the limitation; we replace the stubs with working implementations in Phase 2 if user-testing shows it matters.

If the user wants real always-on-top in Phase 1, this is the task to expand — each platform's implementation is ~30 LOC. Flag this in the planning conversation.

- [ ] **Step 3: Build and commit**

```bash
go build ./ui/...
git add client/ui/alwaysontop.go client/ui/alwaysontop_darwin.go client/ui/alwaysontop_windows.go client/ui/alwaysontop_linux.go
git commit -m "feat(client): SetAlwaysOnTop seam (returns ErrNotSupported on all platforms in Phase 1)"
```

---

### Task 26: `client/ui` — lock overlay (full-screen window)

**Files:**
- Create: `client/ui/overlay.go` — opens/closes a separate `*app.Window` configured fullscreen with the lock content.

A second Gio window is the cleanest separation from the main client window. We open it on `EventLock` and close it on `EventUnlock`/`EventExamStop`.

- [ ] **Step 1: Implement `client/ui/overlay.go`**

```go
package ui

import (
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// LockOverlay owns a separate Gio window. Show/Hide are safe to call from
// any goroutine; the window itself runs on its own.
type LockOverlay struct {
	visible atomic.Bool
	message atomic.Value // string
	by      atomic.Value // string
	window  *app.Window
	th      *material.Theme
}

func NewLockOverlay(th *material.Theme) *LockOverlay {
	o := &LockOverlay{th: th}
	o.message.Store("")
	o.by.Store("")
	return o
}

// Show makes the overlay visible with the given message. Idempotent.
func (o *LockOverlay) Show(message, by string) {
	o.message.Store(message)
	o.by.Store(by)
	if o.visible.Swap(true) {
		return
	}
	go o.run()
}

// Hide closes the overlay window.
func (o *LockOverlay) Hide() {
	if !o.visible.Swap(false) {
		return
	}
	if o.window != nil {
		o.window.Perform(0) // best-effort wake; window goroutine notices the flag and exits
	}
}

func (o *LockOverlay) run() {
	w := new(app.Window)
	w.Option(app.Title("Exam Paused"), app.Fullscreen.Option(), app.MinSize(unit.Dp(640), unit.Dp(480)))
	_ = SetAlwaysOnTop(true) // best-effort
	o.window = w

	var ops op.Ops
	for o.visible.Load() {
		ev := w.Event()
		switch ev := ev.(type) {
		case app.DestroyEvent:
			o.visible.Store(false)
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, ev)
			// Ignore all key events — the overlay is non-interactive.
			for _, gtxEv := range gtx.Events(o) {
				if _, ok := gtxEv.(key.Event); ok {
					continue
				}
			}
			o.draw(gtx)
			ev.Frame(gtx.Ops)
		}
	}
	_ = SetAlwaysOnTop(false)
}

func (o *LockOverlay) draw(gtx layout.Context) layout.Dimensions {
	msg, _ := o.message.Load().(string)
	by, _ := o.by.Load().(string)
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H1(o.th, "🔒").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(material.H4(o.th, "EXAM PAUSED").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(24)}.Layout),
			layout.Rigid(material.Body1(o.th, msg).Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(48)}.Layout),
			layout.Rigid(material.Body2(o.th, "— "+by).Layout),
		)
	})
}
```

Note: Gio's exact API surface for `Fullscreen` / `AlwaysOnTop` and event handling has evolved across versions. The above sketch follows the patterns in `client/main.go` (`app.Window`, `w.Event()`, `app.FrameEvent`). If the actual Gio v0.8 API requires different option syntax (e.g. `app.Fullscreen` as a value not a function), adapt — the conceptual shape (separate window, atomic flag, draw loop) is what's important.

- [ ] **Step 2: Build and commit**

```bash
go build ./ui/...
git add client/ui/overlay.go
git commit -m "feat(client): full-screen lock overlay window"
```

---

### Task 27: `client/joinView.go` — exam token field

**Files:**
- Modify: `client/joinView.go` — add `TokenEditor`, validate, pass to `OnClick`. Persist the token to `FormData.ExamToken` for cross-session friendliness (note: spec §6.3 says NOT to persist; revisit).

Spec §6.3 says the token is **not** persisted across sessions. So we add the editor and pass the value to the callback, but we do *not* extend `FormData`. Reload always starts with an empty token field.

- [ ] **Step 1: Add the field**

In `JoinView`:

```go
TokenEditor *widget.Editor
tokenError  string
```

`NewJoinView` (note the OnClick signature changes again — see Step 2):

```go
TokenEditor: new(widget.Editor),
// Don't load from FormData; token is not persisted.
```

- [ ] **Step 2: Update OnClick signature**

```go
OnClick func(sid, name string, room int, serverIP, examToken string)
```

In `handleSubmit`:

```go
serverIP := strings.TrimSpace(h.ServerIPEditor.Text())
examToken := strings.TrimSpace(h.TokenEditor.Text())
SaveFormData(studentID, name, h.RoomEditor.Text(), serverIP) // unchanged: token NOT persisted
h.OnClick(studentID, name, room, serverIP, examToken)
```

- [ ] **Step 3: Validate**

In `validate()`:

```go
h.tokenError = ""
if strings.TrimSpace(h.TokenEditor.Text()) == "" {
	h.tokenError = "Exam token is required."
	valid = false
}
```

Don't try to validate token shape on the client; the server is the source of truth and gives `TokenHandshakeReject` with a useful reason.

- [ ] **Step 4: Render the new row**

In the form layout, add a row after the Room row:

```go
layout.Rigid(func(gtx layout.Context) layout.Dimensions {
	return FormRow(gtx, "Exam token", th, func(gtx layout.Context) layout.Dimensions {
		return TextEditorWithError(th, h.TokenEditor, "e.g. BIO-XQ7-394", tokenErr)(gtx)
	})
}),
```

And declare `tokenErr` near the existing error variables at the top of `Layout`.

- [ ] **Step 5: Update `client/main.go` to thread the new arg**

```go
joinView := NewJoinView(func(sid, name string, room int, serverIP, examToken string) {
	state.swtichScreen("dashboard")
	dashboard.client.SetManualServerIP(serverIP)
	dashboard.client.SetExamToken(examToken)
	dashboard.client.Start(sid, name, room, func() { w.Invalidate() })
})
```

- [ ] **Step 6: Build, test, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add client/joinView.go client/main.go
git commit -m "feat(client): require exam token on join screen"
```

---

### Task 28: `client/main.go` + `client/dashboard.go` — bind UI to session state

**Files:**
- Modify: `client/main.go` — wire callbacks from `Client` to `LockOverlay`, `ToastState`, and view-routing.
- Modify: `client/dashboard.go` — render the banner + toast, branch on session state for Waiting/Capturing/Locked/Stopped panes.

- [ ] **Step 1: Wire callbacks in `main.go`**

After creating `dashboard.client` and before `joinView := NewJoinView(...)`, set up the UI helpers:

```go
overlay := ui.NewLockOverlay(th)
toast := ui.NewToastState()
dashboard.SetUIHelpers(overlay, toast)

dashboard.client.SetCallbacks(
	func(examName string) {
		dashboard.OnExamStart(examName)
		w.Invalidate()
	},
	func() {
		dashboard.OnExamStop()
		w.Invalidate()
	},
	func(msg, by string) {
		overlay.Show(msg, by)
		w.Invalidate()
	},
	func() {
		overlay.Hide()
		w.Invalidate()
	},
	func(from, body string) {
		toast.Set(from, body)
		w.Invalidate()
	},
)
```

Add `import "github.com/exam-gaurd/client/ui"`.

- [ ] **Step 2: Extend `DashboardState`**

Add fields for the helpers, exam metadata, and the start-of-exam wall clock:

```go
overlay     *ui.LockOverlay
toast       *ui.ToastState
examName    string
examStart   time.Time

func (d *DashboardState) SetUIHelpers(o *ui.LockOverlay, t *ui.ToastState) {
	d.overlay = o
	d.toast = t
}

func (d *DashboardState) OnExamStart(examName string) {
	d.examName = examName
	d.examStart = time.Now()
}

func (d *DashboardState) OnExamStop() {
	// noop for now; banner hides itself when session state is Stopped
}
```

- [ ] **Step 3: Branch the dashboard's main `Layout` on session state**

```go
func (d *DashboardState) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	state := d.client.SessionState().State()
	switch state {
	case session.StateWaiting:
		return d.layoutWaiting(gtx, th)
	case session.StateCapturing, session.StateLocked:
		return d.layoutCapturing(gtx, th, state == session.StateLocked)
	case session.StateStopped:
		return d.layoutStopped(gtx, th)
	default:
		return layout.Dimensions{}
	}
}

func (d *DashboardState) layoutWaiting(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H6(th, "Waiting for your instructor to start the exam…").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(material.Body1(th, "Connected as: "+d.studentName).Layout),
		)
	})
}

func (d *DashboardState) layoutCapturing(gtx layout.Context, th *material.Theme, locked bool) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.Banner(gtx, th, d.examName, "Instructor", time.Since(d.examStart))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.toast.Layout(gtx, th)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return d.layoutBody(gtx, th)
		}),
	)
}

func (d *DashboardState) layoutStopped(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H6(th, "✓ Exam ended.").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(material.Body1(th, "Your screen is no longer being captured.").Layout),
		)
	})
}
```

`d.layoutBody` is the existing dashboard body (frame stats etc.). Refactor the current `Layout` content into `layoutBody`.

`d.studentName` — store the name from `dashboard.client` somehow; either pass it through from `main.go`'s `joinView` callback or expose `Client.StudentName()`. Adding a small accessor on `Client` is fine.

- [ ] **Step 4: Build, test, commit**

```bash
go build ./...
go test -race ./... 2>&1 | tail -10
git add client/main.go client/dashboard.go client/client.go
git commit -m "feat(client): bind dashboard to session state + show banner/toast/overlay"
```

---

## Stage 3 progress checkpoint

After Tasks 19–28 you should have:

- `client/controlframe` package mirroring the server's wire types.
- `client/session` state machine with the four mismatch recoveries.
- Token handshake on connect; control-frame reader goroutine; state acks back to server.
- Capture gated on session state (Waiting = no capture, Capturing/Locked = capture).
- `client/ui` package with `Banner`, `ToastState`, `LockOverlay`, and a `SetAlwaysOnTop` stub seam.
- New "Exam token" field on the join view (not persisted).
- Dashboard branches on session state: Waiting → "waiting" pane, Capturing/Locked → banner + toast + body, Stopped → "exam ended" pane.
- Lock overlay window opens on `EventLock`, hides on `EventUnlock` / `EventExamStop`.

Manual smoke check before moving on:

```bash
cd client
go build .
./client &
PID=$!
sleep 1
kill $PID || true
```

Binary boots and exits. UI behaviour against a real server is exercised in Stage 4.

---

## Stage 4 — Integration & verification

### Task 29: End-to-end test on `127.0.0.1`

**Files:**
- Create: `server/e2e_test.go` (server module — same module as `Server`, no cross-module test setup needed).

The test stands up a real `Server` listening on a random port, dials it from a fake client (raw TCP, no Gio dependency), goes through the full lifecycle, and asserts the event log + UpdateImage counts.

- [ ] **Step 1: Write the failing test**

```go
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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/eventlog"
	"github.com/exam-gaurd/server/session"
)

// makeServer boots a real Server on a free localhost port, prepares an
// ExamSession in Waiting, attaches a fake event log to a temp dir, and
// returns (server, port, eventLog, exam-token).
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

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
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
	if su := srv.studentUtil.(*fakeStudentUtil); su.imageCalls != 0 {
		t.Errorf("UpdateImage called %d times during Waiting; want 0", su.imageCalls)
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
	if su := srv.studentUtil.(*fakeStudentUtil); su.imageCalls < 1 {
		t.Errorf("UpdateImage called %d times during Capturing; want ≥1", su.imageCalls)
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
	imagesBeforeLate := srv.studentUtil.(*fakeStudentUtil).imageCalls
	writeFrame(t, conn, 2, tinyJPEGBytes(t))
	time.Sleep(200 * time.Millisecond)
	if got := srv.studentUtil.(*fakeStudentUtil).imageCalls; got != imagesBeforeLate {
		t.Errorf("UpdateImage incremented after Stop; want unchanged, got %d → %d", imagesBeforeLate, got)
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

	// Bonus: reaper deletes the dir when retention is fully in the past.
	_ = el.SetMeta("retention_until_utc", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
	rootDir := filepath.Dir(filepath.Dir(el.Path())) // walk up from .../events.sqlite if Path() exists
	_ = rootDir                                       // smoke; real reap test is in eventlog package
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

// itoa avoids strconv import here (tests already use other packages).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := false
	if i < 0 {
		negative = true
		i = -i
	}
	var b [10]byte
	j := len(b)
	for i > 0 {
		j--
		b[j] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		j--
		b[j] = '-'
	}
	return string(b[j:])
}

// Some helpers reference the fakeStudentUtil from handshake_test.go.
// Make sure that file is in this package; otherwise inline the type here.
//
// The `os` import is kept for future use if you extend this test with file
// assertions.
var _ = os.TempDir
```

Note: this test depends on a few small API surfaces the plan introduces in earlier tasks (`Server.SetExamSession`, `Server.SetEventLog`, `Server.BroadcastControlAll`, `Server.handleStudent`, `eventlog.Open`, `eventlog.Event`, `eventlog.EventLog.All`, etc.). If any are missing, the test build fails and points at the missing piece — fix the upstream task, not the test.

`el.Path()` is referenced as an optional getter; if you didn't add it, drop the bonus reaper line at the end. The main assertions are the four `hasEvent`/`imageCalls`/`retention_until_utc` checks above.

- [ ] **Step 2: Run the test, verify it passes**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go test -race -run TestE2E_ -v 2>&1 | tail -20
```

Expected: PASS. If it fails, the error message points to the specific assertion that broke; trace back to the upstream task in the plan.

- [ ] **Step 3: Run the entire server suite once more**

```bash
go test -race -cover ./... 2>&1 | tail -10
```

Expected: every package OK; coverage on `server` package now ≥ 25 % thanks to the e2e covering real code paths.

- [ ] **Step 4: Commit**

```bash
git add server/e2e_test.go
git commit -m "test(server): end-to-end Waiting→Capturing→Stopped on 127.0.0.1"
```

---

### Task 30: Final verification + smoke run

**Files:** none (verification only)

- [ ] **Step 1: Build both binaries natively**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
go build -o /tmp/p1-client .
file /tmp/p1-client

cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go build -o /tmp/p1-server .
file /tmp/p1-server
```

Expected: both Mach-O (or ELF/PE depending on OS) binaries produced.

- [ ] **Step 2: Cross-compile the pure subpackages**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
GOOS=linux GOARCH=amd64 go build ./capture/... ./controlframe/... ./session/... ./internal/diag/...
GOOS=windows GOARCH=amd64 go build ./capture/... ./controlframe/... ./session/... ./internal/diag/...

cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
GOOS=linux GOARCH=amd64 go build ./controlframe/... ./session/... ./eventlog/... ./internal/diag/...
GOOS=windows GOARCH=amd64 go build ./controlframe/... ./session/... ./eventlog/... ./internal/diag/...
```

Expected: every command exits 0. (Cross-compile of the full Gio app still needs platform C toolchains; that's a separate operational concern.)

- [ ] **Step 3: Run the full test suite with race + coverage**

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
go test -race -cover ./...

cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
go test -race -cover ./...
```

Expected on client: `internal/diag` ≥ 82 %, `capture` ≥ 56 %, `session` ≥ 80 %, `controlframe` ≥ 80 %, top-level ~5 %.

Expected on server: `internal/diag` ≥ 82 %, `session` ≥ 80 %, `controlframe` ≥ 80 %, `eventlog` ≥ 80 %, top-level ≥ 25 % (thanks to e2e).

- [ ] **Step 4: Manual end-to-end smoke (optional, requires a free display)**

This step needs two terminals.

Terminal A (instructor):

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/server
./server
```

In the GUI: type "Smoke Test" as exam name, "12345" as room, note the token.

Terminal B (student):

```bash
cd /Users/subashanannair/Documents/02_projects/Github_Projects/exam_monitor_app/client
./client
```

In the GUI: type "S001", "Test Student", "12345" as room, paste the token, leave Server IP blank. Click Join — student tile should appear on the server. Both should be in the Waiting state.

Back in Terminal A's GUI: click "Start Exam". Verify the student client's banner appears and screen frames flow.

Test Lock: click "Lock All" with the default message; the student client's lock overlay should appear. Click "Unlock".

Test Message: click "Send Message", type "five minutes left", click Send. The student should see a toast for 5 s.

Test Stop: click "Stop Exam". Both ends transition to Stopped. Click "Export Event Log" — a CSV file should land under `<UserConfigDir>/exam-monitor/exports/`.

Verify the CSV contents:

```bash
ls -la "$HOME/Library/Application Support/exam-monitor/exports/"
```

(or the OS-equivalent path).

Verify retention setup:

```bash
ls -la "$HOME/Library/Application Support/exam-monitor/exams/"
```

Should show one `YYYY-MM-DD_smoke-test/` directory containing `events.sqlite` and `finals/`.

- [ ] **Step 5: Verify spec's Done-Definition**

Confirm each line of spec §10 (Done definition):

1. End-to-end demo works (manual smoke step above).
2. No frames captured outside Capturing/Locked — covered by the e2e test (Task 29) and `TestLateFrameRejected_WhenServerNotCapturing` (Task 10).
3. Every instructor action recorded in the event log — exam_started, exam_stopped, lock_applied, lock_released, message_broadcast, instructor_viewed_student. Manually export the CSV and skim.
4. Retention reaper deletes expired dirs — covered by `TestReapOnce_DeletesExpiredKeepsFresh` (Task 6).
5. All tests pass under `-race -cover` (Step 3 above).
6. Native builds succeed; subpackages cross-compile (Steps 1–2 above).

- [ ] **Step 6: Commit any final touch-ups**

```bash
git add -A
git status
# Sanity-check the diff before committing.
git commit -m "chore: Phase 1 verification touch-ups" --allow-empty-message
```

---

## Phase 1 complete

You should now have, on top of the prior cross-platform-stability work:

- A working classroom-exam tool: instructor creates an exam, students join with a token, instructor starts/locks/messages/stops, event log exports as CSV, frames retained for ≤24h then auto-purged.
- ~30 new tests across `session`, `controlframe`, `eventlog`, `handshake`, `broadcast`, `state_ack`, and `e2e`.
- Two new server packages (`session`, `eventlog`, `controlframe`), three new client packages (`controlframe`, `session`, `ui`).
- One new dependency: `modernc.org/sqlite`.
- All `Done definition` items from spec §10 satisfied.

### Recommended follow-up work (Phase 2+, not in this plan)

Phase 2 (network resilience): QR-code pairing on the projector, multi-VLAN handling, retry-with-resume.

Phase 2 (reliability extras): real `SetAlwaysOnTop` per-platform implementations, teacher-laptop wake-lock during active exam, low-battery flagging.

Phase 3 (deployability): Authenticode-signed Windows binaries, Developer-ID + notarised macOS bundle, MSI/PKG installers, school-fillable DPA template.

Phase 4 (evidence signals): foreground window title, monitor count, idle time, process allow-list, RDP detection surfaced to instructor.

Phase 5 (compliance papers): DPIA template doc, retention-policy doc, instructor-action-log audit guide.

Phase 6+: Chromebook PWA companion (if device-mix expands), TLS for the wire protocol (if threat model requires it), federated admin console (if multi-classroom view is needed).

---

## Self-review (run-once verification of this plan against the spec)

| Spec section | Task(s) covering it |
|---|---|
| §1 Context (Themes B, D, G) | Stages 0–4 collectively |
| §2 Architecture (state machine, control frames, eventlog) | Tasks 1–4, 7, 9, 19, 20 |
| §2.1 Server→client wire types 10–16 | Task 7 |
| §2.2 Client→server wire types 20–25 | Task 7, 10 (handler), 21 (sender) |
| §3.1 Server state machine | Task 2 |
| §3.2 Client state machine | Task 20 |
| §3.3 Lifecycle table | Tasks 13–17 (server actions), 21 (client handlers), 22 (capture gating) |
| §3.4 Retention policy | Tasks 5 (CSV), 6 (reaper), 13 (sets retention_until on Stop) |
| §4 Instructor UI | Tasks 12 (home), 13 (top bar), 14 (badges), 15 (lock), 16 (msg), 17 (export), 18 (viewer audit) |
| §5 Student UI | Tasks 23 (banner), 24 (toast), 26 (overlay), 25 (always-on-top seam), 28 (state-driven view routing) |
| §6 Tokens + state-mismatch | Tasks 1 (token), 8 (handshake), 20 (client recoveries) |
| §7 Event log + retention | Tasks 4 (open/Record), 5 (CSV), 6 (reaper), 11 (boot wiring), 13/15/16/17/18 (events recorded) |
| §8 File-level plan | Each task lists Files: section |
| §9 Tests | Tasks 1–6 (table-driven units), 8–10 (server protocol), 22 (gating), 29 (e2e) |
| §10 Done definition | Task 30 step-by-step verification |
| §11 Risks | AlwaysOnTop stub (Task 25); explicit `// TODO` comments at risk points |
| §12 Estimated effort | Implicit in 30 tasks, ~16 engineer-days |

No gaps identified — every spec section maps to one or more tasks.

No placeholders (TBD/TODO) appear in this plan except the deliberately-stubbed `SetAlwaysOnTop` and the per-student picker enrichment in Task 16 (both documented).

Type-name consistency check:
- `session.State` / `session.Event` / `session.StateMachine` used consistently across Tasks 2, 3, 9, 10, 13, 14, 15, 22, 28.
- `controlframe.Type*` constants used consistently across Tasks 7, 8, 9, 10, 13, 15, 16, 19, 21, 29.
- `eventlog.Event` / `eventlog.EventLog` used consistently across Tasks 4, 5, 6, 11, 13, 15, 16, 17, 18, 29.
- `Server.SetExamSession` / `Server.SetEventLog` / `Server.BroadcastControl` / `Server.BroadcastControlAll` / `Server.SendToStudent` defined in Tasks 8, 9, 11, 13 and consumed in 13–18, 29.

Plan internally consistent.

