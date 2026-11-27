package projectassistantstore

import (
	"fmt"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A finished turn keeps its messages and loses its process: streamed chunks,
// tool steps and the receipts of the events it published. The roster journal
// keeps one row per Bot. Before this, every chunk left an event, a receipt
// and a roster row behind for good (store.db passed 1 GiB on 2026-11-26).
func TestAFinishedTurnKeepsItsMessagesAndDropsItsProcess(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "process")
	fence := populate(t, s, b, pa.EffectDelivered)
	commit := func(id string, m app.Mutation) {
		t.Helper()
		bot, err := s.Bot(testContext, b.Key)
		if err != nil {
			t.Fatal(err)
		}
		m.Command, m.ExpectedVersion = command(b.Key, pa.SendOperation, id), bot.Version
		m.Receipt = pa.Receipt{ID: id, State: pa.EffectDelivered}
		if _, err := s.Commit(testContext, m); err != nil {
			t.Fatal(id, err)
		}
	}
	event := func(id string, trace pa.Trace) pa.Event {
		trace.TurnID = "turn"
		return pa.Event{Version: pa.ContractVersion, ID: id, Fence: fence, Kind: pa.TraceUpdated, Trace: &trace}
	}
	const chunks = 40
	for i := 0; i < chunks; i++ {
		id := fmt.Sprintf("chunk-%d", i)
		commit(pa.RuntimeEventCommandPrefix+"turn-"+id, app.Mutation{Events: []pa.Event{event(id, pa.Trace{ID: "segment-1", Category: "agent_message_chunk", Text: fmt.Sprintf("part %d ", i)})}})
	}
	commit(pa.RuntimeEventCommandPrefix+"turn-tool", app.Mutation{Events: []pa.Event{event("tool", pa.Trace{Category: "tool_call", Text: "Read notes.md"})}})
	commit(pa.RuntimeEventCommandPrefix+"turn-delegated", app.Mutation{Events: []pa.Event{event("delegated", pa.Trace{Category: "bot_request", Text: "The other Bot's answer", Complete: true})}})

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	traces := func() int {
		return count(`SELECT count(*) FROM project_assistant_events WHERE machine=? AND bot=? AND json_extract(CAST(data AS TEXT),'$.kind')='trace_updated'`, b.Key.MachineID, b.Key.BotID)
	}
	runtimeReceipts := func() int {
		return count(`SELECT count(*) FROM project_assistant_receipts WHERE machine=? AND bot=? AND command_key LIKE 'runtime-event-%'`, b.Key.MachineID, b.Key.BotID)
	}
	rosterRows := func() int {
		return count(`SELECT count(*) FROM project_assistant_roster_changes WHERE machine=? AND bot=?`, b.Key.MachineID, b.Key.BotID)
	}
	// While the turn runs, its process is all there for the live view.
	if got := traces(); got != chunks+2 {
		t.Fatalf("traces while running = %d, want %d", got, chunks+2)
	}
	if got := runtimeReceipts(); got != chunks+2 {
		t.Fatalf("runtime receipts while running = %d, want %d", got, chunks+2)
	}
	if got := rosterRows(); got != 1 {
		t.Fatalf("roster rows for one Bot after %d commits = %d, want 1", chunks+3, got)
	}

	x, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	p := x.Permissions[0]
	p.State, p.SelectedOptionID = pa.PermissionResolved, "reject"
	turn, conversation := x.Turns[0], x.Conversation
	turn.State, conversation.ActiveTurnID = pa.TurnSucceeded, ""
	message := pa.Message{ID: "reply", Fence: fence, TurnID: turn.ID, Kind: pa.AssistantMessage, Text: "All done", At: time.Now()}
	commit("complete", app.Mutation{Turn: &turn, Conversation: &conversation, Permissions: []pa.PermissionOffer{p}, Messages: []pa.Message{message}})

	if got := traces(); got != 1 {
		t.Fatalf("traces after the turn ended = %d, want only the delegated reply", got)
	}
	if got := runtimeReceipts(); got != 0 {
		t.Fatalf("runtime receipts after the turn ended = %d, want 0", got)
	}
	if got := rosterRows(); got != 1 {
		t.Fatalf("roster rows after the turn ended = %d, want 1", got)
	}
	x, err = s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Messages) != 2 || x.Messages[0].Text != "Hello" || x.Messages[1].Text != "All done" {
		t.Fatalf("messages after the turn ended: %+v", x.Messages)
	}
	// The person's own send keeps its receipt: a retry still deduplicates.
	if r, err := s.Receipt(testContext, command(b.Key, pa.SendOperation, "send")); err != nil || r == nil || r.ID != "sent" {
		t.Fatalf("the person's send receipt: %+v %v", r, err)
	}
	// The newest event is kept, so a reader's frontier is still valid.
	var frontier int64
	if err := db.QueryRow(eventFrontierSQL, b.Key.MachineID).Scan(&frontier); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(testContext, pa.EventQuery{Key: b.Key, AfterSeq: frontier, PageSize: 10}); err != nil {
		t.Fatalf("a reader at the frontier: %v", err)
	}
	// An old cursor reads the turn's lasting events, without its process.
	page, err := s.Read(testContext, pa.EventQuery{Key: b.Key, AfterSeq: 0, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if e.Kind == pa.TraceUpdated && e.Trace.Category != "bot_request" {
			t.Fatalf("an old cursor read process after the turn ended: %+v", e.Trace)
		}
	}
}
