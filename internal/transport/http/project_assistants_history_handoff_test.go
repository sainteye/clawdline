package http

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func TestReadOnlyMaintenanceHistoryIsComposedByServer(t *testing.T) {
	f := newPAServerFixture(t, true)
	marker := filepath.Join(f.s.cfg.Dir, "project-assistant-native-maintenance.initialized")
	if err := f.s.StartProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("default-off source created a writer marker", err)
	}
	if err := os.WriteFile(marker, []byte("native-maintenance-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	enable := f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`)
	paAssertHTTPFailure(t, enable, http.StatusInternalServerError, pa.ExecutionEffectUnknown)
	conf, err := f.s.settingsFile().Read()
	if err != nil || conf.ProjectAssistantsEnabled() || f.providerCalls.Load() != 0 {
		t.Fatal("failed reader readiness enabled the source or opened a provider", err)
	}
}

func establishedHandoffJournal(t *testing.T, state string) *store.NativeMaintenanceJournal {
	t.Helper()
	dir := filepath.Join(state, "project-assistant-native-maintenance")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "journal.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	j, err := store.NewNativeMaintenanceJournal(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(state, "project-assistant-native-maintenance.initialized"), []byte("native-maintenance-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestHandoffHistoryGateBeforeFreshOpen(t *testing.T) {
	for _, state := range []string{"clean", "pending", "unknown", "failed", "cancelled", "completed"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			f, probe := newHandoffSource(t)
			repository := store.New(f.s.store.ProjectAssistantDatabase())
			before, err := repository.Export(ctx, f.key)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := repository.Binding(ctx, f.key, before.Conversation.BindingID)
			if err != nil || binding.NativeSessionID == "" {
				t.Fatal("seed did not establish native history", binding, err)
			}
			journal := establishedHandoffJournal(t, f.s.cfg.Dir)
			intent := app.NativeMaintenanceIntent{ID: "protected-handoff", Binding: binding,
				PrincipalID: "synthetic-person", ReaderID: "synthetic-reader", AuthorizationRevision: "synthetic-grant",
				CLIVersion: "fixture", AdapterVersion: "fixture", Evidence: "isolated-http"}
			if state != "clean" {
				// A different native ID may share the selected ACP partition.
				intent.Binding.NativeSessionID = "previous-native-id"
				if _, added, err := journal.Reserve(ctx, intent); !added || err != nil {
					t.Fatal("could not reserve protected native history", added, err)
				}
				if state != "pending" {
					terminal := app.NativeMaintenanceRecord{Intent: intent, State: app.NativeCompactionState(state)}
					if terminal.State != app.NativeCompactionUnknown {
						terminal.NativeID = "matched-terminal"
					}
					if got, err := journal.Resolve(ctx, terminal); err != nil || got != terminal {
						t.Fatal("terminal evidence changed", got, err)
					}
				}
			}
			card := handoffPrepare(t, f, "prepare-history")
			send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "adopt-history"), Fence: card.Fence,
				Text: "Explicit next input", Handoff: &card.Card.Ref}
			rec := handoffSend(t, f, f.local, send)
			paServerOK(t, rec)
			var accepted contract.ProjectAssistantReceiptResponse
			if err = json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
				t.Fatal(err)
			}
			after := fallbackHTTPWait(t, f)
			turn, err := repository.Turn(ctx, f.key, accepted.Receipt.ResourceID)
			if err != nil {
				t.Fatal(err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if state == "clean" || state == "completed" {
				if turn.State != pa.TurnSucceeded || len(probe.opens) != 2 || probe.opens[1] || len(probe.opening) != 2 || probe.opening[1] == "" {
					t.Fatal("clean handoff did not open fresh with quoted history", turn, probe.opens, probe.opening)
				}
			} else if turn.Failure == nil || turn.Failure.Code != pa.ExecutionEffectUnknown || len(probe.opens) != 1 || len(probe.prompts) != 1 {
				t.Fatal("protected handoff crossed the native Open boundary", turn, probe.opens, probe.prompts)
			}
			if after.Conversation.ID != before.Conversation.ID || len(after.Messages) < len(before.Messages) {
				t.Fatal("handoff discarded original displayed history")
			}
			for i, message := range before.Messages {
				if after.Messages[i].ID != message.ID || after.Messages[i].Text != message.Text {
					t.Fatal("handoff rewrote original displayed history")
				}
			}
		})
	}
}

type handoffHistoryProvider struct {
	app.Provider
	before func()
}

func (p handoffHistoryProvider) Capabilities(ctx context.Context, bot pa.Bot) (pa.Capabilities, error) {
	p.before()
	return p.Provider.Capabilities(ctx, bot)
}

func TestHandoffHistoryRechecksViewerBeforeOpen(t *testing.T) {
	f := newPAServerFixture(t, true)
	probe := &handoffSourceProbe{}
	var armed atomic.Bool
	revoked := make(chan error, 1)
	matrix := &paMatrix{paServerFixture: f, pins: map[string]adaptercloud.PinnedDevice{}, privs: map[string]ed25519.PrivateKey{},
		bridge: cloudops.Bridge{MachineID: f.key.MachineID, Router: cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}, AllowCommands: func() bool { return true }}}
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		p, err := probe.provider()
		return handoffHistoryProvider{Provider: p, before: func() {
			if !armed.Load() {
				return
			}
			// Admission also discovers capabilities. Wait for the durable input
			// rather than assuming a particular number of discovery calls.
			x, readErr := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
			if readErr != nil {
				if armed.Swap(false) {
					revoked <- readErr
				}
				return
			}
			for _, turn := range x.Turns {
				if turn.IdempotencyKey == "adopt-revocation" && armed.Swap(false) {
					_, err := matrix.pinned().Revoke("handoff-viewer", time.Now())
					revoked <- err
				}
			}
		}}, err
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "seed-revocation"))
	waitInputTurn(t, f)
	establishedHandoffJournal(t, f.s.cfg.Dir)
	card := handoffPrepare(t, f, "prepare-revocation")
	matrix.pair(t, "handoff-viewer", adaptercloud.ScopeOperate, false)
	armed.Store(true)
	// Revoke after admission while capability discovery is still before Open.
	// Publication may report unknown; it must never replay or open the Agent.
	matrix.word(t, "handoff-viewer", "send", contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "adopt-revocation"),
		Fence: card.Fence, Text: "Explicit remote input", Handoff: &card.Card.Ref})
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("admitted handoff never checked capabilities")
	}
	after := fallbackHTTPWait(t, f)
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(after.Turns) != 2 || len(probe.opens) != 1 || len(probe.prompts) != 1 {
		t.Fatal("revoked handoff opened or sent to the native Agent", len(after.Turns), probe.opens, probe.prompts)
	}
}
