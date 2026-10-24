package antigravitysession

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Options selects an already signed-in personal profile. No profile is copied,
// discovered, or enabled by default. The lifetime belongs to the daemon.
type Options struct {
	Executable, AccountDir string
	StateDir               string
	Args                   []string
}

type Host struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	opts    Options
	rows    map[string]*conversation
	opening bool
	closed  bool
	jobs    sync.WaitGroup
	store   *historyStore
}

type conversation struct {
	mu               sync.Mutex
	client           *antigravityprocess.Client
	row              session.Session
	entries          []contract.TranscriptEntry
	tools            map[string]int
	revision         uint64
	omitted          int
	bytes            int
	busy             bool
	textIndex        int
	writeGate        chan struct{}
	model            string
	models           []contract.SessionModel
	effort           string
	efforts          []string
	structuredConfig bool
	quota            *contract.SessionLimits
	contextKnown     bool
	context          antigravityacp.ContextUsage
	store            *historyStore
	profile          string
	generation       string
	ordinal          int64
	textOrdinal      int64
	textRole         string
	uncertain        bool
	hidden           bool
	staging          bool
	replaying        bool
	storeErr         error
	permission       *antigravityprocess.PermissionRequest
}

type Error struct {
	Code     string
	Delivery antigravityprocess.Delivery
}

func (e *Error) BeforeWrite() bool { return e.Delivery == antigravityprocess.NotWritten }
func (e *Error) Error() string     { return "antigravity_session_" + e.Code }
func refusal(code string) error    { return &Error{Code: code, Delivery: antigravityprocess.NotWritten} }

func New(ctx context.Context, opts Options) (*Host, error) {
	if ctx == nil || ctx.Err() != nil || !filepath.IsAbs(opts.Executable) || !filepath.IsAbs(opts.AccountDir) {
		return nil, refusal("invalid_configuration")
	}
	life, cancel := context.WithCancel(ctx)
	h := &Host{ctx: life, cancel: cancel, opts: opts, rows: map[string]*conversation{}}
	if opts.StateDir != "" {
		var err error
		h.store, err = openHistory(opts.StateDir)
		if err != nil {
			cancel()
			return nil, err
		}
		h.rows, err = h.store.restore(filepath.Clean(opts.AccountDir))
		if err != nil {
			h.store.db.Close()
			cancel()
			return nil, err
		}
	}
	return h, nil
}
func (h *Host) Name() string { return string(session.BackendOwned) }

func (h *Host) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	if !filepath.IsAbs(req.Cwd) || req.Command != "" || len(req.Env) != 0 {
		return session.Session{}, refusal("unsupported_launch")
	}
	req.Cwd = filepath.Clean(req.Cwd)
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil || h.opening || h.activeLocked() >= MaxSessions {
		h.mu.Unlock()
		return session.Session{}, refusal("unavailable")
	}
	h.opening = true
	h.jobs.Add(1)
	h.mu.Unlock()
	defer h.jobs.Done()
	defer func() { h.mu.Lock(); h.opening = false; h.mu.Unlock() }()
	c := &conversation{tools: map[string]int{}, textIndex: -1, store: h.store, profile: filepath.Clean(h.opts.AccountDir), generation: rand.Text()}
	client, err := antigravityprocess.Start(h.ctx, antigravityprocess.Options{Executable: h.opts.Executable, Args: h.opts.Args, AccountDir: h.opts.AccountDir, WorkingDir: req.Cwd, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime, OnUpdate: c.update, OnPermission: c.requestPermission})
	if err != nil {
		return session.Session{}, err
	}
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
	keep := false
	defer func() {
		if !keep {
			_ = client.Close()
		}
	}()
	if _, err = client.Initialize(ctx); err != nil {
		return session.Session{}, err
	}
	if _, err = client.AuthenticatePersonal(ctx); err != nil {
		return session.Session{}, err
	}
	created, err := client.NewSession(ctx)
	if err != nil {
		return session.Session{}, err
	}
	now := time.Now()
	c.mu.Lock()
	if err := c.readMetadata(created, true); err != nil {
		c.mu.Unlock()
		return session.Session{}, err
	}
	c.row = session.Session{ID: "owned:acp:" + rand.Text(), Backend: session.BackendOwned, PID: client.PID(), Assistant: session.AssistantAntigravity, CWD: req.Cwd, Label: req.Name, ConversationID: client.SessionID(), Binding: session.BindingProtocol, State: session.StateIdle, Evidence: session.EvidenceStructured, Activity: session.Activity{At: now, Evidence: session.EvidenceStructured}, Observation: session.Observation{ObservedAt: now, Provenance: "owned", Freshness: session.FreshnessCurrent}}
	if err := c.saveState(); err != nil {
		c.mu.Unlock()
		return session.Session{}, err
	}
	row := c.row
	c.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil || ctx.Err() != nil {
		return session.Session{}, refusal("closed")
	}
	h.rows[row.ID] = c
	keep = true
	return row, nil
}

func (h *Host) lookup(s session.Session) (*conversation, error) {
	if s.Backend != session.BackendOwned || s.Binding != session.BindingProtocol || s.Assistant != session.AssistantAntigravity {
		return nil, refusal("identity_changed")
	}
	h.mu.Lock()
	c := h.rows[s.ID]
	h.mu.Unlock()
	if c == nil {
		return nil, refusal("not_found")
	}
	c.mu.Lock()
	matches := c.row.ConversationID == s.ConversationID && c.row.CWD == s.CWD && (c.store != nil || c.row.PID == s.PID)
	c.mu.Unlock()
	if !matches {
		return nil, refusal("identity_changed")
	}
	return c, nil
}
func (h *Host) OwnsProtocol(s session.Session) bool {
	c, err := h.lookup(s)
	if err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.hidden
}
func (h *Host) Inventory(ctx context.Context) (session.Inventory, error) {
	now := time.Now()
	inv := session.Inventory{Complete: true, Provenance: "owned", ObservedAt: now, Sessions: []session.Session{}}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.rows {
		c.mu.Lock()
		if c.hidden {
			c.mu.Unlock()
			continue
		}
		row := c.row
		if c.permission != nil && c.permission.Active() {
			row.Menu = permissionMenu(c.permission)
			row.State = session.StateWaiting
			row.Line = session.MenuRevision(*row.Menu)
		}
		if c.client != nil && c.client.Err() != nil {
			row.Menu = nil
			row.State = session.StateUnknown
			if !c.uncertain {
				c.uncertain = true
				c.row.State = session.StateUnknown
				_ = c.saveState()
			}
		}
		c.mu.Unlock()
		row.Observation = session.Observation{ObservedAt: now, Provenance: "owned", Freshness: session.FreshnessCurrent}
		inv.Sessions = append(inv.Sessions, row)
	}
	return inv, nil
}

// SendProtocol keeps the normal operation check immediately before the single
// pipe write. Its receipt says accepted; the owned job separately completes the
// model turn. Cancellation after acceptance never replays the prompt.
func (h *Host) SendProtocol(ctx context.Context, s session.Session, text string, before func() error) error {
	if text == "" {
		return refusal("empty_text")
	}
	c, err := h.lookup(s)
	if err != nil {
		return err
	}
	if _, err = h.Resume(ctx, s.CWD, s.ConversationID); err != nil {
		return err
	}
	if text == "/model" || strings.HasPrefix(text, "/model ") {
		return h.selectModel(ctx, c, strings.TrimPrefix(text, "/model "), before)
	}
	if text == "/effort" || strings.HasPrefix(text, "/effort ") {
		return h.selectConfig(ctx, c, "thinking_level", strings.TrimPrefix(text, "/effort "), before)
	}
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Lock()
	if c.hidden || c.client == nil {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("closed")
	}
	client := c.client
	if c.busy || c.row.State != session.StateIdle {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("busy")
	}
	encoded, _ := json.Marshal(contract.TranscriptEntry{Role: "user", Text: text, At: time.Now().Unix()})
	if len(encoded)+2 > MaxViewBytes {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("text_too_large")
	}
	c.busy = true
	c.writeGate = make(chan struct{})
	c.mu.Unlock()
	h.jobs.Add(1)
	h.mu.Unlock()
	accepted := make(chan struct{})
	done := make(chan struct{})
	var outcome error
	go func() {
		defer h.jobs.Done()
		defer close(done)
		defer func() {
			c.mu.Lock()
			c.busy = false
			if c.writeGate != nil {
				close(c.writeGate)
				c.writeGate = nil
			}
			c.mu.Unlock()
		}()
		if ctx.Err() != nil {
			outcome = refusal("cancelled")
			return
		}
		// Only the initial acceptance uses the HTTP context. The model response is
		// owned by the daemon and survives the caller disconnecting after acceptance.
		turn, cancel := context.WithCancel(h.ctx)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		result, err := client.PromptChecked(turn, text, func() error {
			if before != nil {
				if err := before(); err != nil {
					return err
				}
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.storeErr != nil {
				return c.storeErr
			}
			// Persist uncertainty before the pipe write. A crash never replays a prompt.
			c.uncertain = true
			return c.saveState()
		}, func() {
			stop()
			c.mu.Lock()
			c.row.State = session.StateWorking
			c.textIndex = -1
			c.textOrdinal = 0
			c.textRole = ""
			c.append(contract.TranscriptEntry{Role: "user", Text: text, At: time.Now().Unix()})
			close(c.writeGate)
			c.writeGate = nil
			c.mu.Unlock()
			close(accepted)
		})
		stop()
		c.mu.Lock()
		defer c.mu.Unlock()
		outcome = err
		c.permission = nil
		if err != nil {
			var local interface{ BeforeWrite() bool }
			if errors.As(err, &local) && local.BeforeWrite() {
				c.row.State = session.StateIdle
				c.uncertain = false
				_ = c.saveState()
				return
			}
			c.row.State = session.StateUnknown
			c.uncertain = true
			c.append(contract.TranscriptEntry{Role: "error", Text: err.Error(), At: time.Now().Unix()})
			return
		}
		c.row.State = session.StateIdle
		c.uncertain = false
		if c.storeErr != nil {
			c.row.State = session.StateUnknown
			c.uncertain = true
		}
		if saveErr := c.saveState(); saveErr != nil {
			outcome = saveErr
			c.row.State = session.StateUnknown
		}
		if result.StopReason != "end_turn" {
			c.append(contract.TranscriptEntry{Role: "error", Text: "antigravity_turn_did_not_end_normally", At: time.Now().Unix()})
		}
	}()
	select {
	case <-accepted:
		return nil
	case <-done:
		select {
		case <-accepted:
			return nil
		default:
			return outcome
		}
	}
}
func (h *Host) Send(ctx context.Context, s session.Session, text string) error {
	return h.SendProtocol(ctx, s, text, nil)
}
func (h *Host) Interrupt(context.Context, session.Session) error {
	return refusal("turn_interrupt_unsupported")
}
func (h *Host) Reveal(context.Context, session.Session, bool) error {
	return refusal("terminal_unavailable")
}
func (h *Host) Screen(context.Context, session.Session, int) (string, bool) { return "", false }
func (h *Host) Close(ctx context.Context, s session.Session) error {
	c, err := h.lookup(s)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Lock()
	client := c.client
	if c.busy && (c.permission == nil || !c.permission.Active()) {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("busy")
	}
	c.busy = true
	h.jobs.Add(1)
	h.mu.Unlock()
	defer h.jobs.Done()
	defer func() { c.mu.Lock(); c.busy = false; c.mu.Unlock() }()
	c.hidden = true
	c.permission = nil
	if client != nil && client.Err() != nil {
		c.uncertain = true
		c.row.State = session.StateUnknown
	}
	if err := c.saveState(); err != nil {
		c.hidden = false
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	if client != nil {
		if err = client.Close(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.client = nil
	c.row.PID = 0
	c.mu.Unlock()
	h.mu.Lock()
	if c.store == nil {
		delete(h.rows, s.ID)
	}
	h.mu.Unlock()
	return nil
}
func (h *Host) Shutdown() error {
	var err error
	h.mu.Lock()
	h.closed = true
	var clients []*antigravityprocess.Client
	for _, c := range h.rows {
		c.mu.Lock()
		if c.client != nil {
			if c.client.Err() != nil && !c.uncertain {
				c.uncertain = true
				c.row.State = session.StateUnknown
				err = errors.Join(err, c.saveState())
			}
			clients = append(clients, c.client)
		}
		c.mu.Unlock()
	}
	h.cancel()
	h.mu.Unlock()
	for _, c := range clients {
		err = errors.Join(err, c.Close())
	}
	h.jobs.Wait()
	h.mu.Lock()
	clear(h.rows)
	h.mu.Unlock()
	if h.store != nil {
		err = errors.Join(err, h.store.db.Close())
	}
	return err
}

func (c *conversation) changed() {
	c.revision++
	c.row.Activity = session.Activity{At: time.Now(), Evidence: session.EvidenceStructured}
}
func (c *conversation) append(e contract.TranscriptEntry) {
	if c.store != nil {
		c.ordinal++
		c.changed()
		if err := c.saveEntry(c.ordinal, "", e); err != nil {
			c.storeErr = err
		}
		return
	}
	c.entries = append(c.entries, e)
	c.changed()
	c.bound()
}
func (c *conversation) bound() {
	// Charge serialized entries and retained exact tool IDs to the same budget.
	// Evict whole entries, including an oversized single entry, with an explicit
	// truncation receipt. Clear the backing slot so evicted payload is released.
	raw, _ := json.Marshal(c.entries)
	c.bytes = len(raw)
	for id := range c.tools {
		c.bytes += len(id)
	}
	for c.bytes > MaxViewBytes && len(c.entries) > 0 {
		c.entries[0] = contract.TranscriptEntry{}
		c.entries = c.entries[1:]
		c.omitted++
		c.textIndex--
		for id, index := range c.tools {
			if index == 0 {
				delete(c.tools, id)
			} else {
				c.tools[id] = index - 1
			}
		}
		raw, _ = json.Marshal(c.entries)
		c.bytes = len(raw)
		for id := range c.tools {
			c.bytes += len(id)
		}
	}
	if len(c.entries) == 0 {
		c.entries = nil
	}
}
func (c *conversation) update(ctx context.Context, frame json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	gate := c.writeGate
	c.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Bootstrap notifications precede any user prompt; they are not a transcript.
	if c.client == nil {
		return nil
	}
	if c.store != nil && c.row.ConversationID == "" {
		return nil
	}
	id := c.client.SessionID()
	if id == "" {
		return nil
	}
	u, err := antigravityacp.DecodeSessionUpdate(frame, id)
	if err != nil {
		return err
	}
	if u.Kind == "config_option_update" {
		var envelope struct {
			Params struct {
				Update json.RawMessage `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(frame, &envelope) != nil {
			return refusal("invalid_config")
		}
		return c.readMetadata(envelope.Params.Update, false)
	}
	if u.Ignored {
		return nil
	}
	if c.store != nil {
		return c.project(u)
	}
	switch {
	case u.Context != nil:
		c.context = *u.Context
		c.contextKnown = true
	case u.Tool != nil:
		d := u.Tool
		index, ok := c.tools[d.ToolCallID]
		if !ok {
			index = len(c.entries)
			c.entries = append(c.entries, contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called", DurationMs: -1, Actions: []contract.TranscriptAction{}}, At: time.Now().Unix()})
			c.tools[d.ToolCallID] = index
		}
		applyToolDelta(&c.entries[index], d)
		c.textIndex = -1
		c.changed()
		c.bound()
	case u.Kind == "agent_message_chunk":
		if u.Text == "" {
			return nil
		}
		if c.textIndex < 0 || c.textIndex >= len(c.entries) {
			c.entries = append(c.entries, contract.TranscriptEntry{Role: "assistant", At: time.Now().Unix()})
			c.textIndex = len(c.entries) - 1
		}
		c.entries[c.textIndex].Text += u.Text
		c.changed()
		c.bound()
		// Thought chunks remain distinct and are not presented as assistant replies.
	}
	if c.bytes > MaxViewBytes {
		return refusal("view_too_large")
	}
	return nil
}

// Transcript is a complete retained snapshot. It deliberately offers no byte
// cursors or durable-history claim; revisions change on real view mutations.
func (h *Host) Transcript(s session.Session, limit int) (contract.TranscriptPage, error) {
	return h.TranscriptPage(context.Background(), s, limit, 0, "")
}
func (c *conversation) memoryTranscript(s session.Session, limit int) contract.TranscriptPage {
	start := 0
	if limit > 0 && len(c.entries) > limit {
		start = len(c.entries) - limit
	}
	raw, _ := json.Marshal(c.entries[start:])
	var entries []contract.TranscriptEntry
	_ = json.Unmarshal(raw, &entries)
	if entries == nil {
		entries = []contract.TranscriptEntry{}
	}
	page := contract.TranscriptPage{ID: s.ID, Entries: entries, Evidence: contract.EvidenceStructured, Signature: fmt.Sprintf("acp2:%s:%d", c.generation, c.revision)}
	if omitted := c.omitted + start; omitted > 0 {
		page.Truncation = &contract.TranscriptTruncation{Reason: "transcript_byte_budget", EntriesOmittedCount: int64(omitted), BudgetBytes: MaxViewBytes}
	}
	return page
}
func (h *Host) Stats() (sessions int, viewBytes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.rows {
		c.mu.Lock()
		if c.client != nil {
			sessions++
		}
		viewBytes += c.bytes
		c.mu.Unlock()
	}
	return
}

func (h *Host) HistoryConnections() int {
	if h.store == nil {
		return 0
	}
	return h.store.db.Stats().OpenConnections
}

// Facts are provider observations, not inferred prices or cumulative spend.
type Facts struct {
	Model   string
	Models  []contract.SessionModel
	Effort  string
	Efforts []string
	Limits  *contract.SessionLimits
	Context *antigravityacp.ContextUsage
}

func (h *Host) Facts(s session.Session) (Facts, error) {
	c, err := h.lookup(s)
	if err != nil {
		return Facts{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	f := Facts{Model: c.model, Models: append([]contract.SessionModel{}, c.models...), Effort: c.effort, Efforts: append([]string(nil), c.efforts...), Limits: quotaSnapshot(c.quota, time.Now())}
	if c.contextKnown {
		reading := c.context
		f.Context = &reading
	}
	return f, nil
}
