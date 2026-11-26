package projectassistanthost

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Shared memory saved while a Bot's native session is resumable reaches that
// session once, in its next prompt, even when the Bot's own memory did not
// move. An unchanged shared memory is not sent again.
func TestSharedMemoryChangeReachesAResumedSessionOnce(t *testing.T) {
	var shared atomic.Value
	shared.Store("- First synthetic fact.\n")
	f := newHostFixture(t)
	f.o.SharedMemory = func(context.Context, pa.Bot) string { return shared.Load().(string) }
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	turn := func(text string) (app.PromptRequest, *fixtureSession) {
		t.Helper()
		if _, err := f.send(t, b, text); err != nil {
			t.Fatal(err)
		}
		prompt := fixtureReceive(t, f.p.sent)
		session := fixtureReceive(t, f.p.sessions)
		session.reply <- app.PromptResult{FinalText: "ok"}
		fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
		return prompt, session
	}
	turn("one")

	shared.Store("- Second synthetic fact.\n")
	prompt, session := turn("two")
	if !session.q.Resume || prompt.Handoff != nil {
		t.Fatalf("a shared memory change keeps the native session: resume=%v handoff=%v", session.q.Resume, prompt.Handoff != nil)
	}
	if !prompt.MemoryChanged || !strings.Contains(prompt.Context.Text, "Second synthetic fact.") {
		t.Fatalf("the resumed prompt carries the new shared memory: changed=%v %q", prompt.MemoryChanged, prompt.Context.Text)
	}

	prompt, session = turn("three")
	if !session.q.Resume || prompt.MemoryChanged {
		t.Fatalf("an unchanged shared memory is not sent again: resume=%v changed=%v", session.q.Resume, prompt.MemoryChanged)
	}
}
