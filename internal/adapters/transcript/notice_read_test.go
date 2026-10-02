package transcript

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

const readNoticeID = "b1d00000-0000-4000-8000-00000000000a"

var readNotice = `<clawdline-notice>{"protocol":"clawdline.notice","version":2,"kind":"task_finished","audience":"root","task":{"id":"b1d00000-0000-4000-8000-000000000002","title":"Ship it"},"state":"success","result_path":"/tmp/result.json","outstanding":0,"claims_released":false,"child_may_still_write":false,"body":"task finished","notice_id":"` + readNoticeID + `","ack_path":"/v1/orchestrator/tasks/b1d00000-0000-4000-8000-000000000002/completion/ack"}</clawdline-notice>`

func noticeRecord(t *testing.T, rows ...any) string {
	t.Helper()
	var b strings.Builder
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "record.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The two ways a typed notice reaches a Claude conversation count, and the
// rows that are not the model being handed it do not: a line still waiting in
// the queue, the root's own ACK command naming the id, and a subagent's turn.
func TestANoticeIsReadOnlyWhenTheModelWasHandedIt(t *testing.T) {
	enqueue := m{"type": "queue-operation", "operation": "enqueue", "content": readNotice}
	ackCall := m{"type": "assistant", "message": m{"content": []m{{"type": "tool_use", "name": "Bash",
		"input": m{"command": "clawdline task ack b1d00000-0000-4000-8000-000000000002 " + readNoticeID}}}}}
	cases := []struct {
		name      string
		assistant session.Assistant
		rows      []any
		want      bool
	}{
		{"submitted as a turn", session.AssistantClaude,
			[]any{m{"type": "user", "message": m{"role": "user", "content": readNotice}}}, true},
		{"submitted as one text block", session.AssistantClaude,
			[]any{m{"type": "user", "message": m{"role": "user", "content": []m{{"type": "text", "text": readNotice}}}}}, true},
		{"queued and taken by a running turn", session.AssistantClaude,
			[]any{enqueue, m{"type": "queue-operation", "operation": "remove"},
				m{"type": "attachment", "attachment": m{"type": "queued_command", "prompt": readNotice}}}, true},
		{"queued and still waiting", session.AssistantClaude, []any{enqueue}, false},
		{"named only by the root's own command", session.AssistantClaude, []any{ackCall}, false},
		{"a subagent's turn", session.AssistantClaude,
			[]any{m{"type": "user", "isSidechain": true, "message": m{"role": "user", "content": readNotice}}}, false},
		{"another notice", session.AssistantClaude,
			[]any{m{"type": "user", "message": m{"role": "user",
				"content": strings.ReplaceAll(readNotice, readNoticeID, "b1d00000-0000-4000-8000-00000000000b")}}}, false},
		{"a Codex user message", session.AssistantCodex,
			[]any{m{"type": "event_msg", "payload": m{"type": "item_completed",
				"item": m{"type": "UserMessage", "content": []m{{"type": "text", "text": readNotice}}}}}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NoticeHanded(noticeRecord(t, c.rows...), c.assistant, readNoticeID)
			if err != nil || got != c.want {
				t.Fatalf("read = %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

// Could not check is not "not read": a record that is not there, and one longer
// than the reader's budget whose notice is not in the part it read, are errors
// the broker treats as "type it again", never a false that looks like a look.
func TestANoticeRecordThatCannotBeReadIsNotAnAnswer(t *testing.T) {
	if _, err := NoticeHanded(filepath.Join(t.TempDir(), "absent.jsonl"), session.AssistantClaude, readNoticeID); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("a missing record answered %v", err)
	}
	rows := []any{m{"type": "user", "message": m{"role": "user", "content": readNotice}}}
	pad := m{"type": "assistant", "message": m{"content": []m{{"type": "text", "text": strings.Repeat("x", 1<<20)}}}}
	for i := 0; i < ReadBudget/(1<<20)+1; i++ {
		rows = append(rows, pad)
	}
	got, err := NoticeHanded(noticeRecord(t, rows...), session.AssistantClaude, readNoticeID)
	if got || !errors.Is(err, ErrNotFound) {
		t.Fatalf("a notice past the budget answered %v, %v", got, err)
	}
	h := &Host{}
	if _, err := h.NoticeRead(t.Context(), session.Session{Assistant: session.AssistantClaude}, readNoticeID); !errors.Is(err, ErrNoNoticeRecord) {
		t.Fatalf("a session with no conversation answered %v", err)
	}
}
