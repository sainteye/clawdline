package projectassistanthost

import (
	"context"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A change request aimed at the workflow brings the lead the current
// workflow as JSON; only the lead may propose one, on the current base;
// once the person applies it every Bot of the Project hears the charter
// again exactly once, with the workflow's Bots by name, followed by the
// charter check.
func TestCharterWorkflowProposalReachesEveryBotOnce(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Projects = leadDispatchResolver{fixtureResolver: fixtureResolver{f}, personal: t.TempDir()}
	f.o.DefaultLeadAgent = func(time.Time) pa.Agent { return pa.Codex }
	f.o.LeadTimeZone = "UTC"
	f.start(t)
	f.enable(t)
	maker, other := f.create(t, "maker"), f.create(t, "other")
	lead, err := f.h.store.Bot(ctx, pa.BotKey{MachineID: f.o.MachineID, BotID: "default-lead"})
	if err != nil || !lead.Lead {
		t.Fatalf("expected the lead: %+v %v", lead, err)
	}
	saveFixtureCharter(t, f, maker.Project, 0, "Makers build; reviewers review.")
	turn := func(b pa.Bot, text string) (string, *fixtureSession) {
		t.Helper()
		if _, err := f.send(t, b, text); err != nil {
			t.Fatal(err)
		}
		prompt := fixtureReceive(t, f.p.sent)
		session := fixtureReceive(t, f.p.sessions)
		out := prompt.Text
		if prompt.Handoff != nil {
			out = fixtureOpening(t, prompt)
		}
		return out, session
	}
	finish := func(s *fixtureSession) {
		t.Helper()
		s.reply <- app.PromptResult{FinalText: "ok"}
		fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	}
	_, s := turn(maker, "one")
	finish(s)
	_, s = turn(other, "one")
	// An ordinary Bot cannot propose a workflow.
	member, ok := s.q.Tools.(app.CharterWorkflowTools)
	if !ok || member.CharterProposalAvailable(ctx) == nil {
		t.Fatal("an ordinary Bot is not offered the workflow tool")
	}
	if _, err := member.ProposeCharterWorkflow(ctx, app.CharterWorkflowProposal{Project: maker.Project.ProjectID, BaseVersion: 1, Workflow: hostWorkflow(maker.Key.BotID)}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a Bot that is not the lead is refused: %v", err)
	}
	finish(s)

	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, err := service.Get(ctx, lead.Key)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: lead.Key, Project: lead.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	if _, err = service.SendCharterEdit(ctx, pa.Command{Key: lead.Key, Operation: pa.SendOperation, IdempotencyKey: "workflow-request"}, fence, "Add a review step before done.",
		pa.CharterEdit{ProjectID: maker.Project.ProjectID, Version: 1, Target: pa.CharterEditWorkflow}); err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	if !strings.Contains(prompt.Text, `<charter_workflow_edit_request project="project" version="1">`) || !strings.Contains(prompt.Text, "<current_workflow>\nnull\n</current_workflow>") ||
		!strings.Contains(prompt.Text, "Add a review step before done.") || strings.Contains(prompt.Text, "<charter_edit_request") {
		t.Fatalf("the lead's prompt carries the workflow edit context: %q", prompt.Text)
	}
	tools, ok := session.q.Tools.(app.CharterWorkflowTools)
	if !ok || tools.CharterProposalAvailable(ctx) != nil {
		t.Fatal("the lead is offered the workflow tool")
	}
	_, err = tools.ProposeCharterWorkflow(ctx, app.CharterWorkflowProposal{Project: maker.Project.ProjectID, BaseVersion: 0, Workflow: hostWorkflow(maker.Key.BotID), Summary: "stale"})
	if pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a proposal on an old base is refused: %v", err)
	}
	_, err = tools.ProposeCharterWorkflow(ctx, app.CharterWorkflowProposal{Project: maker.Project.ProjectID, BaseVersion: 1, Workflow: hostWorkflow("ghost"), Summary: "ghost"})
	if f, ok := err.(*pa.Failure); !ok || f.Code != pa.Invalid || f.Field != "workflow_state_bot:build" {
		t.Fatalf("a Bot outside the Project is refused by state: %v", err)
	}
	p, err := tools.ProposeCharterWorkflow(ctx, app.CharterWorkflowProposal{Project: maker.Project.ProjectID, BaseVersion: 1, Workflow: hostWorkflow(maker.Key.BotID), Summary: "Added the build step."})
	if err != nil || p.Kind != pa.CharterProposalWorkflow {
		t.Fatalf("propose: %+v %v", p, err)
	}
	finish(session)
	if current, _ := f.h.store.Charter(ctx, maker.Project.MachineID, maker.Project.ProjectID); current.Version != 1 || current.Workflow != nil {
		t.Fatalf("a proposal changes nothing: %+v", current)
	}
	applied, err := f.h.store.ApplyCharterProposal(ctx, maker.Project.MachineID, maker.Project.ProjectID, p.ID, 1, "apply", time.Now())
	if err != nil || applied.Version != 2 || applied.Text != "Makers build; reviewers review." {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	for _, b := range []pa.Bot{maker, other} {
		got, s := turn(b, "two")
		update, check := strings.Index(got, `<team_charter_update version="2">`), strings.Index(got, `<charter_check version="2">`)
		if update != 0 || check < update || !strings.Contains(got, "- Build（Bot maker）：→ Done") || !strings.Contains(got, "Makers build; reviewers review.") {
			t.Fatalf("%s hears the workflow once, by name, then the check: %q", b.Key.BotID, got)
		}
		finish(s)
		got, s = turn(b, "three")
		if strings.Contains(got, "team_charter") || strings.Contains(got, "charter_check") {
			t.Fatalf("%s does not hear it again: %q", b.Key.BotID, got)
		}
		finish(s)
	}
}

func hostWorkflow(bot string) pa.CharterWorkflow {
	return pa.CharterWorkflow{
		States:      []pa.WorkflowState{{ID: "open", Title: "Open", Kind: pa.WorkflowStart}, {ID: "build", Title: "Build", BotID: bot, Kind: pa.WorkflowStep}, {ID: "done", Title: "Done", Kind: pa.WorkflowDone}},
		Transitions: []pa.WorkflowTransition{{From: "open", To: "build", Kind: pa.WorkflowNormal}, {From: "build", To: "done", Kind: pa.WorkflowPass}},
	}
}
