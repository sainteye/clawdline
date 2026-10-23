// Package antigravityprocess owns one personal ACP process and its regular pipes.
// It does not discover credentials or retry calls. Model selection and the exact
// metadata-only /usage command use the same authenticated personal/free session.
package antigravityprocess

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
)

// Delivery distinguishes a local refusal from an acknowledged response and an
// uncertain write. Uncertain calls must never be automatically replayed.
type Delivery string

const (
	NotWritten   Delivery = "not_written"
	Acknowledged Delivery = "acknowledged"
	Uncertain    Delivery = "uncertain"
)

// Error contains no provider message, path, auth URL or underlying OS error.
type Error struct {
	Code     string
	RPCCode  int64
	Delivery Delivery
}

func (e *Error) BeforeWrite() bool                  { return e.Delivery == NotWritten }
func (e *Error) Error() string                      { return "antigravity_process_" + e.Code }
func failure(code string, delivery Delivery) *Error { return &Error{Code: code, Delivery: delivery} }

// Options are explicit owner policy. MaxFrameBytes bounds incoming/outgoing
// frames and the settings read. CallTimeout bounds calls and callback work.
// OnUpdate receives a complete owned session/update envelope in wire order,
// including notifications after a prompt response. It must honor ctx and must
// not call Close or another client method synchronously. Returning an error stops
// the process; its text is never propagated. No callback means discard updates.
type Options struct {
	Executable, AccountDir, WorkingDir string
	Args                               []string
	MaxFrameBytes                      int
	CallTimeout                        time.Duration
	OnUpdate                           func(ctx context.Context, frame json.RawMessage) error
}

type Client struct {
	opts                             Options
	cmd                              *exec.Cmd
	in, out                          *os.File
	ctx                              context.Context
	cancel                           context.CancelFunc
	done, readDone, watchDone        chan struct{}
	responses                        chan json.RawMessage
	once                             sync.Once
	callMu, writeMu, stateMu         sync.Mutex
	terminal                         *Error
	closeErr                         error
	sequence                         uint64
	initialized, authenticated, free bool
	sessionID                        string
	usage                            *usageCapture // stateMu; readLoop owns chunk order
}

type usageCapture struct {
	text             string
	active, overflow bool
}

// Start does not authenticate. ctx owns the entire process lifetime. Cancellation
// kills the private group before reaping its leader and unblocks both pipes.
func Start(ctx context.Context, opts Options) (*Client, error) {
	if ctx == nil || opts.MaxFrameBytes <= 0 || opts.MaxFrameBytes == int(^uint(0)>>1) || opts.CallTimeout <= 0 ||
		!filepath.IsAbs(opts.Executable) || !filepath.IsAbs(opts.AccountDir) || !filepath.IsAbs(opts.WorkingDir) {
		return nil, failure("invalid_options", NotWritten)
	}
	if err := ctx.Err(); err != nil {
		return nil, failure("cancelled", NotWritten)
	}
	cmd := exec.Command(opts.Executable, append([]string(nil), opts.Args...)...)
	if err := prepareProcess(cmd); err != nil {
		return nil, err
	}
	// A real null FD avoids exec.Cmd's implicit stderr copy goroutine and
	// discards provider diagnostics without persisting auth links or paths.
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, failure("pipe_failed", NotWritten)
	}
	defer stderr.Close()
	cmd.Dir, cmd.Env, cmd.Stderr = opts.WorkingDir, personalEnv(opts.AccountDir), stderr
	childIn, in, err := os.Pipe()
	if err != nil {
		return nil, failure("pipe_failed", NotWritten)
	}
	out, childOut, err := os.Pipe()
	if err != nil {
		childIn.Close()
		in.Close()
		return nil, failure("pipe_failed", NotWritten)
	}
	cmd.Stdin, cmd.Stdout = childIn, childOut
	if err := cmd.Start(); err != nil {
		childIn.Close()
		childOut.Close()
		in.Close()
		out.Close()
		return nil, failure("start_failed", NotWritten)
	}
	childIn.Close()
	childOut.Close()
	life, cancel := context.WithCancel(ctx)
	c := &Client{opts: opts, cmd: cmd, in: in, out: out, ctx: life, cancel: cancel,
		done: make(chan struct{}), readDone: make(chan struct{}), watchDone: make(chan struct{}), responses: make(chan json.RawMessage)}
	go c.readLoop()
	go func() {
		defer close(c.watchDone)
		select {
		case <-life.Done():
			c.shutdown(failure("cancelled", Uncertain))
		case <-c.done:
		}
	}()
	return c, nil
}

func personalEnv(account string) []string {
	var env []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		key = strings.ToUpper(key)
		blocked := key == "BROWSER" || key == "LC_ALL"
		for _, marker := range []string{"API_KEY", "APIKEY", "VERTEX", "ADC", "APPLICATION_CREDENTIALS", "GOOGLE", "CLOUDSDK", "GCLOUD", "ENTERPRISE", "MODEL_PROVIDER", "GEMINI", "ANTIGRAVITY", "AGY_", "ANTHROPIC", "OPENAI", "AI_CREDIT"} {
			blocked = blocked || strings.Contains(key, marker)
		}
		if !blocked {
			env = append(env, pair)
		}
	}
	return append(env, "GEMINI_HOME="+account, "BROWSER="+disabledBrowser(), "LC_ALL=C")
}

// Initialize returns the raw result for the owner's capability codec. Success
// requires protocol v1; declarations are never authentication evidence.
func (c *Client) Initialize(ctx context.Context) (json.RawMessage, error) {
	if !c.callMu.TryLock() {
		return nil, failure("busy", NotWritten)
	}
	defer c.callMu.Unlock()
	if c.initialized {
		return nil, failure("invalid_state", NotWritten)
	}
	r, err := c.call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]string{"name": "clawdline", "version": "1"}})
	if err != nil {
		return nil, err
	}
	m, err := object(r)
	if err != nil || !bytes.Equal(bytes.TrimSpace(m["protocolVersion"]), []byte("1")) {
		return nil, c.badResult("unsupported_protocol")
	}
	c.initialized = true
	return r, nil
}

// AuthenticatePersonal requires explicit local personal/no-Credits settings and
// a successful authenticate RPC, not an advertised oauth-personal method.
func (c *Client) AuthenticatePersonal(ctx context.Context) (json.RawMessage, error) {
	if !c.callMu.TryLock() {
		return nil, failure("busy", NotWritten)
	}
	defer c.callMu.Unlock()
	if !c.initialized || c.authenticated {
		return nil, failure("invalid_state", NotWritten)
	}
	if err := c.checkPersonal(); err != nil {
		return nil, err
	}
	r, err := c.call(ctx, "authenticate", map[string]string{"methodId": "oauth-personal"})
	if err == nil {
		c.authenticated = true
	}
	return r, err
}

// NewSession retains the provider identity and refuses prompting unless its
// response explicitly declares the measured personal Starter free tier.
func (c *Client) NewSession(ctx context.Context) (json.RawMessage, error) {
	if !c.callMu.TryLock() {
		return nil, failure("busy", NotWritten)
	}
	defer c.callMu.Unlock()
	if !c.authenticated || c.SessionID() != "" {
		return nil, failure("invalid_state", NotWritten)
	}
	if err := c.checkPersonal(); err != nil {
		return nil, err
	}
	r, err := c.call(ctx, "session/new", map[string]any{"cwd": c.opts.WorkingDir, "mcpServers": []any{}})
	if err != nil {
		return nil, err
	}
	m, err := object(r)
	if err != nil {
		return nil, c.badResult("invalid_session")
	}
	var id string
	if json.Unmarshal(m["sessionId"], &id) != nil || id == "" {
		return nil, c.badResult("invalid_session")
	}
	c.stateMu.Lock()
	c.sessionID = id
	c.stateMu.Unlock()
	meta, _ := object(m["_meta"])
	ag, _ := object(meta["antigravity"])
	tier, _ := object(ag["tier"])
	if !equalString(tier["id"], "free-tier") || !equalString(tier["name"], "Antigravity Starter Quota") {
		return nil, c.badResult("free_tier_required")
	}
	c.free = true
	return r, nil
}

// PID is the originally started child identifier, not a current liveness proof.
func (c *Client) PID() int { return c.cmd.Process.Pid }

// Err reports terminal transport failure without exposing provider diagnostics.
func (c *Client) Err() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.terminal == nil {
		return nil
	}
	e := *c.terminal
	return &e
}

func (c *Client) SessionID() string { c.stateMu.Lock(); defer c.stateMu.Unlock(); return c.sessionID }

type PromptResult struct {
	StopReason string
	Raw        json.RawMessage
}

// Prompt sends one text turn, once. A successful result is completion evidence;
// updates alone do not acknowledge a prompt or establish a stop reason.
func (c *Client) Prompt(ctx context.Context, text string) (PromptResult, error) {
	return c.prompt(ctx, text, nil, nil)
}

// PromptAccepted calls written once after the complete request reaches the
// private pipe. It is acceptance evidence, not completion or provider-read
// evidence. The callback must not block or reenter the client.
func (c *Client) PromptAccepted(ctx context.Context, text string, written func()) (PromptResult, error) {
	return c.prompt(ctx, text, nil, written)
}

// PromptChecked checks existing operation authority under the pipe write lock,
// after profile I/O and serialization. A refusal proves no prompt bytes were sent.
func (c *Client) PromptChecked(ctx context.Context, text string, before func() error, written func()) (PromptResult, error) {
	return c.prompt(ctx, text, before, written)
}

type beforeWriteError struct{ error }

func (e beforeWriteError) Unwrap() error     { return e.error }
func (e beforeWriteError) BeforeWrite() bool { return true }

// SetModelChecked acknowledges the model for the next turn, without prompting.
// The owner must validate modelID against this session's offered model list.
func (c *Client) SetModelChecked(ctx context.Context, modelID string, before func() error) error {
	if !c.callMu.TryLock() {
		return failure("busy", NotWritten)
	}
	defer c.callMu.Unlock()
	if !c.authenticated || !c.free || c.SessionID() == "" {
		return failure("not_ready", NotWritten)
	}
	if modelID == "" {
		return failure("invalid_model", NotWritten)
	}
	if err := c.checkPersonal(); err != nil {
		return err
	}
	r, err := c.callAccepted(ctx, "session/set_model", map[string]string{"sessionId": c.SessionID(), "modelId": modelID}, before, nil)
	if err != nil {
		return err
	}
	if _, err := object(r); err != nil {
		return c.badResult("invalid_model_result")
	}
	return nil
}

// Usage reads only the provider's exact /usage metadata command. It accepts no
// caller text. Its message chunks are withheld from OnUpdate; context and tool
// updates still flow normally. The existing frame budget also bounds the sum.
func (c *Client) Usage(ctx context.Context) (string, error) {
	if !c.callMu.TryLock() {
		return "", failure("busy", NotWritten)
	}
	defer c.callMu.Unlock()
	if !c.authenticated || !c.free || c.SessionID() == "" {
		return "", failure("not_ready", NotWritten)
	}
	if err := c.checkPersonal(); err != nil {
		return "", err
	}
	capture := &usageCapture{}
	c.stateMu.Lock()
	c.usage = capture
	c.stateMu.Unlock()
	defer func() { c.stateMu.Lock(); c.usage = nil; c.stateMu.Unlock() }()
	r, err := c.callAccepted(ctx, "session/prompt", map[string]any{"sessionId": c.SessionID(), "prompt": []any{map[string]string{"type": "text", "text": "/usage"}}}, func() error {
		c.stateMu.Lock()
		capture.active = true
		c.stateMu.Unlock()
		return nil
	}, nil)
	if err != nil {
		return "", err
	}
	m, err := object(r)
	if err != nil || !equalString(m["stopReason"], "end_turn") {
		return "", failure("invalid_usage_result", Acknowledged)
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if capture.overflow {
		return "", failure("usage_too_large", Acknowledged)
	}
	return capture.text, nil
}

func (c *Client) captureUsage(frame json.RawMessage) (bool, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.usage == nil || !c.usage.active {
		return false, nil
	}
	u, err := antigravityacp.DecodeSessionUpdate(frame, c.sessionID)
	if err != nil {
		return false, err
	}
	if u.Kind != "agent_message_chunk" {
		return false, nil
	}
	if len(c.usage.text)+len(u.Text) > c.opts.MaxFrameBytes {
		c.usage.overflow = true
	}
	if !c.usage.overflow {
		c.usage.text += u.Text
	}
	return true, nil
}

func (c *Client) prompt(ctx context.Context, text string, before func() error, written func()) (PromptResult, error) {
	if !c.callMu.TryLock() {
		return PromptResult{}, failure("busy", NotWritten)
	}
	defer c.callMu.Unlock()
	if !c.authenticated || !c.free || c.SessionID() == "" {
		return PromptResult{}, failure("not_ready", NotWritten)
	}
	if err := c.checkPersonal(); err != nil {
		return PromptResult{}, err
	}
	r, err := c.callAccepted(ctx, "session/prompt", map[string]any{"sessionId": c.SessionID(), "prompt": []any{map[string]string{"type": "text", "text": text}}}, before, written)
	if err != nil {
		return PromptResult{}, err
	}
	m, err := object(r)
	var stop string
	if err != nil || json.Unmarshal(m["stopReason"], &stop) != nil || stop == "" {
		return PromptResult{}, c.badResult("invalid_stop_reason")
	}
	return PromptResult{StopReason: stop, Raw: r}, nil
}

// Cancel is destructive local cancellation, not an acknowledged ACP stopReason.
// It also works while a call is blocked writing or awaiting a response.
func (c *Client) Cancel() error { return c.Close() }

// Close is concurrent-safe and idempotent. Owner callbacks must obey their ctx.
func (c *Client) Close() error {
	c.shutdown(failure("closed", Uncertain))
	<-c.readDone
	<-c.watchDone
	return c.closeErr
}

func (c *Client) shutdown(reason *Error) {
	c.once.Do(func() {
		c.stateMu.Lock()
		c.terminal = reason
		c.stateMu.Unlock()
		close(c.done)
		c.cancel()
		// No Wait before group kill: the unreaped leader pins group identity.
		if err := killProcess(c.cmd); err != nil {
			c.closeErr = failure("cleanup_failed", Uncertain)
		}
		c.in.Close()
		c.out.Close()
		_ = c.cmd.Wait() // exit status after owned SIGKILL is expected
	})
}

func (c *Client) badResult(code string) error {
	e := failure(code, Acknowledged)
	c.shutdown(e)
	return e
}

func (c *Client) checkPersonal() error {
	f, err := os.Open(filepath.Join(c.opts.AccountDir, "antigravity-acp", "settings.json"))
	if err != nil {
		return failure("personal_settings_required", NotWritten)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(c.opts.MaxFrameBytes)+1))
	if err != nil || len(b) > c.opts.MaxFrameBytes {
		return failure("personal_settings_required", NotWritten)
	}
	m, err := object(b)
	if err != nil {
		return failure("personal_settings_required", NotWritten)
	}
	auth, err := object(m["auth"])
	if err != nil || !bytes.Equal(bytes.TrimSpace(m["useG1Credits"]), []byte("false")) || !equalString(auth["type"], "oauth-personal") {
		return failure("personal_settings_required", NotWritten)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.callAccepted(ctx, method, params, nil, nil)
}

func (c *Client) callAccepted(ctx context.Context, method string, params any, before func() error, written func()) (json.RawMessage, error) {
	if ctx == nil {
		return nil, failure("invalid_context", NotWritten)
	}
	select {
	case <-c.done:
		return nil, failure("closed", NotWritten)
	default:
	}
	if ctx.Err() != nil {
		return nil, failure("cancelled", NotWritten)
	}
	c.sequence++
	id := strconv.FormatUint(c.sequence, 10)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	if len(b) > c.opts.MaxFrameBytes {
		return nil, failure("frame_too_large", NotWritten)
	}
	callCtx, cancel := context.WithTimeout(ctx, c.opts.CallTimeout)
	defer cancel()
	var attempted atomic.Bool
	type reply struct {
		raw json.RawMessage
		err error
	}
	finished := make(chan reply)
	go func() {
		var r reply
		c.writeMu.Lock()
		if callCtx.Err() != nil {
			c.writeMu.Unlock()
			finished <- reply{err: failure("cancelled", NotWritten)}
			return
		}
		if before != nil {
			if err := before(); err != nil {
				c.writeMu.Unlock()
				finished <- reply{err: beforeWriteError{err}}
				return
			}
		}
		attempted.Store(true)
		writeErr := c.writeUnlocked(b)
		c.writeMu.Unlock()
		if writeErr != nil {
			r.err = failure("write_failed", Uncertain)
		} else {
			if written != nil {
				written()
			}
			select {
			case frame := <-c.responses:
				r.raw, r.err = response(frame, id)
			case <-c.done:
				c.stateMu.Lock()
				e := *c.terminal
				c.stateMu.Unlock()
				e.Delivery = Uncertain
				r.err = &e
			case <-callCtx.Done():
				r.err = failure("cancelled", Uncertain)
			}
		}
		finished <- r
	}()
	select {
	case r := <-finished:
		if e, ok := r.err.(*Error); ok && e.Code != "rpc_error" {
			c.shutdown(e)
		}
		return r.raw, r.err
	case <-callCtx.Done():
		c.shutdown(failure("cancelled", Uncertain))
		<-finished // pipe close + group kill unblocks a stalled write
		delivery := NotWritten
		if attempted.Load() {
			delivery = Uncertain
		}
		return nil, failure("cancelled", delivery)
	}
}

func (c *Client) write(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeUnlocked(b)
}

func (c *Client) writeUnlocked(b []byte) error {
	b = append(b, '\n')
	n, err := c.in.Write(b)
	if err == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return err
}

func (c *Client) readLoop() {
	defer close(c.readDone)
	scanner := bufio.NewScanner(c.out)
	scanner.Buffer(make([]byte, 0), c.opts.MaxFrameBytes+1)
	for scanner.Scan() {
		frame := append(json.RawMessage(nil), scanner.Bytes()...)
		if len(frame) > c.opts.MaxFrameBytes {
			c.shutdown(failure("frame_too_large", Uncertain))
			return
		}
		m, err := object(frame)
		if err != nil || !equalString(m["jsonrpc"], "2.0") {
			c.shutdown(failure("invalid_frame", Uncertain))
			return
		}
		if method, exists := m["method"]; exists {
			if _, ok := m["result"]; ok {
				c.shutdown(failure("invalid_frame", Uncertain))
				return
			}
			if _, ok := m["error"]; ok {
				c.shutdown(failure("invalid_frame", Uncertain))
				return
			}
			if id, request := m["id"]; request {
				if !validID(id) {
					c.shutdown(failure("invalid_id", Uncertain))
					return
				}
				var reply map[string]any
				if equalString(method, "session/request_permission") {
					reply = map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}}
				} else {
					reply = map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "Unsupported client request"}}
				}
				b, _ := json.Marshal(reply)
				if len(b) > c.opts.MaxFrameBytes {
					c.shutdown(failure("frame_too_large", Uncertain))
					return
				}
				// A blocked response write is stopped by the active call deadline,
				// or the owner lifetime ctx. Apply an idle deadline too.
				timer := time.AfterFunc(c.opts.CallTimeout, func() { c.shutdown(failure("cancelled", Uncertain)) })
				err := c.write(b)
				timer.Stop()
				if err != nil {
					c.shutdown(failure("write_failed", Uncertain))
					return
				}
			} else if equalString(method, "session/update") {
				captured, err := c.captureUsage(frame)
				if err != nil {
					c.shutdown(failure("update_rejected", Uncertain))
					return
				}
				if captured || c.opts.OnUpdate == nil {
					continue
				}
				ctx, cancel := context.WithTimeout(c.ctx, c.opts.CallTimeout)
				err = c.opts.OnUpdate(ctx, frame)
				cancel()
				if err != nil {
					c.shutdown(failure("update_rejected", Uncertain))
					return
				}
			}
			continue
		}
		// The reply is the metadata boundary. Clear it here in wire order,
		// before handing the response to the waiting caller.
		c.stateMu.Lock()
		if c.usage != nil {
			c.usage.active = false
		}
		c.stateMu.Unlock()
		select {
		case c.responses <- frame:
		case <-c.done:
			return
		}
	}
	code := "eof"
	if scanner.Err() != nil {
		code = "read_failed"
		if scanner.Err() == bufio.ErrTooLong {
			code = "frame_too_large"
		}
	}
	c.shutdown(failure(code, Uncertain))
}

func response(frame []byte, expectedID string) (json.RawMessage, error) {
	m, err := object(frame)
	if err != nil || string(bytes.TrimSpace(m["id"])) != expectedID {
		return nil, failure("unexpected_id", Uncertain)
	}
	if _, exists := m["params"]; exists {
		return nil, failure("invalid_response", Uncertain)
	}
	r, hasResult := m["result"]
	e, hasError := m["error"]
	if hasResult == hasError {
		return nil, failure("invalid_response", Uncertain)
	}
	if hasError {
		m, err := object(e)
		var code int64
		var message string
		if err != nil || len(m["code"]) == 0 || bytes.Equal(m["code"], []byte("null")) || len(m["message"]) == 0 || m["message"][0] != '"' || json.Unmarshal(m["code"], &code) != nil || json.Unmarshal(m["message"], &message) != nil {
			return nil, failure("invalid_response", Uncertain)
		}
		return nil, &Error{Code: "rpc_error", RPCCode: code, Delivery: Acknowledged}
	}
	if _, err := object(r); err != nil {
		return nil, failure("invalid_response", Uncertain)
	}
	return r, nil
}

// object rejects duplicate direct keys, including escaped aliases. Nested
// security objects are parsed independently; normalized payloads belong to owner.
func object(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, failure("invalid_frame", Uncertain)
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok {
			return nil, failure("invalid_frame", Uncertain)
		}
		if _, exists := m[key]; exists {
			return nil, failure("invalid_frame", Uncertain)
		}
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		m[key] = v
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, failure("invalid_frame", Uncertain)
	}
	return m, nil
}

func equalString(raw []byte, want string) bool {
	var got string
	return json.Unmarshal(raw, &got) == nil && got == want
}
func validID(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	if raw[0] == '"' {
		var id string
		return json.Unmarshal(raw, &id) == nil
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return false
	}
	_, err := n.Int64()
	return err == nil
}
