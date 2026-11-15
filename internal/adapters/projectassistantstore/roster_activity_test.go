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
