package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type peerNoteTools interface {
	SendMessage(context.Context, string, string) error
	EditMemory(context.Context, string, app.MemoryEdit) error
}

func chatTexts(t *testing.T, f *hostFixture, key pa.BotKey) []pa.Message {
	t.Helper()
	x, err := f.h.store.Export(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return x.Messages
}

// 2026-11-24: Atlas asked Lens to remember an acceptance rule. Lens's
// edit_memory was refused in the peer turn and only Atlas heard about it; the
// person opening Lens's chat saw a capsule and "Completed". Lens may now tell
// its own person, in its own main chat and nowhere else. Since the memory
// card, appending a fact no longer fails, so the refusal here is a deletion.
func TestPeerRequestTellsTheRecipientsOwnChatWhenMemoryIsRefused(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	a, b := f.create(t, "Atlas"), f.create(t, "Lens")
	if _, err := f.send(t, a, "Ask Lens to forget the acceptance rule."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := sender.q.Tools.(askingTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Please delete the acceptance rule from your memory.", IdempotencyKey: "remember-rule"})
	if err != nil {
		t.Fatal(err)
	}
	sender.reply <- app.PromptResult{FinalText: "I asked Lens."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, a.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	tools := worker.q.Tools.(peerNoteTools)
	// A new short fact from a peer turn becomes a memory card the person
	// saves; any other memory change, such as a deletion, stays refused.
	if err := tools.EditMemory(ctx, "rule", app.MemoryEdit{Delete: true}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("peer edit_memory = %v, want the refusal this case reproduces", err)
	}
	note := "I could not delete the rule from memory: edit_memory was refused. Please tell me directly."
	if err := tools.SendMessage(ctx, "memory-refused", note); err != nil {
		t.Fatalf("send_message from the peer turn was refused: %v", err)
	}
	if err := tools.SendMessage(ctx, "memory-refused", note); err != nil {
		t.Fatalf("a retried send_message must be idempotent: %v", err)
	}
	for i := 0; i < pa.MaxPeerNotesPerRequest-1; i++ {
		if err := tools.SendMessage(ctx, "more-"+string(rune('a'+i)), "Short follow-up."); err != nil {
			t.Fatal(err)
		}
	}
	if err := tools.SendMessage(ctx, "flood", "One too many."); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("send_message beyond the per-request bound = %v, want %s", err, pa.CapacityFull)
	}
	worker.reply <- app.PromptResult{FinalText: "I could not delete it from memory; edit_memory was refused."}
	follow := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	follow.reply <- app.PromptResult{FinalText: "Lens could not delete it."}

	found := 0
	for _, m := range chatTexts(t, f, b.Key) {
		if m.Kind == pa.AssistantMessage && strings.Contains(m.Text, note) {
			found++
			if !strings.HasPrefix(m.Text, "處理 "+a.Name+" 的請求時／While handling "+a.Name+"'s request:\n") || m.Fence.ConversationID != b.ConversationID {
				t.Fatalf("note is not marked or not in Lens's main chat: %+v", m)
			}
		}
	}
	if found != 1 {
		t.Fatalf("Lens's main chat holds %d copies of the refusal note, want 1", found)
	}
	for _, m := range chatTexts(t, f, a.Key) {
		if strings.Contains(m.Text, note) {
			t.Fatalf("the note reached the requester's chat: %+v", m)
		}
	}
	stored, err := f.h.store.Exchange(ctx, b.Key, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.Reply, "edit_memory was refused") {
		t.Fatalf("the requester's reply changed: %q", stored.Reply)
	}
	if err := tools.SendMessage(ctx, "late", "After the request finished."); err == nil {
		t.Fatal("send_message after the request finished was accepted")
	}
}
