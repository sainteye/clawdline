package projectassistantacp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/grok"
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
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type response struct {
	value json.RawMessage
	err   error
}
type incoming struct {
	f   frame
	err error
}
type pendingResponse struct {
	ch      chan response
	ordered bool
}
type nativePermission struct {
	turnID  string
	id      json.RawMessage
	options map[string]pa.PermissionOption
}

type Client struct {
	extensionFiles string
	discovery      bool
	discoveryApply bool
	configuration  pa.ConfigurationCatalog
	p              *Process
	l              Limits
	fence          pa.Fence
	sink           func(context.Context, app.ProtocolEvent) error
	readOnly       func([]byte) bool
	mu             sync.Mutex
	next           uint64
	pending        map[string]pendingResponse
	permissions    map[string]nativePermission
	queue          []incoming
	queueReady     *sync.Cond
	done           chan struct{}
	workerDone     chan struct{}
	stop           sync.Once
	failure        error
	sessionID      string
	nativeContext  app.NativeContextState
	compaction     *nativeCompaction
	turnID         string
	partial        string
	traceSegment   pa.Trace
	traceTools     map[string]nativeToolTrace
	// reply segments, as Codync's text segments: a tool call or a new message
	// closes one, and the next starts after a blank line.
	segmentStart        int
	segmentClosed       bool
	messageID           string
	steering            bool
	sequence            uint64
	ctx                 context.Context
	cancel              context.CancelFunc
	agent               pa.Agent
	adapterVersion      string
	geminiSettings      string
	openingInstructions string
	promptImages        bool
	sessionFailures     bool
	ownedMCP            *OwnedMCP
	mcpMu               sync.Mutex
	mcpRevoked          bool
	permissionsSealed   bool
	cancelled           bool
	promptID            string
	promptDeadline      time.Time
	answerMu            sync.Mutex
	rpcSlots            chan struct{}
}

func NewClient(p *Process, l Limits, fence pa.Fence, sink func(context.Context, app.ProtocolEvent) error, readOnly func([]byte) bool) (*Client, error) {
	return newClient(p, l, fence, sink, readOnly, "")
}

func newClient(p *Process, l Limits, fence pa.Fence, sink func(context.Context, app.ProtocolEvent) error, readOnly func([]byte) bool, agent pa.Agent) (*Client, error) {
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
	c := &Client{p: p, l: l, fence: fence, agent: agent, sink: sink, readOnly: readOnly, pending: make(map[string]pendingResponse), permissions: make(map[string]nativePermission), done: make(chan struct{}), workerDone: make(chan struct{}), ctx: ctx, cancel: cancel, rpcSlots: make(chan struct{}, l.RPCPending)}
	c.queueReady = sync.NewCond(&c.mu)
	go c.consume()
	go c.read()
	return c, nil
}

func (c *Client) initialize(ctx context.Context, q app.OpenRequest) error {
	return c.initializeSession(ctx, q, false)
}

func (c *Client) initializeSession(ctx context.Context, q app.OpenRequest, maintenance bool) error {
	if maintenance && (!q.Resume || q.Bot.Agent != pa.Codex || c.ownedMCP != nil) {
		return refusal(pa.ScopeDenied, "native_maintenance_open")
	}
	var initialized struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession        bool `json:"loadSession"`
			PromptCapabilities struct {
				Image bool `json:"image"`
			} `json:"promptCapabilities"`
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
		// Both supplied adapters advertise _session/steering here.
		Meta struct {
			Steering struct {
				Supported bool `json:"supported"`
			} `json:"steering"`
		} `json:"_meta"`
	}
	caps := map[string]any{"session": map[string]any{"compaction": map[string]any{}}}
	c.sessionFailures = !maintenance && q.Bot.Agent == pa.ClaudeCode
	if c.sessionFailures {
		caps["_meta"] = map[string]any{"jetbrains": map[string]any{"air": map[string]any{
			"version": 1, "capabilities": []string{"sessionFailure"},
		}}}
	}
	init := map[string]any{"protocolVersion": 1, "clientCapabilities": caps, "clientInfo": map[string]string{"name": "clawdline", "version": pa.ContractVersion}}
	if q.Bot.Agent == pa.Grok {
		init["_meta"] = map[string]string{"clientType": "extension"}
	}
	b, err := c.call(ctx, "initialize", init)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &initialized); err != nil {
		return refusal(pa.Unsupported, "initialize_response")
	}
	c.mu.Lock()
	c.steering = initialized.Meta.Steering.Supported
	c.mu.Unlock()
	if initialized.ProtocolVersion != 1 {
		return refusal(pa.ProviderVersionUnsupported, "acp_protocol_version")
	}
	adapterName := adapterPackage(q.Bot.Agent)
	if q.Bot.Agent == pa.Gemini {
		adapterName = "Antigravity ACP Server"
	}
	if q.Bot.Agent != pa.Grok && c.adapterVersion != "" && (initialized.AgentInfo.Version != c.adapterVersion || initialized.AgentInfo.Name != adapterName) {
		return refusal(pa.ProviderVersionUnsupported, "acp_adapter_identity")
	}
	if !maintenance && q.Bot.Agent == pa.Codex && !hasCapability(initialized.AgentCapabilities.SessionCapabilities["systemPrompt"]) {
		return refusal(pa.Unsupported, "system_prompt")
	}
	if q.Bot.Agent == pa.ClaudeCode && !hasCapability(initialized.AgentCapabilities.Meta["claudeCode"]) {
		return refusal(pa.Unsupported, "claude_session_options")
	}
	if c.ownedMCP != nil && !initialized.AgentCapabilities.MCPCapabilities.HTTP {
		return refusal(pa.Unsupported, "http_mcp")
	}
	if q.Bot.Agent == pa.Grok {
		if err := c.authenticateGrok(ctx, b); err != nil {
			return err
		}
		c.promptImages = initialized.AgentCapabilities.PromptCapabilities.Image
	}
	if q.Bot.Agent == pa.Gemini {
		if err := c.authenticateGemini(ctx, b); err != nil {
			return err
		}
		c.promptImages = initialized.AgentCapabilities.PromptCapabilities.Image
		if !c.discovery {
			c.openingInstructions = systemPrompt(q)
		}
	}
	if q.Resume && q.PersistNativeSession && !nativeSessionUUID.MatchString(q.Binding.NativeSessionID) {
		return refusal(pa.ScopeDenied, "native_session_id")
	}
	// Maintenance loads existing history without applying a chat prompt,
	// tool server, model, mode or runtime setting.
	params := map[string]any{"cwd": q.Directory, "mcpServers": []any{}}
	if !maintenance {
		params, err = sessionParameters(q, c.ownedMCP)
		if err != nil {
			return err
		}
	}
	method := "session/new"
	if q.Resume {
		switch {
		case q.Bot.Agent == pa.Grok:
			if !hasCapability(initialized.AgentCapabilities.SessionCapabilities["resume"]) {
				return refusal(pa.ResumeUnsupported, "native_resume")
			}
			method = "session/resume"
		case q.Bot.Agent == pa.Gemini && hasCapability(initialized.AgentCapabilities.SessionCapabilities["resume"]):
			// Resume without replaying native transcript text into the Bot.
			method = "session/resume"
		case initialized.AgentCapabilities.LoadSession:
			method = "session/load"
		default:
			return refusal(pa.ResumeUnsupported, "load_session")
		}
		params["sessionId"] = q.Binding.NativeSessionID
		c.mu.Lock()
		c.sessionID = q.Binding.NativeSessionID
		c.mu.Unlock()
	}
	if q.Bot.Agent == pa.Gemini {
		if err := checkGeminiSettings(c.geminiSettings); err != nil {
			return err
		}
	}
	b, err = c.call(ctx, method, params)
	if err != nil {
		var f *pa.Failure
		var provider *app.ProviderFailure
		if q.Bot.Agent == pa.Gemini && errors.As(err, &provider) && provider.Reason == app.ProviderSignedOut {
			return geminiSignIn()
		}
		if q.Resume && errors.As(err, &f) && strings.HasPrefix(f.Field, "rpc_") {
			// The native side answered that it cannot load this session, for
			// example after its transcript moved. The caller starts anew.
			return refusal(pa.ResumeUnsupported, "native_session_missing")
		}
		return err
	}
	var session struct {
		SessionID string `json:"sessionId"`
		Modes     *struct {
			CurrentModeID  string `json:"currentModeId"`
			AvailableModes []struct {
				ID string `json:"id"`
			} `json:"availableModes"`
		} `json:"modes"`
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
	if maintenance {
		return nil
	}
	c.configuration, err = c.configureModel(ctx, q.Bot, b, !c.discovery || c.discoveryApply)
	if err != nil {
		return err
	}
	if c.discovery {
		return nil
	}
	// Grok's session modes are a separate control from its launch-level
	// permission mode. The Bot's permission mode is set on the command line.
	if session.Modes == nil && q.Bot.Agent == pa.Gemini {
		return refusal(pa.Unsupported, "agent_mode")
	}
	if q.Bot.Agent == pa.Grok || session.Modes == nil {
		return nil
	}
	return c.applyMode(ctx, q.Bot, session.SessionID, session.Modes.CurrentModeID, func(id string) bool {
		for _, m := range session.Modes.AvailableModes {
			if m.ID == id {
				return true
			}
		}
		return false
	})
}

func (c *Client) authenticateGrok(ctx context.Context, initialized []byte) error {
	valid, cached := grok.ACPIdentity(initialized)
	if !valid {
		return refusal(pa.ProviderVersionUnsupported, "grok_cli")
	}
	signIn := func() error {
		return &app.ProviderFailure{Reason: app.ProviderSignedOut, Cause: refusal(pa.AuthRejected, "grok_login")}
	}
	if !cached {
		return signIn()
	}
	raw, err := c.call(ctx, "authenticate", map[string]any{
		"methodId": "cached_token", "_meta": map[string]bool{"headless": true},
	})
	if err != nil {
		var provider *app.ProviderFailure
		if errors.As(err, &provider) && provider.Reason == app.ProviderSignedOut {
			return signIn()
		}
		return err
	}
	if problem := grok.PersonalAccountProblem(raw); problem != "" {
		return refusal(pa.AuthRejected, "grok_"+problem)
	}
	return nil
}

func geminiSignIn() error {
	return &app.ProviderFailure{Reason: app.ProviderSignedOut, Cause: refusal(pa.AuthRejected, "gemini_login")}
}

func (c *Client) authenticateGemini(ctx context.Context, initialized []byte) error {
	if err := checkGeminiSettings(c.geminiSettings); err != nil {
		return err
	}
	var offer struct {
		Methods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if json.Unmarshal(initialized, &offer) != nil {
		return refusal(pa.ProviderVersionUnsupported, "gemini_cli")
	}
	for _, method := range offer.Methods {
		if method.ID != "oauth-personal" {
			continue
		}
		_, err := c.call(ctx, "authenticate", map[string]string{"methodId": method.ID})
		var provider *app.ProviderFailure
		if errors.As(err, &provider) && provider.Reason == app.ProviderSignedOut {
			return geminiSignIn()
		}
		return err
	}
	return refusal(pa.ProviderVersionUnsupported, "gemini_cli")
}

// applyMode puts the session in the Bot's mode, as Codync does with
// session/set_mode. Claude's adapter already falls back from Auto to Accept
// edits on a model without Auto, so that session is left as it is; any other
// mode the agent does not offer refuses the turn rather than run it in another.
func (c *Client) applyMode(ctx context.Context, bot pa.Bot, sid, current string, offered func(string) bool) error {
	want := pa.NativeMode(bot.Agent, bot.Mode)
	if current == want {
		return nil
	}
	if bot.Agent == pa.Gemini && want == pa.AutoMode && !offered(want) {
		// Where the native terminal sandbox is unavailable, ask for review.
		want = "default"
		if current == want {
			return nil
		}
	}
	if !offered(want) {
		if bot.Agent == pa.ClaudeCode && want == "auto" && current == "acceptEdits" {
			return nil
		}
		return refusal(pa.Unsupported, "agent_mode")
	}
	if _, err := c.call(ctx, "session/set_mode", map[string]string{"sessionId": sid, "modeId": want}); err != nil {
		return refusal(pa.Unsupported, "agent_mode")
	}
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
			ch.ch <- response{err: err}
			c.removePending(key)
		}
		c.queue = nil
		c.queueReady.Broadcast()
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
	err = c.p.Write(ctx, b)
	if err != nil && !errors.Is(err, errWriteNotStarted) {
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
	c.next++
	id := strconv.FormatUint(c.next, 10)
	ch := make(chan response, 1)
	// Control replies bypass the event sink, as Codync's reader resolves RPC
	// waiters directly. Prompt completion and load replay retain a barrier so
	// prior messages cannot race final text or become a new turn's history.
	c.pending[id] = pendingResponse{ch: ch, ordered: method == "session/prompt" || method == "session/load"}
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

// removePending holds c.mu. Native response correlation does not consume an
// owned HTTP tool handler slot.
func (c *Client) removePending(id string) { delete(c.pending, id) }

type pendingRPC struct {
	c  *Client
	id string
	ch chan response
}

// The caller owns the prompt lifetime. A control RPC has one shorter
// deadline covering both its write and its response.
func (p *pendingRPC) waitResult(ctx context.Context) (json.RawMessage, error) {
	select {
	case x := <-p.ch:
		return x.value, x.err
	case <-ctx.Done():
		select {
		case x := <-p.ch:
			return x.value, x.err
		default:
		}
		p.c.mu.Lock()
		waiting := p.id == p.c.promptID && len(p.c.permissions) > 0
		p.c.removePending(p.id)
		p.c.mu.Unlock()
		var cancelErr error
		if waiting {
			// The prompt context can no longer write. Use one bounded cleanup
			// attempt to release the agent's permission RPC before closing it.
			cleanup, cancel := context.WithTimeout(context.Background(), p.c.l.WriteTimeout)
			cancelErr = p.c.cancelPrompt(cleanup, p.id)
			cancel()
		}
		// Keep the effect unknown while retaining the runtime's distinction
		// between an expired turn deadline and an explicit cancellation.
		err := errors.Join(refusal(pa.ExecutionEffectUnknown, "rpc_timeout"), ctx.Err(), cancelErr)
		p.c.fail(err)
		return nil, err
	case <-p.c.done:
		// A terminal reply already received before EOF/Close wins over the
		// later transport failure. No request is ever sent again here.
		select {
		case x := <-p.ch:
			return x.value, x.err
		default:
		}
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
	ctx, cancel := context.WithTimeout(ctx, c.l.RPCTimeout)
	defer cancel()
	p, err := c.begin(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return p.waitResult(ctx)
}

func (c *Client) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	if err := pa.CheckFence(q.Turn.Fence, c.fence); err != nil {
		return nil, err
	}
	if (c.agent == pa.Grok || c.agent == pa.Gemini) && len(q.Images) > 0 && !c.promptImages {
		return nil, refusal(pa.Unsupported, "images")
	}
	if c.agent == pa.Gemini {
		if err := checkGeminiSettings(c.geminiSettings); err != nil {
			return nil, err
		}
	}
	var ids []string
	for _, file := range q.Files {
		if err := pa.ValidateInput(file.File); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(file.Path) {
			return nil, refusal(pa.ScopeDenied, "input_path")
		}
		ids = append(ids, file.File.ID)
	}
	if err := pa.ValidateInputSend(q.Text, promptAttachments(q.Images), ids); err != nil {
		return nil, err
	}
	// As in Codync, a native session hears its instructions and memory once:
	// the instructions are in its system prompt, and a fresh session's first
	// prompt carries the handoff with the filtered memory. A resumed session
	// already holds both; when memory changed since, its next prompt carries
	// all of memory as it is now. Forgetting or new instructions start a fresh
	// session, so otherwise later prompts carry only the person's text.
	text := q.Text
	if len(q.Files) > 0 {
		text += "\n\nAttached files (explicitly provided for this message; read with your native tools):"
		for _, file := range q.Files {
			quoted, _ := json.Marshal(file.Path)
			text += "\n- " + string(quoted)
		}
	}
	if q.Handoff != nil {
		opening, err := q.Handoff.Opening(q.Turn, q.Context)
		if err != nil {
			return nil, err
		}
		text = opening + "\n" + text
	} else if q.MemoryChanged {
		text = memoryUpdate(q.Context.Text) + "\n" + text
	}
	deadline, _ := ctx.Deadline()
	c.mu.Lock()
	if c.compaction != nil {
		c.mu.Unlock()
		return nil, refusal(pa.Revoked, "compaction_lifetime")
	}
	if c.permissionsSealed {
		c.mu.Unlock()
		return nil, refusal(pa.Revoked, "prompt_lifetime")
	}
	if c.turnID != "" {
		c.mu.Unlock()
		return nil, refusal(pa.CapacityFull, "active_prompt")
	}
	c.turnID = q.Turn.ID
	c.promptDeadline = deadline
	c.partial, c.segmentStart, c.segmentClosed, c.messageID = "", 0, false, ""
	c.traceSegment, c.traceTools = pa.Trace{}, nil
	sid, instructions := c.sessionID, c.openingInstructions
	c.mu.Unlock()
	if instructions != "" {
		text = "<clawdline_instructions>\n" + instructions + "\n</clawdline_instructions>\n\n" + text
	}
	// Only fresh filtered context is sent. Displayed history is never implicit
	// model input. Instructions belong to this turn's immutable snapshot.
	blocks := []any{}
	if q.Quote != nil {
		if err := pa.ValidateUserMessage(q.Quote.Text); err != nil {
			return nil, err
		}
		if q.Quote.MessageID == "" {
			return nil, refusal(pa.Invalid, "quote_id")
		}
		blocks = append(blocks, map[string]string{"type": "text", "text": "Referenced message (" + q.Quote.MessageID + "):\n" + q.Quote.Text})
	}
	// Pictures go first as native ACP image blocks, as Codync sends them.
	for _, image := range q.Images {
		blocks = append(blocks, map[string]string{"type": "image", "mimeType": image.MediaType, "data": base64.StdEncoding.EncodeToString(image.Data)})
	}
	blocks = append(blocks, map[string]string{"type": "text", "text": text})
	p, err := c.begin(ctx, "session/prompt", map[string]any{"sessionId": sid, "prompt": blocks})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.openingInstructions = ""
	c.mu.Unlock()
	return &promptPending{rpc: p, deadline: deadline}, nil
}

type promptPending struct {
	rpc      *pendingRPC
	deadline time.Time
}

func (p *promptPending) Wait(ctx context.Context) (app.PromptResult, error) {
	if !p.deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, p.deadline)
		defer cancel()
	}
	b, err := p.rpc.waitResult(ctx)
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
	p.rpc.c.closeSegment()
	partial := strings.TrimSpace(p.rpc.c.partial[p.rpc.c.segmentStart:])
	p.rpc.c.traceSegment, p.rpc.c.traceTools = pa.Trace{}, nil
	cancelled := p.rpc.c.cancelled
	p.rpc.c.turnID = ""
	p.rpc.c.mu.Unlock()
	if result.Text == "" {
		result.Text = partial
	}
	if cancelled || result.StopReason == "cancelled" || result.StopReason == "refusal" {
		return app.PromptResult{}, refusal(pa.ExecutionEffectUnknown, "prompt_stopped")
	}
	if (p.rpc.c.agent == pa.Grok && result.StopReason != "end_turn") || (p.rpc.c.agent == pa.Gemini && result.StopReason == "") {
		return app.PromptResult{}, refusal(pa.ExecutionEffectUnknown, "prompt_result")
	}
	if p.rpc.c.sessionFailures {
		if err := claudePromptFailure(b, result.StopReason, result.Text); err != nil {
			return app.PromptResult{}, err
		}
	}
	if result.StopReason == "max_tokens" || result.StopReason == "max_turn_requests" {
		return app.PromptResult{}, refusal(pa.Unsupported, "prompt_"+result.StopReason)
	}
	return app.PromptResult{FinalText: result.Text}, nil
}

func (c *Client) read() {
	// Codync reads complete ACP lines and deliberately has no incoming count
	// ceiling: session/load may replay history before its response. Host-owned
	// RPC slots, native permissions, write deadlines and cleanup still apply.
	reader := bufio.NewReader(c.p.stdout)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			field := "acp_read"
			if errors.Is(readErr, io.EOF) {
				field = "acp_eof"
			}
			c.enqueue(incoming{err: refusal(pa.ExecutionEffectUnknown, field)})
			return
		}
		var f frame
		if err := json.Unmarshal(line, &f); err != nil || f.JSONRPC != "2.0" {
			c.enqueue(incoming{err: refusal(pa.ExecutionEffectUnknown, "acp_frame")})
			return
		}
		if f.Method != "" {
			if !c.enqueue(incoming{f: f}) {
				return
			}
			continue
		}
		if len(f.ID) == 0 {
			c.enqueue(incoming{err: refusal(pa.ExecutionEffectUnknown, "response_id")})
			return
		}
		c.mu.Lock()
		ordered := c.pending[string(f.ID)].ordered
		c.mu.Unlock()
		if ordered {
			if !c.enqueue(incoming{f: f}) {
				return
			}
		} else {
			c.resolve(f)
		}
	}
}

func (c *Client) enqueue(item incoming) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != nil {
		return false
	}
	c.queue = append(c.queue, item)
	c.queueReady.Signal()
	return true
}

func (c *Client) resolve(f frame) {
	c.mu.Lock()
	p := c.pending[string(f.ID)]
	if string(f.ID) == c.promptID {
		c.permissionsSealed = true
		c.permissions = make(map[string]nativePermission)
	}
	c.removePending(string(f.ID))
	if p.ch != nil {
		x := response{value: f.Result}
		if f.Error != nil {
			x.err = providerFailure(f.Error)
			if c.agent == pa.Grok && f.Error.Code == -32003 {
				x.err = &app.ProviderFailure{Reason: app.ProviderRateLimited, RPCCode: f.Error.Code, Cause: refusal(pa.Unsupported, "rpc_-32003")}
			}
		}
		p.ch <- x
	}
	c.mu.Unlock()
}
func (c *Client) consume() {
	defer close(c.workerDone)
	for {
		c.mu.Lock()
		for len(c.queue) == 0 && c.failure == nil {
			c.queueReady.Wait()
		}
		if c.failure != nil {
			c.mu.Unlock()
			return
		}
		item := c.queue[0]
		c.queue[0] = incoming{}
		c.queue = c.queue[1:]
		if len(c.queue) == 0 {
			c.queue = nil
		}
		c.mu.Unlock()
		if item.err != nil {
			c.fail(item.err)
			return
		}
		f := item.f
		if f.Method == "" {
			c.resolve(f)
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
				MessageID     string `json:"messageId"`
				Title         string `json:"title"`
				// A message chunk carries one content block; a tool call
				// carries an array. Only a chunk's block is decoded below.
				Content      json.RawMessage `json:"content"`
				Entries      json.RawMessage `json:"entries,omitempty"`
				ToolCallID   string          `json:"toolCallId"`
				Status       string          `json:"status"`
				Meta         json.RawMessage `json:"_meta"`
				Commands     json.RawMessage `json:"availableCommands"`
				Used         json.RawMessage `json:"used"`
				Size         json.RawMessage `json:"size"`
				CompactionID string          `json:"compactionId"`
			} `json:"update"`
		}
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return refusal(pa.Invalid, "update")
		}
		// Native agents can publish initial session metadata before session/new replies.
		// Nothing is routed to the Bot until the native session is bound.
		if (c.agent == pa.Grok || c.agent == pa.Gemini) && c.SessionID() == "" {
			return nil
		}
		if p.Update.SessionUpdate == "usage_update" || p.Update.SessionUpdate == "available_commands_update" {
			return c.updateNativeContext(p.SessionID, p.Update.SessionUpdate, p.Update.Commands, p.Update.Used, p.Update.Size)
		}
		if p.Update.SessionUpdate == "compaction_update" {
			return c.updateNativeCompaction(p.SessionID, p.Update.CompactionID, p.Update.Status)
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
		if p.Update.SessionUpdate == "tool_call" {
			c.mu.Lock()
			c.segmentClosed = true
			c.mu.Unlock()
			return c.traceUpdate(raw(p.Update))
		}
		if p.Update.SessionUpdate != "agent_message_chunk" {
			if p.Update.SessionUpdate == "agent_thought_chunk" || p.Update.SessionUpdate == "plan" {
				c.mu.Lock()
				c.segmentClosed = true
				c.mu.Unlock()
			}
			return c.traceUpdate(raw(p.Update))
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
			if p.Update.MessageID != "" && c.messageID != "" && p.Update.MessageID != c.messageID {
				c.segmentClosed = true
				c.traceSegment = pa.Trace{}
			}
			if p.Update.MessageID != "" {
				c.messageID = p.Update.MessageID
			}
			if c.segmentClosed {
				c.traceSegment = pa.Trace{}
				c.closeSegment()
				if strings.TrimSpace(c.partial) != "" {
					c.partial += "\n\n"
				}
				c.segmentStart = len(c.partial)
			}
			c.partial += content.Text
			c.mu.Unlock()
			return c.traceUpdate(raw(p.Update))
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
		deadline := c.promptDeadline
		c.mu.Unlock()
		if sealed {
			return c.write(c.ctx, frame{ID: f.ID, Result: raw(map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})})
		}
		if e.TurnID == "" {
			return refusal(pa.StaleGeneration, "permission_turn")
		}
		readOnly := c.readOnly != nil && c.readOnly(f.Params)
		offer := pa.PermissionOffer{ID: "native-" + e.TurnID + "-" + id, Fence: c.fence, TurnID: e.TurnID, NativeRequestID: id, ExpiresAt: deadline, State: pa.PermissionPending, Action: permissionAction(f.Params)}
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
	ctx, cancel := context.WithTimeout(ctx, c.l.RPCTimeout)
	defer cancel()
	c.answerMu.Lock()
	defer c.answerMu.Unlock()
	c.mu.Lock()
	p, ok := c.permissions[nativeID]
	option, exists := p.options[optionID]
	if !ok || !exists || c.permissionsSealed || c.failure != nil || p.turnID != c.turnID {
		c.mu.Unlock()
		return refusal(pa.NotFound, "native_permission")
	}
	deadline := c.promptDeadline
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		c.mu.Unlock()
		return refusal(pa.Expired, "native_permission")
	}
	// As in Codync, an always option hands the agent the rule it defines.
	if option.Kind != pa.AllowOnce && option.Kind != pa.RejectOnce && option.Kind != pa.AllowAlways && option.Kind != pa.RejectAlways {
		c.mu.Unlock()
		return refusal(pa.Invalid, "native_permission")
	}
	delete(c.permissions, nativeID)
	c.mu.Unlock()
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	return c.write(ctx, frame{ID: p.id, Result: raw(map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": optionID}})})
}
func (c *Client) Cancel(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.l.CleanupTimeout)
	defer cancel()
	return c.cancelPrompt(ctx, "")
}

func (c *Client) cancelPrompt(ctx context.Context, waitingPromptID string) error {
	c.answerMu.Lock()
	defer c.answerMu.Unlock()
	c.mu.Lock()
	// A prompt reply seals permission offers. An explicit cancellation can still retire the context.
	if c.failure != nil || c.cancelled ||
		(waitingPromptID != "" && (c.permissionsSealed || c.promptID != waitingPromptID || len(c.permissions) == 0)) {
		err := c.failure
		c.mu.Unlock()
		return err
	}
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
	if err == nil && c.extensionFiles != "" {
		if os.RemoveAll(c.extensionFiles) != nil {
			return refusal(pa.ExecutionEffectUnknown, "skill_cleanup")
		}
		c.extensionFiles = ""
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

// promptAttachments describes prompt pictures for the same validation a
// stored message passes; the digest is recomputed from the bytes.
func promptAttachments(images []app.PromptImage) []pa.Attachment {
	out := make([]pa.Attachment, 0, len(images))
	for _, image := range images {
		sum := sha256.Sum256(image.Data)
		out = append(out, pa.Attachment{ID: hex.EncodeToString(sum[:]), MediaType: image.MediaType, Bytes: int64(len(image.Data))})
	}
	return out
}

// memoryUpdate is Codync's change notice (host/src/chat/memory/mod.rs
// change_notice): the whole memory replaces what the session heard before.
func memoryUpdate(memory string) string {
	if strings.TrimSpace(memory) == "" {
		memory = "(empty)"
	}
	return "<memory_update>\nYour memory changed. This is all of it now and replaces any earlier memory in this conversation.\n" + memory + "\n</memory_update>"
}

// toolMarkup matches a segment that is nothing but a tool call written out as
// text. Claude Code's adapter can stream a call's markup as message chunks
// before the call itself (observed with ToolSearch on 2026-11-14); it is not
// a reply. Markup inside a segment with other text is left alone.
var toolMarkup = regexp.MustCompile(`^(?s)(<function_calls>\s*)?(<invoke name="[^"]+">.*?</invoke>\s*)+(</function_calls>)?$`)

// closeSegment ends the current reply segment, dropping it when it is only
// tool-call markup. The caller holds c.mu.
func (c *Client) closeSegment() {
	if c.segmentStart <= len(c.partial) && toolMarkup.MatchString(strings.TrimSpace(c.partial[c.segmentStart:])) {
		c.partial = strings.TrimRight(c.partial[:c.segmentStart], "\n")
		c.segmentStart = len(c.partial)
	}
	c.segmentClosed = false
}

// Steer is Codync's hand_to_agent: the message goes into the running turn
// through ACP _session/steering; a turn that just ended answers
// promptRequired instead of starting one. What the agent writes next starts a
// new paragraph of the reply.
func (c *Client) Steer(ctx context.Context, text string) (app.SteerOutcome, error) {
	if err := pa.ValidateUserMessage(text); err != nil {
		return "", err
	}
	c.mu.Lock()
	steering, sid, active := c.steering, c.sessionID, c.turnID != ""
	if active {
		c.segmentClosed = true
	}
	c.mu.Unlock()
	if !steering {
		return "", refusal(pa.Unsupported, "steering")
	}
	if !active {
		return app.SteerNext, nil
	}
	b, err := c.call(ctx, "_session/steering", map[string]any{"sessionId": sid, "prompt": []any{map[string]string{"type": "text", "text": text}}, "_meta": map[string]any{"steering": map[string]string{"idleBehavior": "promptRequired"}}})
	if err != nil {
		return "", err
	}
	var answer struct {
		Outcome string `json:"outcome"`
	}
	if json.Unmarshal(b, &answer) != nil {
		return "", refusal(pa.OutcomeUnknown, "steering_answer")
	}
	switch app.SteerOutcome(answer.Outcome) {
	case app.SteerJoined, app.SteerNext:
		return app.SteerOutcome(answer.Outcome), nil
	}
	return "", refusal(pa.OutcomeUnknown, "steering_answer")
}
