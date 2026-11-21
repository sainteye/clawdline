package projectassistantacp

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A Gemini Bot that made a picture wrote its local path into the reply, which
// no phone or browser can open. The frame and the tool now say to send it.
func TestEveryBotFrameSaysToSendFilesNotPaths(t *testing.T) {
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex, pa.Grok, pa.Gemini} {
		prompt := systemPrompt(projectassistant.OpenRequest{Bot: pa.Bot{Name: "Synthetic", Agent: agent}, Directory: t.TempDir()})
		for _, fact := range []string{"call send_file with its path", "file:// link in your text is not a download", "呼叫 send_file"} {
			if !strings.Contains(prompt, fact) {
				t.Fatalf("%s frame does not say %q", agent, fact)
			}
		}
	}
	for _, tool := range mcpTools() {
		m := tool.(map[string]any)
		if m["name"] == "send_file" {
			d := m["description"].(string)
			if !strings.Contains(d, "copy a file made elsewhere") || !strings.Contains(d, "picture shows in place") {
				t.Fatalf("send_file description: %s", d)
			}
			return
		}
	}
	t.Fatal("send_file is not listed")
}

func TestResumedSessionHearsTheSendFileReminderOnPersonTurnsOnly(t *testing.T) {
	cases := []struct {
		remind bool
		origin pa.TurnOrigin
		want   bool
	}{
		{true, pa.UserOrigin, true},
		{true, "", true},
		{true, pa.BotRequestOrigin, false},
		{true, pa.BotReplyOrigin, false},
		{true, pa.HostNoticeOrigin, false},
		{false, pa.UserOrigin, false},
	}
	for _, c := range cases {
		got := withSendFileReminder("hello", c.remind, c.origin)
		if strings.HasSuffix(got, SendFileReminder) != c.want || !strings.HasPrefix(got, "hello") {
			t.Fatalf("remind=%v origin=%q: %q", c.remind, c.origin, got)
		}
	}
}

func TestOnlyResumedNonGeminiSessionsAreReminded(t *testing.T) {
	fresh, _ := openTestClient(t, "explicit", func(context.Context, projectassistant.ProtocolEvent) error { return nil })
	if fresh.remindFiles {
		t.Fatal("a fresh session already has the current frame")
	}
	l := testLimits()
	l.Notifications = 256
	resumed, _ := openLimitedTestClient(t, "load-burst", func(context.Context, projectassistant.ProtocolEvent) error { return nil }, true, l)
	if err := resumed.initialize(context.Background(), projectassistant.OpenRequest{Directory: t.TempDir(), Resume: true, Binding: pa.ExecutionBinding{NativeSessionID: "scripted-session"}}); err != nil {
		t.Fatal(err)
	}
	if !resumed.remindFiles {
		t.Fatal("a resumed session may hold a frame frozen before send_file was described")
	}
}
