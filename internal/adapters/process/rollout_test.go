//go:build darwin || linux

package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The four shapes a Codex process's open transcripts come in, as files on
// disk: one thread, a thread and a sub-agent of it, two threads that started
// themselves, and nothing open yet. Only the third is a guess, and only the
// third goes unnamed.
//
// The fixtures are written here rather than copied from a machine. A rollout
// is somebody's conversation; what this rule depends on is the head record's
// shape, and that is all these carry.
const (
	rootThread = "c0de0001-0000-4000-8000-000000000001"
	subThread  = "c0de0001-0000-4000-8000-0000000000a1"
	deepThread = "c0de0001-0000-4000-8000-0000000000a2"
	otherRoot  = "c0de0002-0000-4000-8000-000000000002"
)

// The directories those fixtures claim to have been started in. They are
// invented for the same reason the ids are: what the rule turns on is that the
// head carries a `cwd` and which thread wrote it, never whose directory it is.
const (
	rootDir  = "/fixture/one"
	otherDir = "/fixture/two"
)

// writeRollout puts one rollout on disk with the head record Codex writes:
// `session_id` is the conversation, `id` is this file's thread within it, a
// thread that started the conversation has them equal, and `cwd` is where that
// thread was started.
func writeRollout(t *testing.T, dir, conversation, thread, cwd string) string {
	t.Helper()
	head := map[string]any{
		"timestamp": "2026-09-20T16-34-27.000Z",
		"type":      "session_meta",
		"payload": map[string]any{
			"session_id":  conversation,
			"id":          thread,
			"cwd":         cwd,
			"cli_version": "0.155.1",
		},
	}
	if thread != conversation {
		head["payload"].(map[string]any)["parent_thread_id"] = conversation
		head["payload"].(map[string]any)["source"] = map[string]any{"subagent": map[string]any{"thread_spawn": true}}
	} else {
		head["payload"].(map[string]any)["source"] = "cli"
	}
	line, err := json.Marshal(head)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-09-20T16-34-27-"+thread+".jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// bound runs one pid's open files through the whole rule, reading the heads
// off disk exactly as a scan does.
func bound(t *testing.T, open []string) session.Session {
	t.Helper()
	p := &PS{
		Open: func(ctx context.Context, pids []int) (map[int][]string, map[int]string, bool) {
			return map[int][]string{7: open}, nil, true
		},
		Head: readRolloutHead,
	}
	rows := p.bindCodex(context.Background(), []session.Session{{Assistant: session.AssistantCodex, PID: 7}})
	return rows[0]
}

// One thread, its own rollout, nothing else: the case that already worked and
// must keep working.
func TestOneThreadIsNamedByItsOwnRollout(t *testing.T) {
	dir := t.TempDir()
	row := bound(t, []string{writeRollout(t, dir, rootThread, rootThread, rootDir)})
	if row.ConversationID != rootThread || row.Binding != session.BindingOpenFile {
		t.Fatalf("got %q/%q", row.ConversationID, row.Binding)
	}
	if row.BindingDetail != "" {
		t.Fatalf("a bound row explains nothing, got %q", row.BindingDetail)
	}
}

// A session running a sub-agent holds both transcripts open at once. It is one
// conversation and every one of its rollouts says so, so it is named — this is
// the case that left `ttys032` and `ttys050` with no name and no transcript.
func TestASubThreadDoesNotMakeASessionAmbiguous(t *testing.T) {
	dir := t.TempDir()
	row := bound(t, []string{
		writeRollout(t, dir, rootThread, rootThread, rootDir),
		writeRollout(t, dir, rootThread, subThread, rootDir),
	})
	if row.Binding != session.BindingOpenFile {
		t.Fatalf("got %q: %s", row.Binding, row.BindingDetail)
	}
	if row.ConversationID != rootThread {
		t.Fatalf("named %q, want the conversation %q and never the sub-thread", row.ConversationID, rootThread)
	}
}

// A sub-agent's sub-agent names its parent rather than the conversation, and
// the conversation's own rollout need not be among the open ones at all. Both
// are answered by the same field, which is why it is read instead of the tree
// being walked.
func TestASubThreadNamesTheConversationWithoutItsParentBeingOpen(t *testing.T) {
	dir := t.TempDir()
	deep := writeRollout(t, dir, rootThread, deepThread, rootDir)
	if row := bound(t, []string{deep}); row.ConversationID != rootThread || row.Binding != session.BindingOpenFile {
		t.Fatalf("got %q/%q", row.ConversationID, row.Binding)
	}
}

// Two conversations on one process is the case this must refuse. Codex's own
// app-server holds every thread it serves open, and a session wearing another
// session's name costs more than a row with no name on it.
func TestTwoConversationsStayAmbiguous(t *testing.T) {
	dir := t.TempDir()
	row := bound(t, []string{
		writeRollout(t, dir, rootThread, rootThread, rootDir),
		writeRollout(t, dir, rootThread, subThread, rootDir),
		writeRollout(t, dir, otherRoot, otherRoot, otherDir),
	})
	if row.Binding != session.BindingAmbiguous || row.ConversationID != "" {
		t.Fatalf("got %q/%q", row.ConversationID, row.Binding)
	}
	// And it says what it counted, so the next reader does not open the same
	// files to find out why.
	want := "3 transcripts open on this process, belonging to 2 conversations (1 sub-thread folded into the thread that started it)"
	if !strings.HasPrefix(row.BindingDetail, want) {
		t.Fatalf("got %q, want it to begin %q", row.BindingDetail, want)
	}
}

// Nothing open is a session that has not been typed into yet, which somebody
// typing resolves — and is still not the same answer as a table that could not
// be read.
func TestNoRolloutOpenIsStillNoRecord(t *testing.T) {
	row := bound(t, nil)
	if row.Binding != session.BindingNoRecord || row.ConversationID != "" {
		t.Fatalf("got %q/%q", row.ConversationID, row.Binding)
	}
	if row.BindingDetail != session.NoTranscriptDetail {
		t.Fatalf("got %q", row.BindingDetail)
	}
}

// A head that cannot be read falls back to the file's own name, which can only
// ever refuse: a sub-thread's name disagrees with its conversation, so the
// answer stays ambiguous rather than becoming another session's.
func TestAnUnreadableHeadRefusesRatherThanGuesses(t *testing.T) {
	dir := t.TempDir()
	root := writeRollout(t, dir, rootThread, rootThread, rootDir)
	sub := writeRollout(t, dir, rootThread, subThread, rootDir)
	if err := os.WriteFile(sub, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := bound(t, []string{root, sub})
	if row.Binding != session.BindingAmbiguous {
		t.Fatalf("got %q", row.Binding)
	}
	if want := "1 head this machine could not read"; !strings.Contains(row.BindingDetail, want) {
		t.Fatalf("got %q, want it to mention %q", row.BindingDetail, want)
	}
}

// The directory is the whole point of reading the head a second field deep: a
// Codex row reached the console with its project cell empty, because the only
// directory anything here knew about came from Claude Code's registry and
// Codex has no entry in it.
func TestACodexRowCarriesTheDirectoryItsHeadNamed(t *testing.T) {
	dir := t.TempDir()
	row := bound(t, []string{writeRollout(t, dir, rootThread, rootThread, rootDir)})
	if row.CWD != rootDir {
		t.Fatalf("directory %q, want %q", row.CWD, rootDir)
	}
}

// A sub-agent can be started somewhere else, and the session is still where
// the thread that started the conversation is. So the root thread's head
// answers whatever the sub-thread's says, and the order they were read in
// changes nothing.
func TestTheThreadThatStartedTheConversationSaysWhereItIs(t *testing.T) {
	dir := t.TempDir()
	root := writeRollout(t, dir, rootThread, rootThread, rootDir)
	sub := writeRollout(t, dir, rootThread, subThread, otherDir)
	for _, open := range [][]string{{root, sub}, {sub, root}} {
		if row := bound(t, open); row.CWD != rootDir {
			t.Fatalf("directory %q, want the conversation's own %q", row.CWD, rootDir)
		}
	}
}

// Without that thread's rollout open, the heads that were read have to agree.
// One directory among them is an answer; two is not, and two is not resolved
// by picking one.
func TestSubThreadsThatDisagreeLeaveTheDirectoryEmpty(t *testing.T) {
	dir := t.TempDir()
	agree := bound(t, []string{
		writeRollout(t, dir, rootThread, subThread, rootDir),
		writeRollout(t, dir, rootThread, deepThread, rootDir),
	})
	if agree.CWD != rootDir {
		t.Fatalf("agreeing sub-threads gave %q, want %q", agree.CWD, rootDir)
	}
	split := bound(t, []string{
		writeRollout(t, dir, rootThread, subThread, rootDir),
		writeRollout(t, dir, rootThread, deepThread, otherDir),
	})
	if split.ConversationID != rootThread {
		t.Fatalf("named %q, want the conversation still named", split.ConversationID)
	}
	if split.CWD != "" {
		t.Fatalf("two directories became %q, want none", split.CWD)
	}
}

// Every kind of not knowing leaves the cell empty rather than filling it from
// somewhere else. A head with no `cwd` in it is one of them: this machine did
// not read a directory, and a row that says so is the honest row.
func TestNoDirectoryIsEverInferred(t *testing.T) {
	dir := t.TempDir()
	nameless := write(t, dir, "rollout-2026-09-20T16-34-27-"+rootThread+".jsonl",
		`{"type":"session_meta","payload":{"session_id":"`+rootThread+`","id":"`+rootThread+`"}}`+"\n")
	if row := bound(t, []string{nameless}); row.CWD != "" || row.ConversationID != rootThread {
		t.Fatalf("got %q/%q, want the conversation named and no directory", row.ConversationID, row.CWD)
	}
	// Two conversations on one process is a row with no name, and a row with
	// no name is a row with no project either.
	ambiguous := bound(t, []string{
		writeRollout(t, dir, rootThread, rootThread, rootDir),
		writeRollout(t, dir, otherRoot, otherRoot, otherDir),
	})
	if ambiguous.Binding != session.BindingAmbiguous || ambiguous.CWD != "" {
		t.Fatalf("got %q/%q", ambiguous.Binding, ambiguous.CWD)
	}
	// And nothing open at all is the session nobody has typed into.
	if row := bound(t, nil); row.CWD != "" {
		t.Fatalf("an empty process got %q", row.CWD)
	}
}

func TestRolloutHeadReadsTheConversationTheThreadAndTheDirectory(t *testing.T) {
	dir := t.TempDir()
	meta, ok := readRolloutHead(writeRollout(t, dir, rootThread, subThread, rootDir))
	if !ok || meta.Conversation != rootThread || meta.Thread != subThread || meta.CWD != rootDir {
		t.Fatalf("got %+v/%v", meta, ok)
	}
	// A head with no `cwd` is read, and says nothing about a directory. The
	// three answers are kept apart here because they are kept apart
	// everywhere: unreadable is false, absent is the empty string, and there
	// is no third thing this may put in its place.
	without := write(t, dir, "nocwd.jsonl", `{"type":"session_meta","payload":{"session_id":"`+rootThread+`","id":"`+rootThread+`"}}`)
	if meta, ok := readRolloutHead(without); !ok || meta.Conversation != rootThread || meta.CWD != "" {
		t.Fatalf("got %+v/%v", meta, ok)
	}
	for _, bad := range []string{
		filepath.Join(dir, "absent.jsonl"),
		write(t, dir, "empty.jsonl", ""),
		write(t, dir, "truncated.jsonl", `{"type":"session_meta","payl`),
		write(t, dir, "other.jsonl", `{"type":"response_item","payload":{"id":"x"}}`),
		write(t, dir, "nameless.jsonl", `{"type":"session_meta","payload":{"cwd":"/x"}}`),
	} {
		if _, ok := readRolloutHead(bad); ok {
			t.Fatalf("%q was read as a head", filepath.Base(bad))
		}
	}
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
