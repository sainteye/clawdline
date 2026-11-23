package projectassistanthost

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type peerWorkTools interface {
	Dispatch(context.Context, string, string, string, []string) (pa.WorkLink, error)
	Status(context.Context, string) (pa.WorkLink, error)
	MessageWork(context.Context, string, string, string) (pa.WorkMessageReceipt, error)
	MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
}

var launchedSecret = regexp.MustCompile(`TASK_SECRET=([0-9a-f]{64})`)

// A Bot answering another Bot's message_bot runs in a private thread worker.
// That turn still reads and adds to the work this same Bot dispatched from its
// own chat, and still cannot reach the work any other Bot dispatched.
func TestPeerRequestTurnReachesOnlyItsOwnBotsDispatchedWork(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxBots, f.o.Limits.MaxProcesses = 4, 3
	var mu sync.Mutex
	var typed []string
	f.o.Broker.Type = func(_ context.Context, _, text string) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, text)
		return nil
	}
	// relayed returns the Session messages typed so far, not briefings.
	relayed := func() []transcript.Entry {
		mu.Lock()
		defer mu.Unlock()
		var out []transcript.Entry
		for _, line := range typed {
			if m, ok := transcript.SessionMessage(line, 1); ok {
				out = append(out, m)
			}
		}
		return out
	}
	f.start(t)
	f.enable(t)
	atlas, lens := f.create(t, "atlas"), f.create(t, "lens")

	// Atlas dispatches from its own chat, and the work Session signs in.
	if _, err := f.send(t, atlas, "dispatch"); err != nil {
		t.Fatal(err)
	}
	main := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	own, ok := main.q.Tools.(peerWorkTools)
	if !ok {
		t.Fatal("Atlas's chat has no work tools")
	}
	link, err := own.Dispatch(ctx, "work", "Show unconfirmed delivery", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	// The broker types the briefing, carrying the secret, into the worker.
	var secret []string
	fixtureEventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, line := range typed {
			if secret = launchedSecret.FindStringSubmatch(line); secret != nil {
				typed = nil
				return true
			}
		}
		return false
	})
	if _, err = f.o.Broker.Accept(ctx, link.TaskID, secret[1]); err != nil {
		t.Fatal(err)
	}
	main.reply <- app.PromptResult{FinalText: "Dispatched."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Export(ctx, atlas.Key)
		return err == nil && turn.Conversation.ActiveTurnID == ""
	})

	// Lens dispatches work of its own, then asks Atlas to pass on a spec.
	if _, err = f.send(t, lens, "ask"); err != nil {
		t.Fatal(err)
	}
	asker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	lensTools, ok := asker.q.Tools.(peerWorkTools)
	if !ok {
		t.Fatal("Lens's chat has no work tools")
	}
	foreign, err := lensTools.Dispatch(ctx, "lens-work", "Check the delivery copy", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := lensTools.MessageBot(ctx, pa.BotMessageRequest{TargetBotID: atlas.Key.BotID, Text: "Please add the new delivery spec to your running task.", IdempotencyKey: "spec"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("request was not assigned to Atlas's thread worker: %+v, %v", x, err)
	}
	asker.reply <- app.PromptResult{FinalText: "Asked Atlas."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, lens.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	request := fixtureReceive(t, f.p.sent)
	if request.Turn.Origin != pa.BotRequestOrigin || worker.q.Bot.Key == atlas.Key {
		t.Fatalf("not a peer-request worker turn: %+v", request.Turn)
	}
	peer, ok := worker.q.Tools.(peerWorkTools)
	if !ok {
		t.Fatal("the peer-request turn has no work tools")
	}

	read, err := peer.Status(ctx, link.TaskID)
	if err != nil || read.TaskID != link.TaskID || read.State != pa.WorkExecuting {
		t.Fatalf("work_status on Atlas's own work from the peer-request turn: %+v, %v", read, err)
	}
	sent, err := peer.MessageWork(ctx, link.TaskID, "Show 'delivery not confirmed' when no receipt arrives.", "spec-1")
	if err != nil || sent.Stage != pa.WorkMessageDelivered {
		t.Fatalf("message_work on Atlas's own work from the peer-request turn: %+v, %v", sent, err)
	}
	lines := relayed()
	if len(lines) != 1 {
		t.Fatalf("typed %d messages, want 1: %+v", len(lines), lines)
	}
	m := lines[0]
	if !strings.Contains(m.Text, "來自 Bot atlas（派工者）") || !strings.Contains(m.Text, "轉述 Bot lens 的請求") ||
		!strings.Contains(m.Text, "relaying a request from Bot lens") || !strings.Contains(m.Text, "not the person's authorization") ||
		!strings.HasSuffix(m.Text, "Show 'delivery not confirmed' when no receipt arrives.") {
		t.Fatalf("the worker cannot tell the message relays another Bot's request: %+v", m)
	}

	// Lens's work stays Lens's, even from inside Atlas's peer-request turn.
	var refused *pa.Failure
	if _, err = peer.Status(ctx, foreign.TaskID); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "work_dispatched_by_another_bot" {
		t.Fatalf("work_status reached another Bot's work: %v", err)
	}
	if _, err = peer.MessageWork(ctx, foreign.TaskID, "Not yours.", "spec-2"); !errors.As(err, &refused) || refused.Field != "work_dispatched_by_another_bot" {
		t.Fatalf("message_work reached another Bot's work: %v", err)
	}
	if n := len(relayed()); n != 1 {
		t.Fatalf("a refused message was typed: %d", n)
	}
	worker.reply <- app.PromptResult{FinalText: "Passed the spec to my running task."}
}
