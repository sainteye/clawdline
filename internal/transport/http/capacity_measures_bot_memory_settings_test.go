package http

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestBotCharterCapacityReadsDurableUsage(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	st := f.s.projectAssistantFloorsStore()
	for _, w := range []projectassistantstore.CharterWrite{
		{MachineID: "one", ProjectID: "p1", IdempotencyKey: "first", Text: "Earlier", Reason: "章程"},
		{MachineID: "one", ProjectID: "p1", IdempotencyKey: "second", Text: "Current", ExpectedVersion: 1},
		{MachineID: "one", ProjectID: "p2", IdempotencyKey: "first", Text: "Second"},
		{MachineID: "two", ProjectID: "p1", IdempotencyKey: "first", Text: "Remote"},
	} {
		w.At = time.Now()
		w.Author = pa.CharterByPerson
		if _, err := st.SaveCharter(t.Context(), w); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []pa.CharterProposal{
		{ProjectID: "p1", TurnID: "first", BaseVersion: 2, Text: "團隊章程", Summary: "Short"},
		{ProjectID: "p1", TurnID: "second", BaseVersion: 2, Text: "Other"},
		{ProjectID: "p3", TurnID: "third", Text: "Draft"},
	} {
		p.BotID, p.CreatedAt = "lead", time.Now()
		if _, err := st.ProposeCharter(t.Context(), "one", p); err != nil {
			t.Fatal(err)
		}
	}
	for i, p := range []pa.CharterPlaybook{{Title: "發布", Text: "Tag", BotIDs: []string{"a", "b"}}, {Title: "Review", Text: "Two approvals"}} {
		if _, err := st.SaveCharterPlaybook(t.Context(), projectassistantstore.CharterPlaybookWrite{MachineID: "two", ProjectID: "p1", ExpectedVersion: int64(i + 1), Playbook: p, IdempotencyKey: p.Title, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	flow := pa.CharterWorkflow{States: []pa.WorkflowState{{ID: "open", Title: "開始", Kind: pa.WorkflowStart, Note: "備註"}, {ID: "finished", Title: "Done", Kind: pa.WorkflowDone}},
		Transitions: []pa.WorkflowTransition{{From: "open", To: "finished", Label: "通過審查", Kind: pa.WorkflowPass}}}
	if _, err := st.SaveCharterWorkflow(t.Context(), projectassistantstore.CharterWorkflowWrite{MachineID: "one", ProjectID: "p2", ExpectedVersion: 1, Workflow: flow, IdempotencyKey: "wf", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	measures := f.s.capacityMeasures()
	for name, want := range map[string]int64{
		"bots.charter_workflow_states":      2,
		"bots.charter_workflow_transitions": 1,
		"bots.charter_workflow_title_bytes": int64(len("通過審查")),
		"bots.charter_workflow_note_bytes":  int64(len("備註")),
		"bots.charter_workflow_id_bytes":    int64(len("finished")),
		"bots.charter_playbooks":            2,
		"bots.charter_playbook_text_bytes":  int64(len("Two approvals")),
		"bots.charter_playbook_title_bytes": int64(len("發布")),
		"bots.charter_playbook_bots":        2,
		"bots.charter_playbook_id_bytes":    int64(len("pb-") + 16),
		"bots.charter_document_bytes":       int64(len("Remote") + len("Tag") + len("Two approvals")),
		"bots.charter_versions":             3,
		"bots.charter_proposals":            2,
		"bots.charter_projects":             2,
		"bots.charter_text_bytes":           int64(len("團隊章程")),
		"bots.charter_reason_bytes":         int64(len("章程")),
	} {
		if got := measures[name](); !got.Known || got.Used != want {
			t.Errorf("%s = %+v; want known usage %d", name, got, want)
		}
	}
}

func TestBotSettingsCapacityReadsDurableValuesAndUnknown(t *testing.T) {
	f := newPAServerFixture(t, true)
	measures := f.s.projectAssistantCapacityMeasures()
	for _, name := range []string{"bots.config_options", "bots.config_value_bytes", "bots.login_profiles", "bots.extensions"} {
		r := measures[name]()
		if !r.Known || r.Used != 0 {
			t.Fatal("absent optional settings", name, r)
		}
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Bot.runtime_settings',json('{"model":"synthetic-large","options":{"effort":"high","fast":"true"}}'))`); err != nil {
		t.Fatal(err)
	}
	if r := measures["bots.config_options"](); !r.Known || r.Used != 2 {
		t.Fatal("real durable options", r)
	}
	if r := measures["bots.config_value_bytes"](); !r.Known || r.Used != 15 {
		t.Fatal("real durable UTF-8 value bytes", r)
	}
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_remove(CAST(data AS TEXT),'$.Bot.runtime_settings')`); err != nil {
		t.Fatal(err)
	}
	if r := measures["bots.config_options"](); !r.Known || r.Used != 0 {
		t.Fatal("cleared settings", r)
	}
	if _, e := db.Exec(`INSERT INTO project_assistant_login_profiles(source,id,agent,name,state,revision,removed) VALUES('synthetic','one','codex','Fixture one','signed_out',1,0),('synthetic','two','codex','Fixture two','signed_out',1,0)`); e != nil {
		t.Fatal(e)
	}
	if r := measures["bots.login_profiles"](); !r.Known || r.Used != 2 {
		t.Fatal("real persisted profile count", r)
	}
	if _, e := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Bot.extensions',json('{"skills":[{"id":"one","fingerprint":"synthetic"}],"connectors":[{"id":"two","fingerprint":"synthetic"}]}'))`); e != nil {
		t.Fatal(e)
	}
	if r := measures["bots.extensions"](); !r.Known || r.Used != 2 {
		t.Fatal("real persisted extension count", r)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bots.config_options", "bots.config_value_bytes", "bots.login_profiles", "bots.extensions"} {
		r := measures[name]()
		if r.Known || r.Err == "" {
			t.Fatal("unreadable settings invented zero", name, r)
		}
	}
}
