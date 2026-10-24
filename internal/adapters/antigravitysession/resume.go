package antigravitysession

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// activeLocked counts processes, never saved history rows. Caller holds h.mu.
func (h *Host) activeLocked() int {
	n := 0
	for _, c := range h.rows {
		c.mu.Lock()
		if c.client != nil {
			n++
		}
		c.mu.Unlock()
	}
	return n
}

// Resume restores only a locally owned exact native identity under its original
// cwd/profile. It never sends a prompt, discovers credentials, or creates a
// replacement native session. Stored rows are readable before authentication.
func (h *Host) Resume(ctx context.Context, cwd, conversationID string) (session.Session, error) {
	return h.restore(ctx, cwd, conversationID, false)
}

// Import is an explicit upgrade/adoption operation. The native listing must
// verify both the complete identity and cwd before any history is loaded.
func (h *Host) Import(ctx context.Context, cwd, conversationID string) (session.Session, error) {
	return h.restore(ctx, cwd, conversationID, true)
}

func (h *Host) restore(ctx context.Context, cwd, conversationID string, allowImport bool) (session.Session, error) {
	if !filepath.IsAbs(cwd) || conversationID == "" {
		return session.Session{}, refusal("invalid_identity")
	}
	cwd = filepath.Clean(cwd)
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		return session.Session{}, refusal("closed")
	}
	var c *conversation
	for _, candidate := range h.rows {
		candidate.mu.Lock()
		matches := candidate.row.CWD == cwd && candidate.row.ConversationID == conversationID
		candidate.mu.Unlock()
		if matches {
			c = candidate
			break
		}
	}
	importing := c == nil && allowImport && h.store != nil
	if c == nil && importing {
		now := time.Now()
		c = &conversation{store: h.store, profile: filepath.Clean(h.opts.AccountDir), generation: rand.Text(), tools: map[string]int{}, textIndex: -1,
			row: session.Session{ID: "owned:acp:" + rand.Text(), Backend: session.BackendOwned, Assistant: session.AssistantAntigravity, CWD: cwd, ConversationID: conversationID, Binding: session.BindingProtocol, State: session.StateUnknown, Evidence: session.EvidenceStructured, Activity: session.Activity{At: now, Evidence: session.EvidenceStructured}}}
	}
	if c == nil {
		h.mu.Unlock()
		return session.Session{}, refusal("not_found")
	}
	c.mu.Lock()
	if c.busy || c.stopping {
		c.mu.Unlock()
		h.mu.Unlock()
		return session.Session{}, refusal("busy")
	}
	if c.client != nil && c.client.Err() == nil && !c.uncertain {
		var err error
		if c.hidden {
			c.hidden = false
			err = c.saveState()
		}
		row := c.row
		c.mu.Unlock()
		h.mu.Unlock()
		return row, err
	}
	if h.opening || h.activeOtherLocked(c) >= MaxSessions {
		c.mu.Unlock()
		h.mu.Unlock()
		return session.Session{}, refusal("unavailable")
	}
	c.busy = true
	h.opening = true
	h.jobs.Add(1)
	oldClient := c.client
	c.client = nil
	stage := &conversation{store: c.store, profile: c.profile, row: c.row, generation: c.generation, revision: c.revision, ordinal: c.ordinal, tools: map[string]int{}, textIndex: -1, staging: true, launchContext: c.launchContext, injected: c.injected}
	needsLoad := importing || c.uncertain || c.store == nil || (oldClient != nil && oldClient.Err() != nil)
	c.mu.Unlock()
	h.mu.Unlock()
	defer h.jobs.Done()
	defer func() {
		h.mu.Lock()
		h.opening = false
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
		h.mu.Unlock()
	}()
	if oldClient != nil {
		if err := oldClient.Close(); err != nil {
			return session.Session{}, err
		}
	}
	identityPath := h.identityPath(stage.row.ID)
	if err := publishIdentity(identityPath, conversationID); err != nil {
		return session.Session{}, err
	}
	importedIdentityKept := false
	defer func() {
		if importing && !importedIdentityKept && identityPath != "" {
			_ = os.Remove(identityPath)
		}
	}()
	// The relay lock serializes update delivery and publication. readLoop owns
	// callbacks; it never calls a client method or waits on an RPC from a callback.
	var relay sync.Mutex
	target := stage
	client, err := antigravityprocess.Start(h.ctx, antigravityprocess.Options{SessionIdentityPath: identityPath, Executable: h.opts.Executable, Args: h.opts.Args, AccountDir: h.opts.AccountDir, WorkingDir: cwd, SquadCapabilityPath: stage.launchContext.SquadCapabilityPath, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime, OnUpdate: func(ctx context.Context, raw json.RawMessage) error {
		relay.Lock()
		defer relay.Unlock()
		return target.update(ctx, raw)
	}, OnPermission: func(ctx context.Context, p *antigravityprocess.PermissionRequest) error {
		relay.Lock()
		current := target
		relay.Unlock()
		return current.requestPermission(ctx, p)
	}})
	if err != nil {
		return session.Session{}, err
	}
	stage.mu.Lock()
	stage.client = client
	stage.mu.Unlock()
	keep := false
	defer func() {
		if !keep {
			_ = client.Close()
			if stage.store != nil && stage.generation != c.generation {
				_, _ = stage.store.db.Exec(`DELETE FROM entries WHERE id=? AND generation=?`, stage.row.ID, stage.generation)
			}
		}
	}()
	if _, err = client.Initialize(ctx); err != nil {
		return session.Session{}, err
	}
	if _, err = client.AuthenticatePersonal(ctx); err != nil {
		return session.Session{}, err
	}
	native := client
	if importing {
		title, err := findNativeIdentity(ctx, native, cwd, conversationID)
		if err != nil {
			return session.Session{}, err
		}
		stage.mu.Lock()
		stage.row.Label = title
		stage.mu.Unlock()
	}
	needsLoad = needsLoad || !native.CanResume()
	stage.mu.Lock()
	if needsLoad {
		stage.generation = rand.Text()
		stage.revision = 0
		stage.ordinal = 0
	}
	stage.replaying = needsLoad
	stage.mu.Unlock()
	var metadata json.RawMessage
	if needsLoad {
		metadata, err = native.LoadSession(ctx, conversationID)
	} else {
		metadata, err = native.ResumeSession(ctx, conversationID)
	}
	if err != nil {
		return session.Session{}, err
	}
	relay.Lock()
	defer relay.Unlock()
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if stage.storeErr != nil {
		return session.Session{}, stage.storeErr
	}
	if err = stage.readMetadata(metadata, true); err != nil {
		return session.Session{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil || ctx.Err() != nil {
		return session.Session{}, refusal("closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stage.row.PID = client.PID()
	stage.row.State = session.StateIdle
	stage.row.Observation = session.Observation{ObservedAt: time.Now(), Provenance: "owned", Freshness: session.FreshnessCurrent}
	stage.hidden = false
	stage.uncertain = false
	stage.staging = false
	if stage.store != nil {
		tx, err := stage.store.db.Begin()
		if err != nil {
			return session.Session{}, refusal("history_unavailable")
		}
		defer tx.Rollback()
		if err = stage.saveRow(tx); err == nil && needsLoad {
			_, err = tx.Exec(`DELETE FROM entries WHERE id=? AND generation<>?`, stage.row.ID, stage.generation)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			return session.Session{}, refusal("history_unavailable")
		}
	}
	c.client = client
	c.launchContext = stage.launchContext
	c.injected = stage.injected
	c.entries = stage.entries
	c.tools = stage.tools
	c.omitted = stage.omitted
	c.bytes = stage.bytes
	c.row = stage.row
	c.generation = stage.generation
	c.revision = stage.revision
	c.ordinal = stage.ordinal
	c.hidden = false
	c.uncertain = false
	c.storeErr = nil
	c.model = stage.model
	c.models = stage.models
	c.effort = stage.effort
	c.efforts = stage.efforts
	c.structuredConfig = stage.structuredConfig
	c.context = stage.context
	c.contextKnown = stage.contextKnown
	c.textOrdinal = 0
	c.textIndex = -1
	c.textRole = ""
	if importing {
		h.rows[c.row.ID] = c
	}
	target = c
	keep = true
	importedIdentityKept = true
	return c.row, nil
}

func findNativeIdentity(ctx context.Context, client *antigravityprocess.Client, cwd, id string) (string, error) {
	// The whole pagination operation shares the existing RPC deadline; fast
	// pages cannot turn discovery into an open-ended sequence of calls.
	ctx, cancel := context.WithTimeout(ctx, MaxCallTime)
	defer cancel()
	cursor := ""
	for {
		raw, err := client.ListSessions(ctx, cursor)
		if err != nil {
			return "", err
		}
		title, next, found, err := verifyNativeIdentity(raw, cwd, id)
		if err != nil {
			return "", err
		}
		if found {
			return title, nil
		}
		if next == "" {
			return "", refusal("native_identity_not_found")
		}
		if next == cursor {
			return "", refusal("native_history_incomplete")
		}
		cursor = next
	}
}

func verifyNativeIdentity(raw json.RawMessage, cwd, id string) (string, string, bool, error) {
	var page struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Title     string `json:"title"`
		} `json:"sessions"`
		NextCursor string `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &page) != nil || page.Sessions == nil {
		return "", "", false, refusal("invalid_native_list")
	}
	found := false
	title := ""
	for _, row := range page.Sessions {
		if row.SessionID != id {
			continue
		}
		if found || !filepath.IsAbs(row.CWD) || filepath.Clean(row.CWD) != cwd {
			return "", "", false, refusal("native_identity_unverified")
		}
		found = true
		title = row.Title
	}
	return title, page.NextCursor, found, nil
}

// c is already locked when excluding it from the process capacity count.
func (h *Host) activeOtherLocked(exclude *conversation) int {
	n := 0
	for _, c := range h.rows {
		if c == exclude {
			continue
		}
		c.mu.Lock()
		if c.client != nil {
			n++
		}
		c.mu.Unlock()
	}
	return n
}

func (c *conversation) project(u antigravityacp.SessionUpdate) (err error) {
	defer func() {
		if err != nil && !c.staging {
			c.uncertain = true
			c.row.State = session.StateUnknown
			_ = c.saveState()
		}
	}()
	if c.storeErr != nil {
		return c.storeErr
	}
	if u.Context != nil {
		c.context = *u.Context
		c.contextKnown = true
		return nil
	}
	if u.Tool != nil {
		d := u.Tool
		ordinal, e, err := c.readEntry(0, d.ToolCallID)
		if err != nil {
			return err
		}
		if ordinal == 0 {
			c.ordinal++
			ordinal = c.ordinal
			e = contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called", DurationMs: -1, Actions: []contract.TranscriptAction{}}}
			if !c.replaying {
				e.At = time.Now().Unix()
			}
		}
		applyToolDelta(&e, d)
		c.textOrdinal = 0
		c.textRole = ""
		c.changed()
		return c.saveEntry(ordinal, d.ToolCallID, e)
	}
	if u.Kind != "agent_message_chunk" && u.Kind != "user_message_chunk" {
		return nil
	}
	// Live prompts already have a local acceptance echo. Only load's isolated
	// replay projection accepts native user chunks; no text/hash deduplication.
	if u.Kind == "user_message_chunk" && !c.replaying {
		return nil
	}
	if u.Text == "" {
		return nil
	}
	role := "assistant"
	if u.Kind == "user_message_chunk" {
		role = "user"
	}
	ordinal := c.textOrdinal
	e := contract.TranscriptEntry{Role: role}
	if !c.replaying {
		e.At = time.Now().Unix()
	}
	if ordinal != 0 && c.textRole == role {
		var err error
		ordinal, e, err = c.readEntry(ordinal, "")
		if err != nil {
			return err
		}
	} else {
		ordinal = 0
	}
	if ordinal == 0 {
		c.ordinal++
		ordinal = c.ordinal
	}
	e.Text += u.Text
	// A long streamed message spans stable adjacent ordinals so every byte is
	// still pageable. The page budget never becomes a disk-retention policy.
	for {
		e.ID = c.entryID(ordinal)
		raw, _ := json.Marshal(e)
		if len(raw)+2 <= MaxViewBytes {
			c.textOrdinal = ordinal
			c.textRole = role
			c.changed()
			return c.saveEntry(ordinal, "", e)
		}
		lo, hi := 0, len(e.Text)
		for lo < hi {
			mid := lo + (hi-lo+1)/2
			part := e
			part.Text = e.Text[:mid]
			raw, _ := json.Marshal(part)
			if len(raw)+2 <= MaxViewBytes {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		for lo > 0 && lo < len(e.Text) && !utf8.RuneStart(e.Text[lo]) {
			lo--
		}
		if lo == 0 {
			return refusal("view_too_large")
		}
		rest := e.Text[lo:]
		e.Text = e.Text[:lo]
		c.changed()
		if err := c.saveEntry(ordinal, "", e); err != nil {
			return err
		}
		c.ordinal++
		ordinal = c.ordinal
		e.Text = rest
	}
}

func applyToolDelta(e *contract.TranscriptEntry, d *antigravityacp.ToolCallDelta) {
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
		for _, v := range *d.Locations {
			e.Activity.Actions = append(e.Activity.Actions, contract.TranscriptAction{Kind: "read", Path: v.Path})
		}
	}
	if d.Content != nil {
		parts := []string{}
		for _, v := range *d.Content {
			if v.Content != nil && v.Content.Text != nil {
				parts = append(parts, *v.Content.Text)
			}
		}
		e.Activity.Result = strings.Join(parts, "\n")
	}
}
