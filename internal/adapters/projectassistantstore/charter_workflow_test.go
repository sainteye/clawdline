package projectassistantstore

import (
	"database/sql"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// seedCharterBot stores a minimal Bot row of Project project, as the charter
// pages read it.
func seedCharterBot(t *testing.T, db *sql.DB, bot, project string, hidden bool) {
	t.Helper()
	data := `{"Bot":{"name":"` + bot + `","project":{"project_id":"` + project + `"},"hidden":` + map[bool]string{false: "false", true: "true"}[hidden] + `}}`
	if _, err := db.ExecContext(testContext, `INSERT INTO project_assistant_bots(machine,bot,version,data) VALUES('m',?,1,?)`, bot, data); err != nil {
		t.Fatal(err)
	}
}

func storeWorkflow(bots ...string) pa.CharterWorkflow {
	w := pa.CharterWorkflow{States: []pa.WorkflowState{{ID: "open", Title: "Open", Kind: pa.WorkflowStart}}}
	prev := "open"
	for _, b := range bots {
		w.States = append(w.States, pa.WorkflowState{ID: b + "-step", Title: b, BotID: b, Kind: pa.WorkflowStep})
		w.Transitions = append(w.Transitions, pa.WorkflowTransition{From: prev, To: b + "-step", Kind: pa.WorkflowNormal})
		prev = b + "-step"
	}
	w.States = append(w.States, pa.WorkflowState{ID: "done", Title: "Done", Kind: pa.WorkflowDone})
	w.Transitions = append(w.Transitions, pa.WorkflowTransition{From: prev, To: "done", Kind: pa.WorkflowNormal})
	return w
}

func saveWorkflow(t *testing.T, s *Store, expected int64, w pa.CharterWorkflow, key string) pa.CharterVersion {
	t.Helper()
	v, err := s.SaveCharterWorkflow(testContext, CharterWorkflowWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: expected, Workflow: w, Reason: "flow", IdempotencyKey: key, At: charterAt})
	if err != nil {
		t.Fatalf("save workflow at %d: %v", expected, err)
	}
	return v
}

// The workflow is part of the charter: a workflow save is a version that
// keeps the text and playbooks; text and playbook saves keep the workflow;
// restore brings an older one back and removal clears it.
func TestCharterWorkflowIsKeptRestoredAndCleared(t *testing.T) {
	db, store, _ := testDB(t)
	seedCharterBot(t, db, "atlas", "p", false)
	seedCharterBot(t, db, "lens", "p", false)
	seedCharterBot(t, db, "shy", "p", true)
	seedCharterBot(t, db, "stranger", "other", false)

	saveCharter(t, store, 0, "Lead plans.", "k1")
	savePlaybook(t, store, 1, pa.CharterPlaybook{Title: "Release", Text: "Tag.", BotIDs: []string{"atlas"}}, "k2")
	flow := storeWorkflow("atlas", "lens")
	v3 := saveWorkflow(t, store, 2, flow, "k3")
	if v3.Author != pa.CharterByPerson || v3.Text != "Lead plans." || len(v3.Playbooks) != 1 || !pa.SameWorkflow(v3.Workflow, &flow) || v3.DeliveryVersion("lens") != 3 || v3.DeliveryVersion("idle") != 3 {
		t.Fatalf("a workflow save keeps text and playbooks and reaches every Bot: %+v", v3)
	}
	if again := saveWorkflow(t, store, 2, flow, "k3"); again.Version != 3 {
		t.Fatalf("a repeated workflow save answers its own version: %+v", again)
	}
	for name, c := range map[string]struct {
		bot   string
		field string
	}{"hidden Bot": {"shy", "workflow_state_bot:shy-step"}, "another Project's Bot": {"stranger", "workflow_state_bot:stranger-step"}, "unknown Bot": {"ghost", "workflow_state_bot:ghost-step"}} {
		_, err := store.SaveCharterWorkflow(testContext, CharterWorkflowWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 3, Workflow: storeWorkflow(c.bot), IdempotencyKey: "bad-" + c.bot, At: charterAt})
		if f, ok := err.(*pa.Failure); !ok || f.Code != pa.Invalid || f.Field != c.field {
			t.Errorf("%s: %v", name, err)
		}
	}
	broken := storeWorkflow("atlas")
	broken.Transitions = broken.Transitions[:1]
	_, err := store.SaveCharterWorkflow(testContext, CharterWorkflowWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 3, Workflow: broken, IdempotencyKey: "broken", At: charterAt})
	requireFailure(t, err, pa.Invalid, "workflow_unreachable:done")
	_, err = store.SaveCharterWorkflow(testContext, CharterWorkflowWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 2, Workflow: flow, IdempotencyKey: "stale", At: charterAt})
	requireFailure(t, err, pa.StaleVersion, "charter_version")

	v4 := saveCharter(t, store, 3, "Lead plans; makers build.", "k4")
	v5 := savePlaybook(t, store, 4, pa.CharterPlaybook{Title: "Review", Text: "Two.", BotIDs: []string{"lens"}}, "k5")
	current, err := store.Charter(testContext, "m", "p")
	if err != nil || !pa.SameWorkflow(v4.Workflow, &flow) || !pa.SameWorkflow(v5.Workflow, &flow) || !pa.SameWorkflow(current.Workflow, &flow) || current.DeliveryVersion("atlas") != 4 || current.DeliveryVersion("lens") != 5 {
		t.Fatalf("text and playbook saves keep the workflow: %+v %v", current, err)
	}
	if at, err := store.CharterAt(testContext, "m", "p", 3); err != nil || !pa.SameWorkflow(at.Workflow, &flow) {
		t.Fatalf("a selected version reads its workflow: %+v %v", at, err)
	}
	other := storeWorkflow("lens")
	saveWorkflow(t, store, 5, other, "k6")
	restored, err := store.RestoreCharter(testContext, "m", "p", 6, 3, "k7", charterAt)
	if err != nil || !pa.SameWorkflow(restored.Workflow, &flow) || restored.DeliveryVersion("idle") != 7 {
		t.Fatalf("restore brings back that version's workflow: %+v %v", restored, err)
	}
	none, err := store.RestoreCharter(testContext, "m", "p", 7, 2, "k8", charterAt)
	if err != nil || none.Workflow != nil || none.DeliveryVersion("idle") != 8 {
		t.Fatalf("restoring a version without a workflow clears it: %+v %v", none, err)
	}
	if _, err = store.RemoveCharter(testContext, "m", "p", 8, "k9", charterAt); err != nil {
		t.Fatal(err)
	}
	fresh := saveCharter(t, store, 0, "New charter.", "k10")
	if fresh.Workflow != nil {
		t.Fatalf("a charter after a removal starts without the old workflow: %+v", fresh)
	}
}

// A workflow proposal waits like a text proposal, goes stale the same way,
// and applying it saves a lead_proposal version with the text and playbooks
// unchanged.
func TestCharterWorkflowProposalAppliesOnlyTheWorkflow(t *testing.T) {
	db, store, _ := testDB(t)
	seedCharterBot(t, db, "atlas", "p", false)
	saveCharter(t, store, 0, "Lead plans.", "k1")
	savePlaybook(t, store, 1, pa.CharterPlaybook{Title: "Release", Text: "Tag.", BotIDs: []string{"atlas"}}, "k2")
	flow := storeWorkflow("atlas")
	propose := func(base int64, w *pa.CharterWorkflow, turn string) (pa.CharterProposal, error) {
		return store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: base, Kind: pa.CharterProposalWorkflow, Workflow: w, Summary: "Added a flow.", BotID: "lead", TurnID: turn, CreatedAt: charterAt})
	}
	_, err := propose(1, &flow, "t0")
	requireFailure(t, err, pa.StaleVersion, "charter_proposal_base")
	ghost := storeWorkflow("ghost")
	_, err = propose(2, &ghost, "t1")
	requireFailure(t, err, pa.Invalid, "workflow_state_bot:ghost-step")
	_, err = store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 2, Kind: pa.CharterProposalWorkflow, Text: "also text", Workflow: &flow, BotID: "lead", TurnID: "t2", CreatedAt: charterAt})
	requireFailure(t, err, pa.Invalid, "workflow")
	p, err := propose(2, &flow, "t3")
	if err != nil || p.Kind != pa.CharterProposalWorkflow {
		t.Fatalf("propose: %+v %v", p, err)
	}
	if again, err := propose(2, &flow, "t3"); err != nil || again.ID != p.ID {
		t.Fatalf("the same turn proposing again answers the proposal: %+v %v", again, err)
	}
	if _, err = store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 2, Text: "Lead plans well.", Summary: "text", BotID: "lead", TurnID: "t4", CreatedAt: charterAt}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.CharterProposals(testContext, "m", "p")
	if err != nil || len(pending) != 2 || pending[0].Kind != pa.CharterProposalWorkflow || !pa.SameWorkflow(pending[0].Workflow, &flow) || pending[1].Kind != pa.CharterProposalText || pending[1].Workflow != nil {
		t.Fatalf("proposals carry their kind and workflow: %+v %v", pending, err)
	}
	applied, err := store.ApplyCharterProposal(testContext, "m", "p", p.ID, 2, "k3", charterAt)
	if err != nil || applied.Author != pa.CharterByLeadProposal || applied.Text != "Lead plans." || len(applied.Playbooks) != 1 || !pa.SameWorkflow(applied.Workflow, &flow) || applied.DeliveryVersion("idle") != 3 {
		t.Fatalf("an applied workflow proposal: %+v %v", applied, err)
	}
	// The text proposal was made on version 2 and is now stale.
	_, err = store.ApplyCharterProposal(testContext, "m", "p", pending[1].ID, 3, "k4", charterAt)
	requireFailure(t, err, pa.StaleVersion, "charter_proposal_base")
}

// A version row written before workflows existed reads as no workflow, and
// a proposal row from before reads as a text proposal.
func TestCharterRowsFromBeforeWorkflowsReadAsNone(t *testing.T) {
	db, store, _ := testDB(t)
	if _, err := db.Exec(`INSERT INTO project_assistant_charter_versions(machine,project,version,author,reason,created_at,text,idem,request,playbooks,text_version,deliveries) VALUES('m','p',4,'person','',0,'Old charter.','old','old','[]',0,'{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project_assistant_charter_proposals(machine,project,id,base,text,summary,bot,turn,created_at) VALUES('m','p','cp-old',4,'Old proposal.','','lead','t',0)`); err != nil {
		t.Fatal(err)
	}
	old, err := store.Charter(testContext, "m", "p")
	if err != nil || old.Version != 4 || old.Workflow != nil {
		t.Fatalf("an older row: %+v %v", old, err)
	}
	pending, err := store.CharterProposals(testContext, "m", "p")
	if err != nil || len(pending) != 1 || pending[0].Kind != pa.CharterProposalText || pending[0].Workflow != nil {
		t.Fatalf("an older proposal: %+v %v", pending, err)
	}
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
}
