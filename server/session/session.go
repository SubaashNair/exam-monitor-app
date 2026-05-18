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
