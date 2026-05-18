package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nfnt/resize"

	"github.com/exam-gaurd/client/capture"
	"github.com/exam-gaurd/client/controlframe"
	"github.com/exam-gaurd/client/internal/diag"
	"github.com/exam-gaurd/client/session"
)

const (
	UPDATE_INTERVAL = time.Second / 2
	NAME            = 0
	MESSAGE         = 1
	PICTURE         = 2
	HEADER_SIZE     = 8
)

const (
	CONNECTED int = iota
	RUNNING
	NOT_RUNNING
)

type Client struct {
	isRunning      atomic.Bool
	isConnected    atomic.Bool
	framesSent     atomic.Int64
	socket         *net.TCPConn
	lastSentTime   atomic.Value
	onConnected    func()
	onError        func(error)
	cachedServerIP string
	manualServerIP string
	capturer       capture.Capturer

	// Session fields (added Task 21)
	examToken    string
	examName     string
	studentName  string
	sessionState *session.StateMachine
	onExamStart  func(examName string)
	onExamStop   func()
	onLock       func(message, lockedBy string)
	onUnlock     func()
	onMessage    func(from, body string)
}

func NewClient() *Client {
	client := Client{
		isRunning:    atomic.Bool{},
		isConnected:  atomic.Bool{},
		socket:       nil,
		sessionState: session.NewStateMachine(),
	}
	client.isConnected.Store(false)
	client.isRunning.Store(false)
	client.lastSentTime.Store(time.Time{})
	return &client
}

func (client *Client) SetCallbacks(onConnected func(), onError func(error)) {
	client.onConnected = onConnected
	client.onError = onError
}

// SetExamToken stores the exam token used during the handshake phase.
func (client *Client) SetExamToken(token string) { client.examToken = strings.TrimSpace(token) }

// SetSessionCallbacks registers callbacks for exam lifecycle events received
// from the server via control frames. Named SetSessionCallbacks to avoid
// collision with the existing SetCallbacks (connect/error callbacks).
func (client *Client) SetSessionCallbacks(
	onExamStart func(examName string),
	onExamStop func(),
	onLock func(message, lockedBy string),
	onUnlock func(),
	onMessage func(from, body string),
) {
	client.onExamStart = onExamStart
	client.onExamStop = onExamStop
	client.onLock = onLock
	client.onUnlock = onUnlock
	client.onMessage = onMessage
}

// SessionState returns the client-side session state machine (read-only use).
func (client *Client) SessionState() *session.StateMachine { return client.sessionState }

// StudentName returns the display name stored when Start was called.
func (client *Client) StudentName() string { return client.studentName }

// FramesSent returns the total number of PICTURE frames the client has
// successfully sent since the capture loop entered Capturing state.
func (client *Client) FramesSent() int64 { return client.framesSent.Load() }

func (client *Client) GetLastSentTime() time.Time {
	if t := client.lastSentTime.Load(); t != nil {
		return t.(time.Time)
	}
	return time.Time{}
}

// SetManualServerIP sets a user-entered server address. Empty string disables it.
func (client *Client) SetManualServerIP(ip string) {
	client.manualServerIP = strings.TrimSpace(ip)
}

func (client *Client) Start(studentId, studentName string, port int, updateUI func()) {
	client.studentName = studentName
	client.isRunning.Store(true)
	go func() {
		defer diag.RecoverPanic("client_run_goroutine")
		retryDelay := 1 * time.Second

		res := &resolver{
			manualIP:  client.manualServerIP,
			mdns:      discoverMDNS,
			broadcast: discoverServerWithTimeout,
		}

		for client.isRunning.Load() {
			client.isConnected.Store(false)
			updateUI()

			var serverAddress string
			var source string
			var err error

			if client.cachedServerIP != "" {
				serverAddress = client.cachedServerIP
				source = "cached"
			} else {
				serverAddress, source, err = res.Resolve(port)
				if err != nil {
					slog.Info("server discovery failed", "err", err)
					if client.onError != nil {
						client.onError(err)
					}
					client.cachedServerIP = ""
					time.Sleep(retryDelay)
					retryDelay = min(retryDelay*2, 8*time.Second)
					continue
				}

				client.cachedServerIP = serverAddress
			}
			slog.Info("resolved server", "ip", serverAddress, "source", source, "port", port)

			client.socket, err = net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.ParseIP(serverAddress), Port: port})
			if err != nil {
				if client.onError != nil {
					client.onError(err)
				}
				client.cachedServerIP = ""
				time.Sleep(retryDelay)
				retryDelay = min(retryDelay*2, 8*time.Second)
				continue
			}

			client.socket.SetKeepAlive(true)
			client.socket.SetKeepAlivePeriod(5 * time.Second)
			client.socket.SetNoDelay(true)

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

			client.isConnected.Store(true)
			if client.onConnected != nil {
				client.onConnected()
			}
			updateUI()
			retryDelay = 1 * time.Second

			client.SendStudentName(studentId + "###" + studentName)

			// Start reader goroutine for server→client control frames.
			go client.readControlFrames()

			for client.isConnected.Load() && client.isRunning.Load() {
				if client.sessionState.IsCapturing() {
					screenshot, err := client.captureScreen()
					if err != nil {
						client.isConnected.Store(false)
						break
					}
					err = client.SendScreenshot(screenshot)
					if err != nil {
						break
					}
					client.framesSent.Add(1)
					client.lastSentTime.Store(time.Now())
					updateUI()
				}
				time.Sleep(UPDATE_INTERVAL)
			}

			client.socket.Close()
		}

	}()
}

func discoverServerWithTimeout(port int, timeout time.Duration) (string, error) {
	address := net.UDPAddr{
		IP:   net.IPv4(0, 0, 0, 0),
		Port: port,
	}

	conn, err := net.ListenUDP("udp", &address)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(timeout))

	buffer := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return "", err
		}
		if string(buffer[:n]) == "server" {
			return addr.IP.String(), nil
		}
	}
}

func (client *Client) captureScreen() ([]byte, error) {
	if client.capturer == nil {
		c, err := capture.New()
		if err != nil {
			return nil, err
		}
		client.capturer = c
		slog.Info("capture backend initialised", "diagnostics", c.Diagnostics())
	}
	img, err := client.capturer.Capture()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	resizedImg := resize.Resize(720, 0, img, resize.Lanczos3)

	options := jpeg.Options{Quality: 80}
	if err := jpeg.Encode(&buf, resizedImg, &options); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (client *Client) SendStudentName(name string) error {
	if client.socket != nil {
		return client.sendData(NAME, []byte(name))
	}
	return nil
}

func (client *Client) SendScreenshot(screenshot []byte) error {
	if client.socket != nil {
		return client.sendData(PICTURE, screenshot)
	}
	return nil
}

func (client *Client) SendMessage(msg string) error {
	if client.socket != nil {
		return client.sendData(MESSAGE, []byte(msg))
	}
	return nil
}

func (client *Client) Stop() {
	client.isRunning.Store(false)
	client.isConnected.Store(false)
	if client.socket != nil {
		client.socket.Close()
	}
	if client.capturer != nil {
		_ = client.capturer.Close()
		client.capturer = nil
	}
}

func (client *Client) sendData(dataType uint16, dataBytes []byte) error {
	data := make([]byte, HEADER_SIZE+len(dataBytes))
	copy(data, client.packHeader(dataType, len(dataBytes)))
	copy(data[HEADER_SIZE:], dataBytes)
	_, err := client.socket.Write(data)

	if err != nil {
		slog.Debug("send failed; marking disconnected", "type", dataType, "size", len(dataBytes), "err", err)
		client.isConnected.Store(false)
		return err
	}
	return nil
}

func (client *Client) packHeader(status uint16, length int) []byte {
	data := make([]byte, HEADER_SIZE)

	copy(data, []byte("HE"))
	binary.BigEndian.PutUint16(data[2:], status)
	binary.BigEndian.PutUint32(data[4:], uint32(length))

	return data
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// sendFrame writes a single HE-header frame to the socket.
func (client *Client) sendFrame(typ uint16, body []byte) error {
	frame := make([]byte, HEADER_SIZE+len(body))
	copy(frame, "HE")
	binary.BigEndian.PutUint16(frame[2:4], typ)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(body)))
	copy(frame[HEADER_SIZE:], body)
	_, err := client.socket.Write(frame)
	return err
}

// unpackHeader parses a HEADER_SIZE-byte buffer and returns (type, bodyLen, err).
func (client *Client) unpackHeader(hdr []byte) (uint16, int, error) {
	if len(hdr) < HEADER_SIZE || string(hdr[:2]) != "HE" {
		return 0, 0, fmt.Errorf("invalid header")
	}
	typ := binary.BigEndian.Uint16(hdr[2:4])
	length := int(binary.BigEndian.Uint32(hdr[4:8]))
	return typ, length, nil
}

// sendHandshake sends a TokenHandshake frame and waits for the server's
// OK/Reject reply (10-second deadline).
func (client *Client) sendHandshake() error {
	body, err := controlframe.Marshal(controlframe.TokenHandshake{Token: client.examToken})
	if err != nil {
		return err
	}
	if err := client.sendFrame(controlframe.TypeTokenHandshake, body); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	// Read reply: one HE-header frame, must be OK or Reject.
	client.socket.SetReadDeadline(time.Now().Add(10 * time.Second))
	hdr := make([]byte, HEADER_SIZE)
	if _, err := io.ReadFull(client.socket, hdr); err != nil {
		return fmt.Errorf("read reply header: %w", err)
	}
	typ, length, err := client.unpackHeader(hdr)
	if err != nil {
		return fmt.Errorf("invalid reply header: %w", err)
	}
	replyBody := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(client.socket, replyBody); err != nil {
			return fmt.Errorf("read reply body: %w", err)
		}
	}
	client.socket.SetReadDeadline(time.Time{})

	switch typ {
	case controlframe.TypeTokenHandshakeOK:
		var ok controlframe.TokenHandshakeOK
		_ = controlframe.Unmarshal(replyBody, &ok)
		client.examName = ok.ExamName
		if ok.Started {
			// Joined an exam already in progress — recover into Capturing.
			_, _ = client.sessionState.Apply(session.EventExamStart)
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

// readControlFrames is a long-running goroutine that reads server→client
// control frames and routes them via dispatchControl.
func (client *Client) readControlFrames() {
	defer diag.RecoverPanic("client_reader_goroutine")
	hdr := make([]byte, HEADER_SIZE)
	for client.isConnected.Load() && client.isRunning.Load() {
		client.socket.SetReadDeadline(time.Now().Add(30 * time.Second))
		if _, err := io.ReadFull(client.socket, hdr); err != nil {
			slog.Debug("reader: header read failed", "err", err)
			return
		}
		typ, length, err := client.unpackHeader(hdr)
		if err != nil || length < 0 || length > 1024*1024 {
			return
		}
		body := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(client.socket, body); err != nil {
				return
			}
		}
		client.dispatchControl(typ, body)
	}
}

// dispatchControl routes a single incoming control frame to the appropriate
// callback and sends a state-ack reply.
func (client *Client) dispatchControl(typ uint16, body []byte) {
	switch typ {
	case controlframe.TypeExamStart:
		var p controlframe.ExamStart
		_ = controlframe.Unmarshal(body, &p)
		client.examName = p.ExamName
		// Fire the dashboard callback BEFORE applying the state transition.
		// If the UI thread happens to render between Apply() and the callback,
		// it would see state=Capturing with examStart=zero, producing a
		// saturated elapsed-time display. Setting examStart first avoids
		// that window.
		if client.onExamStart != nil {
			client.onExamStart(p.ExamName)
		}
		_, _ = client.sessionState.Apply(session.EventExamStart)
		_ = client.sendStateAck(controlframe.TypeStateCapturing)
	case controlframe.TypeExamStop:
		_, _ = client.sessionState.Apply(session.EventExamStop)
		if client.onExamStop != nil {
			client.onExamStop()
		}
		_ = client.sendStateAck(controlframe.TypeStateStopped)
	case controlframe.TypeLockScreen:
		var p controlframe.LockScreen
		_ = controlframe.Unmarshal(body, &p)
		_, _ = client.sessionState.Apply(session.EventLock)
		if client.onLock != nil {
			client.onLock(p.Message, p.LockedBy)
		}
		_ = client.sendStateAck(controlframe.TypeStateLocked)
	case controlframe.TypeUnlockScreen:
		_, _ = client.sessionState.Apply(session.EventUnlock)
		if client.onUnlock != nil {
			client.onUnlock()
		}
		_ = client.sendStateAck(controlframe.TypeStateCapturing)
	case controlframe.TypeBroadcastMsg:
		var p controlframe.BroadcastMsg
		_ = controlframe.Unmarshal(body, &p)
		if client.onMessage != nil {
			client.onMessage(p.From, p.Body)
		}
	default:
		slog.Debug("client: unknown control frame", "type", typ, "len", len(body))
	}
}

// sendStateAck sends a state acknowledgement frame with an empty JSON body.
func (client *Client) sendStateAck(typ uint16) error {
	return client.sendFrame(typ, []byte("{}"))
}
