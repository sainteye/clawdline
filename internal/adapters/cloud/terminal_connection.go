package cloud

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type terminalControlFrame struct {
	Type       string `json:"type"`
	Action     string `json:"action"`
	RequestID  string `json:"request_id"`
	Machine    string `json:"machine"`
	Viewer     string `json:"viewer"`
	Connection string `json:"connection"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type terminalControlResult struct {
	Type       string `json:"type"`
	Action     string `json:"action"`
	RequestID  string `json:"request_id"`
	Machine    string `json:"machine"`
	Viewer     string `json:"viewer"`
	Connection string `json:"connection"`
	Code       string `json:"code,omitempty"`
}

func terminalControlID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	var id [36]byte
	hex.Encode(id[0:8], raw[0:4])
	id[8] = '-'
	hex.Encode(id[9:13], raw[4:6])
	id[13] = '-'
	hex.Encode(id[14:18], raw[6:8])
	id[18] = '-'
	hex.Encode(id[19:23], raw[8:10])
	id[23] = '-'
	hex.Encode(id[24:36], raw[10:16])
	return string(id[:]), nil
}

// TerminalConnection sends relay-visible metadata only over the authenticated
// machine socket. One outstanding operation makes a stale or uncorrelated ack
// unable to activate a different connection.
func (t *Transport) TerminalConnection(ctx context.Context, action, viewer, connection string, expires time.Time) error {
	if action != "register" && action != "retire" {
		return errors.New("invalid terminal metadata action")
	}
	id, err := terminalControlID()
	if err != nil {
		return err
	}
	frame := terminalControlFrame{Type: "terminal_connection", Action: action, RequestID: id,
		Machine: t.opts.Identity.MachineID, Viewer: viewer, Connection: connection}
	if action == "register" {
		frame.ExpiresAt = expires.UnixMilli()
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	t.terminalControlMu.Lock()
	defer t.terminalControlMu.Unlock()
	deadlineCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	t.mu.Lock()
	conn := t.conn
	if conn == nil {
		t.mu.Unlock()
		return ErrConnClosed
	}
	answer := make(chan terminalControlResult, 1)
	t.terminalExpected, t.terminalPending = frame, answer
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.terminalPending = nil
		t.terminalExpected = terminalControlFrame{}
		t.mu.Unlock()
	}()
	if err := conn.WriteText(data, time.Now().Add(2*time.Second)); err != nil {
		return err
	}
	select {
	case <-deadlineCtx.Done():
		return deadlineCtx.Err()
	case result := <-answer:
		if result.Type == FrameTerminalConnectionRefused {
			return fmt.Errorf("relay terminal metadata refused: %s", result.Code)
		}
		t.mu.Lock()
		same := t.conn == conn
		t.mu.Unlock()
		if !same {
			return ErrConnClosed
		}
		return nil
	}
}

func (t *Transport) handleTerminalConnection(data []byte) {
	var result terminalControlResult
	if json.Unmarshal(data, &result) != nil {
		return
	}
	t.mu.Lock()
	expected, pending := t.terminalExpected, t.terminalPending
	t.mu.Unlock()
	if pending == nil || result.RequestID != expected.RequestID || result.Action != expected.Action ||
		result.Machine != expected.Machine || result.Viewer != expected.Viewer || result.Connection != expected.Connection {
		return
	}
	if (expected.Action == "register" && result.Type != FrameTerminalConnectionRegistered && result.Type != FrameTerminalConnectionRefused) ||
		(expected.Action == "retire" && result.Type != FrameTerminalConnectionRetired && result.Type != FrameTerminalConnectionRefused) {
		return
	}
	select {
	case pending <- result:
	default:
	}
}
