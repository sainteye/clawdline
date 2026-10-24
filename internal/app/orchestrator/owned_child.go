package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// OwnedChild is the configured Host's actual launch, before any first input.
// Open may return Row together with an error so cleanup retains its identity.
type OwnedChild struct {
	Row             session.Session
	Model           string
	ReasoningEffort string
}

func (b *Broker) ownedChildAvailable(ctx context.Context) bool {
	return b.OwnedChildAvailable != nil && b.OpenOwnedChild != nil && b.OwnedChildAvailable(ctx)
}

func childCWD(r Record) string {
	if r.Worktree != nil {
		rel, err := filepath.Rel(r.Worktree.Repository, r.ProjectDir)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = "."
		}
		return filepath.Join(r.Worktree.Path, rel)
	}
	return r.ProjectDir
}

func ownedChildMatches(s session.Session, id, native, cwd string) bool {
	return id != "" && native != "" && filepath.IsAbs(cwd) &&
		strings.HasPrefix(id, "owned:acp:") && s.ID == id &&
		s.Backend == session.BackendOwned && s.Binding == session.BindingProtocol &&
		s.Assistant == session.AssistantAntigravity && s.ConversationID == native &&
		filepath.Clean(s.CWD) == filepath.Clean(cwd)
}

func (b *Broker) spawnOwnedChild(ctx context.Context, r Record, cwd, secret string, opened func()) Record {
	fail := func(err error) Record {
		r.SpawnError = err.Error()
		if r.ChildTerminalID != "" {
			r.Unbriefed = true
		} else {
			r.State = StateSpawnFailed
		}
		return r
	}
	if !b.ownedChildAvailable(ctx) {
		return fail(errors.New("provider_permission_unsupported"))
	}
	if r.Model != "" && !modelName(r.Model) {
		return fail(errors.New("invalid owned child model"))
	}
	if r.ReasoningEffort != "" && !projects.KnownAntigravityEffort(r.ReasoningEffort) {
		return fail(errors.New("invalid owned child reasoning effort"))
	}
	prepared, err := b.prepareSquadLaunch(ctx, r.Persona, cwd, r.Claims)
	if err != nil {
		return fail(err)
	}
	if err := outside(); err != nil {
		return fail(err)
	}
	launchCtx, launchCancel := effectContext(ctx)
	defer launchCancel()
	owned, openErr := b.OpenOwnedChild(launchCtx, r, cwd, prepared.launch.ID, prepared.files)
	opened()
	if owned.Row.ID == "" {
		if prepared.launch.ID != "" {
			_ = b.Store.FailSquadLaunch(ctx, prepared.launch.ID)
		}
		if openErr == nil {
			openErr = errors.New("owned_child_binding_invalid")
		}
		return fail(openErr)
	}
	// Retain even a failed configuration's identity. No input is possible until
	// both the task binding and the squad conversation are durable.
	r.ChildTerminalID, r.ChildConversationID = owned.Row.ID, owned.Row.ConversationID
	r.ChildBackend, r.SpawnedAt, r.State = "owned", b.now(), StateSpawning
	r.Model, r.ReasoningEffort = owned.Model, owned.ReasoningEffort
	rec, cancel := recordContext(ctx)
	defer cancel()
	held, err := b.mutate(rec, r.ID, "task.owned_send_intent", func(t *Record) error {
		t.ChildTerminalID, t.ChildConversationID, t.ChildBackend = r.ChildTerminalID, r.ChildConversationID, r.ChildBackend
		t.SpawnedAt, t.Model, t.ReasoningEffort = r.SpawnedAt, r.Model, r.ReasoningEffort
		if t.State == StateQueued {
			t.State = StateSpawning
		}
		return nil
	})
	if err != nil {
		if b.CloseOwnedChild != nil {
			if closeErr := b.CloseOwnedChild(rec, r.ChildTerminalID, r.ChildConversationID, cwd); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		return fail(err)
	}
	if openErr != nil {
		if prepared.launch.ID != "" {
			_ = b.Store.FailSquadLaunch(rec, prepared.launch.ID)
		}
		return fail(openErr)
	}
	if !ownedChildMatches(owned.Row, r.ChildTerminalID, r.ChildConversationID, cwd) {
		return fail(errors.New("owned_child_binding_invalid"))
	}
	if prepared.launch.ID != "" {
		b.recordSquadTerminal(rec, prepared, r.ChildTerminalID)
		if err := b.Store.RecordSquadTerminal(rec, prepared.launch.ID, r.ChildTerminalID); err != nil {
			return fail(err)
		}
		if err := b.Store.BindSquadConversation(rec, prepared.launch.ID, r.ChildConversationID); err != nil {
			return fail(err)
		}
	}
	held, _, err = b.Record(rec, r.ID)
	if err != nil {
		return fail(err)
	}
	// A concurrent cancellation/result already owns the terminal state. Never
	// send a briefing to a task that ended while its Host was configuring.
	if held.State.Terminal() {
		// The task may have been cancelled before the open returned its row.
		// The Host callback still checks the original native binding itself.
		if b.CloseOwnedChild != nil {
			if err := b.CloseOwnedChild(rec, r.ChildTerminalID, r.ChildConversationID, cwd); err != nil {
				r.SpawnError = "owned_child_close_failed: " + err.Error()
			}
		}
		return r
	}
	if b.Type == nil {
		return fail(errors.New("owned_child_sender_unavailable"))
	}
	if err := b.typeLine(launchCtx, r.ChildTerminalID, FirstLine(r, secret, b.Language)); err != nil {
		r.SpawnError = err.Error()
		r.Unbriefed = nothingTyped(err)
	}
	// Uncertain writes are deliberately never replayed. A crash between open
	// and durable binding is not exactly-once; no second outbox is introduced.
	return r
}

func (b *Broker) runCloseOwnedChild(ctx context.Context, e store.Effect, c closeChildEffect) effectResult {
	fail := func(code string, err error) effectResult {
		return effectResult{state: store.EffectFailed, outcome: code, err: err}
	}
	if b.CloseOwnedChild == nil {
		return fail("owned_child_close_unavailable", nil)
	}
	if !strings.HasPrefix(c.Pane, "owned:acp:") || c.NativeID == "" || !filepath.IsAbs(c.CWD) {
		return fail("owned_child_binding_invalid", nil)
	}
	rd := b.read(ctx)
	if s, present := rd.session(c.Pane); present {
		if !ownedChildMatches(s, c.Pane, c.NativeID, c.CWD) {
			return fail("owned_child_binding_mismatch", nil)
		}
		if s.Observation.Freshness == session.FreshnessUnverified || s.Observation.Freshness == session.FreshnessMissing {
			return fail("owned_child_inventory_unknown", nil)
		}
	} else if !rd.sourceComplete("owned") {
		return fail("owned_child_inventory_unknown", nil)
	}
	if err := b.CloseOwnedChild(ctx, c.Pane, c.NativeID, c.CWD); err != nil {
		return fail("owned_child_close_failed", err)
	}
	payload, _ := json.Marshal(map[string]any{"task": e.Subject, "backend": "owned", "pane": c.Pane, "closed": true})
	return effectResult{state: store.EffectDone, outcome: "closed",
		events: []store.Event{{Kind: "task.child.closed", Subject: e.Subject, Payload: payload}}}
}
