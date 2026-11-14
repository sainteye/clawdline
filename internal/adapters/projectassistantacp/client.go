package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	value json.RawMessage
	err   error
}
type incoming struct {
	f          frame
	completion chan response
}
type nativePermission struct {
	turnID  string
	id      json.RawMessage
	options map[string]pa.PermissionOption
}

type Client struct {
	p                 *Process
	l                 Limits
	fence             pa.Fence
	sink              func(context.Context, app.ProtocolEvent) error
	readOnly          func([]byte) bool
	mu                sync.Mutex
	next              uint64
	pending           map[string]chan response
	permissions       map[string]nativePermission
	queue             chan incoming
	done              chan struct{}
	workerDone        chan struct{}
	stop              sync.Once
	failure           error
	sessionID         string
	turnID            string
	partial           string
	sequence          uint64
	ctx               context.Context
	cancel            context.CancelFunc
	adapterVersion    string
	ownedMCP          *OwnedMCP
	mcpMu             sync.Mutex
	mcpRevoked        bool
	permissionsSealed bool
	cancelled         bool
	promptID          string
	answerMu          sync.Mutex
	rpcSlots          chan struct{}
}

func NewClient(p *Process, l Limits, fence pa.Fence, sink func(context.Context, app.ProtocolEvent) error, readOnly func([]byte) bool) (*Client, error) {
	if p == nil || sink == nil {
		return nil, refusal(pa.Invalid, "acp_client")
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if err := pa.CheckFence(fence, fence); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{p: p, l: l, fence: fence, sink: sink, readOnly: readOnly, pending: make(map[string]chan response), permissions: make(map[string]nativePermission), queue: make(chan incoming, l.Notifications), done: make(chan struct{}), workerDone: make(chan struct{}), ctx: ctx, cancel: cancel, rpcSlots: make(chan struct{}, l.RPCPending)}
	go c.consume()
	go c.read()
	return c, nil
}

func (c *Client) initialize(ctx context.Context, q app.OpenRequest) error {
	var initialized struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession     bool `json:"loadSession"`
			MCPCapabilities struct {
				HTTP bool `json:"http"`
			} `json:"mcpCapabilities"`
			SessionCapabilities map[string]json.RawMessage `json:"sessionCapabilities"`
			Meta                map[string]json.RawMessage `json:"_meta"`
		} `json:"agentCapabilities"`
		AgentInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	b, err := c.call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]string{"name": "clawdline", "version": pa.ContractVersion}})
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &initialized); err != nil {
		return refusal(pa.Unsupported, "initialize_response")
	}
	if initialized.ProtocolVersion != 1 {
		return refusal(pa.ProviderVersionUnsupported, "acp_protocol_version")
	}
	if c.adapterVersion != "" && (initialized.AgentInfo.Version != c.adapterVersion || initialized.AgentInfo.Name != adapterPackage(q.Bot.Agent)) {
		return refusal(pa.ProviderVersionUnsupported, "acp_adapter_identity")
	}
	if q.Bot.Agent == pa.Codex && !hasCapability(initialized.AgentCapabilities.SessionCapabilities["systemPrompt"]) {
		return refusal(pa.Unsupported, "system_prompt")
	}
	if q.Bot.Agent == pa.ClaudeCode && !hasCapability(initialized.AgentCapabilities.Meta["claudeCode"]) {
		return refusal(pa.Unsupported, "claude_session_options")
	}
	if c.ownedMCP != nil && !initialized.AgentCapabilities.MCPCapabilities.HTTP {
		return refusal(pa.Unsupported, "http_mcp")
	}
	if q.Resume && q.PersistNativeSession && !nativeSessionUUID.MatchString(q.Binding.NativeSessionID) {
		return refusal(pa.ScopeDenied, "native_session_id")
	}
	params, err := sessionParameters(q, c.ownedMCP)
	if err != nil {
		return err
	}
	method := "session/new"
	if q.Resume {
		if !initialized.AgentCapabilities.LoadSession {
			return refusal(pa.ResumeUnsupported, "load_session")
		}
		method = "session/load"
		params["sessionId"] = q.Binding.NativeSessionID
		c.mu.Lock()
		c.sessionID = q.Binding.NativeSessionID
		c.mu.Unlock()
	}
	b, err = c.call(ctx, method, params)
	if err != nil {
		var f *pa.Failure
		if q.Resume && errors.As(err, &f) && strings.HasPrefix(f.Field, "rpc_") {
			// The native side answered that it cannot load this session, for
			// example after its transcript moved. The caller starts anew.
			return refusal(pa.ResumeUnsupported, "native_session_missing")
		}
		return err
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err = json.Unmarshal(b, &session); err != nil {
		return refusal(pa.Invalid, "session_response")
	}
	if q.Resume && session.SessionID == "" {
		session.SessionID = q.Binding.NativeSessionID
	}
	if q.Resume && session.SessionID != q.Binding.NativeSessionID {
		return refusal(pa.ScopeDenied, "native_session_id_changed")
	}
	if q.PersistNativeSession && !nativeSessionUUID.MatchString(session.SessionID) {
		return refusal(pa.Invalid, "native_session_id")
	}
	if session.SessionID == "" {
		return refusal(pa.Invalid, "session_id")
	}
	c.mu.Lock()
	c.sessionID = session.SessionID
	c.mu.Unlock()
	return nil
}
func (c *Client) SessionID() string { c.mu.Lock(); defer c.mu.Unlock(); return c.sessionID }

func (c *Client) Usage() app.SessionUsage {
	c.mu.Lock()
	usage := app.SessionUsage{PendingRPC: len(c.rpcSlots), PendingPermissions: len(c.permissions), QueuedNotifications: len(c.queue), CapturedBytes: len(c.partial)}
	c.mu.Unlock()
	c.p.mu.Lock()
	usage.StderrBytes = len(c.p.stderr)
	c.p.mu.Unlock()
	return usage
}

func (c *Client) fail(err error) {
	c.stop.Do(func() {
		c.mu.Lock()
		c.failure = err
		c.permissionsSealed = true
		c.permissions = make(map[string]nativePermission)
		for key, ch := range c.pending {
			ch <- response{err: err}
			c.removePending(key)
		}
		c.mu.Unlock()
		c.cancel()
		close(c.done)
	})
}
func (c *Client) write(ctx context.Context, f frame) error {
	f.JSONRPC = "2.0"
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b) > c.l.FrameBytes {
		return refusal(pa.CapacityFull, "frame_bytes")
	}
	err = c.p.Write(ctx, b)
	if err != nil {
		c.fail(err)
	}
	return err
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (c *Client) begin(ctx context.Context, method string, params any) (*pendingRPC, error) {
	c.mu.Lock()
	if c.failure != nil {
		err := c.failure
		c.mu.Unlock()
		return nil, err
	}
	select {
	case c.rpcSlots <- struct{}{}:
	default:
		c.mu.Unlock()
		return nil, refusal(pa.CapacityFull, "pending_rpc")
	}
	c.next++
	id := strconv.FormatUint(c.next, 10)
	ch := make(chan response, 1)
	c.pending[id] = ch
	if method == "session/prompt" {
		c.promptID = id
	}
	c.mu.Unlock()
	if err := c.write(ctx, frame{ID: json.RawMessage(id), Method: method, Params: raw(params)}); err != nil {
		c.mu.Lock()
		c.removePending(id)
		c.mu.Unlock()
		return nil, err
	}
	return &pendingRPC{c: c, id: id, ch: ch}, nil
}

// removePending holds c.mu; only the owner of an existing entry releases its
// shared ACP/MCP request slot. Late replies cannot release another request.
func (c *Client) removePending(id string) {
	if _, ok := c.pending[id]; ok {
		delete(c.pending, id)
		<-c.rpcSlots
	}
}

type pendingRPC struct {
	c  *Client
	id string
	ch chan response
}

func (p *pendingRPC) wait(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, p.c.l.RPCTimeout)
	defer cancel()
	select {
	case x := <-p.ch:
		return x.value, x.err
	case <-ctx.Done():
		p.c.mu.Lock()
		p.c.removePending(p.id)
		p.c.mu.Unlock()
		err := refusal(pa.ExecutionEffectUnknown, "rpc_timeout")
		p.c.fail(err)
		return nil, err
	case <-p.c.done:
		return nil, p.c.err()
	}
}
func (c *Client) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != nil {
		return c.failure
	}
	return refusal(pa.ExecutionEffectUnknown, "acp_closed")
}
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	p, err := c.begin(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return p.wait(ctx)
}

func (c *Client) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	if err := pa.CheckFence(q.Turn.Fence, c.fence); err != nil {
		return nil, err
	}
	if err := pa.ValidateMessage(q.Text); err != nil {
		return nil, err
	}
	// Prepare the complete opening before claiming the pending prompt lane.
	text := q.Turn.Instructions.Text + "\n" + q.Context.Text + "\n" + q.Text
	if q.Handoff != nil {
		opening, err := q.Handoff.Opening(q.Turn, q.Context)
		if err != nil {
			return nil, err
		}
		text = opening + "\n" + q.Text
	}
	c.mu.Lock()
	if c.permissionsSealed {
		c.mu.Unlock()
		return nil, refusal(pa.Revoked, "prompt_lifetime")
	}
	if c.turnID != "" {
		c.mu.Unlock()
		return nil, refusal(pa.CapacityFull, "active_prompt")
	}
	c.turnID = q.Turn.ID
	c.partial = ""
	sid := c.sessionID
	c.mu.Unlock()
	// Only fresh filtered context is sent. Displayed history is never implicit
	// model input. Instructions belong to this turn's immutable snapshot.
	blocks := []any{}
	if q.Quote != nil {
		if err := pa.ValidateMessage(q.Quote.Text); err != nil {
			return nil, err
		}
		if q.Quote.MessageID == "" {
			return nil, refusal(pa.Invalid, "quote_id")
		}
		blocks = append(blocks, map[string]string{"type": "text", "text": "Referenced message (" + q.Quote.MessageID + "):\n" + q.Quote.Text})
	}
	blocks = append(blocks, map[string]string{"type": "text", "text": text})
	p, err := c.begin(ctx, "session/prompt", map[string]any{"sessionId": sid, "prompt": blocks})
	if err != nil {
		return nil, err
	}
	return &promptPending{rpc: p}, nil
}

type promptPending struct{ rpc *pendingRPC }

func (p *promptPending) Wait(ctx context.Context) (app.PromptResult, error) {
	b, err := p.rpc.wait(ctx)
	if err != nil {
		return app.PromptResult{}, err
	}
	var result struct {
		Text       string `json:"text"`
		StopReason string `json:"stopReason"`
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return app.PromptResult{}, refusal(pa.ExecutionEffectUnknown, "prompt_result")
	}
	p.rpc.c.mu.Lock()
	partial := p.rpc.c.partial
	cancelled := p.rpc.c.cancelled
	p.rpc.c.turnID = ""
	p.rpc.c.mu.Unlock()
	if result.Text == "" {
		result.Text = partial
	}
	if len(result.Text) > p.rpc.c.l.CaptureBytes {
		return app.PromptResult{}, refusal(pa.CapacityFull, "reply_capture")
	}
	if cancelled || result.StopReason == "cancelled" || result.StopReason == "refusal" {
		return app.PromptResult{}, refusal(pa.ExecutionEffectUnknown, "prompt_stopped")
	}
	return app.PromptResult{FinalText: result.Text}, nil
}

func (c *Client) read() {
	scanner := bufio.NewScanner(c.p.stdout)
	scanner.Buffer(make([]byte, 0, c.l.FrameBytes), c.l.FrameBytes)
	for scanner.Scan() {
		var f frame
		if err := json.Unmarshal(scanner.Bytes(), &f); err != nil || f.JSONRPC != "2.0" {
			c.fail(refusal(pa.ExecutionEffectUnknown, "acp_frame"))
			return
		}
		if f.Method != "" {
			select {
			case c.queue <- incoming{f: f}:
			case <-c.done:
				return
			default:
				c.fail(refusal(pa.CapacityFull, "incoming_queue"))
				return
			}
			continue
		}
		if len(f.ID) == 0 {
			c.fail(refusal(pa.ExecutionEffectUnknown, "response_id"))
			return
		}
		// Responses enter the same ordered consumer after prior notifications.
		// This prevents final-text fallback racing a queued explicit message.
		select {
		case c.queue <- incoming{f: f}:
		case <-c.done:
			return
		default:
			c.fail(refusal(pa.CapacityFull, "incoming_queue"))
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.fail(refusal(pa.CapacityFull, "frame_bytes"))
	} else {
		c.fail(refusal(pa.ExecutionEffectUnknown, "acp_eof"))
	}
}
func (c *Client) consume() {
	defer close(c.workerDone)
	for {
		select {
		case <-c.done:
			return
		case item := <-c.queue:
			f := item.f
			if f.Method == "" {
				c.mu.Lock()
				ch := c.pending[string(f.ID)]
				if string(f.ID) == c.promptID {
					c.permissionsSealed = true
					c.permissions = make(map[string]nativePermission)
				}
				c.removePending(string(f.ID))
				c.mu.Unlock()
				if ch != nil {
					x := response{value: f.Result}
					if f.Error != nil {
						x.err = refusal(pa.Unsupported, "rpc_"+strconv.Itoa(f.Error.Code))
					}
					ch <- x
				}
				continue
			}
			if err := c.handle(f); err != nil {
				if len(f.ID) > 0 {
					_ = c.write(c.ctx, frame{ID: f.ID, Error: &rpcError{Code: -32603, Message: string(pa.Code(err))}})
				}
				c.fail(err)
				return
			}
		}
	}
}

func (c *Client) eventID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	return strconv.FormatUint(c.sequence, 10)
}
func (c *Client) event(id string) app.ProtocolEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return app.ProtocolEvent{ID: id, Fence: c.fence, TurnID: c.turnID}
}
func (c *Client) handle(f frame) error {
	switch f.Method {
	case "session/update":
		var p struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				SessionUpdate string `json:"sessionUpdate"`
				// A message chunk carries one content block; a tool call
				// carries an array. Only a chunk's block is decoded below.
				Content json.RawMessage `json:"content"`
			} `json:"update"`
		}
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return refusal(pa.Invalid, "update")
		}
		if p.SessionID != c.SessionID() {
			return refusal(pa.StaleGeneration, "native_session")
		}
		c.mu.Lock()
		active := c.turnID != ""
		c.mu.Unlock()
		if !active {
			return nil
		}
		if p.Update.SessionUpdate != "agent_message_chunk" {
			return nil
		}
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(p.Update.Content, &content); err != nil {
			return refusal(pa.Invalid, "update")
		}
		if content.Type == "text" {
			c.mu.Lock()
			if len(c.partial)+len(content.Text) > c.l.CaptureBytes {
				c.mu.Unlock()
				return refusal(pa.CapacityFull, "partial_capture")
			}
			c.partial += content.Text
			c.mu.Unlock()
			e := c.event(c.eventID())
			e.Trace = &pa.Trace{TurnID: e.TurnID, Category: "agent_message_chunk", Text: content.Text}
			return c.sink(c.ctx, e)
		}
		return nil
	case "session/request_permission":
		var p struct {
			SessionID string `json:"sessionId"`
			Options   []struct {
				OptionID string                  `json:"optionId"`
				Name     string                  `json:"name"`
				Kind     pa.PermissionOptionKind `json:"kind"`
			} `json:"options"`
		}
		if len(f.ID) == 0 || json.Unmarshal(f.Params, &p) != nil || p.SessionID != c.SessionID() {
			return refusal(pa.Invalid, "permission_request")
		}
		id := string(f.ID)
		e := c.event("permission-" + id)
		c.mu.Lock()
		sealed := c.permissionsSealed || c.failure != nil
		c.mu.Unlock()
		if sealed {
			return c.write(c.ctx, frame{ID: f.ID, Result: raw(map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})})
		}
		if e.TurnID == "" {
			return refusal(pa.StaleGeneration, "permission_turn")
		}
		readOnly := c.readOnly != nil && c.readOnly(f.Params)
		offer := pa.PermissionOffer{ID: "native-" + e.TurnID + "-" + id, Fence: c.fence, TurnID: e.TurnID, NativeRequestID: id, ExpiresAt: time.Now().Add(c.l.RPCTimeout), State: pa.PermissionPending, Action: permissionAction(f.Params)}
		options := make(map[string]pa.PermissionOption)
		for _, option := range p.Options {
			o := pa.PermissionOption{ID: option.OptionID, Label: option.Name, Kind: option.Kind, Allow: option.Kind == pa.AllowOnce || option.Kind == pa.AllowAlways, ReadOnly: readOnly}
			if o.ID == "" {
				return refusal(pa.Invalid, "permission_option")
			}
			offer.Options = append(offer.Options, o)
			options[o.ID] = o
		}
		c.mu.Lock()
		if c.permissionsSealed || c.failure != nil || c.turnID != e.TurnID {
			c.mu.Unlock()
			return c.write(c.ctx, frame{ID: f.ID, Result: raw(map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})})
		}
		if len(c.permissions) >= c.l.IncomingPermissions {
			c.mu.Unlock()
			return refusal(pa.CapacityFull, "permissions")
		}
		if _, exists := c.permissions[id]; exists {
			c.mu.Unlock()
			return nil
		}
		c.permissions[id] = nativePermission{turnID: e.TurnID, id: append(json.RawMessage(nil), f.ID...), options: options}
		c.mu.Unlock()
		e.Permission = &offer
		return c.sink(c.ctx, e)
	case "clawdline/send_message":
		var p struct {
			SessionID string `json:"sessionId"`
			ID        string `json:"messageId"`
			Text      string `json:"text"`
		}
		if len(f.ID) == 0 || json.Unmarshal(f.Params, &p) != nil || p.SessionID != c.SessionID() || p.ID == "" {
			return refusal(pa.Invalid, "send_message")
		}
		e := c.event("message-" + p.ID)
		e.Message = p.Text
		if err := c.sink(c.ctx, e); err != nil {
			return err
		}
		return c.write(c.ctx, frame{ID: f.ID, Result: raw(map[string]bool{"sent": true})})
	default:
		// No host filesystem, terminal, browser or arbitrary MCP execution is
		// granted by the client. Native tool enforcement is a separate admission.
		if len(f.ID) > 0 {
			return c.write(c.ctx, frame{ID: f.ID, Error: &rpcError{Code: -32601, Message: "method unavailable"}})
		}
		return nil
	}
}

func (c *Client) Answer(ctx context.Context, nativeID, optionID string) error {
	c.answerMu.Lock()
	defer c.answerMu.Unlock()
	c.mu.Lock()
	p, ok := c.permissions[nativeID]
	option, exists := p.options[optionID]
	if !ok || !exists || c.permissionsSealed || c.failure != nil || p.turnID != c.turnID {
		c.mu.Unlock()
		return refusal(pa.NotFound, "native_permission")
	}
	// The person approves each action once. A persistent rule would outlive
	// the turn and the generation that asked for it.
	if option.Kind == pa.AllowAlways || option.Kind == pa.RejectAlways {
		c.mu.Unlock()
		return refusal(pa.ScopeExpansionDenied, "native_permission")
	}
	if option.Kind != pa.AllowOnce && option.Kind != pa.RejectOnce {
		c.mu.Unlock()
		return refusal(pa.Invalid, "native_permission")
	}
	delete(c.permissions, nativeID)
	c.mu.Unlock()
	return c.write(ctx, frame{ID: p.id, Result: raw(map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": optionID}})})
}
func (c *Client) Cancel(ctx context.Context) error {
	c.answerMu.Lock()
	defer c.answerMu.Unlock()
	c.mu.Lock()
	c.permissionsSealed, c.cancelled = true, true
	permissions := c.permissions
	c.permissions = make(map[string]nativePermission)
	sid := c.sessionID
	c.mu.Unlock()
	for _, p := range permissions {
		if err := c.write(ctx, frame{ID: p.id, Result: raw(map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})}); err != nil {
			return err
		}
	}
	return c.write(ctx, frame{Method: "session/cancel", Params: raw(map[string]string{"sessionId": sid})})
}
func (c *Client) Close(ctx context.Context) error {
	c.answerMu.Lock()
	defer c.answerMu.Unlock()
	cleanup, cancel := context.WithTimeout(ctx, c.l.CleanupTimeout)
	defer cancel()
	c.fail(refusal(pa.ExecutionEffectUnknown, "closed"))
	err := c.revokeMCP(cleanup)
	err = errors.Join(err, c.p.Close(cleanup))
	if !waitOwnedDone(cleanup, c.workerDone) {
		err = errors.Join(err, refusal(pa.ExecutionEffectUnknown, "incoming_cleanup"))
	}
	return err
}

func (c *Client) revokeMCP(ctx context.Context) error {
	c.mcpMu.Lock()
	defer c.mcpMu.Unlock()
	if c.ownedMCP == nil || c.mcpRevoked {
		return nil
	}
	if c.ownedMCP.Revoke == nil {
		return refusal(pa.ExecutionEffectUnknown, "owned_mcp_cleanup")
	}
	if err := c.ownedMCP.Revoke(ctx); err != nil {
		return errors.Join(refusal(pa.ExecutionEffectUnknown, "owned_mcp_cleanup"), err)
	}
	c.mcpRevoked = true
	return nil
}

var _ app.ProviderSession = (*Client)(nil)
