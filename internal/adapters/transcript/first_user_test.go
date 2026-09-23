package transcript

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFirstUserUsesTheFirstVisiblePersonTurn(t *testing.T) {
	cases := []struct {
		name, assistant, body string
	}{
		{
			name: "claude", assistant: "claude",
			body: `{"type":"system","content":"bookkeeping"}` + "\n" +
				`{"type":"user","message":{"content":[{"type":"text","text":"  Name the release helper  "}]}}` + "\n" +
				`{"type":"user","message":{"content":[{"type":"text","text":"later words"}]}}` + "\n",
		},
		{
			name: "codex", assistant: "codex",
			body: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"Text","text":"preface"}]}}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"  Name the release helper  "}]}}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"later words"}]}}}` + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "record.jsonl")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := FirstUser(path, tc.assistant)
			if err != nil || got != "Name the release helper" {
				t.Fatalf("first user = %q, %v", got, err)
			}
		})
	}
}

func TestFirstUserDistinguishesNoConversationFromAnUnreadableRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"system","content":"only bookkeeping"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FirstUser(path, "claude"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no person turn = %v, want ErrNotFound", err)
	}
	if _, err := FirstUser(path+"-missing", "claude"); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("missing record = %v, want ErrNoRecord", err)
	}
}
