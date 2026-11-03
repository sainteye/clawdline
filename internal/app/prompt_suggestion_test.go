package app

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func promptScreen(text string, dim bool) string {
	if dim {
		text = "\x1b[2m" + text + "\x1b[22m"
	}
	return "─────────────────────\n❯ " + text + "\n─────────────────────\n? for shortcuts\n"
}

func TestPromptSuggestionFollowsTheObservedExecutionAndLease(t *testing.T) {
	screens := NewScreens(nil, &terminalPipeSentinel{}, nil)
	defer screens.Stop()
	row := session.Session{ID: "%12", PID: 123, ExpectedStart: time.Unix(100, 0), Assistant: session.AssistantClaude, Backend: session.BackendTmux, ConversationID: "fixture-conversation"}
	watch := &screenWatch{session: row, until: time.Now().Add(ScreenLease)}
	screens.watches[row.ID] = watch
	native := promptScreen("run the fixture tests", true)
	screens.finish(row.ID, watch, &native)
	reading := screens.Peek(row)
	if reading.PromptText != "run the fixture tests" || reading.PromptID == "" || reading.PromptConversation != row.ConversationID {
		t.Fatalf("native reading: %+v", reading)
	}
	firstID := reading.PromptID
	select {
	case <-screens.PromptChanges():
	default:
		t.Fatal("native suggestion did not invalidate publication")
	}
	// Other terminal output changes do not republish the same suggestion.
	moved := "changed answer\n" + native
	screens.finish(row.ID, watch, &moved)
	if got := screens.Peek(row).PromptID; got != firstID {
		t.Fatalf("same suggestion changed identity: %q", got)
	}
	select {
	case <-screens.PromptChanges():
		t.Fatal("unrelated output woke prompt publication")
	default:
	}
	draft := promptScreen("run the fixture tests", false)
	screens.finish(row.ID, watch, &draft)
	if got := screens.Peek(row); got.PromptText != "" || got.PromptID != "" || got.PromptAt == 0 {
		t.Fatalf("accepted native draft remained a suggestion: %+v", got)
	}
	select {
	case <-screens.PromptChanges():
	default:
		t.Fatal("native clear did not invalidate publication")
	}
	screens.finish(row.ID, watch, &native)
	if screens.Peek(row).PromptID == firstID {
		t.Fatal("new appearance reused a dismissed identity")
	}
	changed := row
	changed.ExpectedStart = row.ExpectedStart.Add(time.Second)
	if got := screens.Peek(changed); got.PromptText != "" || got.Text != nil {
		t.Fatal("old process screen crossed an execution change")
	}
	screens.watches[row.ID] = &screenWatch{session: changed, until: time.Now().Add(ScreenLease)}
	screens.finish(row.ID, watch, &native)
	if screens.Peek(changed).Text != nil {
		t.Fatal("an in-flight old capture replaced the new process")
	}
	screens.watches[row.ID] = watch
	watch.until = time.Now().Add(-time.Second)
	if got := screens.Peek(row); got.PromptText != "" || got.PromptID != "" {
		t.Fatal("expired lease retained a completion")
	}
}
