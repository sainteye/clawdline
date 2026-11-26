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

// A change request from a charter page reaches the lead as the person's own
// turn carrying the Project's current charter; the lead's proposal waits for
// the person. An ordinary Bot is offered no proposal tool.
func TestCharterChangeRequestLetsTheLeadPropose(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Projects = leadDispatchResolver{fixtureResolver: fixtureResolver{f}, personal: t.TempDir()}
	f.o.DefaultLeadAgent = func(time.Time) pa.Agent { return pa.Codex }
	f.o.LeadTimeZone = "UTC"
	f.start(t)
	f.enable(t)
	member := f.create(t, "member")
	lead, err := f.h.store.Bot(ctx, pa.BotKey{MachineID: f.o.MachineID, BotID: "default-lead"})
	if err != nil || !lead.Lead {
		t.Fatalf("expected the lead: %+v %v", lead, err)
	}
	saveFixtureCharter(t, f, member.Project, 0, "Makers build; reviewers review.")
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, err := service.Get(ctx, lead.Key)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: lead.Key, Project: lead.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	edit := pa.CharterEdit{ProjectID: member.Project.ProjectID, Version: 1}
	// Only the lead accepts a change request.
	_, mc, _, _ := service.Get(ctx, member.Key)
	_, err = service.SendCharterEdit(ctx, pa.Command{Key: member.Key, Operation: pa.SendOperation, IdempotencyKey: "not-lead"},
		pa.Fence{Key: member.Key, Project: member.Project, ConversationID: mc.ID, BindingID: mc.BindingID, Generation: mc.Generation}, "Add a review rule.", edit)
	if pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a change request to an ordinary Bot is refused: %v", err)
	}
	if _, err = service.SendCharterEdit(ctx, pa.Command{Key: lead.Key, Operation: pa.SendOperation, IdempotencyKey: "charter-request"}, fence, "Add that the lead reviews every change.", edit); err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	if prompt.Turn.CharterEdit == nil || prompt.Turn.Origin != pa.UserOrigin ||
		!strings.Contains(prompt.Text, `<charter_edit_request project="project" version="1">`) ||
		!strings.Contains(prompt.Text, "Makers build; reviewers review.") || !strings.Contains(prompt.Text, "Add that the lead reviews every change.") {
		t.Fatalf("the lead's prompt carries the charter edit context: %+v %q", prompt.Turn.CharterEdit, prompt.Text)
	}
	tools, ok := session.q.Tools.(app.CharterTools)
	if !ok || tools.CharterProposalAvailable(ctx) != nil {
		t.Fatalf("the lead is offered the proposal tool")
	}
	_, err = tools.ProposeCharterEdit(ctx, app.CharterEditProposal{Project: member.Project.ProjectID, BaseVersion: 0, NewText: "x", Summary: "stale"})
	if pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a proposal on an old base is refused: %v", err)
	}
	p, err := tools.ProposeCharterEdit(ctx, app.CharterEditProposal{Project: member.Project.ProjectID, BaseVersion: 1,
		NewText: "Makers build; reviewers review.\nThe lead reviews every change.", Summary: "Added: the lead reviews every change."})
	if err != nil || p.ID == "" {
		t.Fatalf("propose: %+v %v", p, err)
	}
	if current, _ := f.h.store.Charter(ctx, member.Project.MachineID, member.Project.ProjectID); current.Version != 1 {
		t.Fatalf("a proposal changes nothing: %+v", current)
	}
	pending, err := f.h.store.CharterProposals(ctx, member.Project.MachineID, member.Project.ProjectID)
	if err != nil || len(pending) != 1 || pending[0].BotID != lead.Key.BotID || pending[0].TurnID != prompt.Turn.ID {
		t.Fatalf("the proposal waits for the person: %+v %v", pending, err)
	}
	session.reply <- app.PromptResult{FinalText: "Proposed."}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })

	if _, err = f.send(t, member, "member"); err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.sent)
	other := fixtureReceive(t, f.p.sessions)
	if tools, ok := other.q.Tools.(app.CharterTools); ok && tools.CharterProposalAvailable(ctx) == nil {
		t.Fatal("an ordinary Bot must not be offered the proposal tool")
	}
	other.reply <- app.PromptResult{FinalText: "ok"}
}
