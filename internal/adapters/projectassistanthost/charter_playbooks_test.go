package projectassistanthost

import (
	"context"
	"slices"
	"testing"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestPlaybookProposalFromATeammateRequestWaitsForThePerson(t *testing.T) {
	ctx := t.Context()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	requester, editor := f.create(t, "requester"), f.create(t, "editor")
	saveFixtureCharter(t, f, editor.Project, 0, "Ask before deploying.")
	saved, err := f.h.store.SaveCharterPlaybook(ctx, featurestore.CharterPlaybookWrite{
		MachineID: editor.Project.MachineID, ProjectID: editor.Project.ProjectID, ExpectedVersion: 1,
		Playbook:       pa.CharterPlaybook{Title: "Review", Text: "Read the diff.", BotIDs: []string{requester.Key.BotID}},
		IdempotencyKey: "review", At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Playbooks[0].ID
	if _, err := f.send(t, requester, "Ask the editor."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)

	x, err := sender.q.Tools.(interface {
		MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	}).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: editor.Key.BotID, Text: "Update my Review Playbook.", IdempotencyKey: "review-edit"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("request worker: %+v, %v", x, err)
	}
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	tools := worker.q.Tools.(app.PlaybookTools)
	edit := app.PlaybookEdit{ID: id, BaseVersion: saved.Version, Title: "Review steps", Text: "Read the diff and run tests.", Reason: "Add tests to review."}
	if err = tools.PlaybooksAvailable(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := tools.ReadPlaybooks(ctx, ""); err != nil || got.Version != saved.Version || len(got.Playbooks) != 1 || got.Playbooks[0].ID != id || got.Playbooks[0].Text != "" {
		t.Fatalf("project Playbook listing: %+v, %v", got, err)
	}
	if got, err := tools.ReadPlaybooks(ctx, id); err != nil || len(got.Playbooks) != 1 || got.Playbooks[0].Text != "Read the diff." {
		t.Fatalf("teammate's Playbook: %+v, %v", got, err)
	}
	proposal, err := tools.ProposePlaybookEdit(ctx, edit)
	if err != nil || proposal.ID == "" || proposal.BotID != editor.Key.BotID {
		t.Fatalf("pending proposal must name the visible editor: %+v, %v", proposal, err)
	}
	again, err := tools.ProposePlaybookEdit(ctx, edit)
	if err != nil || again.ID != proposal.ID {
		t.Fatalf("a retry returns the same proposal: %+v, %v", again, err)
	}
	got, err := f.h.store.Charter(ctx, editor.Project.MachineID, editor.Project.ProjectID)
	if err != nil || got.Version != saved.Version || got.Playbooks[0].Text != saved.Playbooks[0].Text {
		t.Fatalf("a proposal must leave the existing Playbook in effect: %+v, %v", got, err)
	}
	stale := edit
	stale.BaseVersion--
	if _, err := tools.ProposePlaybookEdit(ctx, stale); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("stale proposal accepted: %v", err)
	}
	_, err = f.h.store.ApplyCharterProposal(ctx, editor.Project.MachineID, editor.Project.ProjectID, proposal.ID, saved.Version, "old-client-apply", "", time.Now())
	if pa.Code(err) != pa.Invalid {
		t.Fatalf("an older client cannot approve a Playbook proposal without reviewing it: %v", err)
	}
	got, err = f.h.store.ApplyCharterProposal(ctx, editor.Project.MachineID, editor.Project.ProjectID, proposal.ID, saved.Version, "person-applies", edit.ID, time.Now())
	if err != nil || got.Version != saved.Version+1 || got.Text != saved.Text || got.Author != pa.CharterByLeadProposal ||
		len(got.Playbooks) != 1 || got.Playbooks[0].Title != edit.Title || got.Playbooks[0].Text != edit.Text || !slices.Equal(got.Playbooks[0].BotIDs, saved.Playbooks[0].BotIDs) ||
		got.Reason != edit.Reason || got.DeliveryVersion(requester.Key.BotID) != got.Version {
		t.Fatalf("approved charter: %+v, %v", got, err)
	}
	worker.reply <- app.PromptResult{FinalText: "Playbook edited."}
	sender.reply <- app.PromptResult{FinalText: "Request sent."}
}
