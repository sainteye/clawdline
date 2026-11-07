package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The Console is real; deterministic provider ports avoid model/API usage.
// Run after the web build with CLAWDLINE_ROTATION_BROWSER=1. The browser press
// reaches a synthetic source assistant, then this real broker writes and reads
// the package, binds its receiver and moves an actual isolated Board item.
func TestRotationConsoleUsesRealMilestoneTransfer(t *testing.T) {
	if os.Getenv("CLAWDLINE_ROTATION_BROWSER") != "1" {
		t.Skip("opt-in Chrome integration; build web first")
	}
	b, ctx := newTestBroker(t)
	project := t.TempDir()
	source := "10000000-0000-4000-8000-000000000001"
	receiver := "10000000-0000-4000-8000-000000000002"
	sourcePane, receiverPane := "%"+"801", "ttys008"
	now := time.Now()
	itemID := NewUUID()
	if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateItem(work.ItemV2{ID: itemID, ProjectID: "rotation-fixture", ProjectPath: project,
			Kind: work.KindFeature, Title: "換手測試", Phase: work.PhaseImplementing, DeploymentPolicy: work.DeployAgentDecides,
			OwnerSession: source, CreatedBy: "person", CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}, "person", "{}"); err != nil {
			return err
		}
		return tx.CreateAssignment(work.AssignmentV2{ID: NewUUID(), WorkID: itemID, Mode: "existing_session", SessionID: source,
			TerminalID: sourcePane, Assistant: "codex", State: "active", HumanActor: "person", CreatedAt: now, UpdatedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	b.Launcher = &openingLauncher{pane: receiverPane}
	readPackage := false
	live := []session.Session{{ID: sourcePane, Assistant: session.AssistantCodex, ConversationID: source, State: session.StateIdle},
		{ID: receiverPane, Assistant: session.AssistantCodex, State: session.StateIdle}}
	b.Live = func(context.Context) []session.Session { return live }
	summary := "## Goal\nContinue the rotation fixture.\n## Verified decisions\nKeep provider files and the source Session.\n## Blockers\nNone.\n## Evidence\n" + itemID + "\n## Next step\nRead the transferred item and continue from its current phase.\n"
	id := NewUUID()
	b.Type = func(_ context.Context, terminal, text string) error {
		if terminal != receiverPane {
			return nil
		}
		packagePath := filepath.Join(b.HandoffRoot(), id, "handoff.md")
		if !strings.Contains(text, packagePath) {
			return fmt.Errorf("receiver was not pointed at its package")
		}
		data, err := os.ReadFile(packagePath)
		if err != nil || string(data) != summary {
			return fmt.Errorf("receiver did not read the exact state: %v", err)
		}
		obligations, err := os.ReadFile(filepath.Join(filepath.Dir(packagePath), MilestoneObligationsFile))
		if err != nil || !strings.Contains(string(obligations), itemID) {
			return fmt.Errorf("receiver lost its Board obligation: %v", err)
		}
		readPackage = true
		live[1] = session.Session{ID: receiverPane, Assistant: session.AssistantCodex, ConversationID: receiver,
			State: session.StateIdle, Activity: session.Activity{At: time.Now()}}
		return nil
	}
	var backendErr error
	called := false
	var mu sync.Mutex
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fail := func(err error) { backendErr = err; http.Error(w, err.Error(), 500) }
		if called {
			fail(fmt.Errorf("duplicate source handoff"))
			return
		}
		called = true
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8192))
		if err != nil || !strings.Contains(string(raw), "clawdline guide landing") {
			fail(fmt.Errorf("missing source request"))
			return
		}
		yes := true
		h, _, err := b.OpenHandoff(r.Context(), HandoffRequest{ID: id, ProjectDir: project, FromSession: source, Assistant: "codex", Plain: &yes, Milestone: &yes, Summary: &summary})
		if err != nil || h.State != HandoffDelivered || !readPackage {
			fail(fmt.Errorf("handoff not delivered: %+v %v", h, err))
			return
		}
		sessions := map[string]session.Session{}
		for _, s := range live {
			sessions[s.ID] = s
		}
		if b.tendHandoffBoards(ctx, reading{sessions: sessions}) != 1 {
			fail(fmt.Errorf("receiver did not acquire its work"))
			return
		}
		moved, err := b.Store.WorkV2Item(ctx, itemID)
		if err != nil || moved.OwnerSession != receiver {
			fail(fmt.Errorf("wrong Board owner: %v", err))
			return
		}
		h, err = b.HandoffByID(ctx, id)
		if err != nil || h.Receiver != receiver {
			fail(fmt.Errorf("receiver binding missing: %v", err))
			return
		}
		at := time.Now()
		snapshot := ownrecords.Snapshot{}
		snapshot.Handoffs = []ownrecords.Handoff{{ID: id, State: "delivered", FromSession: &source}}
		closeability := snapshot.Closeability(ownrecords.CloseInput{Live: ownrecords.Live{TerminalID: sourcePane, Assistant: "codex", ConversationID: source, PID: 1, ProcessStart: now},
			TerminalState: "idle", Bound: true, Matches: 1, InventoryComplete: true, InventoryObservedAt: at, Now: at})
		if closeability.State != "safe" {
			fail(fmt.Errorf("source closeability: %+v", closeability))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"handoff": h.ID, "receiver": h.Receiver, "board_transferred": true, "package_read": readPackage, "source_closeability": closeability.State, "source_kept": true})
	}))
	defer bridge.Close()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "node", "--experimental-strip-types", "--test", "--test-name-pattern=rotation reminder", filepath.Join(root, "web/console/src/session/address.e2e.ts"))
	command.Dir = root
	command.Env = append(os.Environ(), "CLAWDLINE_ROTATION_BACKEND="+bridge.URL)
	output, err := command.CombinedOutput()
	mu.Lock()
	defer mu.Unlock()
	if err != nil || backendErr != nil || !called {
		t.Fatalf("browser: %v; broker: %v; called: %v\n%s", err, backendErr, called, output)
	}
	t.Log(string(output))
}
