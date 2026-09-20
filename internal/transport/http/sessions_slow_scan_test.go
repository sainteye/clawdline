package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/icon"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// eatsTheDeadline is a process source that spends the whole of the caller's
// budget and then answers. It is the slow terminal scan, made to happen.
type eatsTheDeadline struct{ inv session.Inventory }

func (p eatsTheDeadline) Scan(ctx context.Context) (session.Inventory, error) {
	<-ctx.Done()
	return p.inv, nil
}

// A scan that spends the request's whole deadline must not cost the rows their
// names.
//
// This is the defect as it was measured on 2026-09-20: `/v1/sessions` carried
// an eight-second context, the terminal scan took 8.89 s of it, and the reads
// that follow — what is owed, and this daemon's own records — then ran on a
// context that was already dead. Both failed at once, the answer carried
// `obligation_list_unreadable` and `own_records_unreadable`, and every child
// card fell back to a generic name while the broker's database held the title
// the whole time.
//
// The control is the second half: read on the request's own context, the
// overlay does fail. So this test fails for the reason it names.
func TestASlowScanDoesNotCostARowItsBrokerTitle(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const terminal = "7A5C0000-0000-4000-8000-000000000009"
	const title = "一次盤點餵所有人"
	r := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000001", Kind: "custom", Title: title,
		Assistant: "claude", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		ChildTerminalID: terminal, ChildBackend: "iterm", SpawnedAt: time.Now()}
	body, _ := json.Marshal(r)
	if _, err := st.CreateBrokerTask(context.Background(), store.BrokerRow{ID: r.ID, Project: "/p",
		Assistant: "claude", State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
		t.Fatal(err)
	}

	row := session.Session{ID: terminal, TTY: "ttys009", Assistant: session.AssistantClaude,
		Backend: session.BackendITerm, State: session.StateIdle, PID: 4242, ConversationID: "conv-1"}
	reading := session.Inventory{Sessions: []session.Session{row}, Complete: true, Provenance: "ps",
		Sources: map[string]bool{"ps": true}}
	s := &Server{
		store:     st,
		broker:    &orchestrator.Broker{Store: st, Dir: dir},
		icons:     &icon.Registry{},
		inventory: app.Inventory{Process: eatsTheDeadline{reading}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	inv := s.inventory.Read(ctx)
	if len(inv.Sessions) != 1 {
		t.Fatalf("the scan answered %d rows", len(inv.Sessions))
	}
	if ctx.Err() == nil {
		t.Fatal("the scan was meant to spend the whole deadline and did not")
	}

	// The control: on the context the scan has just emptied, this daemon's own
	// records cannot be read at all. That is the failure being fixed.
	if _, err := s.ownOverlay(ctx, []swiftstore.Live{liveOf(row)}); err == nil {
		t.Fatal("the control did not fail: the records were readable on a dead context")
	}

	snap := s.sessionsPayloadFrom(ctx, inv)
	if len(snap.Sessions) != 1 {
		t.Fatalf("the payload carried %d rows", len(snap.Sessions))
	}
	got := snap.Sessions[0]
	if got.Label != title {
		t.Errorf("the row is named %q; the broker's record says %q", got.Label, title)
	}
	for _, reason := range got.Closeability.Reasons {
		switch reason.Code {
		case "own_records_unreadable", "obligation_list_unreadable":
			t.Errorf("the answer says %s, but nothing about this daemon's own store was unreadable — "+
				"only the scan in front of it was slow", reason.Code)
		}
	}
}
