package projectassistant

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func continuationFixture() (Export, pa.Turn, pa.ContextSnapshot) {
	x, turn, filtered := handoffFixture()
	turn.MessageID = "current-input"
	old := x.Messages[0].Fence
	x.Messages = []pa.Message{
		{ID: "topic", TurnID: "prior", Fence: old, Seq: 1, Kind: pa.UserMessage, Text: "Research https://example.invalid/topic-alpha"},
		{ID: "answer", TurnID: "prior", Fence: old, Seq: 2, Kind: pa.AssistantMessage, Text: "Banner: 1600 by 900; violet mountains"},
		{ID: turn.MessageID, TurnID: turn.ID, Fence: turn.Fence, Seq: 3, Kind: pa.UserMessage, Text: "Please start"},
	}
	x.Turns = []pa.Turn{{ID: "prior", MessageID: "topic", Fence: old, State: pa.TurnSucceeded, MemoryRevision: 2, Origin: pa.UserOrigin}, turn}
	x.Bindings = []pa.ExecutionBinding{{Fence: old, Agent: pa.Codex}}
	return x, turn, filtered
}

func TestFreshContinuationProvenanceAndForget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Export, *pa.ContextSnapshot)
		want   int
		code   pa.ErrorCode
	}{
		{name: "own main chat", want: 2},
		{name: "routine", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns[0].RoutineRunID = "routine" }},
		{name: "routine slot", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Bindings[0].SessionSlot = "routine" }},
		{name: "bot exchange", mutate: func(x *Export, _ *pa.ContextSnapshot) {
			x.Turns[0].Origin = pa.BotRequestOrigin
			x.Turns[0].ExchangeID = "exchange"
		}},
		{name: "host notice", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns[0].Origin = pa.HostNoticeOrigin }},
		{name: "work and notice", mutate: func(x *Export, _ *pa.ContextSnapshot) {
			x.Messages[0].Kind = pa.WorkMessage
			x.Messages[1].Kind = pa.NoticeMessage
		}},
		{name: "queued", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns[0].State = pa.TurnQueued }},
		{name: "unknown human only", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns[0].State = pa.TurnUnknown }, want: 1},
		{name: "acknowledged unknown human only", mutate: func(x *Export, _ *pa.ContextSnapshot) {
			x.Turns[0].State = pa.TurnFailed
			x.Turns[0].Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown}
		}, want: 1},
		{name: "unknown after forget", mutate: func(x *Export, m *pa.ContextSnapshot) {
			x.Turns[0].State = pa.TurnUnknown
			m.Memory.ForgottenThrough = 1
		}},
		{name: "future", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns[0].MemoryRevision = 3 }},
		{name: "before forget", mutate: func(x *Export, m *pa.ContextSnapshot) { x.Turns[0].MemoryRevision = 1; m.Memory.ForgottenThrough = 2 }},
		{name: "forgotten input", mutate: func(_ *Export, m *pa.ContextSnapshot) { m.Memory.ForgottenSourceIDs = []string{"topic"} }},
		{name: "forgotten turn", mutate: func(_ *Export, m *pa.ContextSnapshot) { m.Memory.ForgottenSourceIDs = []string{"prior"} }},
		{name: "forgotten reply", mutate: func(_ *Export, m *pa.ContextSnapshot) { m.Memory.ForgottenSourceIDs = []string{"answer"} }, want: 1},
		{name: "ambiguous message", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Messages = append(x.Messages, x.Messages[0]) }, code: pa.Invalid},
		{name: "ambiguous turn", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns = append(x.Turns, x.Turns[0]) }, code: pa.Invalid},
		{name: "foreign machine", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Messages[0].Fence.Key.MachineID = "foreign" }, code: pa.ScopeDenied},
		{name: "foreign bot", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Turns[0].Fence.Key.BotID = "foreign" }, code: pa.ScopeDenied},
		{name: "foreign canonical project", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Messages[0].Fence.Project.CanonicalID = "foreign" }, code: pa.ScopeDenied},
		{name: "foreign conversation", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Messages[0].Fence.ConversationID = "foreign" }, code: pa.ScopeDenied},
		{name: "foreign agent", mutate: func(x *Export, _ *pa.ContextSnapshot) { x.Bindings[0].Agent = "foreign" }, code: pa.ScopeDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, turn, filtered := continuationFixture()
			if tc.mutate != nil {
				tc.mutate(&x, &filtered)
			}
			h, err := newSessionHandoff(x, turn, filtered)
			if pa.Code(err) != tc.code {
				t.Fatalf("error=%v want=%s", err, tc.code)
			}
			if err != nil {
				return
			}
			if len(h.Continuation) != tc.want {
				t.Fatalf("quotes=%+v want=%d", h.Continuation, tc.want)
			}
			if _, err = h.Opening(turn, filtered); err != nil {
				t.Fatal(err)
			}
			if len(h.Continuation) > 0 && strings.Contains(tc.name, "unknown") && h.Continuation[0].State != pa.TurnUnknown {
				t.Fatal("unknown became successful")
			}
		})
	}
}

func TestFreshContinuationIsBoundedAndFinalSnapshotChecked(t *testing.T) {
	x, turn, filtered := continuationFixture()
	old := x.Messages[0].Fence
	x.Messages = nil
	x.Turns = nil
	for i := 0; i < capacity.MaximumBotContinuationMessages+2; i++ {
		id := fmt.Sprintf("message-%d", i)
		tid := fmt.Sprintf("turn-%d", i)
		x.Messages = append(x.Messages, pa.Message{ID: id, TurnID: tid, Fence: old, Seq: int64(i + 1), Kind: pa.UserMessage, Text: strings.Repeat("字", capacity.MaximumBotContinuationQuoteBytes)})
		x.Turns = append(x.Turns, pa.Turn{ID: tid, MessageID: id, Fence: old, State: pa.TurnSucceeded, MemoryRevision: 2})
	}
	x.Messages = append(x.Messages, pa.Message{ID: turn.MessageID, TurnID: turn.ID, Fence: turn.Fence, Seq: 100, Kind: pa.UserMessage, Text: "Now"})
	h, err := newSessionHandoff(x, turn, filtered)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Continuation) != capacity.MaximumBotContinuationMessages || h.Continuation[0].MessageID != "message-2" {
		t.Fatal("recent bound lost", h.Continuation)
	}
	for _, q := range h.Continuation {
		if !utf8.ValidString(q.Text) || len(q.Text) > capacity.MaximumBotContinuationQuoteBytes || !q.Truncated {
			t.Fatal("quote bound or UTF-8 lost", q)
		}
	}
	h.Continuation[0].Text = "forged"
	if _, err = h.Opening(turn, filtered); pa.Code(err) != pa.ContextStale {
		t.Fatal("mutated quote admitted", err)
	}
	x, turn, filtered = continuationFixture()
	h, err = newSessionHandoff(x, turn, filtered)
	if err != nil {
		t.Fatal(err)
	}
	filtered.Memory.ForgottenSourceIDs = []string{"topic"}
	if _, err = h.Opening(turn, filtered); pa.Code(err) != pa.ContextStale {
		t.Fatal("post-capture forget admitted", err)
	}
}

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

func TestFreshContinuationRecapAndBoundaryProtection(t *testing.T) {
	x, turn, filtered := continuationFixture()
	old := x.Messages[0].Fence
	x.Messages = nil
	x.Turns = nil

	// Create 15 turns of Q&A (30 messages total)
	for i := 1; i <= 15; i++ {
		tid := fmt.Sprintf("turn-%d", i)
		qid := fmt.Sprintf("q-%d", i)
		aid := fmt.Sprintf("a-%d", i)
		x.Messages = append(x.Messages,
			pa.Message{ID: qid, TurnID: tid, Fence: old, Seq: int64(i*2 - 1), Kind: pa.UserMessage, Text: fmt.Sprintf("Question %d", i)},
			pa.Message{ID: aid, TurnID: tid, Fence: old, Seq: int64(i * 2), Kind: pa.AssistantMessage, Text: fmt.Sprintf("Answer %d", i)},
		)
		x.Turns = append(x.Turns, pa.Turn{ID: tid, MessageID: qid, Fence: old, State: pa.TurnSucceeded, MemoryRevision: 2})
	}
	x.Messages = append(x.Messages, pa.Message{ID: turn.MessageID, TurnID: turn.ID, Fence: turn.Fence, Seq: 100, Kind: pa.UserMessage, Text: "Current ask"})

	h, err := newSessionHandoff(x, turn, filtered)
	if err != nil {
		t.Fatal(err)
	}

	if h.Recap == nil {
		t.Fatal("expected non-nil recap when earlier turns are omitted")
	}
	if h.Recap.OmittedTurns <= 0 || h.Recap.OmittedMessages <= 0 {
		t.Fatalf("expected positive omitted count, got %+v", h.Recap)
	}
	if !strings.Contains(h.Recap.Summary, "omitted") {
		t.Fatalf("recap summary missing expected text: %s", h.Recap.Summary)
	}

	// Boundary protection: every assistant message must have its user question present
	for _, c := range h.Continuation {
		if c.Kind == pa.AssistantMessage {
			foundInput := false
			for _, other := range h.Continuation {
				if other.MessageID == c.InputID {
					foundInput = true
					break
				}
			}
			if !foundInput {
				t.Fatalf("orphan assistant message found without its question: %+v", c)
			}
		}
	}

	// Opening succeeds with valid recap
	if _, err = h.Opening(turn, filtered); err != nil {
		t.Fatalf("opening failed with valid recap: %v", err)
	}

	// Recap tamper detection
	h.Recap.OmittedTurns += 99
	if _, err = h.Opening(turn, filtered); pa.Code(err) != pa.ContextStale {
		t.Fatalf("expected context stale on tampered recap, got %v", err)
	}
}
