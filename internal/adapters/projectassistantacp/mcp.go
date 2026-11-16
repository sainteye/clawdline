package projectassistantacp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// ownedMCPServer implements the JSON response profile of MCP Streamable HTTP
// (2025-06-18). It exposes only host-bound functions, never a daemon credential,
// filesystem, terminal, arbitrary actor, or human approval operation.
// One listener belongs to one already bounded runtime process slot. Accepted
// connections and outbound ACP requests share that client's RPCPending budget.
type ownedMCPServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	server      *http.Server
	listener    net.Listener
	token       string
	tools       app.NativeTools
	limits      Limits
	slots       chan struct{}
	done        chan struct{}
	mu          sync.Mutex
	closing     bool
	initialized bool
	ready       bool
	protocol    string
	effects     sync.WaitGroup
	// conns counts accepted connections until each returns its RPC slot, so
	// a verified close also means every slot it held is free again.
	conns    sync.WaitGroup
	stopOnce sync.Once
	stopDone chan struct{}
}

type mcpConnContext struct{}

func (c *Client) openMCP(parent context.Context, q app.OpenRequest) (OwnedMCP, error) {
	if q.Tools == nil || parent.Err() != nil {
		return OwnedMCP{}, refusal(pa.Revoked, "native_tools")
	}
	// Existing host IDs are random SHA-256 capabilities. A new random digest
	// makes this bearer independent of those signing/daemon credentials.
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return OwnedMCP{}, refusal(pa.ExecutionEffectUnknown, "mcp_random")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return OwnedMCP{}, refusal(pa.Offline, "mcp_listener")
	}
	ctx, cancel := context.WithCancel(parent)
	s := &ownedMCPServer{ctx: ctx, cancel: cancel, listener: listener, token: hex.EncodeToString(secret[:]), tools: q.Tools, limits: c.l, slots: c.rpcSlots, done: make(chan struct{}), stopDone: make(chan struct{})}
	s.server = &http.Server{
		Handler: s, ReadHeaderTimeout: c.l.WriteTimeout, ReadTimeout: c.l.RPCTimeout,
		WriteTimeout: c.l.RPCTimeout, IdleTimeout: c.l.WriteTimeout, MaxHeaderBytes: c.l.FrameBytes,
		BaseContext: func(net.Listener) context.Context { return ctx },
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			return context.WithValue(ctx, mcpConnContext{}, conn.(*mcpConn))
		},
		// Native request bodies, bearer values and panic diagnostics cannot
		// escape into the shared daemon log.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	s.server.SetKeepAlivesEnabled(false)
	go func() { defer close(s.done); _ = s.server.Serve(&mcpListener{Listener: listener, owner: s}) }()
	return OwnedMCP{Fence: q.Binding.Fence, URL: "http://" + listener.Addr().String() + "/mcp", BearerToken: s.token, Revoke: s.close}, nil
}

// No handler may acquire a new effect owner after close seals admission.
// A callback that ignores cancellation retains its existing process/RPC slot;
// close reports unknown instead of detaching it or allowing replacement work.
func (s *ownedMCPServer) close(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.cancel()
		s.mu.Unlock()
		_ = s.server.Close()
		go func() { <-s.done; s.effects.Wait(); s.conns.Wait(); close(s.stopDone) }()
	})
	if !waitOwnedDone(ctx, s.stopDone) {
		return refusal(pa.ExecutionEffectUnknown, "mcp_cleanup")
	}
	return nil
}

type mcpListener struct {
	net.Listener
	owner *ownedMCPServer
}

func (l *mcpListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.owner.slots <- struct{}{}:
			l.owner.mu.Lock()
			if l.owner.closing {
				l.owner.mu.Unlock()
				<-l.owner.slots
				_ = conn.Close()
				continue
			}
			l.owner.conns.Add(1)
			l.owner.mu.Unlock()
			return &mcpConn{Conn: conn, slots: l.owner.slots, done: l.owner.conns.Done}, nil
		default:
			// Read a bounded HTTP header before sending a response. An early
			// unsolicited 503 can race the client's first request write.
			// There is one synchronous refusal reader, no handler or effect.
			_ = conn.SetDeadline(time.Now().Add(l.owner.limits.WriteTimeout))
			reader := bufio.NewReader(io.LimitReader(conn, int64(l.owner.limits.FrameBytes)))
			if _, err := http.ReadRequest(reader); err == nil {
				body := `{"code":"pa_capacity_full","field":"pending_rpc"}`
				_, _ = fmt.Fprintf(conn, "HTTP/1.1 503 Service Unavailable\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			}
			_ = conn.Close()
		}
	}
}

type mcpConn struct {
	net.Conn
	slots                    chan struct{}
	done                     func()
	mu                       sync.Mutex
	closed, active, released bool
}

func (c *mcpConn) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.active = true
	return true
}
func (c *mcpConn) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = false
	c.release()
}
func (c *mcpConn) release() {
	if c.closed && !c.active && !c.released {
		c.released = true
		<-c.slots
		c.done()
	}
}
func (c *mcpConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.release()
	c.mu.Unlock()
	return c.Conn.Close()
}

func (s *ownedMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	conn, ok := r.Context().Value(mcpConnContext{}).(*mcpConn)
	if !ok || !conn.begin() {
		http.Error(w, string(pa.Revoked), http.StatusForbidden)
		return
	}
	defer conn.finish()
	s.mu.Lock()
	if s.closing || s.ctx.Err() != nil {
		s.mu.Unlock()
		http.Error(w, string(pa.Revoked), http.StatusForbidden)
		return
	}
	s.effects.Add(1)
	s.mu.Unlock()
	defer s.effects.Done()
	if r.Host != s.listener.Addr().String() || r.Header.Get("Origin") != "" || len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		http.Error(w, string(pa.ScopeDenied), http.StatusForbidden)
		return
	}
	if r.URL.Path != "/mcp" || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		http.Error(w, string(pa.Invalid), http.StatusUnsupportedMediaType)
		return
	}
	if !mcpAccepts(r.Header.Get("Accept"), "application/json") || !mcpAccepts(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, string(pa.Unsupported), http.StatusNotAcceptable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(s.limits.FrameBytes)))
	if err != nil {
		http.Error(w, string(pa.CapacityFull), http.StatusRequestEntityTooLarge)
		return
	}
	// Reuse the existing duplicate-key/Unicode/depth/safe-integer boundary.
	// This MCP profile accepts integer/string request IDs, not fractional IDs.
	if _, err = domaincloud.Parse(body); err != nil {
		http.Error(w, string(pa.Invalid), http.StatusBadRequest)
		return
	}
	var f frame
	if err = mcpDecode(body, &f); err != nil || f.JSONRPC != "2.0" || f.Method == "" || len(f.Result) != 0 || f.Error != nil {
		http.Error(w, string(pa.Invalid), http.StatusBadRequest)
		return
	}
	if f.Method != "initialize" {
		s.mu.Lock()
		version := s.protocol
		initialized := s.initialized
		s.mu.Unlock()
		provided := r.Header.Get("MCP-Protocol-Version")
		if provided == "" {
			provided = "2025-03-26"
		}
		if !initialized {
			http.Error(w, string(pa.Invalid), http.StatusBadRequest)
			return
		}
		if provided != version || len(r.Header.Values("MCP-Protocol-Version")) > 1 {
			http.Error(w, string(pa.Unsupported), http.StatusBadRequest)
			return
		}
	}
	if len(f.ID) == 0 {
		if f.Method != "notifications/initialized" {
			http.Error(w, string(pa.Unsupported), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.ready = s.initialized
		ready := s.ready
		s.mu.Unlock()
		if !ready {
			http.Error(w, string(pa.Invalid), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var id any
	if json.Unmarshal(f.ID, &id) != nil || id == nil {
		http.Error(w, string(pa.Invalid), http.StatusBadRequest)
		return
	}
	switch id.(type) {
	case string, float64:
	default:
		http.Error(w, string(pa.Invalid), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.limits.RPCTimeout)
	defer cancel()
	result, err := s.request(ctx, f)
	var reply frame
	reply.JSONRPC, reply.ID = "2.0", f.ID
	if err != nil {
		reply.Error = &rpcError{Code: -32602, Message: string(pa.Code(err))}
	} else {
		reply.Result = raw(result)
	}
	encoded, encodeErr := json.Marshal(reply)
	if encodeErr != nil || len(encoded) > s.limits.FrameBytes {
		reply.Result = nil
		reply.Error = &rpcError{Code: -32603, Message: string(pa.OutcomeUnknown)}
		encoded, _ = json.Marshal(reply)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

func mcpAccepts(header, want string) bool {
	for _, item := range strings.Split(header, ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err == nil && kind == want && params["q"] != "0" {
			return true
		}
	}
	return false
}

func mcpDecode(body []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	return d.Decode(value)
}

func (s *ownedMCPServer) request(ctx context.Context, f frame) (any, error) {
	if f.Method == "initialize" {
		var q struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ClientInfo      json.RawMessage `json:"clientInfo"`
			Meta            json.RawMessage `json:"_meta"`
		}
		if mcpDecode(f.Params, &q) != nil || q.ProtocolVersion == "" {
			return nil, refusal(pa.Invalid, "mcp_initialize")
		}
		version := q.ProtocolVersion
		if version != "2025-03-26" && version != "2025-06-18" && version != "2025-11-25" {
			version = "2025-06-18"
		}
		s.mu.Lock()
		if s.initialized {
			s.mu.Unlock()
			return nil, refusal(pa.Invalid, "mcp_initialized")
		}
		s.initialized, s.protocol = true, version
		s.mu.Unlock()
		return map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "clawdline", "version": pa.ContractVersion}}, nil
	}
	s.mu.Lock()
	ready := s.ready
	s.mu.Unlock()
	if !ready {
		return nil, refusal(pa.Invalid, "mcp_not_initialized")
	}
	switch f.Method {
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools()}, nil
	case "tools/call":
		var q struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      json.RawMessage `json:"_meta"`
		}
		if mcpDecode(f.Params, &q) != nil {
			return nil, refusal(pa.Invalid, "mcp_tool")
		}
		if q.Name == "browser_status" || q.Name == "browser_command" {
			return s.browserTool(ctx, q.Name, q.Arguments), nil
		}
		value, err := s.tool(ctx, q.Name, q.Arguments)
		if err != nil {
			return mcpToolResult(map[string]string{"code": string(pa.Code(err))}, true), nil
		}
		return mcpToolResult(value, false), nil
	default:
		return nil, refusal(pa.Unsupported, "mcp_method")
	}
}

func mcpToolResult(value any, failed bool) any {
	text, err := json.Marshal(value)
	if err != nil {
		text, failed = []byte(`{"code":"pa_outcome_unknown"}`), true
	}
	return map[string]any{"isError": failed, "content": []any{map[string]string{"type": "text", "text": string(text)}}}
}

func (s *ownedMCPServer) tool(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
	// Idempotency IDs are explicit durable operation keys, independent of
	// transport RPC IDs. HTTP/native SDK retries cannot invent a new effect.
	switch name {
	case "list_bots":
		tools, ok := s.tools.(app.CollaborationTools)
		if !ok {
			return nil, refusal(pa.Unsupported, "collaboration_tools")
		}
		var q pa.BotDirectoryQuery
		if mcpDecode(mcpEmpty(arguments), &q) != nil {
			return nil, refusal(pa.Invalid, "bot_directory_arguments")
		}
		return tools.ListBots(ctx, q)
	case "message_bot":
		tools, ok := s.tools.(app.CollaborationTools)
		if !ok {
			return nil, refusal(pa.Unsupported, "collaboration_tools")
		}
		var q pa.BotMessageRequest
		if mcpDecode(arguments, &q) != nil {
			return nil, refusal(pa.Invalid, "bot_message_arguments")
		}
		return tools.MessageBot(ctx, q)
	case "list_routines", "save_routine", "enable_routine", "delete_routine", "test_run_routine":
		tools, ok := s.tools.(app.RoutineTools)
		if !ok {
			return nil, refusal(pa.Unsupported, "routine_tools")
		}
		var q app.RoutineRequest
		if mcpDecode(mcpEmpty(arguments), &q) != nil {
			return nil, refusal(pa.Invalid, "routine_arguments")
		}
		switch name {
		case "list_routines":
			q.Action = "list"
		case "save_routine":
			q.Action = "save"
		case "enable_routine":
			q.Action = "enable"
		case "delete_routine":
			q.Action = "delete"
		case "test_run_routine":
			q.Action = "test"
		}
		return tools.Routines(ctx, q)
	case "send_file", "publish_artifact":
		var q struct {
			ID       string `json:"idempotency_key"`
			Path     string `json:"path"`
			Name     string `json:"name"`
			Title    string `json:"title"`
			Artifact string `json:"artifact"`
		}
		if mcpDecode(arguments, &q) != nil || q.ID == "" || q.Path == "" || name == "send_file" && (q.Title != "" || q.Artifact != "") || name == "publish_artifact" && q.Name != "" {
			return nil, refusal(pa.Invalid, "mcp_output")
		}
		tools, ok := s.tools.(app.OutputTools)
		if !ok {
			return nil, refusal(pa.Unsupported, "mcp_output")
		}
		return tools.PublishOutput(ctx, q.ID, app.OutputRequest{Path: q.Path, Name: q.Name, Title: q.Title, Artifact: q.Artifact, HTML: name == "publish_artifact"})
	case "list_artifacts":
		if mcpDecode(mcpEmpty(arguments), &struct{}{}) != nil {
			return nil, refusal(pa.Invalid, "mcp_artifacts")
		}
		tools, ok := s.tools.(app.OutputTools)
		if !ok {
			return nil, refusal(pa.Unsupported, "mcp_output")
		}
		return tools.ListArtifacts(ctx)
	case "send_message":
		var q struct {
			ID   string `json:"idempotency_key"`
			Text string `json:"text"`
		}
		if mcpDecode(arguments, &q) != nil || q.ID == "" || pa.ValidateMessage(q.Text) != nil {
			return nil, refusal(pa.Invalid, "mcp_message")
		}
		if err := s.tools.SendMessage(ctx, q.ID, q.Text); err != nil {
			return nil, err
		}
		return map[string]string{"state": "delivered", "idempotency_key": q.ID}, nil
	case "dispatch_work":
		var q struct {
			ID     string   `json:"idempotency_key"`
			Brief  string   `json:"brief"`
			Claims []string `json:"claims"`
		}
		if mcpDecode(arguments, &q) != nil || q.ID == "" || q.Brief == "" || len(q.Claims) == 0 {
			return nil, refusal(pa.Invalid, "mcp_dispatch")
		}
		return s.tools.Dispatch(ctx, q.ID, q.Brief, q.Claims)
	case "work_status":
		var q struct {
			TaskID string `json:"task_id"`
		}
		if mcpDecode(arguments, &q) != nil || q.TaskID == "" {
			return nil, refusal(pa.Invalid, "mcp_work")
		}
		return s.tools.Status(ctx, q.TaskID)
	case "read_memory":
		if mcpDecode(mcpEmpty(arguments), &struct{}{}) != nil {
			return nil, refusal(pa.Invalid, "mcp_memory")
		}
		entries, err := s.tools.Memory(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entries": mcpMemoryLines(entries)}, nil
	case "search_memory":
		var q struct {
			Query string `json:"query"`
		}
		if mcpDecode(arguments, &q) != nil || strings.TrimSpace(q.Query) == "" {
			return nil, refusal(pa.Invalid, "mcp_memory")
		}
		entries, err := s.tools.Memory(ctx)
		if err != nil {
			return nil, err
		}
		found, more := app.SearchMemory(entries, q.Query)
		return map[string]any{"entries": mcpMemoryLines(found), "more": more}, nil
	case "edit_memory":
		var q struct {
			ID     string `json:"idempotency_key"`
			Append string `json:"append"`
			Old    string `json:"old"`
			New    string `json:"new"`
			Delete bool   `json:"delete"`
		}
		if mcpDecode(arguments, &q) != nil || q.ID == "" || q.Append == "" && q.Old == "" {
			return nil, refusal(pa.Invalid, "mcp_memory")
		}
		if err := s.tools.EditMemory(ctx, q.ID, app.MemoryEdit{Append: q.Append, Old: q.Old, New: q.New, Delete: q.Delete}); err != nil {
			return nil, err
		}
		return map[string]string{"state": "saved", "idempotency_key": q.ID}, nil
	default:
		return nil, refusal(pa.Unsupported, "mcp_tool")
	}
}

func mcpTools() []any {
	tool := func(name, description string, properties map[string]any, required ...string) any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
	}
	text := func() any { return map[string]any{"type": "string", "minLength": 1} }
	return []any{
		tool("list_bots", "List visible Bots on this machine in your exact Project. Returns minimal metadata, never chat history or memory. 列出同一台機器、同一個完整 Project 中可見的 Bot；僅回傳基本資料，不含對話或記憶。", map[string]any{"query": text(), "cursor": text(), "page_size": map[string]any{"type": "integer", "minimum": 1, "maximum": pa.MaxPageRows}}),
		tool("message_bot", "Queue one self-contained review request and return immediately. A correlated reply arrives later in your own chat. Do not wait or poll. Reuse the idempotency key after an uncertain response. 排入一個獨立完整的互審請求後立即返回；關聯回覆稍後會進入自己的對話。不要等待或輪詢；結果不明時沿用原冪等鍵。", map[string]any{"idempotency_key": text(), "target_bot_id": text(), "text": text()}, "idempotency_key", "target_bot_id", "text"),
		tool("list_routines", "List this Bot's routines, their saved triggers and next due times. Webhook and event schedules are unsupported.", map[string]any{}),
		tool("save_routine", "Save an explicitly requested schedule. Supply either legacy interval_seconds or triggers (OR). Triggers use type interval with seconds, once with at (Unix milliseconds), or cron with expression (five fields) and timeZone (explicit IANA name). Ask for missing calendar intent or timezone; never approximate it as an interval. Preserve unchanged triggers when editing name/instruction. Saving never test-runs. Reuse the idempotency key on retry.", map[string]any{"idempotency_key": text(), "id": text(), "name": text(), "instruction": text(), "interval_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": pa.MaxRoutineIntervalSeconds}, "triggers": routineTriggerSchema(), "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": pa.MaxRoutineTimeoutSeconds}, "enabled": map[string]any{"type": "boolean"}}, "idempotency_key", "name", "instruction"),
		tool("enable_routine", "Enable or pause this Bot's routine. A paused pending wake cannot send a prompt.", map[string]any{"idempotency_key": text(), "id": text(), "enabled": map[string]any{"type": "boolean"}}, "idempotency_key", "id", "enabled"),
		tool("delete_routine", "Delete this Bot's routine and cancel its pending wake; an already running task may finish.", map[string]any{"idempotency_key": text(), "id": text()}, "idempotency_key", "id"),
		tool("test_run_routine", "Explicitly enqueue one test run of this routine, including when paused. It runs in a separate session and returns its result to this chat.", map[string]any{"idempotency_key": text(), "id": text()}, "idempotency_key", "id"),
		tool("send_file", "Explicitly share a regular file in your Project as an immutable chat snapshot (up to 100 MiB). Reuse the idempotency key on retry.", map[string]any{"idempotency_key": text(), "path": text(), "name": text()}, "idempotency_key", "path"),
		tool("publish_artifact", "Publish self-contained HTML (inline CSS and JavaScript; up to 5 MiB). New artifacts need a title. To update one subject, reuse its artifact ID and publish the whole HTML. Reuse the idempotency key on retry.", map[string]any{"idempotency_key": text(), "path": text(), "title": text(), "artifact": text()}, "idempotency_key", "path"),
		tool("list_artifacts", "List your published HTML artifacts, newest first. Use the artifact ID to publish an updated whole snapshot.", map[string]any{}),
		tool("browser_status", "Read your own browser state: the selected browser profile, whether the person stopped the browser channel, and only the tabs you opened. 讀取自己的瀏覽器狀態：選用的瀏覽器、是否被停止，以及只有自己開的分頁。", map[string]any{}),
		tool("browser_command", "Run one browser operation in the Clawdline browser the person allowed for automatic use: open (args.url), read, click (args.label or args.x/args.y), type (args.text), key (args.key), scroll, screenshot, viewport, release or close. args.tab is optional and must be a tab you opened; omit it for your current tab. browser_mode may name only a browser automatic selection already allows. A refusal keeps its code: browser_stopped and browser_person_controlling mean wait for the person; browser_setup_required means ask them to set the browser up in Settings, Chrome. After browser_timeout the action may have happened: read the page before repeating it, and never repeat it in another browser.",
			map[string]any{"op": map[string]any{"type": "string", "enum": []string{"open", "read", "click", "type", "key", "scroll", "screenshot", "viewport", "release", "close"}}, "args": map[string]any{"type": "object"}, "browser_mode": map[string]any{"type": "string", "enum": []string{"isolated", "user_chrome"}}}, "op"),
		tool("send_message", "Publish an explicit message to this Bot conversation. Reuse its idempotency key after an uncertain response; do not invent a replacement key.", map[string]any{"idempotency_key": text(), "text": text()}, "idempotency_key", "text"),
		tool("dispatch_work", "Dispatch isolated work under the current Project policy. Unknown effects require receipt inspection, never automatic replay with a new key.", map[string]any{"idempotency_key": text(), "brief": text(), "claims": map[string]any{"type": "array", "items": text()}}, "idempotency_key", "brief", "claims"),
		tool("work_status", "Read a work receipt owned by this execution; it cannot grant human approval.", map[string]any{"task_id": text()}, "task_id"),
		tool("read_memory", "Read your memory: every entry, one short fact per entry, oldest first.", map[string]any{}),
		tool("search_memory", "Search your memory for entries containing every word of the query (case-insensitive).", map[string]any{"query": text()}, "query"),
		tool("edit_memory", "Change your memory. Give append to add one short fact as a new entry; old and new to replace text that appears exactly once in your memory; or old with delete to forget the entry holding it. A forgotten entry does not come back unless the user asks. Reuse the idempotency key after an uncertain response.",
			map[string]any{"idempotency_key": text(), "append": text(), "old": text(), "new": map[string]any{"type": "string"}, "delete": map[string]any{"type": "boolean"}}, "idempotency_key"),
	}
}

// mcpEmpty lets a tool without arguments be called with none at all.
func mcpEmpty(arguments json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(arguments))) == 0 || string(arguments) == "null" {
		return json.RawMessage("{}")
	}
	return arguments
}

func mcpMemoryLines(entries []pa.MemoryEntry) []map[string]string {
	out := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]string{"id": e.ID, "text": e.Text})
	}
	return out
}

var _ http.Handler = (*ownedMCPServer)(nil)

// browserResultTooLarge refuses a page result that cannot fit in one owned MCP
// frame. The command already ran; nothing was cut to make it fit.
const browserResultTooLarge = "browser_result_too_large"

// browserFrameMargin leaves room for the JSON-RPC envelope around a result.
const browserFrameMargin = 1024

func (s *ownedMCPServer) browserTool(ctx context.Context, name string, arguments json.RawMessage) any {
	tools, ok := s.tools.(app.BrowserTools)
	if !ok {
		return mcpToolResult(map[string]string{"code": string(pa.Unsupported)}, true)
	}
	if name == "browser_status" {
		if mcpDecode(mcpEmpty(arguments), &struct{}{}) != nil {
			return mcpToolResult(map[string]string{"code": string(pa.Invalid)}, true)
		}
		status, err := tools.BrowserStatus(ctx)
		if err != nil {
			return browserFailureResult(err)
		}
		if status.Tabs == nil {
			status.Tabs = []app.BrowserTab{}
		}
		return s.fit(mcpToolResult(status, false))
	}
	var q app.BrowserCommand
	if mcpDecode(arguments, &q) != nil || q.Op == "" {
		return mcpToolResult(map[string]string{"code": string(pa.Invalid)}, true)
	}
	if len(q.Args) == 0 || string(q.Args) == "null" {
		q.Args = nil
	}
	result, err := tools.BrowserCommand(ctx, q)
	if err != nil {
		return browserFailureResult(err)
	}
	if q.Op == "screenshot" {
		var shot struct {
			Data string `json:"data"`
			URL  string `json:"url"`
		}
		if json.Unmarshal(result.Value, &shot) != nil || shot.Data == "" {
			return mcpToolResult(map[string]string{"code": "browser_page_error", "message": "the extension returned no image"}, true)
		}
		if _, e := base64.StdEncoding.DecodeString(shot.Data); e != nil {
			return mcpToolResult(map[string]string{"code": "browser_page_error", "message": "the image was not valid base64"}, true)
		}
		caption, _ := json.Marshal(map[string]string{"op": "screenshot", "url": shot.URL})
		return s.fit(map[string]any{"isError": false, "content": []any{
			map[string]string{"type": "text", "text": string(caption)},
			map[string]string{"type": "image", "data": shot.Data, "mimeType": "image/jpeg"},
		}})
	}
	value := result.Value
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	return s.fit(mcpToolResult(map[string]any{"op": q.Op, "value": value}, false))
}

// fit returns the result, or a typed capacity refusal when its encoding would
// exceed the existing owned MCP frame (which would otherwise become unknown).
func (s *ownedMCPServer) fit(result any) any {
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded)+browserFrameMargin > s.limits.FrameBytes {
		return mcpToolResult(map[string]string{"code": browserResultTooLarge}, true)
	}
	return result
}

func browserFailureResult(err error) any {
	var browser *app.BrowserFailure
	if errors.As(err, &browser) {
		out := map[string]string{"code": browser.Code}
		if browser.Message != "" {
			out["message"] = browser.Message
		}
		return mcpToolResult(out, true)
	}
	return mcpToolResult(map[string]string{"code": string(pa.Code(err))}, true)
}
