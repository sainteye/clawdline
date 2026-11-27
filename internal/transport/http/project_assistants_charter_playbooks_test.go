package http

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func (c *conflictFixture) playbook(t *testing.T, expected int64, playbook map[string]any, key string) contract.ProjectAssistantCharterResponse {
	t.Helper()
	return paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/playbook", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": c.bot.Project.ProjectID, "idempotency_key": key, "expected_version": expected, "playbook": playbook, "reason": ""}))
}

// The person creates, edits and removes a playbook over HTTP. The charter
// answers it with its Bots; the readers say who has heard its latest content.
func TestCharterPlaybookRoutes(t *testing.T) {
	c := newConflictFixture(t, "Synthetic fixture")
	project := c.bot.Project.ProjectID
	created := c.playbook(t, c.version, map[string]any{"title": "Release", "text": "Tag, then ship.", "bot_ids": []string{c.key.BotID}}, "pb-1")
	if created.Version != c.version+1 || len(created.Playbooks) != 1 || created.Playbooks[0].Title != "Release" || created.Playbooks[0].BotIds[0] != c.key.BotID ||
		created.Text != "## Releases\n\nMerge to main without asking." || created.Limits.Playbooks != pa.MaxCharterPlaybooks || created.Limits.DocumentBytes != pa.MaxCharterDocumentBytes {
		t.Fatalf("created: %+v", created)
	}
	id := created.Playbooks[0].ID

	// A stale base, an unknown Bot and a Bot of another Project are refused.
	stale := paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/playbook", c.lan, map[string]any{"machine_id": c.key.MachineID, "project_id": project,
		"idempotency_key": "pb-stale", "reason": "", "expected_version": c.version, "playbook": map[string]any{"title": "Late", "text": "", "bot_ids": []string{}}}), 409, pa.StaleVersion)
	if stale.Field != "charter_version" {
		t.Fatalf("stale field: %+v", stale)
	}
	ghost := paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/playbook", c.lan, map[string]any{"machine_id": c.key.MachineID, "project_id": project,
		"idempotency_key": "pb-ghost", "reason": "", "expected_version": created.Version, "playbook": map[string]any{"title": "Ghost", "text": "", "bot_ids": []string{"ghost"}}}), 400, pa.Invalid)
	if ghost.Field != "bot_ids" {
		t.Fatalf("unknown Bot field: %+v", ghost)
	}
	paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/charter", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": "other-project", "idempotency_key": "other-charter", "expected_version": 0, "text": "Other.", "reason": "other"}))
	foreign := paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/playbook", c.lan, map[string]any{"machine_id": c.key.MachineID, "project_id": "other-project",
		"idempotency_key": "pb-foreign", "reason": "", "expected_version": 1, "playbook": map[string]any{"title": "Foreign", "text": "", "bot_ids": []string{c.key.BotID}}}), 400, pa.Invalid)
	if foreign.Field != "bot_ids" {
		t.Fatalf("another Project's Bot field: %+v", foreign)
	}
	none := paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/playbook", c.lan, map[string]any{"machine_id": c.key.MachineID, "project_id": "no-charter",
		"idempotency_key": "pb-none", "reason": "", "expected_version": 0, "playbook": map[string]any{"title": "Orphan", "text": "", "bot_ids": []string{}}}), 400, pa.Invalid)
	if none.Field != "charter" {
		t.Fatalf("a Project without a charter: %+v", none)
	}

	// An older client's text save keeps the playbook.
	saved := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "PUT", chartersPath+"/charter", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "text-2", "expected_version": created.Version, "text": "## Releases\n\nAsk first.", "reason": "edit"}))
	if len(saved.Playbooks) != 1 || saved.Playbooks[0].ID != id {
		t.Fatalf("a text save keeps the playbooks: %+v", saved.Playbooks)
	}

	readers := func() contract.ProjectAssistantCharterReader {
		answer := paCharterAnswer[contract.ProjectAssistantCharterReadersResponse](t, c.request(t, "GET", chartersPath+"/readers?machine_id="+c.key.MachineID+"&project_id="+project, c.readonly, nil))
		for _, r := range answer.Readers {
			if r.BotID == c.key.BotID {
				return r
			}
		}
		t.Fatalf("no reader row: %+v", answer)
		return contract.ProjectAssistantCharterReader{}
	}
	if r := readers(); r.DeliveryVersion != saved.Version || len(r.PlaybookIds) != 1 || r.PlaybookIds[0] != id || r.Version >= r.DeliveryVersion {
		t.Fatalf("the reader has not heard the latest playbook: %+v", r)
	}
	if err := c.store.RecordCharterRead(c.ctx, c.key.MachineID, project, c.key.BotID, saved.Version, "turn-read", time.Now()); err != nil {
		t.Fatal(err)
	}
	edited := c.playbook(t, saved.Version, map[string]any{"id": id, "title": "Release", "text": "Tag, then ship. Then announce.", "bot_ids": []string{}}, "pb-edit")
	if r := readers(); r.DeliveryVersion != edited.Version || len(r.PlaybookIds) != 0 {
		t.Fatalf("unassigning moves the Bot's delivery: %+v", r)
	}
	selected := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, c.request(t, "GET", chartersPath+"/charter?machine_id="+c.key.MachineID+"&project_id="+project+"&version="+strconv.FormatInt(created.Version, 10), c.readonly, nil))
	if selected.Selected == nil || len(selected.Selected.Playbooks) != 1 || selected.Selected.Playbooks[0].Text != "Tag, then ship." {
		t.Fatalf("a requested version answers its playbooks: %+v", selected.Selected)
	}
	removed := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/playbook-remove", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "pb-remove", "expected_version": edited.Version, "id": id}))
	if removed.Version != edited.Version+1 || len(removed.Playbooks) != 0 {
		t.Fatalf("removed: %+v", removed)
	}
	paAssertHTTPFailure(t, paCharterWrite(t, c.paServerFixture, "POST", chartersPath+"/playbook-remove", c.lan,
		map[string]any{"machine_id": c.key.MachineID, "project_id": project, "idempotency_key": "pb-remove-stale", "expected_version": edited.Version, "id": id}), 409, pa.StaleVersion)
}

// The largest playbook write the bounds allow fits the charter body bound,
// even when every character is escaped.
func TestCharterBodyCoversAFullPlaybookWrite(t *testing.T) {
	bots := make([]string, pa.MaxCharterPlaybookBots)
	for i := range bots {
		bots[i] = strings.Repeat("\x01", pa.MaxTowerFloorProjectBytes)
	}
	raw, err := json.Marshal(contract.ProjectAssistantCharterPlaybookSaveRequest{MachineID: strings.Repeat("m", 200), ProjectID: strings.Repeat("\x01", pa.MaxTowerFloorProjectBytes),
		IdempotencyKey: strings.Repeat("k", 200), ExpectedVersion: 1 << 53, Reason: strings.Repeat("\x01", pa.MaxCharterReasonBytes),
		Playbook: contract.ProjectAssistantCharterPlaybookInput{ID: strings.Repeat("p", 64), Title: strings.Repeat("\x01", pa.MaxCharterPlaybookTitleBytes), Text: strings.Repeat("\x01", pa.MaxCharterPlaybookTextBytes), BotIds: bots}})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > pa.MaxCharterBodyBytes {
		t.Fatalf("a full playbook write is %d bytes, past the %d byte body bound", len(raw), pa.MaxCharterBodyBytes)
	}
}

// Following the charter for a playbook sentence found once saves the
// replacement as the person's new charter version; the playbook Cloud words
// write and an operating device is required.
func TestPlaybookConflictsAndCloudWords(t *testing.T) {
	c := newConflictFixture(t, "Synthetic fixture")
	project := c.bot.Project.ProjectID
	created := c.playbook(t, c.version, map[string]any{"title": "Release", "text": "Ask before every merge. Tag it.", "bot_ids": []string{c.key.BotID}}, "pb-1")
	id := created.Playbooks[0].ID
	current, err := c.store.Charter(c.ctx, c.key.MachineID, project)
	if err != nil {
		t.Fatal(err)
	}
	// A report against the charter version, not this Bot's delivery, is stale.
	_, err = c.store.ReportCharterConflict(c.ctx, c.key.MachineID, pa.CharterConflict{ProjectID: project, BotID: c.key.BotID, TurnID: "t0", Version: c.version, Source: pa.ConflictInPlaybook,
		Path: id, BotSentence: "Ask before every merge.", CharterSentence: "Merge to main without asking.", Section: "Releases", CreatedAt: time.Now()})
	if pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a report at an older delivery: %v", err)
	}
	report, err := c.store.ReportCharterConflict(c.ctx, c.key.MachineID, pa.CharterConflict{ProjectID: project, BotID: c.key.BotID, TurnID: "t1", Version: current.DeliveryVersion(c.key.BotID),
		Source: pa.ConflictInPlaybook, Path: id, BotSentence: "Ask before every merge.", CharterSentence: "Merge to main without asking.", Section: "Releases", Suggested: "Merge to main without asking.", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	conflicts := paCharterAnswer[contract.ProjectAssistantCharterConflictsResponse](t, c.request(t, "GET", chartersPath+"/conflicts?machine_id="+c.key.MachineID+"&project_id="+project, c.readonly, nil))
	if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].Outdated || conflicts.Conflicts[0].Source != "playbook" {
		t.Fatalf("a report at the Bot's delivery version is current: %+v", conflicts.Conflicts)
	}
	answer := c.resolve(t, report.ID, "follow_charter", "follow-pb")
	if conflictState(t, answer, report.ID) != contract.ProjectAssistantCharterConflictStateFixed || c.fixTurn(t, report.ID) != nil {
		t.Fatalf("a playbook sentence found once is fixed by the daemon: %+v", answer.Conflicts)
	}
	after, err := c.store.Charter(c.ctx, c.key.MachineID, project)
	if err != nil || after.Version != created.Version+1 || after.Author != pa.CharterByPerson || after.Playbooks[0].Text != "Merge to main without asking. Tag it." {
		t.Fatalf("the playbook after follow_charter: %+v %v", after, err)
	}

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
	save := map[string]any{"idempotency_key": "cloud-pb", "reason": "", "expected_version": after.Version, "playbook": map[string]any{"title": "Review", "text": "Two approvals.", "bot_ids": []string{c.key.BotID}}}
	if a := word("reader", "charter-playbook-save", "cloud-pb", save); a.Status < 400 {
		t.Fatalf("a read-only device saved a playbook: %d %s", a.Status, a.Payload)
	}
	a := word("owner", "charter-playbook-save", "cloud-pb", save)
	var envelope struct {
		Body contract.ProjectAssistantCharterResponse `json:"body"`
	}
	if a.Status != 200 || json.Unmarshal(a.Payload, &envelope) != nil || len(envelope.Body.Playbooks) != 2 {
		t.Fatalf("Cloud playbook save: %d %s %s %s", a.Status, a.Code, a.Field, a.Payload)
	}
	review := envelope.Body.Playbooks[1].ID
	a = word("owner", "charter-playbook-remove", "cloud-pb-remove", map[string]any{"idempotency_key": "cloud-pb-remove", "expected_version": envelope.Body.Version, "id": review})
	if a.Status != 200 || json.Unmarshal(a.Payload, &envelope) != nil || len(envelope.Body.Playbooks) != 1 {
		t.Fatalf("Cloud playbook remove: %d %s %s %s", a.Status, a.Code, a.Field, a.Payload)
	}
}
