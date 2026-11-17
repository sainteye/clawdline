package http

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectAssistantMarkAllReadBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_MARK_READ_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_MARK_READ_BROWSER_DIR for the owned read observation browser")
	}
	g, server := newSendLifetimeFixture(t)
	f := g.f
	for _, text := range []string{"Synthetic first message", "Synthetic last message"} {
		paServerOK(t, f.send(t, f.local, text))
		fallbackHTTPWait(t, f)
	}
	before := fallbackHTTPWait(t, f)
	sends := g.providerSends.Load()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-mark-read.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	after := fallbackHTTPWait(t, f)
	if len(after.ReadCursors) != 2 {
		t.Fatalf("expected two independent authenticated readers, got %v", after.ReadCursors)
	}
	for _, cursor := range after.ReadCursors {
		if cursor.MessageSeq != after.Conversation.LastMessageSeq || cursor.ConversationID != before.Conversation.ID {
			t.Fatalf("read cursor did not reach the actual conversation frontier: %v", cursor)
		}
	}
	// Observations create their own facts, never an execution or a changed message.
	before.ReadCursors, after.ReadCursors = nil, nil
	before.Events, after.Events = nil, nil
	before.Receipts, after.Receipts = nil, nil
	if !reflect.DeepEqual(before, after) || g.providerSends.Load() != sends {
		t.Fatal("marking as read changed execution state, conversation contents or provider sends")
	}
	t.Log("Real HTTP/Host/SQLite read cursors reach the complete sequence; two reader identities; execution CAS and provider sends unchanged")
}
