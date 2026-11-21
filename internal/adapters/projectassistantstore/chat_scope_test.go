package projectassistantstore

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Restored synthetic facts model data written before the read projection was
// introduced. No history is rewritten and no provider is involved.
func TestOwnChatExcludesStoredRoomTranscripts(t *testing.T) {
	e, _, current := viewFixture(t, 6, 1, false)
	e.Turns[0].Room = &pa.RoomTicket{Group: pa.BotKey{MachineID: e.Bot.Key.MachineID, BotID: "room-a"}, Member: e.Bot.Key, Epoch: 1, JoinedRevision: 1}
	e.Turns[0].Origin = pa.HostNoticeOrigin
	e.Messages[0].Kind = pa.NoticeMessage
	for i := range e.Messages {
		e.Messages[i].TurnID = e.Turns[0].ID
		if i > 0 {
			e.Messages[i].Kind = pa.AssistantMessage
		}
		e.Events[i].Message = &e.Messages[i]
	}
	trace := pa.Trace{TurnID: e.Turns[0].ID, Category: "tool_call", Text: "room-only tool"}
	e.Events = append(e.Events, pa.Event{Version: pa.ContractVersion, ID: "room-trace", Seq: 7, Fence: current, Kind: pa.TraceUpdated, At: e.Bot.CreatedAt, Trace: &trace})
	db, s, v, path := restoreViews(t, e)
	q := viewQuery(e.Bot)
	snapshot := readView(t, v, q)
	if len(snapshot.Items) != 0 || snapshot.Head.Conversation.LastMessageSeq != 0 || snapshot.Snapshot.LastTurn != nil || snapshot.Head.TraceCategoryCount != 0 {
		t.Fatalf("room transcript reached own chat: items=%d lastSeq=%d lastTurn=%+v traces=%d", len(snapshot.Items), snapshot.Head.Conversation.LastMessageSeq, snapshot.Snapshot.LastTurn, snapshot.Head.TraceCategoryCount)
	}
	history, err := s.Messages(testContext, app.HistoryQuery{Key: e.Bot.Key, ConversationID: e.Conversation.ID, PageSize: 1})
	if err != nil || len(history.Items) != 0 || history.HasMore {
		t.Fatalf("legacy history: %+v %v", history, err)
	}
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewEvents, snapshot.SourceEpoch
	events := readView(t, v, q)
	if len(events.Items) != 0 {
		t.Fatalf("room events reached own chat: %+v", events.Items)
	}
	page, err := s.ListForReader(testContext, pa.ListBots{MachineID: e.Bot.Key.MachineID, PageSize: 1}, "reader")
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	row := page.Items[0]
	if row.LatestMessage != "" || row.UnreadCount != 0 || row.Status != "idle" || !row.ActivityAt.Equal(e.Bot.CreatedAt) {
		t.Fatalf("legacy roster leaked room: %+v", row)
	}
	roster, err := v.ReadRoster(testContext, app.RosterQuery{Request: pa.RosterRequest{MachineID: e.Bot.Key.MachineID}, AuthorizationBinding: "synthetic", ReaderID: "reader", ByteLimit: q.ByteLimit})
	if err != nil || len(roster.Frame.Changes) != 1 {
		t.Fatal(roster, err)
	}
	if got := roster.Frame.Changes[0].Summary; got.LatestMessage != "" || got.UnreadCount != 0 || got.Status != "idle" || !got.ActivityAt.Equal(e.Bot.CreatedAt) {
		t.Fatalf("live roster leaked room: %+v", got)
	}
	// Reopen the actual SQLite file; both the original facts and their provenance survive.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again := readView(t, newTestViews(t, reopened), viewQuery(e.Bot))
	if len(again.Items) != 0 {
		t.Fatal("reopen exposed room history")
	}
	raw, err := New(reopened).Export(testContext, e.Bot.Key)
	bound := false
	for _, binding := range raw.Bindings {
		if binding.Fence == current {
			bound = true
		}
	}
	if err != nil || len(raw.Messages) != 6 || raw.Turns[0].Room == nil || !bound {
		t.Fatal("diagnostic archive or binding was altered", err)
	}
}

func TestOwnChatPagesAcrossInterleavedRooms(t *testing.T) {
	e, _, current := viewFixture(t, 9, 0, false)
	e.Turns = nil
	for i := range e.Messages {
		m := &e.Messages[i]
		m.At = e.Bot.CreatedAt.Add(time.Duration(i+1) * time.Minute)
		m.Kind, m.TurnID = pa.NoticeMessage, m.ID+"-turn"
		turn := pa.Turn{ID: m.TurnID, MessageID: m.ID, Fence: current, State: pa.TurnSucceeded, Origin: pa.HostNoticeOrigin, Instructions: e.Bot.Instructions, MemoryRevision: 1, IdempotencyKey: m.ID}
		if i%3 != 1 {
			turn.Room = &pa.RoomTicket{Group: pa.BotKey{MachineID: e.Bot.Key.MachineID, BotID: []string{"room-a", "room-b"}[i%2]}, Member: e.Bot.Key, Epoch: 1, JoinedRevision: 1}
		}
		e.Turns = append(e.Turns, turn)
		e.Events[i].Message = m
	}
	db, s, v, _ := restoreViews(t, e)
	_ = db
	q := viewQuery(e.Bot)
	q.Request.PageSize = 1
	var got []int64
	for {
		page := readView(t, v, q)
		for _, item := range page.Items {
			got = append(got, item.Message.Seq)
		}
		if !page.HasMore {
			break
		}
		q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, page.SourceEpoch, page.Items[len(page.Items)-1].AfterCursor
	}
	bytes, _ := json.Marshal(got)
	if string(bytes) != "[8,5,2]" {
		t.Fatalf("pagination=%s", bytes)
	}
	h, err := s.Messages(testContext, app.HistoryQuery{Key: e.Bot.Key, ConversationID: e.Conversation.ID, PageSize: 2})
	if err != nil || len(h.Items) != 2 || h.Items[0].Seq != 8 || h.Items[1].Seq != 5 || !h.HasMore || h.NextBeforeSeq != 5 {
		t.Fatal(h, err)
	}
	// Notices without a Room ticket remain legitimate own-chat messages: they
	// are the latest activity, but narration is never unread.
	rows, err := s.ListForReader(testContext, pa.ListBots{MachineID: e.Bot.Key.MachineID, PageSize: 1}, "reader")
	if err != nil || rows.Items[0].UnreadCount != 0 || rows.Items[0].LatestMessage != e.Messages[7].Text || !rows.Items[0].ActivityAt.Equal(e.Messages[7].At) {
		t.Fatal(rows, err)
	}
	roster := rosterRead(t, v, rosterQuery())
	if got := roster.Frame.Changes[0].Summary; got.LatestMessage != e.Messages[7].Text || !got.ActivityAt.Equal(e.Messages[7].At) {
		t.Fatalf("later room message changed direct-chat sorting: %+v", got)
	}
}

func TestOwnChatMetadataReplayDoesNotAcquireFutureActivity(t *testing.T) {
	e, _, current := viewFixture(t, 3, 0, false)
	// This event predates both the room and the later direct reply. Its zero
	// activity is a metadata patch, not an empty current roster projection.
	metadata := pa.BotSummary{Bot: e.Bot, Version: e.Bot.Version, Source: pa.SourceStatus{MachineID: e.Bot.Key.MachineID}}
	metadata.Bot.Name = "Name at the event"
	metadata.Bot.Hidden = true
	e.Events = append([]pa.Event{{Version: pa.ContractVersion, ID: "metadata-before-messages", Seq: 1, Fence: current, Kind: pa.BotUpdated, At: e.Bot.CreatedAt, Bot: &metadata}}, e.Events...)
	room := e.Turns[0]
	room.ID, room.MessageID, room.Origin = "room", e.Messages[1].ID, pa.HostNoticeOrigin
	room.Room = &pa.RoomTicket{Group: pa.BotKey{MachineID: e.Bot.Key.MachineID, BotID: "room-bot"}, Member: e.Bot.Key, Epoch: 1, JoinedRevision: 1}
	e.Turns = append(e.Turns, room)
	e.Messages[1].TurnID, e.Messages[1].Kind = room.ID, pa.NoticeMessage
	for i := range e.Messages {
		e.Messages[i].At = e.Bot.CreatedAt.Add(time.Duration(i+1) * time.Minute)
		e.Events[i+1].Seq = int64(i + 2)
		e.Events[i+1].At = e.Messages[i].At
		e.Events[i+1].Message = &e.Messages[i]
	}
	_, s, v, _ := restoreViews(t, e)
	check := func(events []pa.Event) {
		t.Helper()
		if len(events) != 3 || events[0].ID != "metadata-before-messages" || !reflect.DeepEqual(events[0].Bot, &metadata) || events[1].Message.ID != e.Messages[0].ID || events[2].Message.ID != e.Messages[2].ID {
			t.Fatalf("metadata or transcript changed during replay: %+v", events)
		}
	}
	legacy, err := s.Read(testContext, pa.EventQuery{Key: e.Bot.Key, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	check(legacy.Items)
	q := viewQuery(e.Bot)
	snapshot := readView(t, v, q)
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewEvents, snapshot.SourceEpoch
	page := readView(t, v, q)
	var events []pa.Event
	for _, row := range page.Items {
		events = append(events, *row.Event)
	}
	check(events)
	q.Request.AfterSeq = page.NextSeq
	if next := readView(t, v, q); len(next.Items) != 0 || next.NextSeq != page.NextSeq {
		t.Fatal("metadata replay changed the resume frontier", next)
	}
	rows, err := s.ListForReader(testContext, pa.ListBots{MachineID: e.Bot.Key.MachineID, PageSize: 1}, "reader")
	if err != nil || rows.Items[0].LatestMessage != e.Messages[2].Text || !rows.Items[0].ActivityAt.Equal(e.Messages[2].At) {
		t.Fatal("current roster lost its direct reply", rows, err)
	}
	if got := rosterRead(t, v, rosterQuery()).Frame.Changes[0].Summary; got.LatestMessage != e.Messages[2].Text || !got.ActivityAt.Equal(e.Messages[2].At) {
		t.Fatal("live roster lost its direct reply", got)
	}
}

func TestRoomTranscriptOutcomesDoNotCreateDirectMessages(t *testing.T) {
	for _, outcome := range []string{"plain", "pass", "failed", "unknown", "late"} {
		t.Run(outcome, func(t *testing.T) {
			e, _, _ := viewFixture(t, 2, 0, false)
			e.Turns[0].Origin = pa.HostNoticeOrigin
			e.Turns[0].Room = &pa.RoomTicket{Group: pa.BotKey{MachineID: e.Bot.Key.MachineID, BotID: "room"}, Member: e.Bot.Key, Epoch: 1, JoinedRevision: 1, Late: outcome == "late"}
			e.Messages[0].Kind = pa.NoticeMessage
			e.Messages[1].TurnID = e.Turns[0].ID
			if outcome == "pass" {
				e.Messages[1].Text = pa.RoomPass
			}
			if outcome == "failed" {
				e.Turns[0].State = pa.TurnFailed
				e.Turns[0].Failure = &pa.Failure{Code: pa.Offline}
			}
			if outcome == "unknown" {
				e.Turns[0].State = pa.TurnUnknown
			}
			for i := range e.Events {
				e.Events[i].Message = &e.Messages[i]
			}
			_, s, v, _ := restoreViews(t, e)
			page := readView(t, v, viewQuery(e.Bot))
			if len(page.Items) != 0 || page.Snapshot.LastTurn != nil {
				t.Fatal("room outcome entered own chat", outcome, page)
			}
			rows, err := s.ListForReader(testContext, pa.ListBots{MachineID: e.Bot.Key.MachineID, PageSize: 1}, "reader")
			if err != nil || rows.Items[0].UnreadCount != 0 || rows.Items[0].Status != "idle" {
				t.Fatal("room outcome changed own roster", outcome, rows, err)
			}
			raw, err := s.Export(testContext, e.Bot.Key)
			if err != nil || len(raw.Messages) != 2 || raw.Messages[1].Text != e.Messages[1].Text || raw.Turns[0].State != e.Turns[0].State {
				t.Fatal("room diagnostics changed", outcome, err)
			}
		})
	}
}

func TestRoomExecutionControlsRemainAvailable(t *testing.T) {
	e, _, current := viewFixture(t, 18, 0, true)
	for i := range e.Turns {
		if e.Turns[i].ID != "t18" {
			continue
		}
		e.Turns[i].Origin = pa.HostNoticeOrigin
		e.Turns[i].Room = &pa.RoomTicket{Group: pa.BotKey{MachineID: e.Bot.Key.MachineID, BotID: "room"}, Member: e.Bot.Key, Epoch: 1, JoinedRevision: 1}
	}
	e.Messages[17].Kind = pa.NoticeMessage
	e.Events[17].Message = &e.Messages[17]
	db, s, v, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	snapshot := readView(t, v, q)
	if snapshot.Head.Conversation.ActiveTurnID != "t18" || snapshot.Head.PendingPermissionCount != int64(len(e.Permissions)) || snapshot.Head.UnknownTurnCount != 17 || snapshot.Head.TraceCategoryCount != 0 {
		t.Fatalf("execution controls changed: %+v", snapshot.Head)
	}
	if len(snapshot.Snapshot.PendingTurns) != 0 || len(snapshot.Snapshot.QueuedMessages) != 0 {
		t.Fatal("room prompt escaped through queued messages")
	}
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewPermissions, snapshot.SourceEpoch
	permissions := readView(t, v, q)
	if len(permissions.Items) == 0 {
		t.Fatal("room approvals disappeared")
	}
	q.Request.Mode, q.Request.PageSize, q.Request.Cursor = pa.ReadViewDetail, 1, permissions.Items[0].PermissionSummary.DetailCursor
	if page := readView(t, v, q); page.Items[0].Permission == nil {
		t.Fatal("room approval detail disappeared")
	}
	// Even a valid signed trace reference may not cross the transcript scope.
	q.Request.Cursor, _ = v.cursor(q, snapshot.Head, viewPosition{Collection: "trace", Anchor: 19, ID: viewTraceID("t18", "agent_message_chunk")})
	if _, err := v.Read(testContext, q); pa.Code(err) != pa.CursorExpired {
		t.Fatalf("room trace detail: %v", err)
	}
	legacy, err := s.Read(testContext, pa.EventQuery{Key: e.Bot.Key, PageSize: 1, AfterSeq: 17})
	if err != nil || len(legacy.Items) != 0 {
		t.Fatal("room legacy events", legacy, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM project_assistant_events WHERE machine=? AND bot=?`, current.Key.MachineID, current.Key.BotID).Scan(&count); err != nil || count != 19 {
		t.Fatal("raw diagnostic events changed", count, err)
	}
}

func TestRoomActivityPreservesTheOwnChatOutcome(t *testing.T) {
	a := aggregate{Conversation: pa.Conversation{ActiveTurnID: "room"}, Turns: map[string]pa.Turn{
		"direct": {ID: "direct", MessageID: "dm", State: pa.TurnFailed, Failure: &pa.Failure{Code: pa.AdapterNotFound}},
		"room":   {ID: "room", MessageID: "prompt", State: pa.TurnRunning, Room: &pa.RoomTicket{}},
	}, Messages: []pa.Message{{ID: "dm", TurnID: "direct", Kind: pa.UserMessage}, {ID: "prompt", TurnID: "room", Kind: pa.NoticeMessage}}}
	row := summary(a, "")
	if row.Status != "error" || row.Failure == nil || row.Failure.Code != pa.AdapterNotFound {
		t.Fatal("room execution replaced own-chat outcome", row)
	}
	room := a.Turns["room"]
	room.State = pa.TurnFailed
	room.Failure = &pa.Failure{Code: pa.Offline}
	a.Turns["room"] = room
	a.Conversation.ActiveTurnID = ""
	row = summary(a, "")
	if row.Status != "error" || row.Failure == nil || row.Failure.Code != pa.AdapterNotFound {
		t.Fatal("room failure replaced own-chat outcome", row)
	}
}

func TestHistoricalOwnChatTraceRequiresItsExactStoredBinding(t *testing.T) {
	e, old, own := viewFixture(t, 1, 0, false)
	next := own
	next.BindingID, next.Generation = "later-room-generation", 3
	e.Conversation.BindingID, e.Conversation.Generation = next.BindingID, next.Generation
	e.Bindings = append(e.Bindings, pa.ExecutionBinding{Fence: next, Agent: e.Bot.Agent, MemoryRevision: 1, InstructionRevision: 1})
	alternate := own
	alternate.BindingID = "alternate-own-generation"
	e.Bindings = append(e.Bindings, pa.ExecutionBinding{Fence: alternate, Agent: e.Bot.Agent, MemoryRevision: 1, InstructionRevision: 1})
	trace := pa.Trace{TurnID: "t1", Category: "tool_call", Text: "Direct tool", Complete: true}
	event := pa.Event{Version: pa.ContractVersion, ID: "historical-direct-trace", Seq: 2, Fence: own, Kind: pa.TraceUpdated, At: e.Bot.CreatedAt, Trace: &trace}
	e.Events = append(e.Events, event)
	e.Permissions = []pa.PermissionOffer{{ID: "old-approval", Fence: own, TurnID: "t1", NativeRequestID: "old-native-request", State: pa.PermissionPending, Options: []pa.PermissionOption{{ID: "reject", Label: "Reject", Kind: pa.RejectOnce}}, ExpiresAt: e.Bot.CreatedAt.Add(time.Hour)}}
	db, s, v, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	snapshot := readView(t, v, q)
	if snapshot.Head.TraceCategoryCount != 1 || snapshot.Snapshot.LastTurn == nil || snapshot.Snapshot.LastTurn.Fence != own {
		t.Fatal("later room generation erased own-chat facts", snapshot)
	}
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewTraces, snapshot.SourceEpoch
	traces := readView(t, v, q)
	q.Request.Mode, q.Request.PageSize, q.Request.Cursor = pa.ReadViewDetail, 1, traces.Items[0].TraceSummary.DetailCursor
	if got := readView(t, v, q); got.Items[0].Trace.Text != "Direct tool" {
		t.Fatal(got)
	}
	q.Request.Cursor, _ = v.cursor(q, snapshot.Head, viewPosition{Collection: "permission", ID: "old-approval"})
	if _, err := v.Read(testContext, q); pa.Code(err) != pa.CursorExpired {
		t.Fatalf("old approval retained current authority: %v", err)
	}
	other := seed(t, s, "other")
	foreign := viewQuery(other)
	foreign.Request.Mode, foreign.Request.SourceEpoch, foreign.Request.PageSize, foreign.Request.Cursor = pa.ReadViewDetail, snapshot.SourceEpoch, 1, traces.Items[0].TraceSummary.DetailCursor
	if _, err := v.Read(testContext, foreign); pa.Code(err) != pa.CursorExpired {
		t.Fatalf("foreign Bot used trace cursor: %v", err)
	}
	foreign = q
	foreign.Request.Cursor = traces.Items[0].TraceSummary.DetailCursor
	foreign.Request.ConversationID = "foreign-conversation"
	if _, err := v.Read(testContext, foreign); pa.Code(err) != pa.NotFound {
		t.Fatalf("foreign conversation used trace cursor: %v", err)
	}
	for _, bad := range []pa.Fence{old, alternate, {Key: own.Key, Project: own.Project, ConversationID: own.ConversationID, BindingID: own.BindingID, Generation: 1}} {
		t.Run(bad.BindingID+"-"+string(rune('0'+bad.Generation)), func(t *testing.T) {
			changed := event
			changed.Fence = bad
			raw, _ := json.Marshal(changed)
			if _, err := db.Exec(`UPDATE project_assistant_events SET data=? WHERE event_id=?`, raw, event.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := v.Read(testContext, viewQuery(e.Bot)); pa.Code(err) != pa.ScopeDenied {
				t.Fatalf("trace fence mismatch was not rejected: %v", err)
			}
		})
	}
	// Matching fabricated event/turn fences still cannot invent a binding.
	event.Fence.BindingID = "missing-binding"
	raw, _ := json.Marshal(event)
	if _, err := db.Exec(`UPDATE project_assistant_events SET data=? WHERE event_id=?`, raw, event.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Turns.t1.fence.binding_id','missing-binding','$.Permissions',json('{}')) WHERE bot=?`, e.Bot.Key.BotID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Read(testContext, viewQuery(e.Bot)); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("invented historical binding accepted: %v", err)
	}
}
