package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A task of this daemon's is tied to the session in its child terminal only
// when the broker's own evidence says so: that terminal, the task's
// assistant, and a process that was running between the tab's opening and
// the task's end. A later process in the same pane, or another assistant, is
// somebody else — the controls, each of which a looser rule would pass.
func TestAnOwnTaskIsTiedOnlyToItsOwnProcess(t *testing.T) {
	r := orchestrator.Record{ID: "t1", Assistant: "claude", State: orchestrator.StateSuccess, ChildTerminalID: "%3",
		ChildConversationID: "c",
		CreatedAt:           time.Unix(990, 0), SpawnedAt: time.Unix(1000, 0), FinishedAt: time.Unix(2000, 0),
		Landing: &orchestrator.Landing{State: orchestrator.LandingPending}}
	live := ownrecords.Live{TerminalID: "%3", TTY: "ttys003", Assistant: "claude", PID: 9,
		ProcessStart: time.Unix(1001, 0), ConversationID: "c"}

	got := ownTasks([]orchestrator.Record{r}, map[string]ownrecords.Live{"%3": live})[0]
	if !got.TranscriptProven || got.ChildPID == nil || *got.ChildPID != 9 || *got.ChildSession != "c" {
		t.Fatalf("the task's own child was not tied to it: %+v", got)
	}
	if got.Landing != nil {
		t.Fatalf("a pending landing was carried twice (the broker's obligations carry it, D01): %+v", got.Landing)
	}
	if got.ResultVerifiedAt == nil {
		t.Fatal("a finished task reads as one without a result")
	}

	for name, change := range map[string]func(*ownrecords.Live){
		"a process started after the task ended": func(l *ownrecords.Live) { l.ProcessStart = time.Unix(3000, 0) },
		"a process older than the tab":           func(l *ownrecords.Live) { l.ProcessStart = time.Unix(500, 0) },
		"another assistant":                      func(l *ownrecords.Live) { l.Assistant = "codex" },
		"an unbound conversation":                func(l *ownrecords.Live) { l.ConversationID = "" },
	} {
		l := live
		change(&l)
		got := ownTasks([]orchestrator.Record{r}, map[string]ownrecords.Live{"%3": l})[0]
		if got.TranscriptProven || got.ChildPID != nil {
			t.Errorf("%s was taken for the task's child", name)
		}
	}
}

func TestAFinishedTaskNeedsItsRecordedConversationToNameAChild(t *testing.T) {
	r := orchestrator.Record{ID: "t1", Assistant: "codex", State: orchestrator.StateSuccess,
		ChildTerminalID: "%1", CreatedAt: time.Unix(990, 0), FinishedAt: time.Unix(2000, 0)}
	live := ownrecords.Live{TerminalID: "%1", Assistant: "codex", PID: 9,
		ProcessStart: time.Unix(1000, 0), ConversationID: "new-conversation"}
	read := func() ownrecords.Task {
		return ownTasks([]orchestrator.Record{r}, map[string]ownrecords.Live{"%1": live})[0]
	}
	if got := read(); got.ChildSession != nil || got.TranscriptProven {
		t.Fatalf("finished task without a recorded conversation borrowed the current one: %+v", got)
	}
	r.ChildConversationID = "old-conversation"
	if got := read(); got.ChildSession == nil || *got.ChildSession != "old-conversation" || got.TranscriptProven {
		t.Fatalf("finished task borrowed a different conversation: %+v", got)
	}
	r.ChildConversationID = live.ConversationID
	if got := read(); got.ChildSession == nil || *got.ChildSession != live.ConversationID || !got.TranscriptProven {
		t.Fatalf("finished task lost its recorded conversation: %+v", got)
	}
}

// The assistant in a child's tmux session starts as soon as the session
// exists, and SpawnedAt is stamped only after the tab that shows it has
// opened. A child whose process predates SpawnedAt by more than the tolerance
// is still that task's child as long as it started after the task was created.
func TestAChildStartedBeforeItsTabFinishedOpeningIsStillTheTasksChild(t *testing.T) {
	r := orchestrator.Record{ID: "t2", Assistant: "codex", State: orchestrator.StateSuccess, ChildTerminalID: "%42",
		ChildConversationID: "c2",
		CreatedAt:           time.Unix(1000, 0), SpawnedAt: time.Unix(1021, 0), FinishedAt: time.Unix(1055, 0),
		Landing: &orchestrator.Landing{State: orchestrator.LandingPending}}
	live := ownrecords.Live{TerminalID: "%42", TTY: "ttys009", Assistant: "codex", PID: 4242,
		ProcessStart: time.Unix(1002, 0), ConversationID: "c2"}

	got := ownTasks([]orchestrator.Record{r}, map[string]ownrecords.Live{"%42": live})[0]
	if !got.TranscriptProven || got.ChildPID == nil || *got.ChildPID != 4242 {
		t.Fatalf("a child that started while its tab was opening was not tied to its task: %+v", got)
	}

	older := live
	older.ProcessStart = time.Unix(900, 0)
	if got := ownTasks([]orchestrator.Record{r}, map[string]ownrecords.Live{"%42": older})[0]; got.TranscriptProven {
		t.Fatal("a process from before the task existed was taken for its child")
	}
}

// The coordination plane names sessions by conversation; a row is a terminal.
// A conversation exactly one terminal holds is named by it, and one two
// terminals claim names neither.
func TestOwnWaitsAreNamedByTheirTerminal(t *testing.T) {
	lives := []ownrecords.Live{
		{TerminalID: "%1", ConversationID: "owner"},
		{TerminalID: "%2", ConversationID: "waiter"},
		{TerminalID: "%3", ConversationID: "twice"},
		{TerminalID: "%4", ConversationID: "twice"},
	}
	w := store.WaitRow{ID: "w", Owner: "owner", CreatedAt: time.Unix(100, 0), Waiters: []store.WaiterRow{
		{Waiter: "waiter", CreatedAt: time.Unix(100, 0)},
		{Waiter: "twice", CreatedAt: time.Unix(100, 0)},
		{Waiter: "gone", CreatedAt: time.Unix(100, 0), CancelledAt: time.Unix(200, 0)},
	}}
	got := ownWaits([]store.WaitRow{w}, conversationTerminals(lives))[0]
	if got.OwnerSessionID != "%1" || got.Waiters[0].SessionID != "%2" || got.Waiters[1].SessionID != "twice" {
		t.Fatalf("names: owner %s, waiters %s %s", got.OwnerSessionID, got.Waiters[0].SessionID, got.Waiters[1].SessionID)
	}
	if got.Waiters[0].ReleaseDeliveredAt != nil || got.Waiters[2].ReleaseDeliveredAt == nil {
		t.Fatalf("an open waiter reads released, or a cancelled one waits: %+v", got.Waiters)
	}
}

// Linux tmux reuses %0 after its server exits. An old launch record must
// neither rename the replacement process nor make it a Feature Root.
func TestReusedTmuxPaneDoesNotInheritOpenedSessionLabels(t *testing.T) {
	var assignment orchestrator.RootAssignment
	if err := json.Unmarshal([]byte(`{"id":"root","assistant":"codex","label":"Original task","state":"briefed","executor":{"terminal_id":"%0","backend":"tmux","opened_at":1000},"briefed_at":1010}`), &assignment); err != nil {
		t.Fatal(err)
	}
	var handoff orchestrator.Handoff
	if err := json.Unmarshal([]byte(`{"handoff_id":"handoff","assistant":"codex","title":"Original handoff","state":"delivered","opened":{"terminal_id":"%0","backend":"tmux","opened_at":1000},"delivered_at":1010}`), &handoff); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		start     int64
		assistant string
		want      bool
	}{
		{"original process", 1001, "codex", true},
		{"reused pane", 2000, "codex", false},
		{"older process", 500, "codex", false},
		{"another assistant", 1001, "claude", false},
		{"unread start", 0, "codex", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := ownrecords.Live{TerminalID: "%0", Assistant: tc.assistant, PID: 9, ConversationID: "conversation"}
			if tc.start != 0 {
				live.ProcessStart = time.Unix(tc.start, 0)
			}
			lives := []ownrecords.Live{live}
			byTerminal := map[string]ownrecords.Live{"%0": live}
			roots := ownAssignments([]orchestrator.RootAssignment{assignment}, byTerminal)
			_, labels := ownHandoffs([]orchestrator.Handoff{handoff}, byTerminal)
			for name, snapshot := range map[string]ownrecords.Snapshot{
				"root": (ownrecords.Snapshot{}).With(ownrecords.Own{RootAssignments: roots}), "handoff": (ownrecords.Snapshot{}).With(ownrecords.Own{HandoffLabels: labels}),
			} {
				title := snapshot.TitleOf(live, "", lives).Orchestrator
				if (title != "") != tc.want {
					t.Errorf("%s title=%q, want label=%v", name, title, tc.want)
				}
			}
			if ((ownrecords.Snapshot{}).With(ownrecords.Own{RootAssignments: roots})).RootAssignment(live) != nil && !tc.want {
				t.Fatal("replacement inherited root ownership")
			}
		})
	}
}

// The session list reads a delivered receipt as the newest row per terminal;
// the events prune must keep that row however old it is.
func TestTheEventsPruneKeepsTheDeliveredReceipt(t *testing.T) {
	if !store.EventKindKeptLatest(eventSessionDelivered) {
		t.Fatalf("%s is read through LatestEvents and the prune does not keep its newest row", eventSessionDelivered)
	}
}

func TestSessionLabelsIdentifyBotDispatchedTasks(t *testing.T) {
	const title = "Restore drafts after reconnecting"
	live := ownrecords.Live{TerminalID: "%1", Assistant: "claude", ConversationID: "current"}
	for _, tc := range []struct {
		name, title, child, schedule, want string
		owner                              *orchestrator.AssistantOwner
		wantDispatch                       bool
	}{
		{"Bot dispatch", title, "current", "", "", &orchestrator.AssistantOwner{BotName: "Test dispatcher"}, true},
		{"older Bot dispatch", "[Bot Task] " + title, "", "", "", &orchestrator.AssistantOwner{}, true},
		{"reused pane", title, "previous", "", "", &orchestrator.AssistantOwner{}, false},
		{"schedule", title, "current", "schedule", "[Task] " + title, nil, false},
		{"ordinary dispatch", title, "current", "", title, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := orchestrator.Record{ID: "task", Title: tc.title, Assistant: live.Assistant, ChildTerminalID: live.TerminalID,
				ChildConversationID: tc.child, State: orchestrator.StateBriefed, ScheduleID: tc.schedule, AssistantOwner: tc.owner}
			snapshot := (ownrecords.Snapshot{}).With(ownrecords.Own{Tasks: ownTasks([]orchestrator.Record{r}, nil)})
			titles := snapshot.TitleOf(live, "", []ownrecords.Live{live})
			if got := rowLabel(session.Session{}, titles, ""); got != tc.want {
				t.Fatalf("Session label = %q, want %q", got, tc.want)
			}
			if (titles.BotDispatch != nil) != tc.wantDispatch {
				t.Fatalf("Bot dispatch = %+v, want presence %t", titles.BotDispatch, tc.wantDispatch)
			}
			if dispatch := titles.BotDispatch; dispatch != nil && (dispatch.Name != tc.owner.BotName || dispatch.TaskTitle != title) {
				t.Fatalf("Bot dispatch = %+v, want recorded name %q and task %q", dispatch, tc.owner.BotName, title)
			}
		})
	}
}

func TestSessionLabelsIdentifyBotOpenedRoots(t *testing.T) {
	const title = "Install five fixes on local dev"
	key := pa.BotKey{MachineID: "fixture-machine", BotID: "fixture-bot"}
	for _, tc := range []struct {
		name         string
		start        int64
		owner        *orchestrator.RootBotOwner
		wantLabel    string
		wantDispatch bool
	}{
		{"Bot opened Root", 1001, &orchestrator.RootBotOwner{Fence: pa.Fence{Key: key}, BotName: "Test dispatcher"}, title, true},
		{"reused pane", 2000, &orchestrator.RootBotOwner{Fence: pa.Fence{Key: key}, BotName: "Test dispatcher"}, "", false},
		{"ordinary Root", 1001, nil, title, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var assignment orchestrator.RootAssignment
			if err := json.Unmarshal([]byte(`{"id":"root","assistant":"codex","project_dir":"/project","label":"`+title+`","state":"briefed","executor":{"terminal_id":"%1","backend":"tmux","opened_at":1000},"briefed_at":1010}`), &assignment); err != nil {
				t.Fatal(err)
			}
			assignment.BotOwner = tc.owner
			live := ownrecords.Live{TerminalID: "%1", Assistant: "codex", PID: 9, ProcessStart: time.Unix(tc.start, 0), ConversationID: "current"}
			roots := ownAssignments([]orchestrator.RootAssignment{assignment}, map[string]ownrecords.Live{"%1": live})
			snapshot := (ownrecords.Snapshot{}).With(ownrecords.Own{RootAssignments: roots})
			titles := snapshot.TitleOf(live, "", []ownrecords.Live{live})
			if got := rowLabel(session.Session{}, titles, ""); got != tc.wantLabel {
				t.Fatalf("Session label = %q, want %q", got, tc.wantLabel)
			}
			if (titles.BotDispatch != nil) != tc.wantDispatch {
				t.Fatalf("Bot dispatch = %+v, want presence %t", titles.BotDispatch, tc.wantDispatch)
			}
			if dispatch := titles.BotDispatch; dispatch != nil && (dispatch.Key != key || dispatch.Name != tc.owner.BotName || dispatch.ProjectDir != "/project" || dispatch.TaskTitle != "") {
				t.Fatalf("Bot dispatch = %+v, want key %+v name %q project /project and empty task title", dispatch, key, tc.owner.BotName)
			}
		})
	}
}
