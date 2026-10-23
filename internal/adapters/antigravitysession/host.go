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
}

type conversation struct {
	mu           sync.Mutex
	client       *antigravityprocess.Client
	row          session.Session
	entries      []contract.TranscriptEntry
	tools        map[string]int
	revision     uint64
	omitted      int
	bytes        int
	busy         bool
	textIndex    int
	writeGate    chan struct{}
	model        string
	contextKnown bool
	context      antigravityacp.ContextUsage
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
	return &Host{ctx: life, cancel: cancel, opts: opts, rows: map[string]*conversation{}}, nil
}
func (h *Host) Name() string { return string(session.BackendOwned) }

func (h *Host) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	if !filepath.IsAbs(req.Cwd) || req.Command != "" || len(req.Env) != 0 {
		return session.Session{}, refusal("unsupported_launch")
	}
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil || h.opening || len(h.rows) >= MaxSessions {
		h.mu.Unlock()
		return session.Session{}, refusal("unavailable")
	}
	h.opening = true
	h.jobs.Add(1)
	h.mu.Unlock()
	defer h.jobs.Done()
	defer func() { h.mu.Lock(); h.opening = false; h.mu.Unlock() }()
	c := &conversation{tools: map[string]int{}, textIndex: -1}
	client, err := antigravityprocess.Start(h.ctx, antigravityprocess.Options{Executable: h.opts.Executable, Args: h.opts.Args, AccountDir: h.opts.AccountDir, WorkingDir: req.Cwd, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime, OnUpdate: c.update})
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
	var metadata struct {
		Models struct {
			CurrentModelID string `json:"currentModelId"`
		} `json:"models"`
	}
	_ = json.Unmarshal(created, &metadata)
	now := time.Now()
	c.mu.Lock()
	c.model = metadata.Models.CurrentModelID
	c.row = session.Session{ID: "owned:acp:" + rand.Text(), Backend: session.BackendOwned, PID: client.PID(), Assistant: session.AssistantAntigravity, CWD: req.Cwd, Label: req.Name, ConversationID: client.SessionID(), Binding: session.BindingProtocol, State: session.StateIdle, Evidence: session.EvidenceStructured, Activity: session.Activity{At: now, Evidence: session.EvidenceStructured}, Observation: session.Observation{ObservedAt: now, Provenance: "owned", Freshness: session.FreshnessCurrent}}
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
	matches := c.row.PID == s.PID && c.row.ConversationID == s.ConversationID
	c.mu.Unlock()
	if !matches {
		return nil, refusal("identity_changed")
	}
	return c, nil
}
func (h *Host) OwnsProtocol(s session.Session) bool { _, err := h.lookup(s); return err == nil }
func (h *Host) Inventory(ctx context.Context) (session.Inventory, error) {
	now := time.Now()
	inv := session.Inventory{Complete: true, Provenance: "owned", ObservedAt: now, Sessions: []session.Session{}}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.rows {
		c.mu.Lock()
		row := c.row
		if c.client.Err() != nil {
			row.State = session.StateUnknown
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
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Lock()
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
		result, err := c.client.PromptChecked(turn, text, before, func() {
			stop()
			c.mu.Lock()
			c.row.State = session.StateWorking
			c.textIndex = -1
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
		if err != nil {
			var local interface{ BeforeWrite() bool }
			if errors.As(err, &local) && local.BeforeWrite() {
				c.row.State = session.StateIdle
				return
			}
			c.row.State = session.StateUnknown
			c.append(contract.TranscriptEntry{Role: "error", Text: err.Error(), At: time.Now().Unix()})
			return
		}
		c.row.State = session.StateIdle
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
	if err = c.client.Close(); err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.rows, s.ID)
	h.mu.Unlock()
	return nil
}
func (h *Host) Shutdown() error {
	h.mu.Lock()
	h.closed = true
	h.cancel()
	var clients []*antigravityprocess.Client
	for _, c := range h.rows {
		clients = append(clients, c.client)
	}
	h.mu.Unlock()
	var err error
	for _, c := range clients {
		err = errors.Join(err, c.Close())
	}
	h.jobs.Wait()
	h.mu.Lock()
	clear(h.rows)
	h.mu.Unlock()
	return err
}

func (c *conversation) changed() {
	c.revision++
	c.row.Activity = session.Activity{At: time.Now(), Evidence: session.EvidenceStructured}
}
func (c *conversation) append(e contract.TranscriptEntry) {
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
	id := c.client.SessionID()
	if id == "" {
		return nil
	}
	u, err := antigravityacp.DecodeSessionUpdate(frame, id)
	if err != nil {
		return err
	}
	if u.Ignored {
		return nil
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
		e := &c.entries[index]
		if d.Title != nil {
			e.Text = *d.Title
			e.Activity.Title = *d.Title
		}
		if d.Kind != nil {
			e.Tool = *d.Kind
		}
		if d.Status != nil {
			e.Activity.Status = *d.Status
			if *d.Status == "in_progress" {
				e.Activity.Status = "inProgress"
			}
		}
		if d.Locations != nil {
			e.Activity.Actions = []contract.TranscriptAction{}
			for _, location := range *d.Locations {
				e.Activity.Actions = append(e.Activity.Actions, contract.TranscriptAction{Kind: "read", Path: location.Path})
			}
		}
		if d.Content != nil {
			var parts []string
			for _, v := range *d.Content {
				if v.Content != nil && v.Content.Text != nil {
					parts = append(parts, *v.Content.Text)
				}
			}
			e.Activity.Result = strings.Join(parts, "\n")
		}
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
	c, err := h.lookup(s)
	if err != nil {
		return contract.TranscriptPage{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
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
	page := contract.TranscriptPage{ID: s.ID, Entries: entries, Evidence: contract.EvidenceStructured, Signature: fmt.Sprintf("acp:%s:%d", s.ConversationID, c.revision)}
	if omitted := c.omitted + start; omitted > 0 {
		page.Truncation = &contract.TranscriptTruncation{Reason: "transcript_byte_budget", EntriesOmittedCount: int64(omitted), BudgetBytes: MaxViewBytes}
	}
	return page, nil
}
func (h *Host) Stats() (sessions int, viewBytes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.rows {
		sessions++
		c.mu.Lock()
		viewBytes += c.bytes
		c.mu.Unlock()
	}
	return
}

// Facts are provider observations, not inferred prices or cumulative spend.
type Facts struct {
	Model   string
	Context *antigravityacp.ContextUsage
}

func (h *Host) Facts(s session.Session) (Facts, error) {
	c, err := h.lookup(s)
	if err != nil {
		return Facts{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	f := Facts{Model: c.model}
	if c.contextKnown {
		reading := c.context
		f.Context = &reading
	}
	return f, nil
}
