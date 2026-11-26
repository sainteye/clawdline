package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paCharterWrite(t *testing.T, f *paServerFixture, method, path, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r.Host = "127.0.0.1"
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", body["idempotency_key"].(string))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func paCharterAnswer[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	paServerOK(t, w)
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("private response policy: %s", w.Body)
	}
	return out
}

// Any device that may act on the machine's Bots reads and edits a Project's
// charter by version: a stale base is refused, a repeated key answers its own
// version, an old version is restored as a new one, and proposals only change
// the charter when applied against the version they were written for.
func TestProjectAssistantChartersAreVersionedOverHTTP(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	m, p := f.key.MachineID, "project-one"
	read := chartersPath + "/charter?machine_id=" + m + "&project_id=" + p + "&version=0"
	empty := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, f.request(t, "GET", read, f.readonly, nil))
	if empty.Version != 0 || empty.Text != "" || empty.History == nil || empty.Limits.TextBytes != pa.MaxCharterTextBytes {
		t.Fatalf("a Project without a charter: %+v", empty)
	}
	save := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "save-1", "expected_version": 0, "text": "Lead plans; makers build.", "reason": "first"}
	if w := paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.readonly, save); w.Code != 403 {
		t.Fatalf("a read-only device saved a charter: HTTP %d %s", w.Code, w.Body)
	}
	saved := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, save))
	if saved.Version != 1 || saved.Text != "Lead plans; makers build." || len(saved.History) != 1 || saved.History[0].Author != "person" || saved.History[0].Reason != "first" {
		t.Fatalf("saved: %+v", saved)
	}
	again := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, save))
	if again.Version != 1 {
		t.Fatalf("a repeated save answers its own version: %+v", again)
	}
	stale := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "save-2", "expected_version": 0, "text": "Other."}
	paAssertHTTPFailure(t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, stale), 409, pa.StaleVersion)
	stale["expected_version"] = 1
	paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, stale))
	restore := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "restore-1", "expected_version": 2, "version": 1}
	restored := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "POST", chartersPath+"/restore", f.lan, restore))
	if restored.Version != 3 || restored.Text != "Lead plans; makers build." || restored.History[0].Author != "restore" {
		t.Fatalf("restored: %+v", restored)
	}
	old := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, f.request(t, "GET", chartersPath+"/charter?machine_id="+m+"&project_id="+p+"&version=2", f.readonly, nil))
	if old.Selected == nil || old.Selected.Version != 2 || old.Selected.Text != "Other." || old.Version != 3 {
		t.Fatalf("an old version: %+v", old)
	}
	big := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "big", "expected_version": 3, "text": strings.Repeat("x", pa.MaxCharterTextBytes+1)}
	paAssertHTTPFailure(t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, big), 429, pa.CapacityFull)

	store := f.s.projectAssistantFloorsStore()
	current, err := store.ProposeCharter(t.Context(), m, pa.CharterProposal{ProjectID: p, BaseVersion: 3, Text: "Proposed.", Summary: "Replace all", BotID: "lead", TurnID: "t1", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	older, err := store.ProposeCharter(t.Context(), m, pa.CharterProposal{ProjectID: p, BaseVersion: 3, Text: "Also proposed.", BotID: "lead", TurnID: "t2", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	proposals := paCharterAnswer[contract.ProjectAssistantCharterProposalsResponse](t, f.request(t, "GET", chartersPath+"/proposals?machine_id="+m+"&project_id="+p, f.readonly, nil))
	if len(proposals.Proposals) != 2 || proposals.Proposals[0].Stale {
		t.Fatalf("proposals: %+v", proposals)
	}
	apply := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "apply-1", "proposal_id": current.ID, "expected_version": 3}
	applied := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "POST", chartersPath+"/proposal-apply", f.lan, apply))
	if applied.Version != 4 || applied.Text != "Proposed." || applied.History[0].Author != "lead_proposal" || applied.PendingProposals != 1 {
		t.Fatalf("applied: %+v", applied)
	}
	proposals = paCharterAnswer[contract.ProjectAssistantCharterProposalsResponse](t, f.request(t, "GET", chartersPath+"/proposals?machine_id="+m+"&project_id="+p, f.readonly, nil))
	if len(proposals.Proposals) != 1 || !proposals.Proposals[0].Stale {
		t.Fatalf("the remaining proposal is stale: %+v", proposals)
	}
	staleApply := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "apply-2", "proposal_id": older.ID, "expected_version": 4}
	paAssertHTTPFailure(t, paCharterWrite(t, f, "POST", chartersPath+"/proposal-apply", f.lan, staleApply), 409, pa.StaleVersion)
	dismiss := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "dismiss-1", "proposal_id": older.ID}
	left := paCharterAnswer[contract.ProjectAssistantCharterProposalsResponse](t, paCharterWrite(t, f, "POST", chartersPath+"/proposal-dismiss", f.lan, dismiss))
	if len(left.Proposals) != 0 {
		t.Fatalf("dismissed: %+v", left)
	}
	list := paCharterAnswer[contract.ProjectAssistantChartersResponse](t, f.request(t, "GET", chartersPath+"?machine_id="+m, f.readonly, nil))
	if len(list.Charters) != 1 || list.Charters[0].ProjectID != p || list.Charters[0].Version != 4 || list.ProjectsWithoutCharter == nil {
		t.Fatalf("listing: %+v", list)
	}
	readers := paCharterAnswer[contract.ProjectAssistantCharterReadersResponse](t, f.request(t, "GET", chartersPath+"/readers?machine_id="+m+"&project_id="+p, f.readonly, nil))
	if readers.Version != 4 || readers.Readers == nil {
		t.Fatalf("readers: %+v", readers)
	}
	paAssertHTTPFailure(t, f.request(t, "GET", chartersPath+"?machine_id=another-machine", f.local, nil), 403, pa.ScopeDenied)
	paAssertHTTPFailure(t, f.request(t, "GET", chartersPath+"/unknown?machine_id="+m, f.local, nil), 400, pa.Invalid)
}

// A charter is removed only at the version it was read at. The Project then
// reads as having none, its proposals go, the removal stays in the history,
// and a later charter is numbered after it. A removal cannot be restored.
func TestProjectAssistantCharterRemovalOverHTTP(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	m, p := f.key.MachineID, "project-one"
	save := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "save-1", "expected_version": 0, "text": "Lead plans.", "reason": "first"}
	paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, save))
	if _, err := f.s.projectAssistantFloorsStore().ProposeCharter(t.Context(), m, pa.CharterProposal{ProjectID: p, BaseVersion: 1, Text: "Proposed.", BotID: "lead", TurnID: "t1", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	remove := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "remove-1", "expected_version": 1}
	if w := paCharterWrite(t, f, "POST", chartersPath+"/remove", f.readonly, remove); w.Code != 403 {
		t.Fatalf("a read-only device removed a charter: HTTP %d %s", w.Code, w.Body)
	}
	stale := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "remove-stale", "expected_version": 2}
	paAssertHTTPFailure(t, paCharterWrite(t, f, "POST", chartersPath+"/remove", f.lan, stale), 409, pa.StaleVersion)
	gone := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "POST", chartersPath+"/remove", f.lan, remove))
	if gone.Version != 0 || gone.Text != "" || gone.PendingProposals != 0 || len(gone.History) != 2 || gone.History[0].Author != "removed" || gone.History[0].Version != 2 {
		t.Fatalf("removed: %+v", gone)
	}
	again := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "POST", chartersPath+"/remove", f.lan, remove))
	if again.Version != 0 || len(again.History) != 2 {
		t.Fatalf("a repeated removal answers its own effect: %+v", again)
	}
	proposals := paCharterAnswer[contract.ProjectAssistantCharterProposalsResponse](t, f.request(t, "GET", chartersPath+"/proposals?machine_id="+m+"&project_id="+p, f.readonly, nil))
	if proposals.Version != 0 || len(proposals.Proposals) != 0 {
		t.Fatalf("a removal drops the proposals: %+v", proposals)
	}
	list := paCharterAnswer[contract.ProjectAssistantChartersResponse](t, f.request(t, "GET", chartersPath+"?machine_id="+m, f.readonly, nil))
	if len(list.Charters) != 0 {
		t.Fatalf("a removed charter is not listed: %+v", list)
	}
	restore := map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "restore-removal", "expected_version": 0, "version": 2}
	paAssertHTTPFailure(t, paCharterWrite(t, f, "POST", chartersPath+"/restore", f.lan, restore), 404, pa.NotFound)
	save["idempotency_key"], save["text"] = "save-2", "Lead plans again."
	made := paCharterAnswer[contract.ProjectAssistantCharterResponse](t, paCharterWrite(t, f, "PUT", chartersPath+"/charter", f.lan, save))
	if made.Version != 3 || made.Text != "Lead plans again." {
		t.Fatalf("a charter made after a removal is numbered after it: %+v", made)
	}
	paAssertHTTPFailure(t, paCharterWrite(t, f, "POST", chartersPath+"/remove", f.lan, map[string]any{"machine_id": m, "project_id": p, "idempotency_key": "remove-0", "expected_version": 0}), 400, pa.Invalid)
}

// The settings list offers a charter for each Project with Bots, never for a
// Bot's own personal workspace, and the Bots read name their Project's kind.
func TestProjectAssistantChartersListingOmitsPersonalWorkspaces(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	bots, err := f.s.projectAssistantFloorsStore().CharterBots(t.Context(), f.key.MachineID)
	if err != nil || len(bots) != 1 || bots[0].ProjectKind != pa.PlaceProject {
		t.Fatalf("a Bot's Project kind: %+v %v", bots, err)
	}
	place := bots[0]
	personal := projectassistantstore.CharterBot{BotID: "atlas", ProjectID: "bot-workspace-atlas", ProjectKind: pa.PersonalWorkspace}
	other := projectassistantstore.CharterBot{BotID: "orbit", ProjectID: "project-two", ProjectKind: pa.PlaceProject}
	got := projectsWithoutCharter([]projectassistantstore.CharterBot{personal, place, place, other}, map[string]bool{"project-two": true})
	if len(got) != 1 || got[0] != place.ProjectID {
		t.Fatalf("Projects offered a charter: %v", got)
	}
	list := paCharterAnswer[contract.ProjectAssistantChartersResponse](t, f.request(t, "GET", chartersPath+"?machine_id="+f.key.MachineID, f.readonly, nil))
	if len(list.ProjectsWithoutCharter) != 1 || list.ProjectsWithoutCharter[0] != place.ProjectID {
		t.Fatalf("listing: %+v", list.ProjectsWithoutCharter)
	}
}
