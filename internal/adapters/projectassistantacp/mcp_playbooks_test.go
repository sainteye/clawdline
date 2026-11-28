package projectassistantacp

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type playbookMCPFixture struct {
	mcpTestTools
	enabled bool
	last    app.PlaybookEdit
}

func (f *playbookMCPFixture) PlaybooksAvailable(context.Context) error {
	if !f.enabled {
		return refusal(pa.ScopeDenied, "propose_playbook_edits")
	}
	return nil
}

func (f *playbookMCPFixture) ReadPlaybooks(ctx context.Context, id string) (app.PlaybookRead, error) {
	return app.PlaybookRead{Version: 2, Playbooks: []pa.CharterPlaybook{{ID: id, Title: "Review", Text: "Old steps"}}}, f.PlaybooksAvailable(ctx)
}

func (f *playbookMCPFixture) ProposePlaybookEdit(ctx context.Context, q app.PlaybookEdit) (pa.CharterProposal, error) {
	if err := f.PlaybooksAvailable(ctx); err != nil {
		return pa.CharterProposal{}, err
	}
	f.last = q
	return pa.CharterProposal{ID: "cp-synthetic", BaseVersion: q.BaseVersion}, nil
}

func TestMCPPlaybookProposalDiscoveryAndCalls(t *testing.T) {
	tools := &playbookMCPFixture{}
	f := newMCPFixture(t, tools)
	f.initialize(t)
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	_, raw := f.post(t, list, nil)
	if strings.Contains(string(raw), `"name":"propose_playbook_edit"`) || strings.Contains(string(raw), `"name":"read_playbooks"`) {
		t.Fatalf("tools visible outside the Project: %s", raw)
	}
	edit := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"propose_playbook_edit","arguments":{"id":"pb-example","base_version":2,"title":"Review","text":"New steps","reason":"Clarify review."}}}`
	_, raw = f.post(t, edit, nil)
	if !strings.Contains(string(raw), string(pa.ScopeDenied)) || tools.last.ID != "" {
		t.Fatalf("out-of-scope call: %s", raw)
	}
	tools.enabled = true
	_, raw = f.post(t, list, nil)
	if !strings.Contains(string(raw), `"name":"propose_playbook_edit"`) || !strings.Contains(string(raw), `"name":"read_playbooks"`) {
		t.Fatalf("tools missing in the Project: %s", raw)
	}
	_, raw = f.post(t, strings.Replace(edit, `"text":"New steps",`, "", 1), nil)
	if !strings.Contains(string(raw), string(pa.Invalid)) || tools.last.ID != "" {
		t.Fatalf("missing text was accepted: %s", raw)
	}
	_, raw = f.post(t, edit, nil)
	if strings.Contains(string(raw), `"isError":true`) || !strings.Contains(string(raw), "waiting_for_person") || tools.last.Text != "New steps" || tools.last.ID != "pb-example" || tools.last.BaseVersion != 2 {
		t.Fatalf("authorized call: %s, %+v", raw, tools.last)
	}
}
