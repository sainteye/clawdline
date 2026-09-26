package http

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// verifyFixture is the verification routes and the broker's task routes over
// a fresh store, behind the real gate.
type verifyFixture struct {
	t *testing.T
	s *Server
	f *gateFixture
	h http.Handler
}

func newVerifyFixture(t *testing.T) *verifyFixture {
	t.Helper()
	f, _ := newGateFixture(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: t.TempDir(), Port: 7757}, store: st,
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}}
	usageByServer.Store(s, app.NewUsageLedger(st, t.TempDir()))
	t.Cleanup(func() { usageByServer.Delete(s) })
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/verifications", s.verificationsRoute)
	mux.HandleFunc("/v1/verifications/", s.verificationsRoute)
	mux.HandleFunc("/v1/orchestrator/tasks/", s.orchestratorTaskRoute)
	return &verifyFixture{t: t, s: s, f: f, h: f.g.wrap(mux)}
}

func (v *verifyFixture) as(token string) map[string]string {
	if token == v.f.machine {
		return map[string]string{machineHeader: token}
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func (v *verifyFixture) do(method, path, body string, headers map[string]string) (int, []byte) {
	v.t.Helper()
	h := map[string]string{}
	for k, val := range headers {
		h[k] = val
	}
	if body != "" {
		h["Content-Type"] = "application/json"
	}
	rec := call{method: method, path: path, body: body, headers: h}.do(v.h)
	return rec.Code, rec.Body.Bytes()
}

func (v *verifyFixture) want(want int, method, path, payload string, headers map[string]string) contract.VerificationDetail {
	v.t.Helper()
	code, body := v.do(method, path, payload, headers)
	if code != want {
		v.t.Fatalf("%d, wanted %d: %s", code, want, body)
	}
	var d contract.VerificationDetail
	if err := json.Unmarshal(body, &d); err != nil {
		v.t.Fatal(err)
	}
	return d
}

func flatCode(body []byte) string {
	var r contract.Refusal
	_ = json.Unmarshal(body, &r)
	return r.Error
}

// The whole life of one record over the routes: made, read with its data
// source resolved, a criterion marked, a note from the person and one from a
// session, closed, then deleted — and each door says who it is.
func TestAVerificationLivesThroughItsRoutes(t *testing.T) {
	v := newVerifyFixture(t)
	due := time.Now().Add(7 * 24 * time.Hour).Unix()
	create := `{"title":"a check","why":"because","due_at":` + jsonInt(due) +
		`,"criteria":["cheaper","no worse"],"source":{"kind":"compaction_compare","since":"14d"},"schedule_id":"sched-1"}`
	made := v.want(201, http.MethodPost, "/v1/verifications", create, v.as(v.f.send))
	d := v.want(201, http.MethodPost, "/v1/verifications", create,
		merge(v.as(v.f.send), map[string]string{"Idempotency-Key": "k1"}))
	id := d.Verification.ID
	again := v.want(200, http.MethodPost, "/v1/verifications", create,
		merge(v.as(v.f.send), map[string]string{"Idempotency-Key": "k1"}))
	if again.Verification.ID != id {
		t.Fatalf("a repeated key made a second record")
	}

	got := v.want(200, http.MethodGet, "/v1/verifications/"+id, "", v.as(v.f.read))
	if got.Data == nil || got.Data.Kind != "compaction_compare" || got.Data.CompactionCompare == nil ||
		got.Verification.Source == nil || got.Verification.Source.Since != "14d" || len(got.Verification.Criteria) != 2 {
		t.Fatalf("detail: %+v", got)
	}

	marked := v.want(200, http.MethodPost, "/v1/verifications/"+id+"/criteria/1", `{"state":"passed"}`, v.as(v.f.send))
	if marked.Verification.Criteria[1].State != contract.VerificationCriterionStatePassed {
		t.Fatalf("%+v", marked.Verification.Criteria)
	}
	v.want(200, http.MethodPost, "/v1/verifications/"+id+"/notes", `{"text":"looked"}`, v.as(v.f.send))
	noted := v.want(200, http.MethodPost, "/v1/verifications/"+id+"/notes", `{"text":"readout","session":"conv-1"}`, v.as(v.f.machine))
	notes := noted.Verification.Notes
	if len(notes) != 2 || notes[0].AuthorKind != "person" || notes[0].Author != "" ||
		notes[1].AuthorKind != "session" || notes[1].Author != "conv-1" {
		t.Fatalf("notes: %+v", notes)
	}
	if code, body := v.do(http.MethodPost, "/v1/verifications/"+id+"/notes", `{"text":"x","session":"conv-1"}`, v.as(v.f.send)); code != 400 {
		t.Fatalf("a device naming a session: %d %s", code, body)
	}
	if code, body := v.do(http.MethodDelete, "/v1/verifications/"+id, "", v.as(v.f.send)); code != 409 || flatCode(body) != "verification_open" {
		t.Fatalf("an open delete: %d %s", code, body)
	}
	closed := v.want(200, http.MethodPost, "/v1/verifications/"+id+"/close", `{"status":"accepted","reason":"it held"}`, v.as(v.f.send))
	if closed.Verification.Status != "accepted" || closed.Verification.CloseReason != "it held" {
		t.Fatalf("%+v", closed.Verification)
	}
	var list contract.VerificationList
	code, body := v.do(http.MethodGet, "/v1/verifications", "", v.as(v.f.read))
	if code != 200 || json.Unmarshal(body, &list) != nil || len(list.Verifications) != 2 || list.At == 0 {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body := v.do(http.MethodDelete, "/v1/verifications/"+id, "", v.as(v.f.send)); code != 204 {
		t.Fatalf("delete: %d %s", code, body)
	}
	if code, body := v.do(http.MethodDelete, "/v1/verifications/"+made.Verification.ID+"?force=1", "", v.as(v.f.send)); code != 204 {
		t.Fatalf("forced delete: %d %s", code, body)
	}
	if code, _ := v.do(http.MethodGet, "/v1/verifications/"+id, "", v.as(v.f.read)); code != 404 {
		t.Fatalf("a deleted record: %d", code)
	}
}

// Every door and every refusal by its code: a reading device reads and does
// not write, nobody reads without a token, a field the route does not read is
// refused by name, an unknown data source by its kind, and there is no delete
// that does not name one record.
func TestTheVerificationRoutesRefuseByName(t *testing.T) {
	v := newVerifyFixture(t)
	due := jsonInt(time.Now().Add(time.Hour).Unix())
	if code, _ := v.do(http.MethodGet, "/v1/verifications", "", map[string]string{}); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	for _, tc := range []struct {
		method, path, body string
		headers            map[string]string
		status             int
		code               string
	}{
		{http.MethodPost, "/v1/verifications", `{"title":"a","due_at":` + due + `}`, v.as(v.f.read), 403, ""},
		{http.MethodPost, "/v1/verifications", `{"title":"a","due_at":` + due + `,"color":"red"}`, v.as(v.f.send), 400, "bad_request"},
		{http.MethodPost, "/v1/verifications", `{"title":"a","due_at":` + due + `,"source":{"kind":"weather"}}`, v.as(v.f.send), 400, "unknown_source"},
		{http.MethodPost, "/v1/verifications", `{"title":"a"}`, v.as(v.f.send), 400, "bad_request"},
		{http.MethodDelete, "/v1/verifications", "", v.as(v.f.send), 405, "method_not_allowed"},
		{http.MethodDelete, "/v1/verifications/abc?force=yes", "", v.as(v.f.send), 400, "bad_request"},
		{http.MethodDelete, "/v1/verifications/abc", "", v.as(v.f.send), 404, "not_found"},
		{http.MethodGet, "/v1/verifications/a.b", "", v.as(v.f.read), 404, "not_found"},
		{http.MethodGet, "/v1/verifications?status=open", "", v.as(v.f.read), 400, "bad_request"},
		{http.MethodPost, "/v1/verifications/abc/criteria/01", `{"state":"passed"}`, v.as(v.f.send), 404, "not_found"},
		{http.MethodPost, "/v1/verifications/abc/criteria/0", `{"state":"passed"}`, v.as(v.f.send), 404, "not_found"},
		{http.MethodPost, "/v1/verifications/abc/close", `{"status":"accepted","reason":"x"}`, v.as(v.f.read), 403, ""},
	} {
		code, body := v.do(tc.method, tc.path, tc.body, tc.headers)
		if code != tc.status || (tc.code != "" && flatCode(body) != tc.code) {
			t.Errorf("%s %s: %d %s, wanted %d %s", tc.method, tc.path, code, body, tc.status, tc.code)
		}
	}
}

// A scheduled task writes its readout with its own secret, on a record linked
// to its schedule and nowhere else; a wrong secret and an unscheduled task
// are refused; no other verification route takes a secret.
func TestAScheduledTaskWritesItsReadoutWithItsOwnSecret(t *testing.T) {
	v := newVerifyFixture(t)
	ctx := context.Background()
	task := func(id, schedule string) {
		r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
			Title: "readout", ProjectDir: "/p", State: orchestrator.StateBriefed, CreatedAt: time.Now(), ScheduleID: schedule}
		body, _ := json.Marshal(r)
		if _, err := v.s.store.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: "/p", Assistant: "claude",
			State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: orchestrator.HashSecret("s-" + id),
			Record: body}, nil); err != nil {
			t.Fatal(err)
		}
	}
	task("t-sched", "sched-1")
	task("t-plain", "")
	due := jsonInt(time.Now().Add(time.Hour).Unix())
	linked := v.want(201, http.MethodPost, "/v1/verifications", `{"title":"a","due_at":`+due+`,"schedule_id":"sched-1"}`, v.as(v.f.send))
	other := v.want(201, http.MethodPost, "/v1/verifications", `{"title":"b","due_at":`+due+`}`, v.as(v.f.send))
	secret := func(s string) map[string]string { return map[string]string{"X-Clawdline-Task-Secret": s} }
	note := func(id string) string { return `{"verification":"` + id + `","text":"cost -30%, success unchanged"}` }

	code, body := v.do(http.MethodPost, "/v1/orchestrator/tasks/t-sched/verification-note", note(linked.Verification.ID), secret("s-t-sched"))
	var got contract.VerificationNoteResult
	if code != 200 || json.Unmarshal(body, &got) != nil || !got.OK || got.Note.Author != "task:t-sched" || got.Note.AuthorKind != "session" {
		t.Fatalf("%d %s", code, body)
	}
	for _, tc := range []struct {
		path, body, secret string
		status             int
		code               string
	}{
		{"/v1/orchestrator/tasks/t-sched/verification-note", note(linked.Verification.ID), "wrong", 403, ""},
		{"/v1/orchestrator/tasks/t-sched/verification-note", note(other.Verification.ID), "s-t-sched", 403, "schedule_mismatch"},
		{"/v1/orchestrator/tasks/t-plain/verification-note", note(linked.Verification.ID), "s-t-plain", 403, "not_scheduled"},
		{"/v1/orchestrator/tasks/t-sched/verification-note", `{"verification":"../x","text":"x"}`, "s-t-sched", 400, "bad_request"},
		{"/v1/verifications/" + linked.Verification.ID + "/notes", `{"text":"x"}`, "s-t-sched", 401, ""},
	} {
		code, body := v.do(http.MethodPost, tc.path, tc.body, secret(tc.secret))
		if code != tc.status || (tc.code != "" && flatCode(body) != tc.code) {
			t.Errorf("%s with %s: %d %s", tc.path, tc.secret, code, body)
		}
	}
	after := v.want(200, http.MethodGet, "/v1/verifications/"+linked.Verification.ID, "", v.as(v.f.read))
	if len(after.Verification.Notes) != 1 {
		t.Fatalf("notes: %+v", after.Verification.Notes)
	}
}

// StartVerifications plants the seed on the machine holding its schedule and
// not twice.
func TestStartVerificationsPlantsTheSeedOnce(t *testing.T) {
	v := newVerifyFixture(t)
	ctx := context.Background()
	v.s.StartVerifications(ctx)
	if rows, _ := v.s.store.Verifications(ctx); len(rows) != 0 {
		t.Fatalf("planted without the schedule: %d", len(rows))
	}
	const fixture = "5c000000-0000-4000-8000-000000000003"
	defer func(was string) { app.CompactionScheduleDigest = was }(app.CompactionScheduleDigest)
	app.CompactionScheduleDigest = app.ScheduleDigest(fixture)
	if err := v.s.store.CreateScheduleFile(ctx, fixture, []byte(`{}`), time.Now(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	v.s.StartVerifications(ctx)
	v.s.StartVerifications(ctx)
	rows, _ := v.s.store.Verifications(ctx)
	if len(rows) != 1 || rows[0].Seed == "" {
		t.Fatalf("%+v", rows)
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
