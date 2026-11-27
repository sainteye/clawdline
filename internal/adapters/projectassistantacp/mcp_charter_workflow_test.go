package projectassistantacp

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type mcpWorkflowFixtureTools struct {
	mcpTestTools
	lead bool
	last app.CharterWorkflowProposal
}

func (w *mcpWorkflowFixtureTools) CharterProposalAvailable(context.Context) error {
	if !w.lead {
		return &pa.Failure{Code: pa.ScopeDenied, Field: "lead_role"}
	}
	return nil
}

func (w *mcpWorkflowFixtureTools) ProposeCharterEdit(context.Context, app.CharterEditProposal) (pa.CharterProposal, error) {
	return pa.CharterProposal{}, nil
}

func (w *mcpWorkflowFixtureTools) ProposeCharterWorkflow(_ context.Context, q app.CharterWorkflowProposal) (pa.CharterProposal, error) {
	w.last = q
	if err := pa.ValidateCharterWorkflow(q.Workflow, nil); err != nil {
		return pa.CharterProposal{}, err
	}
	return pa.CharterProposal{ID: "cp-synthetic", BaseVersion: q.BaseVersion}, nil
}

// The lead is offered propose_charter_workflow beside propose_charter_edit; a
// refusal tells the Bot which rule and which state broke; a Bot that is not
// the lead neither sees nor may call it.
func TestMCPCharterWorkflowToolIsTheLeadsAndExplainsRefusals(t *testing.T) {
	tools := &mcpWorkflowFixtureTools{lead: true}
	f := newMCPFixture(t, tools)
	f.initialize(t)
	status, raw := f.post(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	if status != 200 || !strings.Contains(string(raw), `"name":"propose_charter_workflow"`) || !strings.Contains(string(raw), `"name":"propose_charter_edit"`) {
		t.Fatalf("the lead is offered the workflow tool: %d %s", status, raw)
	}
	good := `{"project":"p","base_version":2,"summary":"Added review.","workflow":{"states":[{"id":"open","title":"Open","bot_id":"","kind":"start","note":""},{"id":"done","title":"Done","bot_id":"","kind":"done","note":""}],"transitions":[{"from":"open","to":"done","label":"","kind":"normal"}]}}`
	status, raw = f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"propose_charter_workflow","arguments":`+good+`}}`, nil)
	if status != 200 || strings.Contains(string(raw), `"isError":true`) || !strings.Contains(string(raw), `waiting_for_person`) || tools.last.BaseVersion != 2 || len(tools.last.Workflow.States) != 2 {
		t.Fatalf("a valid proposal: %d %s %+v", status, raw, tools.last)
	}
	bad := strings.Replace(good, `"to":"done"`, `"to":"nowhere"`, 1)
	status, raw = f.post(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"propose_charter_workflow","arguments":`+bad+`}}`, nil)
	if status != 200 || !strings.Contains(string(raw), `"isError":true`) || !strings.Contains(string(raw), `workflow_transition_to:open-\\u003enowhere`) {
		t.Fatalf("a refusal names the transition: %d %s", status, raw)
	}

	other := &mcpWorkflowFixtureTools{}
	g := newMCPFixture(t, other)
	g.initialize(t)
	status, raw = g.post(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	if status != 200 || strings.Contains(string(raw), "propose_charter_workflow") {
		t.Fatalf("a Bot that is not the lead is not offered it: %s", raw)
	}
	status, raw = g.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"propose_charter_workflow","arguments":`+good+`}}`, nil)
	if status != 200 || !strings.Contains(string(raw), `"isError":true`) || !strings.Contains(string(raw), `lead_role`) || other.last.Project != "" {
		t.Fatalf("a Bot that is not the lead is refused: %d %s", status, raw)
	}
}
