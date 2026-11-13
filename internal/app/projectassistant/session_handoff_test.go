package projectassistant

import (
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func handoffFixture() (Export, pa.Turn, pa.ContextSnapshot) {
	key := pa.BotKey{MachineID: "machine", BotID: "bot"}
	project := pa.ProjectScope{MachineID: key.MachineID, ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	old := pa.Fence{Key: key, Project: project, ConversationID: "chat", BindingID: "old", Generation: 1}
	current := old
	current.BindingID = "fresh"
	current.Generation = 2
	turn := pa.Turn{ID: "current", Fence: current, State: pa.TurnRunning, MemoryRevision: 2, Instructions: pa.InstructionSnapshot{Revision: 3, RoleSnapshotRef: "role@3", Text: "Captured instructions"}}
	memory := pa.MemoryRevision{Key: key, Project: project, Revision: 2}
	filtered := pa.ContextSnapshot{Memory: memory, ProvenanceComplete: true, Text: "Verified summary", Sources: []pa.MemorySource{{ID: "summary-source", Key: key, Project: project, Revision: 1, Kind: "human"}}}
	x := Export{Bot: pa.Bot{Key: key, Project: project, ConversationID: "chat", Agent: pa.Codex}, Conversation: pa.Conversation{Key: key, ID: "chat", LastMessageSeq: 9}, Messages: []pa.Message{{ID: "bookmark", Seq: 9, Fence: old, Text: "Readable transcript must not enter new model context"}}, Turns: []pa.Turn{{ID: "current", Fence: old, State: pa.TurnQueued}, {ID: "pending", Fence: old, State: pa.TurnQueued}, {ID: "complete", Fence: old, State: pa.TurnSucceeded}}, WorkLinks: []pa.WorkLink{{ID: "uncertain-work", Fence: old, TaskID: "task-1", State: pa.WorkUnknown}, {ID: "completed-work", Fence: old, TaskID: "task-2", State: pa.WorkCompleted}}}
	return x, turn, filtered
}

func TestFreshSessionHandoffCarriesFilteredSummaryIdentityAndBookmarks(t *testing.T) {
	x, turn, filtered := handoffFixture()
	h, err := newSessionHandoff(x, turn, filtered)
	if err != nil {
		t.Fatal(err)
	}
	if h.Identity != turn.Fence || h.Instructions != turn.Instructions || h.Bookmark != (SessionBookmark{MessageID: "bookmark", MessageSeq: 9}) {
		t.Fatal("handoff lost captured identity, role or history bookmark", h)
	}
	if len(h.Unfinished) != 2 || h.Unfinished[0].ID != "pending" || h.Unfinished[1].State != "unknown" {
		t.Fatal("unfinished state changed", h.Unfinished)
	}
	opening, err := h.Opening(turn, filtered)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Verified summary", "role@3", "Captured instructions", "task-1", "bookmark"} {
		if !strings.Contains(opening, expected) {
			t.Fatal("opening omitted", expected)
		}
	}
	for _, absent := range []string{x.Messages[0].Text, "completed-work", "task-2"} {
		if strings.Contains(opening, absent) {
			t.Fatal("opening copied transcript or completed work")
		}
	}
	filtered.Sources[0].ID = "changed-after-capture"
	if h.Context.Sources[0].ID != "summary-source" {
		t.Fatal("handoff aliases mutable source receipt")
	}
}

func TestFreshSessionHandoffRejectsForeignAndStaleData(t *testing.T) {
	for _, mutate := range []func(*Export){
		func(x *Export) { x.Bot.Project.CanonicalID = "foreign" },
		func(x *Export) { x.Messages[0].Fence.Key.BotID = "foreign" },
		func(x *Export) { x.Turns[0].Fence.ConversationID = "foreign" },
		func(x *Export) { x.WorkLinks[0].Fence.Project.ProjectID = "foreign" },
	} {
		x, turn, filtered := handoffFixture()
		mutate(&x)
		if _, err := newSessionHandoff(x, turn, filtered); pa.Code(err) != pa.ScopeDenied {
			t.Fatal("foreign handoff data accepted", err)
		}
	}
	for _, mutate := range []func(*SessionHandoff){
		func(h *SessionHandoff) { h.Instructions.RoleSnapshotRef = "unreviewed-role" },
		func(h *SessionHandoff) { h.Context.Text = "Stale summary" },
		func(h *SessionHandoff) { h.Context.Memory.Revision-- },
	} {
		x, turn, filtered := handoffFixture()
		h, err := newSessionHandoff(x, turn, filtered)
		if err != nil {
			t.Fatal(err)
		}
		mutate(h)
		if _, err = h.Opening(turn, filtered); pa.Code(err) != pa.ContextStale {
			t.Fatal("stale opening accepted", err)
		}
	}
	x, turn, filtered := handoffFixture()
	h, err := newSessionHandoff(x, turn, filtered)
	if err != nil {
		t.Fatal(err)
	}
	filtered.ProvenanceComplete = false
	if _, err = h.Opening(turn, filtered); pa.Code(err) != pa.ContextStale {
		t.Fatal("final unchecked context accepted", err)
	}
	x, turn, filtered = handoffFixture()
	filtered.Memory.ForgottenThrough = 2
	filtered.Memory.ForgottenSourceIDs = []string{"summary-source"}
	if _, err := newSessionHandoff(x, turn, filtered); pa.Code(err) != pa.ContextStale {
		t.Fatal("forgotten summary re-entered fresh context", err)
	}
}
