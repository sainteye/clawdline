package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// Three readings, each saying what it is worth (work-system-review §5.2, W4).
//
// They were written for the console's "now" page, which was retired on
// 2026-09-26 when the v2 Board replaced it. The words stay because they are the
// wire's, not the page's: a reader that could not read a source must never be
// handed `0`, and it can only print the word the daemon gave it. Before this,
// the daemon had three words and all three answered "did the reading happen" —
// so *read in full and possibly already wrong* had no spelling at all and came
// out as `current`.
//
// Each test here has the control that makes it red (DG-8): the neighbouring
// case that must keep the ordinary word.

// A task list carries the word for what it is worth, and it is not the same
// word as the store reading beside it.
func TestTheTaskListSaysWhatItIsWorth(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		name string
		snap swiftstore.Snapshot
		want contract.SourceFreshness
	}{
		{
			// Read, and short: the other store answered nothing, so this list
			// is this daemon's own tasks and is short by however many that
			// store held. Short is not empty and not wrong.
			name: "the other store could not be read",
			snap: swiftstore.Snapshot{Known: false},
			want: contract.SourceFreshnessStale,
		},
		{
			// Every row is here and an earlier reading is what they came
			// from. This is the case the word was added for.
			name: "an earlier reading was carried",
			snap: swiftstore.Snapshot{Known: true, Stale: true},
			want: contract.SourceFreshnessUnverified,
		},
		{
			name: "read in full, now",
			snap: swiftstore.Snapshot{Known: true},
			want: contract.SourceFreshnessCurrent,
		},
		{
			// The control: absent is a known quantity. A machine with no
			// Swift store is not a machine whose list is short — there is
			// nothing there to be short of — and calling it `stale` would
			// put a warning on every list on every such machine forever.
			name: "there is no such store on this machine",
			snap: swiftstore.Snapshot{Known: true, Source: swiftstore.SourceAbsent},
			want: contract.SourceFreshnessCurrent,
		},
		{
			name: "this daemon was told not to read it",
			snap: swiftstore.Snapshot{Known: true, Source: swiftstore.SourceDisabled},
			want: contract.SourceFreshnessCurrent,
		},
	}
	for _, c := range cases {
		got := taskListSource(now, c.snap)
		if got.Freshness != c.want {
			t.Errorf("%s: freshness %q, want %q", c.name, got.Freshness, c.want)
		}
		if got.ObservedAt != now.Unix() {
			t.Errorf("%s: a reading with no time on it cannot be aged", c.name)
		}
		if got.Provenance == "" {
			t.Errorf("%s: a source that will not name itself cannot be chased", c.name)
		}
	}
}

// Every one of the four words the contract allows is a word this daemon can
// actually produce for a task list — or is deliberately not one.
//
// `missing` is the deliberate absence: this route refuses rather than
// answering with an empty list, so a reader who has a list in their hands
// knows the list was read. A word that no producer can reach and no comment
// explains is how a vocabulary rots.
func TestTheTaskListNeverClaimsItsOwnSourceIsMissing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, snap := range []swiftstore.Snapshot{
		{Known: false},
		{Known: true, Stale: true},
		{Known: true},
		{Known: true, Source: swiftstore.SourceAbsent},
	} {
		if taskListSource(now, snap).Freshness == contract.SourceFreshnessMissing {
			t.Fatalf("a list that was answered called its own source missing: %+v", snap)
		}
	}
}

// What is waiting for a person is only as good as the clock it ages on.
func TestWhatIsWaitingSaysWhetherItsClockIsTurning(t *testing.T) {
	cases := []struct {
		name                      string
		running, noPassYet, stall bool
		want                      contract.SourceFreshness
	}{
		{"the sweep is not running at all", false, true, false, contract.SourceFreshnessUnverified},
		{"running, no pass finished yet", true, true, false, contract.SourceFreshnessUnverified},
		{"running and stalled", true, false, true, contract.SourceFreshnessUnverified},
		// The control: a sweep that is running and keeping up is the one case
		// where "5 waiting for you" is a plain fact.
		{"running and keeping up", true, false, false, contract.SourceFreshnessCurrent},
	}
	for _, c := range cases {
		if got := participationFreshness(c.running, c.noPassYet, c.stall); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The proposal and decision routes answer with that word on them.
//
// A field a reader is written against and the route does not send is found by
// something breaking on a phone, which is the worst place to find it.
func TestTheWaitingRoutesCarryTheirFreshness(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	person := access{verdict: auth.Verdict{Allowed: true, Local: true}}
	routes := map[string]http.HandlerFunc{
		"/v1/work/proposals": s.workProposalsRoute,
		"/v1/work/decisions": s.workDecisionsRoute,
	}
	for path, handler := range routes {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, person))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		var body struct {
			Source *contract.BearingsSource `json:"source"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if body.Source == nil {
			t.Fatalf("%s says nothing about what its answer is worth: %s", path, rec.Body)
		}
		if body.Source.Freshness == "" {
			t.Fatalf("%s: an empty freshness is a fifth word: %s", path, rec.Body)
		}
		// An empty board with no sweep is not a board with nothing waiting.
		if !strings.Contains(rec.Body.String(), `"freshness":"unverified"`) {
			t.Fatalf("%s: a stopped clock reads as settled: %s", path, rec.Body)
		}
	}
}

// The landing ledger answers for itself, and no longer borrows the task
// records' word.
//
// This is §1.3's correction made into code. The coordinator was accused of
// ignoring a `stale` that was sitting in front of it; what it was actually
// doing was reporting, honestly, a state it had no word for — `Sources.Landings`
// was literally `src(b.TasksFresh, …)`, the task reading's word under the
// landing ledger's name. The records were readable, so it said `current`, and
// nineteen rows nobody could check were drawn as nineteen rows that had not
// landed yet.
func TestTheLandingLedgerAnswersForItself(t *testing.T) {
	pending := func(target string, settlement orchestrator.LandingSettlement) orchestrator.PendingLanding {
		return orchestrator.PendingLanding{Record: orchestrator.Record{
			Landing: &orchestrator.Landing{State: orchestrator.LandingPending, Target: target, Settlement: settlement},
		}}
	}
	checkable := pending("master", orchestrator.SettlementCarried)
	nameless := pending("", orchestrator.SettlementCarried)
	unreadable := pending("master", orchestrator.SettlementUnreadable)

	cases := []struct {
		name string
		rows []orchestrator.PendingLanding
		want contract.SourceFreshness
	}{
		// The control, and the reason this is not simply "always unverified":
		// a ledger whose every row names a target is a ledger that can be
		// checked, and saying otherwise would make the word mean nothing.
		{"nothing owed", nil, contract.SourceFreshnessCurrent},
		{"every row names a target", []orchestrator.PendingLanding{checkable, checkable},
			contract.SourceFreshnessCurrent},
		{"one row names none", []orchestrator.PendingLanding{checkable, nameless},
			contract.SourceFreshnessUnverified},
		{"one row's branch could not be read", []orchestrator.PendingLanding{checkable, unreadable},
			contract.SourceFreshnessUnverified},
	}
	for _, c := range cases {
		if got := landingsFreshness(c.rows); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// What the branch held reaches the ledger's own rows, not only a task's record.
//
// "Delivered, not recorded" is read from this route, and the difference
// between a row whose branch carries a delivery and one whose branch carried
// nothing is the difference between work waiting to be merged and work that was
// never committed. Those want opposite actions from the person reading them.
func TestTheLedgerRowSaysWhatItsBranchHeld(t *testing.T) {
	row := contract.BrokerPendingLanding{
		ID:         "t1",
		Settlement: contract.BrokerLandingSettlement(orchestrator.SettlementEmpty),
	}
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"settlement":"branch_empty"`) {
		t.Fatalf("a ledger row that does not say what its branch held: %s", body)
	}
	// Nobody asked: a task with no branch of its own. Absent, never a value
	// that reads as an answer.
	bare, err := json.Marshal(contract.BrokerPendingLanding{ID: "t2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "settlement") {
		t.Fatalf("a row nobody asked about still carries a settlement: %s", bare)
	}
}
