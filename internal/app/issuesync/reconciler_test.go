package issuesync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

var now = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

type memStore struct {
	due    []Due
	synced map[string]Result
	failed map[string]struct {
		code     string
		attempts int
		next     time.Time
	}
}

func (m *memStore) PendingIssueLinks(_ context.Context, _ time.Time, limit int) ([]Due, error) {
	if len(m.due) > limit {
		return m.due[:limit], nil
	}
	return m.due, nil
}

func (m *memStore) RecordIssueSynced(_ context.Context, id string, r Result, _ time.Time) error {
	m.synced[id] = r
	return nil
}

func (m *memStore) RecordIssueFailed(_ context.Context, id, code string, attempts int, next, _ time.Time) error {
	m.failed[id] = struct {
		code     string
		attempts int
		next     time.Time
	}{code, attempts, next}
	return nil
}

type fakeTracker struct {
	calls []string
	fail  map[string]error
}

func (f *fakeTracker) Create(_ context.Context, d Due) (Result, error) {
	f.calls = append(f.calls, "create "+d.Link.ItemID)
	if err := f.fail[d.Link.ItemID]; err != nil {
		return Result{}, err
	}
	return Result{Observed: work.IssueOpen, ExternalKey: "#1", URL: "https://github.com/o/r/issues/1"}, nil
}

func (f *fakeTracker) Close(_ context.Context, d Due, notPlanned bool) error {
	f.calls = append(f.calls, map[bool]string{true: "close-not-planned ", false: "close "}[notPlanned]+d.Link.ItemID)
	return f.fail[d.Link.ItemID]
}

func (f *fakeTracker) Reopen(_ context.Context, d Due) error {
	f.calls = append(f.calls, "reopen "+d.Link.ItemID)
	return f.fail[d.Link.ItemID]
}

func due(id string, phase work.Phase, observed work.IssueObserved, attempts int) Due {
	return Due{Phase: phase, Title: "t", Link: Link{ItemID: id, Observed: observed, ExternalKey: "#1", Attempts: attempts,
		Target: work.IssueTarget{Provider: work.IssueProviderGitHub, Repo: "o/r"}}}
}

func run(t *testing.T, s *memStore, tr *fakeTracker) int {
	t.Helper()
	r := &Reconciler{Store: s, Trackers: map[work.IssueProvider]Tracker{work.IssueProviderGitHub: tr}, Now: func() time.Time { return now }}
	n, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func newStore(d ...Due) *memStore {
	return &memStore{due: d, synced: map[string]Result{}, failed: map[string]struct {
		code     string
		attempts int
		next     time.Time
	}{}}
}

func TestAPassMakesOnlyTheCallsTheItemsOwe(t *testing.T) {
	s := newStore(
		due("assigned", work.PhaseAssigned, work.IssueNotCreated, 0),
		due("started", work.PhaseImplementing, work.IssueNotCreated, 0),
		due("working", work.PhaseVerifying, work.IssueOpen, 0),
		due("done", work.PhaseDone, work.IssueOpen, 0),
		due("cancelled", work.PhaseCancelled, work.IssueOpen, 0),
		due("reopened", work.PhaseAssigned, work.IssueClosed, 0),
	)
	tr := &fakeTracker{}
	if n := run(t, s, tr); n != 4 {
		t.Fatalf("%d calls: %v", n, tr.calls)
	}
	want := "create started|close done|close-not-planned cancelled|reopen reopened"
	if got := strings.Join(tr.calls, "|"); got != want {
		t.Fatalf("calls %s", got)
	}
	if s.synced["started"].ExternalKey != "#1" || s.synced["done"].Observed != work.IssueClosed || s.synced["reopened"].Observed != work.IssueOpen {
		t.Fatalf("recorded %v", s.synced)
	}
}

func TestOneFailureIsRecordedAndTheRestGoOn(t *testing.T) {
	s := newStore(due("a", work.PhaseImplementing, work.IssueNotCreated, 2), due("b", work.PhaseImplementing, work.IssueNotCreated, 0))
	tr := &fakeTracker{fail: map[string]error{"a": githubFailure(errors.New("network down"))}}
	run(t, s, tr)
	f := s.failed["a"]
	if f.code != "github_unreachable" || f.attempts != 3 || !f.next.Equal(now.Add(4*time.Minute)) {
		t.Fatalf("failure %+v", f)
	}
	if _, ok := s.synced["b"]; !ok {
		t.Fatal("the second link waited on the first")
	}
}

func TestARateLimitWaitsAsLongAsTheTrackerAsked(t *testing.T) {
	s := newStore(due("a", work.PhaseImplementing, work.IssueNotCreated, 0))
	tr := &fakeTracker{fail: map[string]error{"a": failure{code: "github_rate_limited", wait: 7 * time.Minute}}}
	run(t, s, tr)
	if f := s.failed["a"]; f.code != "github_rate_limited" || !f.next.Equal(now.Add(7*time.Minute)) {
		t.Fatalf("failure %+v", f)
	}
}

func TestRetriesBackOffToAnHour(t *testing.T) {
	for attempts, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 6: 32 * time.Minute, 7: time.Hour, 40: time.Hour} {
		if got := backoff(errors.New("x"), attempts); got != want {
			t.Errorf("attempt %d waits %v, want %v", attempts, got, want)
		}
	}
}

func TestTheIssueBodyCarriesTheMarker(t *testing.T) {
	body := Body("Steps to reproduce.", "2f0c6b9e-0000-4000-8000-000000000001")
	if !strings.HasPrefix(body, "Steps to reproduce.") || !strings.HasSuffix(body, work.IssueMarker("2f0c6b9e-0000-4000-8000-000000000001")) {
		t.Fatalf("body %q", body)
	}
}
