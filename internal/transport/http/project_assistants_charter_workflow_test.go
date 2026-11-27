package http

import (
	"encoding/json"
	"maps"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func httpWorkflow(bot string) map[string]any {
	return map[string]any{
		"states": []map[string]any{{"id": "open", "title": "開交辦卡", "bot_id": "", "kind": "start", "note": ""},
			{"id": "review", "title": "派工・審證據", "bot_id": bot, "kind": "step", "note": "Read the evidence first."},
			{"id": "done", "title": "完成", "bot_id": "", "kind": "done", "note": ""}},
		"transitions": []map[string]any{{"from": "open", "to": "review", "label": "", "kind": "normal"}, {"from": "review", "to": "done", "label": "審過", "kind": "pass"}},
	}
}

// A Session writes the workflow over HTTP on the person's words: it is a
// new version that keeps the text and playbooks, a broken rule names what
// to fix, and reads, history and proposals carry it.
func TestCharterWorkflowRoutes(t *testing.T) {
	c := newConflictFixture(t, "Synthetic fixture")
	project := c.bot.Project.ProjectID
	put := func(expected int64, workflow map[string]any, key string) *httptest.ResponseRecorder {
		return paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/workflow", c.lan,
			map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": key, "expected_version": expected, "workflow": workflow, "reason": "Asked in a Session."})
	}
	saved := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, put(c.version, httpWorkflow(c.key.BotID), "wf-1"))
	if saved.Version != c.version+1 || saved.Workflow == nil || len(saved.Workflow.States) != 3 || saved.Workflow.States[1].BotID != c.key.BotID || saved.Workflow.Transitions[1].Label != "審過" ||
		saved.Text != "## Releases\n\nMerge to main without asking." || saved.Limits.WorkflowStates != pa.MaxCharterWorkflowStates || saved.Limits.WorkflowNoteBytes != pa.MaxCharterWorkflowNoteBytes {
		t.Fatalf("saved: %+v", saved)
	}
	if saved.History[0].Author != contract.ProjectAssistantCharterAuthor(pa.CharterByPerson) || saved.History[0].Reason != "Asked in a Session." {
		t.Fatalf("the version is the person's: %+v", saved.History[0])
	}
	stale := paAssertHTTPFailure(t, put(c.version, httpWorkflow(c.key.BotID), "wf-stale"), 409, pa.StaleVersion)
	if stale.Field != "charter_version" {
		t.Fatalf("stale: %+v", stale)
	}
	ghost := paAssertHTTPFailure(t, put(saved.Version, httpWorkflow("ghost"), "wf-ghost"), 400, pa.Invalid)
	if ghost.Field != "workflow_state_bot:review" {
		t.Fatalf("a Bot outside the Project names its state: %+v", ghost)
	}
	broken := httpWorkflow(c.key.BotID)
	broken["transitions"] = []map[string]any{{"from": "open", "to": "review", "label": "", "kind": "normal"}}
	unreachable := paAssertHTTPFailure(t, put(saved.Version, broken, "wf-broken"), 400, pa.Invalid)
	if unreachable.Field != "workflow_unreachable:done" {
		t.Fatalf("an unreachable state is named: %+v", unreachable)
	}

	read := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, c.request(t, "GET", chartersPath+"/charter?machine_id="+c.key.MachineID+"&project_id="+project+"&version="+strconv.FormatInt(c.version, 10), c.readonly, nil))
	if read.Workflow == nil || len(read.Workflow.States) != 3 || read.Selected == nil || read.Selected.Workflow != nil {
		t.Fatalf("the current workflow, and none on the version before it: %+v %+v", read.Workflow, read.Selected)
	}
	text := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/charter", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "text-2", "expected_version": saved.Version, "text": "## Releases\n\nAsk first.", "reason": "edit"}))
	if text.Workflow == nil || len(text.Workflow.States) != 3 {
		t.Fatalf("an older client's text save keeps the workflow: %+v", text.Workflow)
	}

	flow := pa.CharterWorkflow{States: []pa.WorkflowState{{ID: "open", Title: "Open", Kind: pa.WorkflowStart}, {ID: "done", Title: "Done", Kind: pa.WorkflowDone}},
		Transitions: []pa.WorkflowTransition{{From: "open", To: "done", Kind: pa.WorkflowNormal}}}
	proposal, err := c.store.ProposeCharter(c.ctx, c.key.MachineID, pa.CharterProposal{ProjectID: project, BaseVersion: text.Version, Kind: pa.CharterProposalWorkflow, Workflow: &flow,
		Summary: "Shorter flow.", BotID: "lead", TurnID: "turn-1", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	proposals := paCharterAnswer[contract.ProjectAssistantCharterProposalsResponse](t, c.request(t, "GET", chartersPath+"/proposals?machine_id="+c.key.MachineID+"&project_id="+project, c.readonly, nil))
	if len(proposals.Proposals) != 1 || proposals.Proposals[0].Kind != "workflow" || proposals.Proposals[0].Workflow == nil || len(proposals.Proposals[0].Workflow.States) != 2 || proposals.Proposals[0].Text != "" {
		t.Fatalf("a workflow proposal: %+v", proposals.Proposals)
	}
	applied := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/proposal-apply", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "apply-wf", "proposal_id": proposal.ID, "expected_version": text.Version}))
	if applied.Workflow == nil || len(applied.Workflow.States) != 2 || applied.Text != "## Releases\n\nAsk first." || applied.History[0].Author != contract.ProjectAssistantCharterAuthor(pa.CharterByLeadProposal) {
		t.Fatalf("an applied workflow proposal: %+v", applied)
	}
	restored := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/restore", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "restore-wf", "expected_version": applied.Version, "version": saved.Version}))
	if restored.Workflow == nil || len(restored.Workflow.States) != 3 {
		t.Fatalf("restore brings the workflow back: %+v", restored.Workflow)
	}
	target := paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/change-request", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "cr-bad", "text": "Change it.", "target": "everything"}), 400, pa.Invalid)
	if target.Field != "target" {
		t.Fatalf("an unknown change-request target: %+v", target)
	}
}

// The workflow Cloud word writes under an operating device and is refused
// to a read-only one; the change request carries its target over Cloud.
func TestCharterWorkflowCloudWords(t *testing.T) {
	c := newConflictFixture(t, "Synthetic fixture")
	project := c.bot.Project.ProjectID
	c.pin(t, "owner", adaptercloud.ScopeOperate, false)
	c.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	bridge := cloudops.Bridge{MachineID: c.identity.MachineID, AllowCommands: func() bool { return true }, Router: cloudtransport.Router{
		Handler: c.h, Authorize: cloudtransport.LocalAuthorizer(c.local, c.machine)}}
	sequence := uint64(0)
	word := func(sender, op, request string, fields map[string]any) cloudops.Answer {
		body := map[string]any{"type": "project-assistants." + op, "session": cloudops.MachineReplySession, "request": request, "machine_id": c.key.MachineID, "project_id": project}
		maps.Copy(body, fields)
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		sequence++
		return bridge.Handle(c.ctx, cloudops.Command{Channel: "ctl/" + c.key.MachineID, Class: cloudops.ClassCtl, Sender: sender, Sequence: sequence, Plaintext: raw})
	}
	save := map[string]any{"idempotency_key": "cloud-wf", "reason": "", "expected_version": c.version, "workflow": httpWorkflow(c.key.BotID)}
	if a := word("reader", "charter-workflow-save", "cloud-wf", save); a.Status < 400 {
		t.Fatalf("a read-only device saved a workflow: %d %s", a.Status, a.Payload)
	}
	a := word("owner", "charter-workflow-save", "cloud-wf", save)
	var envelope struct {
		Body contract.ProjectAssistantCharterResponse `json:"body"`
	}
	if a.Status != 200 || json.Unmarshal(a.Payload, &envelope) != nil || envelope.Body.Workflow == nil || len(envelope.Body.Workflow.States) != 3 {
		t.Fatalf("Cloud workflow save: %d %s %s %s", a.Status, a.Code, a.Field, a.Payload)
	}
	bad := httpWorkflow(c.key.BotID)
	bad["states"] = []map[string]any{{"id": "open", "title": "Open", "bot_id": "", "kind": "begin", "note": ""}}
	if a := word("owner", "charter-workflow-save", "cloud-wf-bad", map[string]any{"idempotency_key": "cloud-wf-bad", "reason": "", "expected_version": envelope.Body.Version, "workflow": bad}); a.Status < 400 {
		t.Fatalf("an unknown state kind is malformed on the wire: %d %s", a.Status, a.Payload)
	}
	if a := word("owner", "charter-change-request", "cloud-cr", map[string]any{"idempotency_key": "cloud-cr", "text": "Change it.", "target": "sideways"}); a.Status < 400 {
		t.Fatalf("an unknown target is malformed on the wire: %d %s", a.Status, a.Payload)
	}
}
