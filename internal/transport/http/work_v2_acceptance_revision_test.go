package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func revisionBody(version int64, owner, run, criteria string) string {
	b, _ := json.Marshal(map[string]any{"expected_version": version, "session_id": owner,
		"via": map[string]string{"run": run}, "acceptance_criteria": criteria})
	return string(b)
}

func TestAgentAcceptanceRevisionRequiresCurrentPersonsInstructionAndReplays(t *testing.T) {
	s, p, project := sessionItemServer(t)
	owner := p.s.ConversationID
	createRun := issueTestRun(t, s, p, "Create the release notes item")
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		selfAssigned(t, sessionItemBody(t, owner, createRun, project, "issue")), "revision-create"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var initial struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	item := initial.Item
	path := "/v1/work/v2/agent/items/" + item.ID + "/acceptance-revision"
	request := func(body, key string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path, body, key))
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var got struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		return got.Error
	}
	baseline := func() {
		t.Helper()
		got, err := s.workV2().Item(context.Background(), item.ID)
		if err != nil || got.Item.AcceptanceVersion != item.AcceptanceVersion || got.Item.AcceptanceCriteria != item.AcceptanceCriteria {
			t.Fatalf("refusal mutated acceptance: %+v %v", got.Item, err)
		}
	}
	// Run clocks are injected solely in this isolated daemon. A phase or
	// condition update after a valid message must not make it stale.
	issueAt := time.Unix(item.UpdatedAt+3, 0)
	s.runs().Now = func() time.Time { return issueAt }
	valid := issueTestRun(t, s, p, "Please revise the acceptance of this Issue to require a published note")
	invalid := issueTestRun(t, s, p, "What is the weather today?")
	foreign := issueTestRun(t, s, p, "Revise acceptance for 00000000-0000-4000-8000-000000000099")
	s.runs().Now = func() time.Time { return time.Now() }
	// This status write happens after the instruction. It changes the item
	// timestamp and version, but not the acceptance version's source time.
	s.workV2().Now = func() time.Time { return issueAt.Add(time.Second) }
	blocked := work.ConditionBlocked
	status, err := s.workV2().Edit(context.Background(), item.ID, app.EditWorkV2{ExpectedVersion: item.Version,
		OwnerSession: owner, Actor: owner, Condition: &blocked}, nil)
	if err != nil {
		t.Fatal(err)
	}
	item.Version, item.UpdatedAt = status.Item.Version, status.Item.UpdatedAt.Unix()
	otherSession := p.s
	otherSession.ID = "pane-other"
	otherSession.ConversationID = "10000000-0000-4000-8000-000000000098"
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{p.s, otherSession} }
	otherRun, err := s.runs().Issue(context.Background(), otherSession, "local", "Revise acceptance of this Issue")
	if err != nil {
		t.Fatal(err)
	}
	s.runs().Now = func() time.Time { return time.Now().Add(-25 * time.Hour) }
	expired := issueTestRun(t, s, p, "Revise acceptance of this Issue")
	s.runs().Now = func() time.Time { return time.Now() }
	cases := []struct{ name, body, code string }{
		{"no run", revisionBody(item.Version, owner, "", "new"), "run_unknown"},
		{"other session run", revisionBody(item.Version, owner, otherRun.ID, "new"), "run_other_session"},
		{"expired run", revisionBody(item.Version, owner, expired, "new"), "run_expired"},
		{"unrelated message", revisionBody(item.Version, owner, invalid, "new"), "run_not_acceptance_instruction"},
		{"other item", revisionBody(item.Version, owner, foreign, "new"), "run_not_acceptance_instruction"},
		{"wrong owner", revisionBody(item.Version, otherSession.ConversationID, valid, "new"), "not_item_owner"},
		{"stale version", revisionBody(item.Version-1, owner, valid, "new"), "version_conflict"},
	}
	for n, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(tc.body, fmt.Sprintf("revision-refusal-%d", n))
			if got := code(rec); got != tc.code {
				t.Fatalf("%s: %d %s", tc.code, rec.Code, rec.Body)
			}
			baseline()
		})
	}
	// A message from before the current contract cannot authorize it.
	s.runs().Now = func() time.Time { return time.Unix(item.CreatedAt-2, 0) }
	old := issueTestRun(t, s, p, "Revise the acceptance of this Issue")
	s.runs().Now = func() time.Time { return time.Now() }
	if rec := request(revisionBody(item.Version, owner, old, "new"), "revision-old"); code(rec) != "run_before_acceptance" {
		t.Fatalf("old run: %d %s", rec.Code, rec.Body)
	}
	baseline()
	body := revisionBody(item.Version, owner, valid, "The published note is visible to readers.")
	first := request(body, "revision-success")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"acceptance_source"`) ||
		!strings.Contains(first.Body.String(), valid) {
		t.Fatalf("revision: %d %s", first.Code, first.Body)
	}
	var revised struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &revised); err != nil {
		t.Fatal(err)
	}
	if revised.Item.AcceptanceVersion != item.AcceptanceVersion+1 || revised.Item.Version != item.Version+1 {
		t.Fatalf("revision versions: %s", first.Body)
	}
	replay := request(body, "revision-success")
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() || replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	conflict := request(revisionBody(item.Version, owner, valid, "Different text"), "revision-success")
	if code(conflict) != "idempotency_key_reused" {
		t.Fatalf("key reuse: %d %s", conflict.Code, conflict.Body)
	}
	stale := request(body, "revision-stale")
	if code(stale) != "version_conflict" {
		t.Fatalf("version: %d %s", stale.Code, stale.Body)
	}
	reusedRun := request(revisionBody(revised.Item.Version, owner, valid, "Another contract."), "revision-run-reused")
	if code(reusedRun) != "run_before_acceptance" {
		t.Fatalf("run predating revised contract: %d %s", reusedRun.Code, reusedRun.Body)
	}
	got, err := s.workV2().Item(context.Background(), item.ID)
	if err != nil || got.Item.AcceptanceCriteria != revised.Item.AcceptanceCriteria {
		t.Fatalf("final: %+v %v", got.Item, err)
	}
	if len(got.Events) == 0 || !strings.Contains(got.Events[len(got.Events)-1].Payload, valid) {
		t.Fatalf("audit missing run: %+v", got.Events)
	}
}
