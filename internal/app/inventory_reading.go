package app

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// One reading of this machine, for everybody who asks at about the same time.
//
// Three loops each rebuilt the whole reading on a clock of its own — the event
// stream every two seconds per open page, the Cloud publisher every five, the
// broker's beat every five — and each rebuild scans the process table, asks
// tmux, asks iTerm2 and then captures a screen per qualifying row. Nothing
// shared anything, so the cost was the number of loops times the number of
// rows, and a machine with twenty-five iTerm2 tabs spent more of every second
// scanning itself than not.
//
// **The consumers do not all want the same freshness, and that is the whole of
// this file's design.** A console redrawing every two seconds is content with a
// reading taken a moment ago; the broker deciding whether a child's tab is
// still there is not, because that decision closes sessions and settles tasks,
// and a snapshot taken before the tab opened would be evidence of something
// that was never observed (docs/design-decisions.md D05 ③ — an expired
// snapshot may not stand in for evidence). So there are three words for it, every
// caller says which it needs, and Counts records how each one was answered.
//
//   - Recent may be answered from the held reading while it is younger than
//     the TTL.
//   - Fast returns bounded prior rows marked unverified while one background
//     refresh runs; it is only for drawing and read-only lookups.
//   - Fresh is never answered from a completed reading. It joins a scan that is
//     still running, or starts one; either way what comes back was taken for
//     this call.
//
// All are singleflight: readers arriving during a scan share it rather than
// starting another.

// InventoryTTL is how old the held reading may be before a Recent reader is
// given a new one.
//
// Two seconds because that is the fastest consumer's own clock — the event
// stream's tick (transport/http streamTick). Fast may show an older row while
// refreshing, but labels it unverified and applies the shorter of its source's
// age and the registered retention bound. Closeability checks age separately.
const InventoryTTL = 2 * time.Second

// InventoryScanBudget bounds one scan of the machine.
//
// It is the scan's own clock and not the caller's on purpose: a scan started
// for a reader that has since given up still finishes and still fills the held
// reading, so the next reader is answered at once instead of starting the same
// scan again.
const InventoryScanBudget = 30 * time.Second

// LastGoodInventoryAgeLimit is how long a drawing-only reader may be shown the
// last complete answer from a source that did not answer this pass.
//
// Two minutes spans twelve full iTerm2 listing timeouts, so one busy Apple
// Events queue does not turn every row into "unknown". It is also the longest
// this daemon already leaves a failed sampled screen in backoff
// (ScreenBackoffMax). Past it, continuing to describe a session as working or
// waiting would turn an observation into a claim about the present, so the
// retained answer expires and the source becomes genuinely unknown.
const LastGoodInventoryAgeLimit = 2 * time.Minute

// SourceAnswerAgeLimit is how old one source's own complete answer may be and
// still let a drawing call that source's rows current while a refresh runs.
//
// A refresh waits on its slowest source, and iTerm2's list Apple Event may take
// ten seconds before it is cut off. Before this, every reader that arrived
// during that wait was told that every source was unverified — fifteen minutes
// of `incomplete_sources=[iterm ps tmux]` on 2026-10-01, while the process
// table and tmux answered in milliseconds every scan. Thirty seconds is the
// scan budget: the longest one refresh may run, and so the most a source's
// previous answer can have aged while the next one is on its way. Past it the
// rows are unverified, exactly as before.
const SourceAnswerAgeLimit = 30 * time.Second

// InventoryCounts is how the readers were answered, for /v1/diagnostics. It is
// the evidence for "one producer": Scans is how often this daemon actually
// looked at the machine, and the other three are the readers that cost nothing.
type InventoryCounts struct {
	// Scans is how many readings were taken from the machine.
	Scans int64
	// Held is readers answered from the held reading.
	Held int64
	// Joined is readers that shared a scan already running.
	Joined int64
	// Late is readers whose own clock ran out before the scan they were
	// waiting on finished. A Recent reader is then given the held reading only
	// while it remains inside the retention bound; a Fresh one is always given
	// an unread reading. Neither answer turns old state into current evidence.
	Late int64
}

// InventoryReading is the single producer in front of an Inventory.
type InventoryReading struct {
	scan func(context.Context) session.Inventory
	ttl  time.Duration
	now  func() time.Time
	// retainFor is independently configurable from ttl: ttl avoids duplicate
	// scans measured in seconds; this is the honesty line for a last good row.
	retainFor time.Duration
	// answerAge bounds how old a source's answer may be and still vouch for
	// its rows while a refresh runs (SourceAnswerAgeLimit).
	answerAge time.Duration

	mu    sync.Mutex
	held  session.Inventory
	holds bool
	// finishedAt separates the refresh cadence from the observation time: a
	// slow successful scan must not start the next Apple Event immediately.
	finishedAt time.Time
	good       map[string]sourceReading
	// forgotten is a terminal backend's positive answer that a session was
	// closed. It prevents an older scan already in flight, or the last-good
	// shelf during a source outage, from putting that session back on screen.
	// Entries live inside the same retainFor window as the shelf and leave
	// sooner when a later complete source reading confirms the absence.
	forgotten map[string]forgottenSession
	flight    *inventoryFlight
	// answers is what each source said in the scan that produced held, as
	// it said it (sourceAnswer).
	answers map[string]sourceAnswer
	counts  InventoryCounts
	expired int64
	// answersExpired counts drawings in which a source's complete answer was
	// too old to vouch for its rows (SourceAnswerAgeLimit).
	answersExpired int64
	// observers are shown every scan's raw reading after its readers have
	// been answered (Observe).
	observers []func(context.Context, session.Inventory)
}

// sourceReading is the last complete answer from one terminal source. It is
// deliberately per source: an iTerm2 Apple Event timeout must not age a tmux
// row that tmux read successfully in the same pass.
type sourceReading struct {
	at   time.Time
	rows []session.Session
}

type forgottenSession struct {
	at  time.Time
	row session.Session
}

// inventoryFlight is one scan several readers are waiting on.
type inventoryFlight struct {
	done    chan struct{}
	raw     session.Inventory
	display session.Inventory
	// started and answers are guarded by the reading's mutex: when the scan
	// began, and what each source has said in it so far.
	started time.Time
	answers map[string]sourceAnswer
}

// sourceAnswer is what one source said in one scan, at the moment it said it.
//
// A scan merges its sources only once the slowest has answered, so the merged
// reading cannot tell a reader that tmux answered completely nine seconds
// before iTerm2 was cut off. This can. keys are the rows it listed, by the
// key the merge joins them on (inventoryRowKey), so a drawing can check that
// the rows it shows are exactly what the source answered.
type sourceAnswer struct {
	at       time.Time
	complete bool
	keys     map[string]bool
}

type sourceAnswersKey struct{}

// noteSourceAnswer tells the scan in flight, if one is listening, what one source
// just said. Inventory.Read calls it as each source answers.
func noteSourceAnswer(ctx context.Context, inv session.Inventory) {
	if record, ok := ctx.Value(sourceAnswersKey{}).(func(session.Inventory)); ok {
		record(inv)
	}
}

// recordAnswer keeps one source's answer on the flight. Two sources with one
// name are one source, as in the merge: either failing makes it incomplete.
func (r *InventoryReading) recordAnswer(flight *inventoryFlight, inv session.Inventory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	answer, seen := flight.answers[inv.Provenance]
	if !seen {
		answer = sourceAnswer{complete: true, keys: map[string]bool{}}
	}
	answer.at = r.now()
	answer.complete = answer.complete && inv.Complete
	for _, row := range inv.Sessions {
		answer.keys[inventoryRowKey(row)] = true
	}
	flight.answers[inv.Provenance] = answer
}

// NewInventoryReading wraps a reader. ttl of zero is InventoryTTL.
func NewInventoryReading(scan func(context.Context) session.Inventory, ttl time.Duration) *InventoryReading {
	if ttl <= 0 {
		ttl = InventoryTTL
	}
	return &InventoryReading{
		scan: scan, ttl: ttl, now: time.Now,
		retainFor: LastGoodInventoryAgeLimit, answerAge: SourceAnswerAgeLimit,
		good: map[string]sourceReading{}, forgotten: map[string]forgottenSession{},
	}
}

// Forget records the terminal backend's positive answer that row was closed.
//
// This is not a metadata tombstone and it does not guess from a missing row:
// Actions.Close calls it only after TerminalHost.Close returned success. The
// terminal/tab or tmux pane is the existence authority; this only stops an
// observation taken before that answer from being drawn afterwards.
func (r *InventoryReading) Forget(row session.Session) {
	if row.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgotten[row.ID] = forgottenSession{at: r.now(), row: row}
	if r.holds {
		r.held.Sessions = withoutSession(r.held.Sessions, row)
	}
	for source, good := range r.good {
		good.rows = withoutSession(good.rows, row)
		r.good[source] = good
	}
}

// Observe adds fn to what every scan's raw reading is shown, once the readers
// waiting on that scan have their answer.
//
// It is how something that must follow the machine whether or not anybody is
// looking — the record of which sessions were open in this boot
// (SessionRestore) — rides on the scans the broker's beat already takes every
// few seconds, rather than taking scans of its own. fn runs on the scan's own
// goroutine, so it holds up the next scan and nobody's answer.
func (r *InventoryReading) Observe(fn func(context.Context, session.Inventory)) {
	r.mu.Lock()
	r.observers = append(r.observers, fn)
	r.mu.Unlock()
}

// SetRetentionAge applies the capacity register's seconds limit. A nonpositive
// value keeps the shipped bound.
func (r *InventoryReading) SetRetentionAge(seconds int64) {
	if seconds <= 0 {
		return
	}
	r.mu.Lock()
	r.retainFor = time.Duration(seconds) * time.Second
	r.mu.Unlock()
}

// SetAnswerAge applies the capacity register's seconds limit for
// SourceAnswerAgeLimit. A nonpositive value keeps the shipped bound.
func (r *InventoryReading) SetAnswerAge(seconds int64) {
	if seconds <= 0 {
		return
	}
	r.mu.Lock()
	r.answerAge = time.Duration(seconds) * time.Second
	r.mu.Unlock()
}

// Recent waits for a reading no older than the TTL. Fast is the lower-latency
// drawing path; Within is for a consumer whose own clock is slower.
func (r *InventoryReading) Recent(ctx context.Context) session.Inventory {
	r.mu.Lock()
	if r.holds && r.now().Sub(r.held.ObservedAt) < r.ttl {
		inv := r.held
		r.counts.Held++
		r.mu.Unlock()
		return inv
	}
	r.mu.Unlock()
	return r.take(ctx, false)
}

// Fast is for drawing and reading a row already shown to the person. Once a
// complete reading exists, an Apple Event that has not answered yet must not
// put every viewer behind it. One scan refreshes the held reading in the
// background; the prior rows are explicitly unverified until it finishes.
// Expired rows are never returned. Fresh remains the path for decisions.
//
// **Unverified is per source, not per reading** (verifiedLocked). A refresh
// waits on its slowest source, and the one that is slow is nearly always
// iTerm2's list Apple Event; tmux and the process table answer it in
// milliseconds. A source whose own answer — in this refresh, or in the one
// that produced the held reading — is complete, recent and lists exactly the
// rows being drawn keeps its rows current and its completeness. Only a source
// that has not answered, answered incompletely, or listed rows the drawing
// lacks is unverified, so it still proves no row gone.
func (r *InventoryReading) Fast(ctx context.Context) session.Inventory {
	r.mu.Lock()
	if !r.holds {
		r.mu.Unlock()
		return r.take(ctx, false)
	}
	now := r.now()
	if now.Sub(r.held.ObservedAt) < r.ttl {
		inv := r.held
		r.counts.Held++
		r.mu.Unlock()
		return inv
	}
	if r.flight == nil && (r.finishedAt.IsZero() || now.Sub(r.finishedAt) >= r.ttl) {
		r.startLocked(ctx)
	}
	inv := r.held
	inv.Sessions = append([]session.Session(nil), inv.Sessions...)
	inv.Sources = cloneSources(inv.Sources)
	inv.Notes = append([]string(nil), inv.Notes...)
	r.counts.Held++
	retainFor := r.retainFor
	verified := r.verifiedLocked(now)
	r.mu.Unlock()

	kept := inv.Sessions[:0]
	oldest := now
	unverified := false
	for _, row := range inv.Sessions {
		at := row.Observation.ObservedAt
		if at.IsZero() || now.Sub(at) >= retainFor || now.Before(at) {
			continue
		}
		if !verified[rowSource(row)] || row.Observation.Freshness != session.FreshnessCurrent {
			row.Observation.Freshness = session.FreshnessUnverified
			unverified = true
		}
		kept = append(kept, row)
		if at.Before(oldest) {
			oldest = at
		}
	}
	inv.Sessions = kept
	for source := range inv.Sources {
		if !verified[source] {
			inv.Sources[source] = false
			unverified = true
		}
	}
	inv.Complete = inv.Complete && len(inv.Sources) > 0 && !unverified
	if len(kept) == 0 {
		return unreadInventory()
	}
	if unverified {
		inv.Observation = session.Observation{ObservedAt: oldest, Provenance: inv.Provenance,
			Freshness: session.FreshnessUnverified}
		inv.Notes = append(inv.Notes, "session inventory refresh is in progress; "+
			"rows from a source that has not answered it are unverified")
	}
	return inv
}

// verifiedLocked is which of the held reading's sources may still answer for
// their rows while a refresh is pending: the source's newest answer is
// complete, younger than answerAge, and lists exactly the rows the held
// reading has from it.
//
// The newest answer is this refresh's when the source has given one. When it
// has not, the answer behind the held reading stands in for it only while the
// refresh is younger than the TTL — the same time a held reading stands in for
// a new one (Recent). A source still silent past that is the stalled one, and
// is unverified however recently it last answered.
func (r *InventoryReading) verifiedLocked(now time.Time) map[string]bool {
	out := map[string]bool{}
	for source, complete := range r.held.Sources {
		if !complete {
			continue
		}
		var answer sourceAnswer
		ok := false
		if r.flight != nil {
			answer, ok = r.flight.answers[source]
			if !ok && now.Sub(r.flight.started) >= r.ttl {
				continue
			}
		}
		if !ok {
			answer, ok = r.answers[source]
		}
		if !ok || !answer.complete {
			continue
		}
		if age := now.Sub(answer.at); age < 0 || age >= r.answerAge {
			r.answersExpired++
			continue
		}
		if answer.vouchesFor(source, r.held.Sessions) {
			out[source] = true
		}
	}
	return out
}

// vouchesFor is whether this answer lists exactly the rows drawn from its
// source: every row it listed is drawn, and every drawn row of its source was
// listed. A row it listed that is not drawn would make its completeness a
// claim that the missing row is gone; a drawn row it did not list is one it
// has since stopped seeing. Either way it cannot answer for the drawing.
func (a sourceAnswer) vouchesFor(source string, rows []session.Session) bool {
	drawn := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := inventoryRowKey(row)
		drawn[key] = true
		if rowSource(row) == source && !a.keys[key] {
			return false
		}
	}
	for key := range a.keys {
		if !drawn[key] {
			return false
		}
	}
	return true
}

func cloneSources(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for name, complete := range src {
		dst[name] = complete
	}
	return dst
}

// Within is Recent for a reader whose own clock is slower than the TTL: it is
// answered from the held reading while that is younger than age. The waiting
// watcher asks this way — its rule is measured in minutes, so a reading the
// broker's beat took a few seconds ago is as good as a new one, and taking a
// new one for it would be a scan nobody else asked for.
func (r *InventoryReading) Within(ctx context.Context, age time.Duration) session.Inventory {
	r.mu.Lock()
	if r.holds && r.now().Sub(r.held.ObservedAt) < max(age, r.ttl) {
		inv := r.held
		r.counts.Held++
		r.mu.Unlock()
		return inv
	}
	r.mu.Unlock()
	return r.take(ctx, false)
}

// Fresh is a reading taken for this call: the broker's beat, which decides from
// it whether a child's tab is still there.
//
// It shares a scan that is already running, because such a scan is still being
// taken now and its answer is the one this caller would have got by scanning
// itself. It never accepts a reading that had already finished.
func (r *InventoryReading) Fresh(ctx context.Context) session.Inventory {
	return r.take(ctx, true)
}

// Counts is how the readers have been answered so far.
func (r *InventoryReading) Counts() InventoryCounts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts
}

// Held is the last completed reading and whether there is one. For a caller
// that wants to say how old what it drew was without causing a scan.
func (r *InventoryReading) Held() (session.Inventory, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held, r.holds
}

func (r *InventoryReading) take(ctx context.Context, fresh bool) session.Inventory {
	r.mu.Lock()
	flight := r.startLocked(ctx)
	r.mu.Unlock()

	select {
	case <-flight.done:
		if fresh {
			return flight.raw
		}
		return flight.display
	case <-ctx.Done():
		r.mu.Lock()
		r.counts.Late++
		held, holds, now, retainFor := r.held, r.holds, r.now(), r.retainFor
		r.mu.Unlock()
		if fresh || !holds || held.Observation.ObservedAt.IsZero() ||
			now.Sub(held.Observation.ObservedAt) >= retainFor {
			return unreadInventory()
		}
		return held
	}
}

// startLocked admits one scan regardless of how many pages ask while an Apple
// Event is slow. The scan has its own clock, not the first caller's deadline.
func (r *InventoryReading) startLocked(ctx context.Context) *inventoryFlight {
	if r.flight != nil {
		r.counts.Joined++
		return r.flight
	}
	flight := &inventoryFlight{done: make(chan struct{}), started: r.now(),
		answers: map[string]sourceAnswer{}}
	r.flight = flight
	r.counts.Scans++
	go r.run(ctx, flight)
	return flight
}

// run takes the reading and hands it to everybody waiting.
func (r *InventoryReading) run(ctx context.Context, flight *inventoryFlight) {
	scan, cancel := context.WithTimeout(context.WithoutCancel(ctx), InventoryScanBudget)
	defer cancel()
	raw := r.scan(context.WithValue(scan, sourceAnswersKey{}, func(inv session.Inventory) {
		r.recordAnswer(flight, inv)
	}))
	r.mu.Lock()
	r.answers = flight.answers
	observed := raw.ObservedAt
	if observed.IsZero() {
		observed = r.now()
	}
	raw = r.withoutForgottenLocked(raw, observed, r.now())
	display := r.retainLocked(raw)
	r.held = display
	r.holds = true
	r.finishedAt = r.now()
	r.flight = nil
	observers := r.observers
	r.mu.Unlock()
	flight.raw = raw
	flight.display = display
	close(flight.done)
	for _, fn := range observers {
		fn(scan, raw)
	}
}

// retainLocked turns one failed source reading into an earlier, named reading
// instead of six fresh unknown placeholders. It does not change raw, which is
// what Fresh returns to callers that decide or act.
func (r *InventoryReading) retainLocked(raw session.Inventory) session.Inventory {
	now := r.now()
	observed := raw.ObservedAt
	if observed.IsZero() {
		observed = now
	}
	display := raw
	display.Sessions = append([]session.Session(nil), raw.Sessions...)
	display.Observation = session.Observation{
		ObservedAt: observed, Provenance: raw.Provenance, Freshness: session.FreshnessCurrent,
	}
	for n := range display.Sessions {
		source := rowSource(display.Sessions[n])
		freshness := session.FreshnessCurrent
		if !rowAnswered(raw, display.Sessions[n]) {
			freshness = session.FreshnessMissing
		}
		display.Sessions[n].Observation = session.Observation{
			ObservedAt: observed, Provenance: source, Freshness: freshness,
		}
	}

	missing := !raw.Complete && len(raw.Sources) == 0
	retained := false
	oldest := observed
	for source, complete := range raw.Sources {
		if complete {
			rows := rowsFromSource(display.Sessions, source)
			for n := range rows {
				rows[n].Observation = session.Observation{
					ObservedAt: observed, Provenance: source, Freshness: session.FreshnessCurrent,
				}
			}
			r.good[source] = sourceReading{at: observed, rows: rows}
			continue
		}

		// The placeholders from an incomplete source are the attempted pass,
		// not its last answer. Remove them before considering the shelf.
		attempted := rowsFromSource(display.Sessions, source)
		display.Sessions = withoutSource(display.Sessions, source)
		good, ok := r.good[source]
		age := now.Sub(good.at)
		if !ok || age < 0 || age >= r.retainFor {
			if ok {
				delete(r.good, source)
				r.expired++
			}
			for _, row := range attempted {
				row.Observation = session.Observation{
					Provenance: source, Freshness: session.FreshnessMissing,
				}
				display.Sessions = append(display.Sessions, row)
			}
			missing = true
			continue
		}
		for _, row := range good.rows {
			row.Observation = session.Observation{
				ObservedAt: good.at, Provenance: source, Freshness: session.FreshnessUnverified,
			}
			display.Sessions = append(display.Sessions, row)
		}
		retained = true
		if oldest.IsZero() || good.at.Before(oldest) {
			oldest = good.at
		}
	}
	sort.SliceStable(display.Sessions, func(i, j int) bool {
		return inventoryRowKey(display.Sessions[i]) < inventoryRowKey(display.Sessions[j])
	})
	switch {
	case missing:
		display.Observation = session.Observation{
			Provenance: raw.Provenance, Freshness: session.FreshnessMissing,
		}
	case retained:
		display.Observation = session.Observation{
			ObservedAt: oldest, Provenance: raw.Provenance, Freshness: session.FreshnessUnverified,
		}
	}
	return display
}

// processSource is the process table's name in a reading's Sources. A row no
// terminal backend owns was listed by it, under its tty.
const processSource = "ps"

// rowSource is the source that answers for one row: its terminal backend's,
// or the process table's for a row only the process table saw.
func rowSource(row session.Session) string {
	if session.SourceForID(row.ID) == processSource {
		return processSource
	}
	if source := session.SourceFor(row.Backend); source != "" {
		return source
	}
	if row.Backend == "" {
		return processSource
	}
	return ""
}

// rowAnswered is whether the source that owns row answered completely in raw.
//
// A row only the process table listed is the process table's to answer for.
// It used to fall back to the whole reading, so every such row read missing
// for as long as iTerm2 could not be asked, although the process table had
// just listed it. A reading with no process table in it (a hand-built one)
// still has only the whole reading to answer.
func rowAnswered(raw session.Inventory, row session.Session) bool {
	source := rowSource(row)
	complete, known := raw.Sources[source]
	if source == "" || (source == processSource && !known) {
		return raw.Complete
	}
	return known && complete
}

// withoutForgottenLocked applies a fact newer than an in-flight reading: a
// successful terminal close. A later complete reading owns the next answer —
// absence confirms the close, while presence says the terminal exists again.
// An unanswered source says neither, so the close fact remains for the same
// bounded window as the retained readings it outranks.
func (r *InventoryReading) withoutForgottenLocked(raw session.Inventory, observed, now time.Time) session.Inventory {
	for id, gone := range r.forgotten {
		if now.Sub(gone.at) >= r.retainFor {
			delete(r.forgotten, id)
			continue
		}
		later := observed.After(gone.at)
		proves, _ := raw.ProvesAbsence(rowSource(gone.row))
		present := hasSession(raw.Sessions, gone.row)
		if later && proves {
			// This source has now answered for itself. Whether it confirmed the
			// absence or showed the same terminal id again, its newer complete
			// enumeration is the absolute truth.
			delete(r.forgotten, id)
			if present {
				continue
			}
		}
		raw.Sessions = withoutSession(raw.Sessions, gone.row)
	}
	return raw
}

func hasSession(rows []session.Session, wanted session.Session) bool {
	for _, row := range rows {
		if sameTerminalSession(row, wanted) {
			return true
		}
	}
	return false
}

func withoutSession(rows []session.Session, wanted session.Session) []session.Session {
	out := rows[:0]
	for _, row := range rows {
		if !sameTerminalSession(row, wanted) {
			out = append(out, row)
		}
	}
	return out
}

func sameTerminalSession(a, b session.Session) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	return a.TTY != "" && b.TTY != "" && a.TTY == b.TTY
}

func rowsFromSource(rows []session.Session, source string) []session.Session {
	out := make([]session.Session, 0, len(rows))
	for _, row := range rows {
		if rowSource(row) == source {
			out = append(out, row)
		}
	}
	return out
}

func withoutSource(rows []session.Session, source string) []session.Session {
	out := rows[:0]
	for _, row := range rows {
		if rowSource(row) != source {
			out = append(out, row)
		}
	}
	return out
}

func inventoryRowKey(row session.Session) string {
	if row.TTY != "" {
		return row.TTY
	}
	return row.ID
}

// RetentionReading is the capacity row for the one time-bounded shelf. Used
// is the oldest retained source's age; expiry is counted rather than hidden.
func (r *InventoryReading) RetentionReading() capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	reading := capacity.Reading{
		Known: true, WindowSeconds: int64(r.retainFor / time.Second),
		Counters: capacity.Counters{Expired: r.expired},
	}
	if len(r.good) == 0 {
		reading.Note = "no complete terminal-source reading is held"
		return reading
	}
	now := r.now()
	oldest := now
	for _, good := range r.good {
		if good.at.Before(oldest) {
			oldest = good.at
		}
	}
	age := now.Sub(oldest)
	if age > 0 {
		reading.Used = int64(age / time.Second)
	}
	reading.OldestAt = oldest
	return reading
}

// AnswerReading is the capacity row for SourceAnswerAgeLimit. Used is the
// oldest answer behind the held reading; Expired counts the drawings in which
// a complete answer was too old to vouch for its rows.
func (r *InventoryReading) AnswerReading() capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	reading := capacity.Reading{
		Known: true, WindowSeconds: int64(r.answerAge / time.Second),
		Counters: capacity.Counters{Expired: r.answersExpired},
	}
	if len(r.answers) == 0 {
		reading.Note = "no scan has recorded its sources' answers yet"
		return reading
	}
	now := r.now()
	oldest := now
	for _, answer := range r.answers {
		if answer.at.Before(oldest) {
			oldest = answer.at
		}
	}
	if age := now.Sub(oldest); age > 0 {
		reading.Used = int64(age / time.Second)
	}
	reading.OldestAt = oldest
	return reading
}

// unreadInventory is the answer when the caller's clock ran out before any
// reading was taken for it.
//
// It is not an empty machine and must never be read as one: Complete is false,
// so nothing downstream treats it as authoritative, `EmptyAuthoritative` is
// false, and the note says which it is. Handing back an older reading instead
// would be the defect this whole file is about — an expired snapshot standing
// in for evidence.
func unreadInventory() session.Inventory {
	return session.Inventory{
		Provenance:  "unread",
		Complete:    false,
		Sources:     map[string]bool{},
		Notes:       []string{"this reading was not taken: the caller's own clock ran out before the scan finished"},
		Observation: session.Observation{Provenance: "unread", Freshness: session.FreshnessMissing},
	}
}
