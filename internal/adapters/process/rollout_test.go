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

// writeRollout puts one rollout on disk with the head record Codex writes:
// `session_id` is the conversation, `id` is this file's thread within it, and
// a thread that started the conversation has them equal.
func writeRollout(t *testing.T, dir, conversation, thread string) string {
	t.Helper()
	head := map[string]any{
		"timestamp": "2026-09-20T16-34-27.000Z",
		"type":      "session_meta",
		"payload": map[string]any{
			"session_id":  conversation,
			"id":          thread,
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
		Open: func(ctx context.Context, pids []int) (map[int][]string, bool) {
			return map[int][]string{7: open}, true
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
	row := bound(t, []string{writeRollout(t, dir, rootThread, rootThread)})
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
		writeRollout(t, dir, rootThread, rootThread),
		writeRollout(t, dir, rootThread, subThread),
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
	deep := writeRollout(t, dir, rootThread, deepThread)
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
		writeRollout(t, dir, rootThread, rootThread),
		writeRollout(t, dir, rootThread, subThread),
		writeRollout(t, dir, otherRoot, otherRoot),
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
	root := writeRollout(t, dir, rootThread, rootThread)
	sub := writeRollout(t, dir, rootThread, subThread)
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

func TestRolloutHeadReadsTheConversationAndTheThread(t *testing.T) {
	dir := t.TempDir()
	conv, thread, ok := readRolloutHead(writeRollout(t, dir, rootThread, subThread))
	if !ok || conv != rootThread || thread != subThread {
		t.Fatalf("got %q/%q/%v", conv, thread, ok)
	}
	for _, bad := range []string{
		filepath.Join(dir, "absent.jsonl"),
		write(t, dir, "empty.jsonl", ""),
		write(t, dir, "truncated.jsonl", `{"type":"session_meta","payl`),
		write(t, dir, "other.jsonl", `{"type":"response_item","payload":{"id":"x"}}`),
		write(t, dir, "nameless.jsonl", `{"type":"session_meta","payload":{"cwd":"/x"}}`),
	} {
		if _, _, ok := readRolloutHead(bad); ok {
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
