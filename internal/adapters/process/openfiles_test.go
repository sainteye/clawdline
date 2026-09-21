//go:build darwin || linux

package process

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

const (
	rolloutA = "/Users/x/.codex/sessions/2026/09/20/rollout-2026-09-20T15-08-18-c0de0001-0000-4000-8000-000000000001.jsonl"
	rolloutB = "/Users/x/.codex/sessions/2026/09/20/rollout-2026-09-20T14-41-39-c0de0002-0000-4000-8000-000000000002.jsonl"
	idA      = "c0de0001-0000-4000-8000-000000000001"
)

// A session that has written nothing yet and a table this machine could not
// read are the two answers this whole reading exists to keep apart. One
// resolves itself the moment somebody types into the session; the other is
// this machine's own fault and will not resolve at all. Flattening them into
// "no id" is what made a fresh Codex look like a session that was not there.
func TestUnreadableIsNotTheSameAnswerAsNoRecord(t *testing.T) {
	unread, unreadBinding, unreadDetail, _ := codexConversation(nil, false, nil)
	absent, absentBinding, absentDetail, _ := codexConversation(nil, true, nil)

	if unreadBinding == absentBinding {
		t.Fatalf("both answered %q; an unread table and an empty one are different facts", unreadBinding)
	}
	if unreadBinding != session.BindingUnreadable {
		t.Fatalf("an unread table answered %q, want %q", unreadBinding, session.BindingUnreadable)
	}
	if absentBinding != session.BindingNoRecord {
		t.Fatalf("a process holding nothing answered %q, want %q", absentBinding, session.BindingNoRecord)
	}
	if unread != "" || absent != "" {
		t.Fatalf("neither may produce an id, got %q and %q", unread, absent)
	}
	if unreadBinding.Bound() || absentBinding.Bound() {
		t.Fatal("neither answer is a binding")
	}
	if unreadDetail == absentDetail || unreadDetail == "" || absentDetail == "" {
		t.Fatalf("each says why in its own words, got %q and %q", unreadDetail, absentDetail)
	}
}

func TestTheOpenRolloutNamesTheConversation(t *testing.T) {
	id, binding, _, _ := codexConversation([]string{
		"/Users/x/.codex/state_5.sqlite",
		"/Users/x/.codex/thread-writer-locks/" + idA + ".lock",
		rolloutA,
	}, true, heads(nil))
	if id != idA {
		t.Fatalf("got %q, want %q", id, idA)
	}
	if binding != session.BindingOpenFile || !binding.Bound() {
		t.Fatalf("got %q", binding)
	}
}

// Codex's own app-server holds every thread it serves open at once. Picking
// one of them would give a session another session's name, which costs far
// more than a row with no name on it.
func TestTwoConversationsAreAmbiguousRatherThanAGuess(t *testing.T) {
	id, binding, detail, _ := codexConversation([]string{rolloutA, rolloutB}, true, heads(nil))
	if id != "" {
		t.Fatalf("got %q, want no id at all", id)
	}
	if binding != session.BindingAmbiguous {
		t.Fatalf("got %q, want %q", binding, session.BindingAmbiguous)
	}
	if detail == "" {
		t.Fatal("an ambiguous answer says what it counted")
	}
	// The same rollout listed twice — one descriptor per reader — is one
	// thread, not two.
	if id, binding, _, _ := codexConversation([]string{rolloutA, rolloutA}, true, heads(nil)); id != idA || binding != session.BindingOpenFile {
		t.Fatalf("got %q/%q", id, binding)
	}
}

func TestOnlyARolloutNamesAThread(t *testing.T) {
	for _, path := range []string{
		"/Users/x/.codex/thread-writer-locks/" + idA + ".lock",
		"/Users/x/.codex/sessions/2026/09/20/rollout-2026-09-20T15-08-18.jsonl",
		"/Users/x/.codex/sessions/2026/09/20/rollout-" + idA + ".json",
		"/Users/x/.codex/history.jsonl",
		"",
	} {
		if id, ok := codexRolloutID(path); ok {
			t.Fatalf("%q was read as thread %q", path, id)
		}
	}
}

// Only Codex rows missing an id or directory are asked about. A command-line
// id still needs the process cwd; its known identity is left untouched.
func TestOnlyIncompleteCodexRowsAreLookedUp(t *testing.T) {
	asked := [][]int{}
	p := &PS{Head: heads(nil), Open: func(ctx context.Context, pids []int) (map[int][]string, map[int]string, bool) {
		asked = append(asked, pids)
		return map[int][]string{41: {rolloutA}}, map[int]string{22: "/code/resumed", 42: "/code/fresh"}, true
	}}
	rows := p.bindCodex(context.Background(), []session.Session{
		{Assistant: session.AssistantClaude, PID: 11},
		{Assistant: session.AssistantCodex, PID: 22, ConversationID: "resumed", Binding: session.BindingCommandLine},
		{Assistant: session.AssistantCodex, PID: 41},
		{Assistant: session.AssistantCodex, PID: 42},
	})
	if len(asked) != 1 || len(asked[0]) != 3 || asked[0][0] != 22 || asked[0][1] != 41 || asked[0][2] != 42 {
		t.Fatalf("asked %v, want one call for pids 22, 41 and 42", asked)
	}
	if rows[0].Binding != "" {
		t.Fatalf("a Claude row was answered for: %q", rows[0].Binding)
	}
	if rows[1].ConversationID != "resumed" || rows[1].Binding != session.BindingCommandLine {
		t.Fatalf("a resumed row was overwritten: %+v", rows[1])
	}
	if rows[1].CWD != "/code/resumed" {
		t.Fatalf("resumed cwd = %q", rows[1].CWD)
	}
	if rows[2].ConversationID != idA || rows[2].Binding != session.BindingOpenFile {
		t.Fatalf("got %+v", rows[2])
	}
	if rows[3].ConversationID != "" || rows[3].Binding != session.BindingNoRecord {
		t.Fatalf("got %+v, want the fresh session named as having written nothing yet", rows[3])
	}
	if rows[3].CWD != "/code/fresh" {
		t.Fatalf("fresh cwd = %q, want the process directory", rows[3].CWD)
	}
}

// With nothing to read the whole answer is unreadable, and no row is called
// empty on the strength of a reading that did not happen.
func TestATableThatCouldNotBeReadLeavesEveryRowUnreadable(t *testing.T) {
	p := &PS{Head: heads(nil), Open: func(ctx context.Context, pids []int) (map[int][]string, map[int]string, bool) { return nil, nil, false }}
	rows := p.bindCodex(context.Background(), []session.Session{{Assistant: session.AssistantCodex, PID: 7}})
	if rows[0].Binding != session.BindingUnreadable {
		t.Fatalf("got %q", rows[0].Binding)
	}
	none := &PS{}
	rows = none.bindCodex(context.Background(), []session.Session{{Assistant: session.AssistantCodex, PID: 7}})
	if rows[0].Binding != session.BindingUnreadable {
		t.Fatalf("a scan with no reader got %q", rows[0].Binding)
	}
}

// heads is a stand-in for reading the rollouts themselves: a path this table
// does not name is a thread that started its own conversation, which is what
// the file's name already says. It carries no directory, which is how a head
// that named none is read — the rollout tests on disk cover the ones that do.
func heads(tree map[string]string) RolloutHead {
	return func(path string) (RolloutMeta, bool) {
		thread, ok := codexRolloutID(path)
		if !ok {
			return RolloutMeta{}, false
		}
		if conversation, sub := tree[thread]; sub {
			return RolloutMeta{Conversation: conversation, Thread: thread}, true
		}
		return RolloutMeta{Conversation: thread, Thread: thread}, true
	}
}
