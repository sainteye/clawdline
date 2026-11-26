package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A Bot is asked once per charter version to compare it with its own
// sentences: in the first prompt of a fresh session, then again only after a
// new version, in the resumed session without replacing it, and never in a
// third prompt on the same version.
func TestTheCharterCheckIsAskedOncePerVersion(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	saveFixtureCharter(t, f, b.Project, 0, "Rule one: ask before deploying.")
	turn := func(text string) (app.PromptRequest, *fixtureSession) {
		t.Helper()
		if _, err := f.send(t, b, text); err != nil {
			t.Fatal(err)
		}
		prompt := fixtureReceive(t, f.p.sent)
		return prompt, fixtureReceive(t, f.p.sessions)
	}
	finish := func(s *fixtureSession) {
		s.reply <- app.PromptResult{FinalText: "ok"}
		fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	}
	prompt, first := turn("one")
	if strings.Count(prompt.Text, `<charter_check version="1">`) != 1 || !strings.HasSuffix(prompt.Text, "one") {
		t.Fatalf("the fresh session's first prompt asks for the check once, before the request: %q", prompt.Text)
	}
	// The check is noted once the prompt is sent.
	fixtureEventually(t, func() bool {
		checks, err := f.h.store.CharterChecks(ctx, b.Project.MachineID, b.Project.ProjectID)
		return err == nil && checks[b.Key.BotID] == 1
	})
	finish(first)
	prompt, second := turn("two")
	if strings.Contains(prompt.Text, "charter_check") {
		t.Fatalf("the same version is not checked twice: %q", prompt.Text)
	}
	finish(second)

	saveFixtureCharter(t, f, b.Project, 1, "Rule two: the lead reviews every change.")
	prompt, third := turn("three")
	if !third.q.Resume || third.q.Binding.NativeSessionID != second.q.Binding.NativeSessionID && second.q.Binding.NativeSessionID != "" {
		t.Fatalf("a new charter keeps the native session: resume=%v", third.q.Resume)
	}
	update, check := strings.Index(prompt.Text, `<team_charter_update version="2">`), strings.Index(prompt.Text, `<charter_check version="2">`)
	if update != 0 || check < update || strings.Count(prompt.Text, "charter_check version") != 1 {
		t.Fatalf("the resumed prompt carries the update, then the check: %q", prompt.Text)
	}
	finish(third)
	prompt, fourth := turn("four")
	if strings.Contains(prompt.Text, "charter_check") || strings.Contains(prompt.Text, "team_charter") {
		t.Fatalf("nothing is repeated on the same version: %q", prompt.Text)
	}
	finish(fourth)
}

// The report tool is offered to an ordinary Bot whose Project has a
// charter, including in a request from another Bot, and stores the report
// against the Bot the request worker works for.
func TestTheConflictToolIsOfferedInRequestTurns(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	a, b := f.create(t, "requester"), f.create(t, "recipient")
	if _, err := f.send(t, a, "Ask the peer."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if tools, ok := sender.q.Tools.(app.CharterConflictTools); !ok || tools.CharterConflictAvailable(ctx) == nil {
		t.Fatalf("without a charter the tool is not offered")
	}
	saveFixtureCharter(t, f, b.Project, 0, "## Review\n\nThe lead reviews every change.")
	x, err := sender.q.Tools.(interface {
		MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	}).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Check one synthetic fact.", IdempotencyKey: "check"})
	if err != nil {
		t.Fatal(err)
	}
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	request := fixtureReceive(t, f.p.sent)
	if request.Turn.Origin != pa.BotRequestOrigin {
		t.Fatalf("unexpected request prompt: %+v", request.Turn)
	}
	tools, ok := worker.q.Tools.(app.CharterConflictTools)
	if !ok || tools.CharterConflictAvailable(ctx) != nil {
		t.Fatalf("the request turn is offered the report tool")
	}
	report := app.CharterConflictReport{CharterVersion: 1, Source: "instructions", BotSentence: "I merge without review.", CharterSentence: "The lead reviews every change.", Section: "Review"}
	got, err := tools.ReportCharterConflict(ctx, report)
	if err != nil || got.State != pa.ConflictOpen || got.Version != 1 {
		t.Fatalf("report: %+v %v", got, err)
	}
	if got.BotID != b.Key.BotID || x.RequestWorkerID == b.Key.BotID {
		t.Fatalf("the report names the Bot: %+v", got)
	}
	report.CharterVersion = 2
	if _, err = tools.ReportCharterConflict(ctx, report); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a report on another version is refused: %v", err)
	}
	worker.reply <- app.PromptResult{FinalText: "Checked."}
	sender.reply <- app.PromptResult{FinalText: "Asked."}
}
