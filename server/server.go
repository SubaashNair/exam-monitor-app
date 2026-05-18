package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/exam-gaurd/server/controlframe"
	"github.com/exam-gaurd/server/eventlog"
	"github.com/exam-gaurd/server/internal/diag"
	"github.com/exam-gaurd/server/session"
)

const (
	HEADER_SIZE          = 8
	READ_TIMEOUT         = 10 * time.Second
	REMOVAL_GRACE_PERIOD = 5 * time.Second // Keep student visible for a few seconds after disconnect
)

// registeredConn is the writer end of an accepted TCP connection plus its
// last-known session state. Held in Server.conns keyed by studentID.
type registeredConn struct {
	writer net.Conn // we only need Write + SetWriteDeadline
	state  session.State
}

type Server struct {
	listener    *net.TCPListener
	isRunning   atomic.Bool
	studentUtil StudentUtil
	// Track active connections per student ID to handle reconnection race conditions
	activeConns    map[string]int64 // studentID -> connection timestamp
	activeConnsMu  sync.Mutex
	mdnsCancel     func() // returned by advertiseMDNS; called on Stop
	mdnsCancelOnce sync.Once

	// NEW (Task 8):
	examSession   *session.ExamSession
	examSessionMu sync.RWMutex

	// per-student registry (NEW Task 9)
	conns   map[string]*registeredConn
	connsMu sync.RWMutex

	// NEW (Task 11):
	eventLog  *eventlog.EventLog
	examsRoot string
}

type StudentUtil interface {
	AddStudent(id, name string)
	RemoveStudent(id string)
	UpdateImage(id string, img image.Image)
	UpdateName(id string, name string)
	isExists(id string) bool
}

func NewServer() *Server {
	server := Server{
		isRunning:   atomic.Bool{},
		activeConns: make(map[string]int64),
		conns:       make(map[string]*registeredConn),
	}
	server.isRunning.Store(false)
	return &server
}

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

func (s *Server) SetEventLog(el *eventlog.EventLog) {
	s.eventLog = el
}

func (s *Server) EventLog() *eventlog.EventLog { return s.eventLog }

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
	// Type-assertion shortcut: in production studentUtil is always *DashboardState.
	// This is intentional for Phase 1; a SetStudentState() interface method is the
	// cleaner long-term approach.
	if ds, ok := s.studentUtil.(*DashboardState); ok {
		if stu := ds.studentManager.GetByID(id); stu != nil {
			stu.State = st
		}
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

func (s *Server) Start(port int) {
	if s.studentUtil == nil {
		return
	}
	s.isRunning.Store(true)
	slog.Info("server starting listeners", "port", port)
	s.mdnsCancel = advertiseMDNS(port)
	go func() {
		defer diag.RecoverPanic("broadcast_host")
		s.broadcastHost(port)
	}()
	go func() {
		defer diag.RecoverPanic("tcp_accept_loop")
		listener, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
		if err != nil {
			slog.Error("tcp listen failed", "port", port, "err", err)
			s.isRunning.Store(false)
			return
		}
		defer listener.Close()
		s.listener = listener
		slog.Info("tcp listener ready", "port", port)
		for s.isRunning.Load() {
			conn, err := listener.AcceptTCP()
			if err != nil {
				continue
			}
			conn.SetKeepAlive(true)
			conn.SetKeepAlivePeriod(5 * time.Second)
			conn.SetNoDelay(true)

			go func() {
				defer diag.RecoverPanic("handle_student")
				s.handleStudent(conn)
			}()
		}

	}()
}

func (s *Server) registerConnection(id string) int64 {
	s.activeConnsMu.Lock()
	defer s.activeConnsMu.Unlock()
	timestamp := time.Now().UnixNano()
	s.activeConns[id] = timestamp
	return timestamp
}

func (s *Server) scheduleStudentRemoval(id string, connTimestamp int64) {
	time.Sleep(REMOVAL_GRACE_PERIOD)

	s.activeConnsMu.Lock()
	defer s.activeConnsMu.Unlock()

	// Only remove if this connection is still the active one for this student
	// If a newer connection exists (reconnected during grace period), don't remove
	if currentTimestamp, exists := s.activeConns[id]; exists {
		if currentTimestamp == connTimestamp {
			delete(s.activeConns, id)
			s.studentUtil.RemoveStudent(id)
		}
		// A newer connection exists, don't remove the student
	}
}

func (s *Server) handleStudent(socket *net.TCPConn) {
	defer socket.Close()

	if err := s.performHandshake(socket); err != nil {
		slog.Info("token handshake failed", "remote", socket.RemoteAddr().String(), "err", err)
		return
	}

	// Register the connection under the remote address as a placeholder key
	// until NAME (type 0) arrives and we know the student ID.
	rc := &registeredConn{writer: socket, state: session.StateWaiting}
	key := socket.RemoteAddr().String()
	s.registerConn(key, rc)
	defer func() {
		s.unregisterConn(key) // unregisters whatever `key` is at function exit
	}()

	id := ""
	var connTimestamp int64 = 0
	header := make([]byte, HEADER_SIZE)
	data := make([]byte, 0)

	for s.isRunning.Load() {
		socket.SetReadDeadline(time.Now().Add(READ_TIMEOUT))

		_, err := io.ReadFull(socket, header)

		if err != nil {
			break
		}

		dataType, dataSize, err := unpackHeader(header)

		if err != nil || dataSize <= 0 || dataSize > 5*1024*1024 {
			break
		}

		if len(data) < dataSize {
			data = make([]byte, dataSize)
		}

		socket.SetReadDeadline(time.Now().Add(READ_TIMEOUT))
		_, err = io.ReadFull(socket, data[:dataSize])

		if err != nil {
			break
		}

		switch dataType {
		case 0:
			info := string(data[:dataSize])
			parts := strings.SplitN(info, "###", 2)
			if len(parts) != 2 {
				break
			}
			id = strings.TrimSpace(parts[0])
			name := strings.TrimSpace(parts[1])

			connTimestamp = s.registerConnection(id)

			// Move conn registry entry from placeholder key to the real student ID.
			s.unregisterConn(key)
			key = id
			s.registerConn(key, rc)

			if !s.studentUtil.isExists(id) {
				slog.Info("student connected", "id", id, "name", name, "remote", socket.RemoteAddr().String())
				s.studentUtil.AddStudent(id, name)
			} else {
				s.studentUtil.UpdateName(id, name)
			}
		case 1:
			slog.Debug("student message", "id", id, "msg", string(data[:dataSize]))
		case 2:
			// PICTURE: only accept while server says Capturing/Locked.
			es := s.ExamSession()
			if es == nil || (es.Machine.State() != session.StateCapturing && es.Machine.State() != session.StateLocked) {
				slog.Debug("late frame rejected", "id", id, "server_state", func() string {
					if es == nil {
						return "no_session"
					}
					return es.Machine.State().String()
				}())
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
	}

	// Schedule student removal with grace period
	// If client reconnects within the grace period, they won't be removed
	if id != "" && connTimestamp != 0 {
		go s.scheduleStudentRemoval(id, connTimestamp)
	}
}

func (s *Server) broadcastHost(port int) {
	address := net.UDPAddr{
		IP:   net.IPv4(255, 255, 255, 255),
		Port: port,
	}

	message := []byte("server")
	for s.isRunning.Load() {
		conn, err := net.DialUDP("udp", nil, &address)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		_, err = conn.Write(message)
		conn.Close()

		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *Server) Stop() {
	s.isRunning.Store(false)
	if s.mdnsCancel != nil {
		s.mdnsCancelOnce.Do(s.mdnsCancel)
	}
	if s.listener != nil {
		s.listener.Close()
	}
}

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

func unpackHeader(data []byte) (uint16, int, error) {
	if len(data) < HEADER_SIZE || string(data[:2]) != "HE" {
		return 0, 0, errors.New("invalid header")
	}

	status := uint16(binary.BigEndian.Uint16(data[2:4]))
	length := int(binary.BigEndian.Uint32(data[4:8]))

	return status, length, nil
}
