package projectassistanthost

import (
	"context"
	"strings"
	"testing"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func saveFixtureCharter(t *testing.T, f *hostFixture, project pa.ProjectScope, expected int64, text string) {
	t.Helper()
	if _, err := f.h.store.SaveCharter(context.Background(), featurestore.CharterWrite{MachineID: project.MachineID, ProjectID: project.ProjectID,
		ExpectedVersion: expected, Text: text, Author: pa.CharterByPerson, IdempotencyKey: text, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func fixtureOpening(t *testing.T, q app.PromptRequest) string {
	t.Helper()
	if q.Handoff == nil {
		t.Fatalf("fresh session %s has no opening", q.Turn.ID)
	}
	// The native adapter renders the opening from the delivered context.
	text, err := q.Handoff.Opening(q.Turn, q.Context)
	if err != nil {
		t.Fatalf("opening for %s refused: %v", q.Turn.ID, err)
	}
	return text
}

// A fresh session opens with the charter ahead of shared memory. A charter
// saved later reaches the resumed session once, in its next prompt, and does
// not replace the native session.
func TestCharterOpensFreshSessionsAndReachesResumedSessionsOnce(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.SharedMemory = func(context.Context, pa.Bot) string { return "- Shared synthetic fact.\n" }
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	saveFixtureCharter(t, f, b.Project, 0, "Rule one: ask before deploying.")
	if _, err := f.send(t, b, "one"); err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	opening := fixtureOpening(t, prompt)
	// The opening is JSON: the delivered blocks appear escaped.
	charterAt, sharedAt := strings.Index(opening, `\u003cteam_charter version=\"1\"\u003e`), strings.Index(opening, `\u003cshared_memory\u003e`)
	if charterAt < 0 || sharedAt < 0 || charterAt > sharedAt || !strings.Contains(opening, "Rule one: ask before deploying.") {
		t.Fatalf("the opening carries the charter before shared memory: charter=%d shared=%d %q", charterAt, sharedAt, opening)
	}
	first.reply <- app.PromptResult{FinalText: "Noted"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })

	saveFixtureCharter(t, f, b.Project, 1, "Rule two: the lead reviews every change.")
	if _, err := f.send(t, b, "two"); err != nil {
		t.Fatal(err)
	}
	prompt = fixtureReceive(t, f.p.sent)
	second := fixtureReceive(t, f.p.sessions)
	if !second.q.Resume || prompt.Handoff != nil || second.q.Binding.NativeSessionID != first.q.Binding.NativeSessionID && first.q.Binding.NativeSessionID != "" {
		t.Fatalf("a charter change keeps the native session: resume=%v handoff=%v", second.q.Resume, prompt.Handoff != nil)
	}
	if !strings.HasPrefix(prompt.Text, `<team_charter_update version="2">`) || !strings.Contains(prompt.Text, "Rule two: the lead reviews every change.") || strings.Contains(prompt.Text, "Rule one") {
		t.Fatalf("the resumed prompt carries the new charter: %q", prompt.Text)
	}
	second.reply <- app.PromptResult{FinalText: "Noted again"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })

	if _, err := f.send(t, b, "three"); err != nil {
		t.Fatal(err)
	}
	prompt = fixtureReceive(t, f.p.sent)
	third := fixtureReceive(t, f.p.sessions)
	if !third.q.Resume || strings.Contains(prompt.Text, "team_charter") {
		t.Fatalf("an unchanged charter is not sent again: resume=%v %q", third.q.Resume, prompt.Text)
	}
	if turn, err := f.h.store.Turn(ctx, b.Key, prompt.Turn.ID); err != nil || turn.CharterVersion != 2 {
		t.Fatalf("the turn records the charter version its session heard: %+v %v", turn.CharterVersion, err)
	}
	third.reply <- app.PromptResult{FinalText: "Done"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	reads, err := f.h.store.CharterReads(ctx, b.Project.MachineID, b.Project.ProjectID)
	if err != nil || reads[b.Key.BotID].Version != 2 || reads[b.Key.BotID].TurnID != prompt.Turn.ID {
		t.Fatalf("readers record the latest delivered version: %+v %v", reads, err)
	}
}

// A request worker has no memory of its own. With only a charter, or a
// charter and shared memory, its fresh session opens and carries the charter.
func TestBotRequestWorkerWithOnlyACharterFinishes(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "charter only", true: "charter and shared memory"}[shared], func(t *testing.T) {
			ctx := context.Background()
			f := newHostFixture(t)
			if shared {
				f.o.SharedMemory = func(context.Context, pa.Bot) string { return "- Shared synthetic fact.\n" }
			}
			f.start(t)
			f.enable(t)
			a, b := f.create(t, "requester"), f.create(t, "recipient")
			saveFixtureCharter(t, f, b.Project, 0, "Workers answer in one paragraph.")
			if _, err := f.send(t, a, "Ask the peer."); err != nil {
				t.Fatal(err)
			}
			sender := fixtureReceive(t, f.p.sessions)
			if text := fixtureOpening(t, fixtureReceive(t, f.p.sent)); !strings.Contains(text, "Workers answer in one paragraph.") {
				t.Fatalf("the sender's opening lacks the charter: %q", text)
			}
			tools := sender.q.Tools.(interface {
				MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
			})
			x, err := tools.MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Check one synthetic fact.", IdempotencyKey: "check"})
			if err != nil || x.RequestWorkerID == "" {
				t.Fatalf("request was not assigned to a background worker: %+v, %v", x, err)
			}
			f.h.tickBackgroundTasks(ctx)
			worker := fixtureReceive(t, f.p.sessions)
			request := fixtureReceive(t, f.p.sent)
			if request.Turn.ID != x.RequestTurnID || request.Turn.Origin != pa.BotRequestOrigin {
				t.Fatalf("unexpected request prompt: %+v", request.Turn)
			}
			text := fixtureOpening(t, request)
			if !strings.Contains(text, "Workers answer in one paragraph.") || shared != strings.Contains(text, "Shared synthetic fact.") {
				t.Fatalf("the worker's opening: %q", text)
			}
			worker.reply <- app.PromptResult{FinalText: "Synthetic fact checked."}
			sender.reply <- app.PromptResult{FinalText: "I asked the peer."}
			fixtureEventually(t, func() bool {
				turn, err := f.h.store.Turn(ctx, x.Recipient().Key, x.RequestTurnID)
				return err == nil && turn.State != pa.TurnRunning
			})
			turn, err := f.h.store.Turn(ctx, x.Recipient().Key, x.RequestTurnID)
			if err != nil || turn.State != pa.TurnSucceeded || turn.Failure != nil || turn.CharterVersion != 1 {
				t.Fatalf("request turn did not finish with the charter: state=%s failure=%+v charter=%d err=%v", turn.State, turn.Failure, turn.CharterVersion, err)
			}
		})
	}
}
