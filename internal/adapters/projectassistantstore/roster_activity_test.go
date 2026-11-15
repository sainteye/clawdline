package projectassistantstore

import (
	"database/sql"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRosterActivityFollowsDurableCurrentTurn(t *testing.T) {
	db, s, path := testDB(t)
	b := seed(t, s, "roster")
	fence := populate(t, s, b, pa.EffectDelivered)
	check := func(status, activity string) pa.BotSummary {
		t.Helper()
		page, err := s.ListForReader(testContext, pa.ListBots{MachineID: b.Key.MachineID, PageSize: 1}, "reader")
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("list: %+v %v", page, err)
		}
		got := page.Items[0]
		if got.Status != status || got.Activity != activity {
			t.Fatalf("got %s %q; want %s %q", got.Status, got.Activity, status, activity)
		}
		return got
	}
	check("needsInput", "")
	x, _ := s.Export(testContext, b.Key)
	p := x.Permissions[0]
	p.Action = &pa.PermissionAction{Title: "Read notes.md"}
	commit := func(id string, m app.Mutation) {
		t.Helper()
		bot, err := s.Bot(testContext, b.Key)
		if err != nil {
			t.Fatal(err)
		}
		m.Command, m.ExpectedVersion = command(b.Key, pa.SendOperation, id), bot.Version
		m.Receipt = pa.Receipt{ID: id, State: pa.EffectDelivered}
		if _, err := s.Commit(testContext, m); err != nil {
			t.Fatal(err)
		}
	}
	commit("action", app.Mutation{Permissions: []pa.PermissionOffer{p}})
	check("needsInput", "Needs approval: Read notes.md")
	p.State, p.SelectedOptionID = pa.PermissionResolved, "reject"
	trace := pa.Trace{TurnID: "turn", Category: "tool_call", Text: "Read notes.md"}
	event := func(id string, trace pa.Trace) pa.Event {
		return pa.Event{Version: pa.ContractVersion, ID: id, Fence: fence, Kind: pa.TraceUpdated, Trace: &trace}
	}
	commit("tool", app.Mutation{Permissions: []pa.PermissionOffer{p}, Events: []pa.Event{event("tool", trace)}})
	if got := check("working", "Read notes.md"); got.UnreadCount != 1 {
		t.Fatal("activity created an unread message", got.UnreadCount)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	s = New(reopened)
	check("working", "Read notes.md")
	trace.Category, trace.Text = "agent_message_chunk", "partial reply"
	commit("writing", app.Mutation{Events: []pa.Event{event("writing", trace)}})
	check("working", "writing")
	// A rejected event cannot alter the saved activity projection.
	bot, _ := s.Bot(testContext, b.Key)
	bad := event("foreign", trace)
	bad.Fence.Generation++
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "bad"), ExpectedVersion: bot.Version, Receipt: pa.Receipt{ID: "bad", State: pa.EffectDelivered}, Events: []pa.Event{bad}})
	if err == nil {
		t.Fatal("accepted a foreign generation")
	}
	check("working", "writing")
	x, _ = s.Export(testContext, b.Key)
	turn, conversation := x.Turns[0], x.Conversation
	turn.State, conversation.ActiveTurnID = pa.TurnSucceeded, ""
	message := pa.Message{ID: "reply", Fence: fence, TurnID: turn.ID, Kind: pa.AssistantMessage, Text: "All done", At: time.Now()}
	commit("complete", app.Mutation{Turn: &turn, Conversation: &conversation, Messages: []pa.Message{message}})
	if got := check("idle", ""); got.LatestMessage != "All done" {
		t.Fatal("completion lost the latest reply", got)
	}
}

func TestRosterFailureUsesAdmissionOrderAndKeepsUnknownDistinct(t *testing.T) {
	b := pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: "b"}}
	a := aggregate{Bot: b, Turns: map[string]pa.Turn{
		"z-old": {ID: "z-old", State: pa.TurnFailed, Failure: &pa.Failure{Code: pa.AdapterNotFound}},
		"a-new": {ID: "a-new", State: pa.TurnSucceeded},
	}, Messages: []pa.Message{{Kind: pa.UserMessage, TurnID: "z-old"}, {Kind: pa.UserMessage, TurnID: "a-new"}}}
	got := summary(a, "")
	if got.Status != "idle" || got.Failure != nil {
		t.Fatal("an older failure overrode the new completed turn", got)
	}
	turn := a.Turns["a-new"]
	turn.State, turn.Failure = pa.TurnUnknown, nil
	a.Turns[turn.ID] = turn
	got = summary(a, "")
	if got.Status != "error" || got.Failure == nil || got.Failure.Code != pa.ExecutionEffectUnknown {
		t.Fatal("unknown effect was hidden or labelled as a known failure", got)
	}
	turn.State, turn.Failure = pa.TurnFailed, &pa.Failure{Code: pa.AdapterNotFound}
	a.Turns[turn.ID] = turn
	got = summary(a, "")
	if got.Status != "error" || got.Failure.Code != pa.AdapterNotFound {
		t.Fatal("failure code was lost", got)
	}
}

func TestRosterFollowsResumedNoticeAdmissionInsteadOfAcknowledgedFailure(t *testing.T) {
	old := pa.Turn{ID: "z-old", MessageID: "old-input", State: pa.TurnFailed, Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "acknowledged"}}
	resumed := pa.Turn{ID: "a-resumed", MessageID: "resume-input", State: pa.TurnSucceeded}
	a := aggregate{Turns: map[string]pa.Turn{old.ID: old, resumed.ID: resumed}, Messages: []pa.Message{
		{ID: old.MessageID, Kind: pa.UserMessage, TurnID: old.ID},
		{ID: resumed.MessageID, Kind: pa.NoticeMessage, TurnID: resumed.ID},
		{ID: "reply", Kind: pa.AssistantMessage, TurnID: resumed.ID, Text: "Completed after restart"},
	}}
	if got := summary(a, ""); got.Status != "idle" || got.Failure != nil || got.LatestMessage != "Completed after restart" {
		t.Fatalf("a completed continuation still shows the older unknown failure: %+v", got)
	}
	// A later log line from an old turn is not another admission.
	a.Messages = append(a.Messages, pa.Message{ID: "late-old-log", Kind: pa.NoticeMessage, TurnID: old.ID})
	if got := summary(a, ""); got.Status != "idle" || got.Failure != nil {
		t.Fatalf("a log line replaced the admitted continuation: %+v", got)
	}
	// Unknown in the current continuation must still be shown as unknown.
	resumed.State = pa.TurnUnknown
	a.Turns[resumed.ID] = resumed
	if got := summary(a, ""); got.Status != "error" || got.Failure == nil || got.Failure.Code != pa.ExecutionEffectUnknown {
		t.Fatalf("current unknown outcome was hidden: %+v", got)
	}
	// A newer human turn wins over a notice's older error, independent of IDs.
	newer := pa.Turn{ID: "0-newest", MessageID: "newest-input", State: pa.TurnSucceeded}
	a.Turns[newer.ID] = newer
	a.Messages = append(a.Messages, pa.Message{ID: newer.MessageID, Kind: pa.UserMessage, TurnID: newer.ID})
	if got := summary(a, ""); got.Status != "idle" || got.Failure != nil {
		t.Fatalf("old notice failure replaced a newer human turn: %+v", got)
	}
}

func TestRosterAndReadViewAgreeAfterDurableContinuation(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "continuation")
	f := populate(t, s, b, pa.EffectUnknown)
	x, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	commit := func(id string, mutation app.Mutation) {
		t.Helper()
		bot, err := s.Bot(testContext, b.Key)
		if err != nil {
			t.Fatal(err)
		}
		mutation.Command, mutation.ExpectedVersion = command(b.Key, pa.SendOperation, id), bot.Version
		mutation.Receipt = pa.Receipt{ID: id, State: pa.EffectDelivered}
		if _, err = s.Commit(testContext, mutation); err != nil {
			t.Fatal(err)
		}
	}
	old, conversation, permission := x.Turns[0], x.Conversation, x.Permissions[0]
	permission.State, permission.SelectedOptionID = pa.PermissionResolved, "reject"
	old.State, old.Failure = pa.TurnUnknown, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "restart"}
	conversation.ActiveTurnID = ""
	commit("interrupted", app.Mutation{Turn: &old, Conversation: &conversation, Permissions: []pa.PermissionOffer{permission}})
	views := newTestViews(t, db)
	interrupted := rosterRead(t, views, rosterQuery())
	if len(interrupted.Frame.Changes) != 1 || interrupted.Frame.Changes[0].Summary == nil || interrupted.Frame.Changes[0].Summary.Status != "error" || interrupted.Frame.Changes[0].Summary.Failure == nil || interrupted.Frame.Changes[0].Summary.Failure.Code != pa.ExecutionEffectUnknown {
		t.Fatal("bounded roster hid the current unknown outcome", interrupted.Frame)
	}
	old.State, old.Failure = pa.TurnFailed, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "acknowledged"}
	commit("acknowledged", app.Mutation{Turn: &old})
	resume := pa.Turn{ID: "resumed-turn", Fence: f, MessageID: "resume-notice", State: pa.TurnQueued, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "resume"}
	notice := pa.Message{ID: resume.MessageID, Fence: f, TurnID: resume.ID, Kind: pa.NoticeMessage, Text: "Continue the interrupted task", At: time.Now()}
	commit("resume", app.Mutation{Turn: &resume, Messages: []pa.Message{notice}})
	conversation, err = s.Conversation(testContext, b.Key, b.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	resume.State, conversation.ActiveTurnID = pa.TurnRunning, resume.ID
	commit("resume-start", app.Mutation{Turn: &resume, Conversation: &conversation})
	resume.State, conversation.ActiveTurnID = pa.TurnSucceeded, ""
	reply := pa.Message{ID: "resume-reply", Fence: f, TurnID: resume.ID, Kind: pa.AssistantMessage, Text: "Completed continuation", At: time.Now()}
	commit("resume-finish", app.Mutation{Turn: &resume, Conversation: &conversation, Messages: []pa.Message{reply}})
	page, err := s.ListForReader(testContext, pa.ListBots{MachineID: b.Key.MachineID, PageSize: 1}, "reader")
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != "idle" || page.Items[0].Failure != nil || page.Items[0].LatestMessage != reply.Text {
		t.Fatalf("roster disagrees with completed continuation: %+v %v", page, err)
	}
	view := readView(t, newTestViews(t, db), viewQuery(b))
	if view.Snapshot == nil || view.Snapshot.LastTurn == nil || view.Snapshot.LastTurn.ID != resume.ID || view.Snapshot.LastTurn.State != pa.TurnSucceeded {
		t.Fatalf("read view selected another turn: %+v", view.Snapshot)
	}
	completed := rosterRead(t, views, rosterQuery())
	if len(completed.Frame.Changes) != 1 || completed.Frame.Changes[0].Summary == nil || completed.Frame.Changes[0].Summary.Status != "idle" || completed.Frame.Changes[0].Summary.Failure != nil || completed.Frame.Changes[0].Summary.LatestMessage != reply.Text {
		t.Fatal("bounded roster disagrees with completed continuation", completed.Frame)
	}
	// Selecting the new state changes no historical uncertainty or receipt.
	prior, err := s.Receipt(testContext, command(b.Key, pa.SendOperation, "send"))
	if err != nil || prior == nil || prior.State != pa.EffectUnknown {
		t.Fatal("historical unknown receipt was changed", prior, err)
	}
	final, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range final.Turns {
		if turn.ID == old.ID && (turn.State != pa.TurnFailed || turn.Failure == nil || turn.Failure.Field != "acknowledged") {
			t.Fatal("old turn was rewritten", turn)
		}
	}
}
