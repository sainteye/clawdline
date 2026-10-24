package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func ownedFixture(cwd string) OwnedChild {
	return OwnedChild{Row: session.Session{ID: "owned:acp:synthetic", Backend: session.BackendOwned,
		Assistant: session.AssistantAntigravity, Binding: session.BindingProtocol,
		ConversationID: "synthetic-native", CWD: cwd, State: session.StateIdle},
		Model: "synthetic-model", ReasoningEffort: "high"}
}

func TestOwnedChildDispatchBindingAndExistingReceipts(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	const id = "d8300001-0000-4000-8000-000000000001"
	b.OwnedChildAvailable = func(context.Context) bool { return true }
	b.TerminalCapabilities = fakeTerminals(lacks(ports.CapReadScreen, "no terminal"), lacks(ports.CapSendKeys, "no terminal"))
	b.Screen = func(context.Context, string) (string, bool) { t.Fatal("owned screen read"); return "", false }
	resolved, opens, sends := 0, 0, 0
	b.ResolveSquadSnapshot = func(_ context.Context, role, cwd string) (json.RawMessage, error) {
		resolved++
		return json.RawMessage(`{"definition_id":"clawdline.persona.backend","scope_id":"global","definition":{"version":"1","body":"Synthetic role"}}`), nil
	}
	var row session.Session
	b.OpenOwnedChild = func(_ context.Context, r Record, cwd, launch string, files squadfiles.Files) (OwnedChild, error) {
		opens++
		if r.Worktree == nil || cwd != r.Worktree.Path || strings.Join(r.Claims, ",") != "worker.go" {
			t.Fatal("original worktree/claims lost")
		}
		if launch == "" || files.PromptPath == "" {
			t.Fatal("missing original squad")
		}
		if body, err := os.ReadFile(files.PromptPath); err != nil || !strings.Contains(string(body), "Synthetic role") {
			t.Fatal("missing role prompt")
		}
		out := ownedFixture(cwd)
		row = out.Row
		return out, nil
	}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sources: map[string]bool{"owned": true}, Sessions: []session.Session{row}}
	}
	b.Type = func(ctx context.Context, terminal, line string) error {
		sends++
		r, _, err := b.Record(ctx, id)
		if err != nil || r.State != StateSpawning || r.ChildConversationID != row.ConversationID || r.ChildTerminalID != terminal || r.SpawnedAt.IsZero() || r.Model != "synthetic-model" || r.ReasoningEffort != "high" {
			t.Fatal("binding/facts not durable before send")
		}
		_, _, ok, err := b.Store.SquadSnapshotForConversation(ctx, row.ConversationID)
		if err != nil || !ok {
			t.Fatal("squad not bound before send")
		}
		if !strings.Contains(line, FirstLine(r, w1Secret, b.Language)) {
			t.Fatal("wrong first input")
		}
		if _, err := b.Accept(ctx, id, w1Secret); err != nil {
			t.Fatal(err)
		}
		return nil
	}
	writeBrief(t, b, id, repo, map[string]any{"assistant": "antigravity", "persona": "backend", "claims": []string{"worker.go"}, "isolation": "worktree"})
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true, Generation: inv.Generation})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateBriefed || out.Record.ChildConversationID != row.ConversationID || opens != 1 || sends != 1 || resolved != 1 {
		t.Fatal("dispatch lost concurrent accept or duplicated launch")
	}
	result := taskdir.Result{Protocol: Protocol, TaskID: id, Secret: w1Secret, Status: "success", Summary: "Synthetic completion"}
	body, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(b.Tasks.Path(id), "result.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.collect(ctx, out.Record); !ok || err != nil {
		t.Fatal("existing result collector", err)
	}
	r, _, _ := b.Record(ctx, id)
	if r.State != StateSuccess || r.Notice == nil {
		t.Fatal("existing settlement not used")
	}
}

func TestOwnedChildFailureAndUncertainSend(t *testing.T) {
	for _, mode := range []string{"open", "config", "uncertain", "unsent"} {
		t.Run(mode, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			const id = "d8300002-0000-4000-8000-000000000002"
			b.OwnedChildAvailable = func(context.Context) bool { return true }
			opens, sends, closes := 0, 0, 0
			owned := ownedFixture(b.Dir)
			b.OpenOwnedChild = func(context.Context, Record, string, string, squadfiles.Files) (OwnedChild, error) {
				opens++
				if mode == "open" {
					return OwnedChild{}, errors.New("open failed")
				}
				if mode == "config" {
					return owned, errors.New("config failed")
				}
				return owned, nil
			}
			b.Reading = func(context.Context) session.Inventory {
				return session.Inventory{Sources: map[string]bool{"owned": true}, Sessions: []session.Session{owned.Row}}
			}
			b.CloseOwnedChild = func(_ context.Context, id, native, cwd string) error {
				if id != owned.Row.ID || native != owned.Row.ConversationID || cwd != b.Dir {
					t.Fatal("wrong close")
				}
				closes++
				return nil
			}
			b.Type = func(context.Context, string, string) error {
				sends++
				if mode == "unsent" {
					return terminal.Unsent{Why: "not written"}
				}
				return errors.New("unknown send outcome")
			}
			writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "antigravity"})
			req := DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true}
			out, err := b.Dispatch(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "uncertain" {
				if out.Record.State != StateSpawning || out.Record.Unbriefed || sends != 1 {
					t.Fatal("uncertain send misclassified")
				}
				if _, err := b.Dispatch(ctx, req); err != nil {
					t.Fatal(err)
				}
				b.Pass(ctx)
				if opens != 1 || sends != 1 || stallCandidate(out.Record) {
					t.Fatal("uncertain send replayed or nudged")
				}
			} else {
				if out.Record.State != StateSpawnFailed {
					t.Fatal("failure not settled")
				}
				if (mode == "open" || mode == "config") && sends != 0 {
					t.Fatal("failed config sent prompt")
				}
				if mode != "open" && (out.Record.ChildConversationID == "" || closes == 0) {
					t.Fatal("failed launch identity/cleanup lost")
				}
			}
		})
	}
}

func TestOwnedChildCloseRequiresExactBinding(t *testing.T) {
	for _, mode := range []string{"exact", "different-native", "different-cwd", "unknown", "gone", "callback-error"} {
		t.Run(mode, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			owned := ownedFixture(b.Dir)
			r := Record{ID: "d8300003-0000-4000-8000-000000000003", ChildBackend: "owned", ChildTerminalID: owned.Row.ID, ChildConversationID: owned.Row.ConversationID, ProjectDir: b.Dir}
			rd := session.Inventory{Sources: map[string]bool{"owned": true}, Sessions: []session.Session{owned.Row}}
			switch mode {
			case "different-native":
				rd.Sessions[0].ConversationID = "other"
			case "different-cwd":
				rd.Sessions[0].CWD = filepath.Join(b.Dir, "other")
			case "unknown":
				rd.Sessions = nil
				rd.Sources["owned"] = false
			case "gone":
				rd.Sessions = nil
			}
			b.Reading = func(context.Context) session.Inventory { return rd }
			closes := 0
			b.CloseOwnedChild = func(_ context.Context, id, native, cwd string) error {
				closes++
				if id != r.ChildTerminalID || native != r.ChildConversationID || cwd != r.ProjectDir {
					t.Fatal("payload lost original binding")
				}
				if mode == "callback-error" {
					return errors.New("join failed")
				}
				return nil
			}
			effects := b.closeChild(r, true)
			if len(effects) != 1 {
				t.Fatal("no launcher must still close owned")
			}
			got := runCloseChild(ctx, b, effects[0])
			wantDone := mode == "exact" || mode == "gone"
			if (got.state == store.EffectDone) != wantDone {
				t.Fatalf("state %s", got.state)
			}
			if !wantDone && mode != "callback-error" && closes != 0 {
				t.Fatal("mismatching or unknown inventory closed")
			}
		})
	}
}

func TestOwnedChildUnavailablePreservesTypedRefusal(t *testing.T) {
	b, ctx := newTestBroker(t)
	const id = "d8300004-0000-4000-8000-000000000004"
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "antigravity"})
	_, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	if refusalCode(err) != "provider_permission_unsupported" {
		t.Fatal(err)
	}
	// Even a complete empty owned source is sufficient to conclude absence;
	// an incomplete source cannot do so, and a replacement native row is left.
	l := store.Linger{Pane: "owned:acp:synthetic", Backend: "owned", Session: "native", Deadline: time.Now()}
	step, _ := lingerStepFor(l, reading{sources: map[string]bool{"owned": true}}, lingerWatch{}, "antigravity")
	if step != lingerGone {
		t.Fatal("complete empty owned source not honored")
	}
}

func TestOwnedChildLifecycleClosesWithoutLauncher(t *testing.T) {
	for _, end := range []State{StateCancelled, StateTimeout, StateSuccess} {
		t.Run(string(end), func(t *testing.T) {
			b, ctx := newTestBroker(t)
			now := time.Now()
			b.Clock = func() time.Time { return now }
			b.ChildLinger = func() time.Duration { return 0 }
			b.lingerStarted = now.Add(-time.Minute)
			owned := ownedFixture(b.Dir)
			r := Record{Protocol: Protocol, ID: "d8300005-0000-4000-8000-000000000005", Assistant: "antigravity", State: StateBriefed,
				ChildBackend: "owned", ChildTerminalID: owned.Row.ID, ChildConversationID: owned.Row.ConversationID,
				ProjectDir: b.Dir, Claims: []string{}, CreatedAt: now}
			if err := b.save(ctx, r, HashSecret("synthetic-secret"), "task.briefed"); err != nil {
				t.Fatal(err)
			}
			b.Reading = func(context.Context) session.Inventory {
				return session.Inventory{Sources: map[string]bool{"owned": true}, Sessions: []session.Session{owned.Row}}
			}
			closed := 0
			b.CloseOwnedChild = func(_ context.Context, id, native, cwd string) error {
				closed++
				if id != owned.Row.ID || native != owned.Row.ConversationID || cwd != b.Dir {
					t.Fatal("lost close identity")
				}
				return nil
			}
			if end == StateCancelled {
				if _, err := b.CancelTask(ctx, r.ID, "Synthetic cancellation", "synthetic-key", CancelCaller{Person: true}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := b.Settle(ctx, r.ID, end, "Synthetic completion", nil); err != nil {
					t.Fatal(err)
				}
				if end == StateSuccess {
					b.closeLingers(ctx, b.read(ctx))
				}
			}
			if closed != 1 {
				t.Fatalf("exact closes=%d", closed)
			}
		})
	}
}

func TestOwnedChildCancelledWhileOpeningNeverReceivesInput(t *testing.T) {
	b, ctx := newTestBroker(t)
	const id = "d8300006-0000-4000-8000-000000000006"
	b.OwnedChildAvailable = func(context.Context) bool { return true }
	closed := 0
	b.CloseOwnedChild = func(context.Context, string, string, string) error { closed++; return nil }
	b.OpenOwnedChild = func(ctx context.Context, _ Record, cwd, _ string, _ squadfiles.Files) (OwnedChild, error) {
		if _, err := b.CancelTask(ctx, id, "Synthetic cancellation while opening", "synthetic-key", CancelCaller{Person: true}); err != nil {
			t.Fatal(err)
		}
		return ownedFixture(cwd), nil
	}
	b.Type = func(context.Context, string, string) error { t.Fatal("cancelled child received input"); return nil }
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "antigravity"})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	if err != nil || out.Record.State != StateCancelled || closed != 1 || out.Record.ChildConversationID == "" {
		t.Fatal("concurrent cancellation lost binding or cleanup", err)
	}
}
