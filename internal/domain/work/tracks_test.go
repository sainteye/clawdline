package work

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// The rules of board-redesign §7.1, one fixture each. Every fixture is a card
// that the named rule — and only that rule — puts where it belongs: taken out
// of the list, the same card lands somewhere else. That is the control that
// says the fixture can fail (docs/design-guidelines.md DG-8), run every time
// rather than once by hand.

const (
	day = 86400.0
	t0  = 1_800_000_000.0 // "now" in every fixture
)

var (
	complete   = Presence{Complete: true, Sessions: map[string]bool{"live-session": true}}
	incomplete = Presence{Complete: false, Sessions: map[string]bool{"live-session": true}}
	// historyAt is a log whose only fact happened at the given time.
	historyAt = func(at float64) *History {
		return &History{Readable: true, Events: []Event{{Kind: "item_created", At: at - 60}, {Kind: "checklist_updated", At: at}}}
	}
)

// person is a card for a person that nobody has touched for ten days and that
// never started: the Backlog's plainest case. Fixtures change what they need.
func person(change ...func(*Card)) Card {
	c := Card{ID: "id", Key: "CLA-1", Type: "feature", Audience: "human",
		Progress:  Progress{State: "planning", Group: "waiting"},
		CreatedAt: t0 - 10*day, UpdatedAt: t0 - 10*day}
	for _, f := range change {
		f(&c)
	}
	return c
}

func session(change ...func(*Card)) Card {
	return person(append([]func(*Card){func(c *Card) { c.Audience = "agent" }}, change...)...)
}

func progress(state, group string, active bool) func(*Card) {
	return func(c *Card) { c.Progress = Progress{State: state, Group: group, Active: active} }
}

func withHistory(h *History) func(*Card) { return func(c *Card) { c.History = h } }

func brokerTask(state string) func(*Card) {
	return func(c *Card) { c.Attempts = append(c.Attempts, Attempt{Source: "broker", State: state}) }
}

func openSpan(sessionID string) func(*Card) {
	return func(c *Card) { c.Spans = append(c.Spans, Span{SessionID: sessionID, Open: true}) }
}

func placeWith(list []rule, c Card, p Presence) (Bucket, string) {
	return classify(list, FactsOf(c, p, t0), DefaultStall.Seconds())
}

func place(c Card, p Presence) (Bucket, string) { return placeWith(rules, c, p) }

func without(code string) []rule {
	out := []rule{}
	for _, r := range rules {
		if r.code != code {
			out = append(out, r)
		}
	}
	return out
}

func TestEveryRuleHasAFixtureThatNeedsIt(t *testing.T) {
	fixtures := []struct {
		rule string
		want Bucket
		card Card
	}{
		{"not_for_person_terminal", TodoDone,
			session(progress("landed", "completed", false), brokerTask("success"))},
		{"not_for_person_active", TodoLive,
			session(progress("execution", "active", true), brokerTask("briefed"))},
		{"not_for_person", TodoAutoclose,
			session(progress("delivered", "waiting", false), brokerTask("success"))},
		// Never started, so without this rule it would be Backlog.
		{"terminal", BoardDone, person(progress("canceled", "canceled", false))},
		{"user_decision_open", BoardNow,
			person(func(c *Card) { c.Obligations = []Obligation{{ActorKind: "user"}} })},
		// Active through a running session's span only: nothing was recorded,
		// so without this rule it never started.
		{"active", BoardNow, person(progress("execution", "active", true), openSpan("live-session"))},
		// Quiet for an hour: without this rule it would be happening now.
		{"never_started", BacklogNeverStarted, person(withHistory(historyAt(t0 - 3600)))},
		{"idle_within_stall", BoardNow,
			person(brokerTask("success"), withHistory(historyAt(t0-1*day)))},
		{"idle_past_stall_delivered", BoardClosure,
			person(progress("delivered", "waiting", false), brokerTask("success"), withHistory(historyAt(t0-5*day)))},
		// The last line: without it the card has nowhere to go.
		{"idle_past_stall", BacklogStalled,
			person(brokerTask("failure"), withHistory(historyAt(t0-5*day)))},
	}
	if len(fixtures) != len(rules) {
		t.Fatalf("%d fixtures for %d rules: a rule was added without one", len(fixtures), len(rules))
	}
	for i, f := range fixtures {
		if rules[i].code != f.rule {
			t.Fatalf("fixture %d is for %s, rule %d is %s", i, f.rule, i, rules[i].code)
		}
		bucket, code := place(f.card, complete)
		if bucket != f.want || code != f.rule {
			t.Errorf("%s: placed in %s by %s, want %s", f.rule, bucket, code, f.want)
		}
		if other, by := placeWith(without(f.rule), f.card, complete); other == f.want {
			t.Errorf("%s: still %s without the rule (by %s), so its fixture cannot fail", f.rule, other, by)
		}
	}
}

func TestACoordinationRecordIsTheSessions(t *testing.T) {
	c := person(func(c *Card) { c.Type = "coordination" })
	if bucket, _ := place(c, complete); bucket.Track() != TrackTodo {
		t.Fatalf("a coordination record for a person: %s", bucket)
	}
	c.Type = "feature"
	if bucket, _ := place(c, complete); bucket.Track() == TrackTodo {
		t.Fatalf("control: the same card as a feature is a person's, got %s", bucket)
	}
}

// §1.4: a card is not in progress because a session that is gone once said
// it was working on it.
func TestAGhostIsNotActive(t *testing.T) {
	ghostly := person(progress("execution", "active", true), openSpan("dead-session"))

	if bucket, _ := place(ghostly, complete); bucket != BacklogNeverStarted {
		t.Errorf("active only through a gone session's span: %s", bucket)
	}
	f := FactsOf(ghostly, complete, t0)
	if !f.Ghost || f.Presence != Gone || f.ActiveNow() {
		t.Errorf("facts: %+v", f)
	}

	// Controls, each one fact away.
	live := person(progress("execution", "active", true), openSpan("LIVE-SESSION"))
	if bucket, _ := place(live, complete); bucket != BoardNow {
		t.Errorf("the span's session is running (ids compare without case): %s", bucket)
	}
	if bucket, _ := place(ghostly, incomplete); bucket != BoardNow {
		t.Errorf("an incomplete reading cannot show the session gone: %s", bucket)
	}
	if f := FactsOf(ghostly, incomplete, t0); f.Ghost || f.Presence != Unknown {
		t.Errorf("unknown presence: %+v", f)
	}
	running := person(progress("execution", "active", true), openSpan("dead-session"), brokerTask("queued"))
	if bucket, _ := place(running, complete); bucket != BoardNow {
		t.Errorf("a running broker attempt is work whatever the spans say: %s", bucket)
	}
	elsewhere := person(progress("execution", "active", true), openSpan("dead-session"),
		func(c *Card) { c.Attempts = []Attempt{{Source: "unknown", State: "queued"}} })
	if !FactsOf(elsewhere, complete, t0).Ghost {
		t.Errorf("only the broker's attempt states are read")
	}
	closed := person(progress("execution", "active", true),
		func(c *Card) { c.Spans = []Span{{SessionID: "dead-session", Open: false}} })
	if FactsOf(closed, complete, t0).Ghost {
		t.Errorf("a closed span is not what makes a card look active")
	}
	mixed := person(progress("execution", "active", true), openSpan("dead-session"), openSpan("live-session"))
	if FactsOf(mixed, complete, t0).Ghost {
		t.Errorf("one live session among the spans is enough")
	}
}

// §1.1: recorded work starts a card; a declaration does not.
func TestWhatCountsAsStarted(t *testing.T) {
	started := map[string]Card{
		"broker task":      person(brokerTask("failure")),
		"session delivery": person(func(c *Card) { c.Deliveries = 1 }),
		"evidence":         person(func(c *Card) { c.Evidence = 1 }),
		"completed row":    person(func(c *Card) { c.Checklist = []string{"pending", "completed"} }),
		"done row":         person(func(c *Card) { c.Checklist = []string{"done"} }),
		"passed row":       person(func(c *Card) { c.Checklist = []string{"passed"} }),
	}
	for name, c := range started {
		if !FactsOf(c, complete, t0).Started {
			t.Errorf("%s: not started", name)
		}
	}
	notStarted := map[string]Card{
		"a declared span":                 person(openSpan("live-session")),
		"a task link not from the broker": person(func(c *Card) { c.Attempts = []Attempt{{Source: "unknown", State: "success"}} }),
		"unticked rows":                   person(func(c *Card) { c.Checklist = []string{"pending", "not_applicable", "in_progress"} }),
		"nothing":                         person(),
	}
	for name, c := range notStarted {
		if FactsOf(c, complete, t0).Started {
			t.Errorf("%s: started", name)
		}
	}
}

// The idle clock reads the last fact, not the last time the old store
// recomputed the card.
func TestTheIdleClock(t *testing.T) {
	recomputed := &History{Readable: true, Events: []Event{
		{Kind: "item_created", At: t0 - 9*day},
		{Kind: "checklist_updated", At: t0 - 5*day},
		{Kind: "automatic_state_reconciled", At: t0 - 1*day},
		{Kind: "catalog_reconciled", At: t0 - 1*day},
		{Kind: "task_reattributed", At: t0 - 1*day},
		{Kind: "session_relation_confirmed", At: t0 - 1*day},
	}}
	c := person(brokerTask("success"), progress("delivered", "waiting", false), withHistory(recomputed))
	c.UpdatedAt = t0 - 1*day
	f := FactsOf(c, complete, t0)
	if f.IdleSeconds != 5*day || f.IdleFrom != IdleFromHistory {
		t.Fatalf("machine events moved the clock: %v from %s", f.IdleSeconds/day, f.IdleFrom)
	}
	if bucket, _ := place(c, complete); bucket != BoardClosure {
		t.Fatalf("quiet for five days after delivering: %s", bucket)
	}
	// Control: the same card whose last event is a fact is happening now.
	fresh := *recomputed
	fresh.Events = append(append([]Event(nil), recomputed.Events...), Event{Kind: "session_delivery_recorded", At: t0 - 1*day})
	c.History = &fresh
	if bucket, _ := place(c, complete); bucket != BoardNow {
		t.Fatalf("control: a fact a day ago: %s", bucket)
	}

	cases := []struct {
		name    string
		history *History
		idle    float64
		from    string
	}{
		{"no log", nil, 2 * day, IdleFromUpdatedAt},
		{"an empty log", &History{Readable: true}, 2 * day, IdleFromUpdatedAt},
		{"an unreadable log", &History{Readable: false}, 2 * day, IdleFromUnreadable},
		{"only its creation", &History{Readable: true, Events: []Event{
			{Kind: "item_created", At: t0 - 7*day}, {Kind: "item_created", At: t0 - 8*day},
			{Kind: "automatic_state_reconciled", At: t0 - 1*day}}}, 8 * day, IdleFromCreated},
		{"no creation event either", &History{Readable: true, Events: []Event{
			{Kind: "automatic_state_reconciled", At: t0 - 1*day}}}, 9 * day, IdleFromCreated},
	}
	for _, tc := range cases {
		c := person(withHistory(tc.history), func(c *Card) { c.CreatedAt, c.UpdatedAt = t0-9*day, t0-2*day })
		f := FactsOf(c, complete, t0)
		if f.IdleSeconds != tc.idle || f.IdleFrom != tc.from {
			t.Errorf("%s: %v days from %s, want %v from %s", tc.name, f.IdleSeconds/day, f.IdleFrom, tc.idle/day, tc.from)
		}
	}

	// The stall window is inclusive: exactly three days is still now.
	edge := person(brokerTask("success"), withHistory(historyAt(t0-DefaultStall.Seconds())))
	if bucket, _ := place(edge, complete); bucket != BoardNow {
		t.Errorf("exactly at the stall window: %s", bucket)
	}
	edge = person(brokerTask("success"), withHistory(historyAt(t0-DefaultStall.Seconds()-0.001)))
	if bucket, _ := place(edge, complete); bucket != BacklogStalled {
		t.Errorf("a millisecond past it: %s", bucket)
	}
}

func TestOnlyAnOpenQuestionToAPersonIsADecision(t *testing.T) {
	for name, o := range map[string][]Obligation{
		"resolved":          {{Resolved: true, ActorKind: "user"}},
		"owed by a session": {{ActorKind: "session"}},
		"unrecorded":        {{}},
	} {
		c := person(func(c *Card) { c.Obligations = o })
		if bucket, _ := place(c, complete); bucket == BoardNow {
			t.Errorf("%s: counted as a decision", name)
		}
	}
}

func TestWhatWaitsToBeClosed(t *testing.T) {
	for state, want := range map[string]Bucket{
		"delivered": BoardClosure, "verified": BoardClosure, "blocked": BoardClosure,
		"correction": BoardClosure, "review_testing": BoardClosure,
		"planning": BacklogStalled, "unknown": BacklogStalled, "execution": BacklogStalled,
	} {
		c := person(brokerTask("success"), progress(state, "waiting", false), withHistory(historyAt(t0-5*day)))
		if bucket, _ := place(c, complete); bucket != want {
			t.Errorf("%s: %s, want %s", state, bucket, want)
		}
	}
}

func TestOnlyCompletedAndCanceledAreOver(t *testing.T) {
	for group, over := range map[string]bool{"completed": true, "canceled": true,
		"waiting": false, "active": false, "planning": false, "coordination": false} {
		c := person(progress("x", group, false))
		if bucket, _ := place(c, complete); (bucket == BoardDone) != over {
			t.Errorf("%s: %s", group, bucket)
		}
	}
}

func TestProjectCountsEveryCardOnce(t *testing.T) {
	cards := []Card{
		session(progress("landed", "completed", false)),
		person(progress("canceled", "canceled", false)),
		person(),
	}
	p := Project(cards, complete, time.Unix(t0, 0), DefaultStall)
	if p.Counts != (Counts{Cards: 3, Todo: 1, Board: 1, Backlog: 1}) || p.ForPeople() != 2 {
		t.Fatalf("counts: %+v", p.Counts)
	}
	if len(p.Buckets) != len(BucketOrder) {
		t.Fatalf("every bucket is listed, empty ones too: %d", len(p.Buckets))
	}
	for i, b := range p.Buckets {
		if b.Bucket != BucketOrder[i] || b.Track != b.Bucket.Track() {
			t.Fatalf("bucket %d: %+v", i, b)
		}
	}
	if p.Buckets[0].Progress["landed"] != 1 || p.Rules != RulesVersion || p.StallSeconds != 3*day {
		t.Fatalf("projection: %+v", p)
	}
}

// A page is a position: walking the pages lists every row once, in order, and
// a cursor this route did not write is refused rather than read as the start.
func TestPagesListEveryRowOnce(t *testing.T) {
	var cards []Card
	for i := 0; i < 450; i++ {
		c := person(func(c *Card) { c.ID = fmt.Sprintf("card-%03d", i); c.CreatedAt = t0 - float64(i%7)*day })
		if i%3 == 0 {
			c.Audience = "agent"
		}
		cards = append(cards, c)
	}
	p := Project(cards, complete, time.Unix(t0, 0), DefaultStall)
	for _, track := range []Track{"", TrackTodo, TrackBacklog} {
		seen, order := map[string]bool{}, []Row{}
		cursor := ""
		for pages := 0; ; pages++ {
			rows, next, err := p.Page(track, cursor, 200)
			if err != nil || len(rows) > 200 || pages > 5 {
				t.Fatalf("%q page %d: %d rows, %v", track, pages, len(rows), err)
			}
			for _, r := range rows {
				if seen[r.ID] || (track != "" && r.Track != track) {
					t.Fatalf("%q: %s listed twice or on the wrong track", track, r.ID)
				}
				seen[r.ID] = true
				order = append(order, r)
			}
			if next == "" {
				break
			}
			cursor = next
		}
		want := 0
		for _, r := range p.Rows {
			if track == "" || r.Track == track {
				want++
			}
		}
		if len(seen) != want {
			t.Fatalf("%q: %d rows paged, %d exist", track, len(seen), want)
		}
		for i := 1; i < len(order); i++ {
			if !order[i-1].before(order[i]) {
				t.Fatalf("%q: out of order at %d", track, i)
			}
		}
	}
	for _, bad := range []string{"x", "9:1:a", "0:nope:a", "0:1:", "-1:1:a", "0:NaN:a"} {
		if _, _, err := p.Page("", bad, 200); !errors.Is(err, ErrCursor) {
			t.Errorf("cursor %q: %v", bad, err)
		}
	}
}
