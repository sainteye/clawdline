package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// What the broker knows about itself, and what it has only observed.
//
// Two rules from docs/broker-design.md live here, and both answer the Swift
// app's measured failures rather than a preference:
//
//   - **The beat reports itself (§6.6).** The Swift app's /v1/health had no
//     field that could say its heartbeat had stopped; four times its main
//     thread was held by a synchronous call, the app looked alive, and every
//     read stopped. So this loop records when each pass began and ended, and
//     a reader can ask "has it finished a pass lately" without trusting the
//     loop to answer — a stopped loop cannot say it stopped, so the question
//     is answered from outside it (Stalled).
//   - **Facts are written, observations are not (§6.2).** The Swift app rewrote
//     a 6.75 MB file every 10.5 seconds, and four times in five the only thing
//     that had changed was "I looked at the child's executor again and it was
//     still there". Which terminal a child is running in, when that was last
//     seen, whether a root is showing a menu — the next reading recomputes all
//     of them, and a restart begins a new epoch in which the old ones are not
//     compared anyway. They live here, in memory, and reach a reader through
//     /v1/diagnostics and the task's own detail, never through the store.

// stallFactor is how many ticks may pass without a finished pass before the
// beat is called stalled. Three: one late pass is a slow terminal, three is a
// loop that is not coming back by itself.
const stallFactor = 3

// Restart backoff after a panic: 1, 2, 4 … seconds, at most 30. A beat that
// panics on every pass must not spin, and one that panicked once must not wait
// long to be back. The first rung is a variable so a test can reach the
// restart without waiting a real second — a limit a test cannot inject is a
// limit whose test stops testing it.
var restartBase = time.Second

const (
	restartCeiling = 30 * time.Second
	panicsKept     = 5
	passesSampled  = 128
)

// PanicTrace is one death of the beat: a recovered panic, or — with Exited —
// a goroutine that left without being asked to, which has no value and no
// stack but is a death all the same.
type PanicTrace struct {
	At     time.Time
	Pass   int64
	Value  string
	Stack  string
	Exited bool
}

type beatState struct {
	mu        sync.Mutex
	running   bool
	tick      time.Duration
	started   time.Time
	passes    int64
	inPass    int
	began     time.Time
	ended     time.Time
	last      time.Duration
	durations [passesSampled]time.Duration
	sampled   int
	next      int
	overlaps  int64
	restarts  int64
	backoff   time.Time
	panics    []PanicTrace
	pulse     Pulse
	storeErr  string
	storeAt   time.Time
}

// begin opens one pass and answers its number. An overlapping pass is not
// refused, only counted: the Swift app's beat was re-entered once through a
// run loop, and the counter it kept afterwards is kept here for the same
// reason — a second cause would look exactly like the first one did, and
// silence is not evidence.
func (s *beatState) begin(at time.Time) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inPass > 0 {
		s.overlaps++
	}
	s.inPass++
	s.passes++
	s.began = at
	return s.passes
}

func (s *beatState) end(at time.Time, p Pulse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inPass > 0 {
		s.inPass--
	}
	s.ended = at
	s.last = at.Sub(s.began)
	s.durations[s.next] = s.last
	s.next = (s.next + 1) % passesSampled
	if s.sampled < passesSampled {
		s.sampled++
	}
	s.pulse = p
	if p.StoreErr == "" {
		s.storeErr = ""
	} else {
		s.storeErr, s.storeAt = p.StoreErr, at
	}
}

// abandon closes a pass that panicked, so the next one is not counted as an
// overlap with a pass that will never finish.
func (s *beatState) abandon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inPass > 0 {
		s.inPass--
	}
}

// Run is the beat under a supervisor: Watch, recovered when it panics, and
// started again with backoff.
//
// The supervisor does one thing, the one thing it can prove is safe (§6.6
// rule 4, and the Swift app's C3): it restarts **this broker's own loop**. It
// does not restart the daemon, does not touch anybody's session, and does not
// start a second loop beside one that is merely slow — a hung pass cannot be
// killed from outside in Go, and a second loop beside it is two beats the day
// the first one wakes. A hang is reported (Stalled), not repaired.
func (b *Broker) Run(ctx context.Context, tick time.Duration, report func(Pulse)) {
	if tick <= 0 {
		tick = 5 * time.Second
	}
	b.beat.mu.Lock()
	b.beat.running = true
	b.beat.tick = tick
	b.beat.started = b.now()
	b.beat.mu.Unlock()
	// A start owes the to-do list its reconcile (todos.go): whatever was
	// recorded while no broker kept it is made now, on the first pass.
	b.todosOwed.Store(true)
	delay := restartBase
	for {
		b.beat.mu.Lock()
		endedBefore := b.beat.ended
		b.beat.mu.Unlock()
		trace, died := b.runOnce(ctx, tick, report)
		if ctx.Err() != nil || !died {
			return
		}
		b.beat.mu.Lock()
		// A loop that finished passes before it died is back on the first
		// rung; one that dies before finishing any climbs the ladder.
		if b.beat.ended.After(endedBefore) {
			delay = restartBase
		}
		b.beat.restarts++
		restarts := b.beat.restarts
		if trace != nil && !trace.Exited {
			b.beat.panics = append(b.beat.panics, *trace)
			if len(b.beat.panics) > panicsKept {
				b.beat.panics = b.beat.panics[len(b.beat.panics)-panicsKept:]
			}
		}
		b.beat.backoff = b.now().Add(delay)
		b.beat.mu.Unlock()
		b.recordDeath(ctx, trace, restarts, delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > restartCeiling {
			delay = restartCeiling
		}
	}
}

// runOnce runs Watch in a goroutine of its own and waits for it. It answers
// whether that goroutine died — by a panic, which comes back as a trace, or by
// leaving without its context having ended, which is a death with no value to
// report — as opposed to stopping because it was asked to.
func (b *Broker) runOnce(ctx context.Context, tick time.Duration, report func(Pulse)) (*PanicTrace, bool) {
	type exit struct {
		trace *PanicTrace
		died  bool
	}
	done := make(chan exit, 1)
	go func() {
		finished := false
		defer func() {
			if v := recover(); v != nil {
				b.beat.abandon()
				b.beat.mu.Lock()
				pass := b.beat.passes
				b.beat.mu.Unlock()
				done <- exit{trace: &PanicTrace{
					At: b.now(), Pass: pass, Value: fmt.Sprint(v), Stack: trimStack(debug.Stack()),
				}, died: true}
				return
			}
			// runtime.Goexit, or a return nobody asked for: still a death.
			if !finished || ctx.Err() == nil {
				b.beat.abandon()
				if ctx.Err() != nil {
					done <- exit{}
					return
				}
				b.beat.mu.Lock()
				pass := b.beat.passes
				b.beat.mu.Unlock()
				done <- exit{trace: &PanicTrace{
					At: b.now(), Pass: pass, Exited: true,
					Value: "the beat goroutine exited without being asked to",
				}, died: true}
				return
			}
			done <- exit{}
		}()
		b.Watch(ctx, tick, report)
		finished = true
	}()
	out := <-done
	return out.trace, out.died
}

// recordDeath leaves the trace a panic owes: a log line a person reads and a
// typed event a program can count, both before the loop is started again.
func (b *Broker) recordDeath(ctx context.Context, trace *PanicTrace, restarts int64, delay time.Duration) {
	value, pass, stack, kind := "the beat goroutine exited without being asked to", int64(0), "", "broker.beat.exited"
	if trace != nil {
		value, pass, stack = trace.Value, trace.Pass, trace.Stack
		if !trace.Exited {
			kind = "broker.beat.panicked"
		}
	}
	log.Printf("orchestrator: beat died on pass %d (%s); restart %d in %s", pass, value, restarts, delay)
	if b.Store == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"pass": pass, "value": value, "restarts": restarts,
		"restart_in_ms": delay.Milliseconds(), "stack": stack,
	})
	// A fresh context: the death is a fact whatever the caller's deadline.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := b.Store.Append(writeCtx, store.Event{Kind: kind, Subject: "broker", Payload: payload}); err != nil {
		log.Printf("orchestrator: the beat's death could not be recorded: %v", err)
	}
}

func trimStack(stack []byte) string {
	const limit = 4096
	if len(stack) > limit {
		return string(stack[:limit])
	}
	return string(stack)
}

// Stalled answers whether the beat has gone more than three ticks without
// finishing a pass, and for how long. A beat that was never started is not
// stalled: there is nothing that should be running, and "not started" has its
// own sentence in the diagnostics.
func (b *Broker) Stalled() (bool, time.Duration) {
	b.beat.mu.Lock()
	defer b.beat.mu.Unlock()
	return b.stalledLocked(b.now())
}

func (b *Broker) stalledLocked(now time.Time) (bool, time.Duration) {
	if !b.beat.running {
		return false, 0
	}
	since := b.beat.started
	if b.beat.ended.After(since) {
		since = b.beat.ended
	}
	quiet := now.Sub(since)
	return quiet > b.beat.tick*stallFactor, quiet
}

// BeatReport is the beat's account of itself, for /v1/diagnostics.
type BeatReport struct {
	Running  bool
	Tick     time.Duration
	Started  time.Time
	At       time.Time // the last finished pass; zero before the first
	Began    time.Time // the pass in progress, when InPass
	InPass   bool
	Passes   int64
	Last     time.Duration
	P99      time.Duration
	Overlaps int64
	Restarts int64
	Backoff  time.Time
	Stalled  bool
	Quiet    time.Duration
	Panics   []PanicTrace
	Pulse    Pulse
	StoreErr string
	StoreAt  time.Time
}

// Beat reads the beat's account.
func (b *Broker) Beat() BeatReport {
	b.beat.mu.Lock()
	defer b.beat.mu.Unlock()
	stalled, quiet := b.stalledLocked(b.now())
	out := BeatReport{
		Running: b.beat.running, Tick: b.beat.tick, Started: b.beat.started, At: b.beat.ended,
		Began: b.beat.began, InPass: b.beat.inPass > 0, Passes: b.beat.passes, Last: b.beat.last,
		Overlaps: b.beat.overlaps, Restarts: b.beat.restarts, Backoff: b.beat.backoff,
		Stalled: stalled, Quiet: quiet, Pulse: b.beat.pulse,
		StoreErr: b.beat.storeErr, StoreAt: b.beat.storeAt,
		Panics: append([]PanicTrace(nil), b.beat.panics...),
	}
	if b.beat.sampled > 0 {
		sample := make([]time.Duration, b.beat.sampled)
		copy(sample, b.beat.durations[:b.beat.sampled])
		sort.Slice(sample, func(i, j int) bool { return sample[i] < sample[j] })
		idx := (len(sample)*99 + 99) / 100
		if idx > len(sample) {
			idx = len(sample)
		}
		out.P99 = sample[idx-1]
	}
	return out
}

// Executor statuses. Only a change into or out of `executor_missing` is a fact
// worth an event; everything else is the reading of the moment.
const (
	ExecutorObserved = "observed"
	ExecutorNotSeen  = "not_seen"
	ExecutorMissing  = "executor_missing"
	ExecutorUnknown  = "unknown"
)

// missingAfter is how far apart two complete readings that both lack a child's
// terminal must be before it is called missing: the Swift app's rule (E6), two
// observations in one process epoch at least a minute apart.
const missingAfter = time.Minute

// Executor is what the beat last saw of the session running one task.
type Executor struct {
	Status        string
	TerminalID    string
	SessionState  string
	ObservedAt    time.Time // the last reading that showed it
	Generation    int64     // the reading that showed it
	FirstUnseenAt time.Time // the first complete reading that did not
}

// Observations is the beat's in-memory reading, for /v1/diagnostics.
type Observations struct {
	Generation int64
	At         time.Time
	Complete   bool
	// Sources is each source's own completeness in that reading (D05 ③).
	Sources   map[string]bool
	Sessions  int
	Executors map[string]Executor
	Deferred  int
}

type observations struct {
	mu         sync.Mutex
	generation int64
	at         time.Time
	complete   bool
	sources    map[string]bool
	sessions   int
	executors  map[string]Executor
	deferred   map[string]time.Time
	progress   map[string]string
	accepted   map[string]string
	// seen is who the last reading showed, for a pending landing's owner
	// (landing.go). Replaced whole by every pass.
	seen presence
}

// presence is the last reading's account of who is here.
func (o *observations) presence() presence {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.seen
}

// deferredUntil is when a deferred notice may be tried again, zero when it is
// not deferred. A deferral that has passed is forgotten on the way out, so the
// map holds only waits that are still running.
func (o *observations) deferredUntil(noticeID string, now time.Time) time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	until, ok := o.deferred[noticeID]
	if ok && !until.After(now) {
		delete(o.deferred, noticeID)
		return time.Time{}
	}
	return until
}

func (o *observations) deferNotice(noticeID string, until time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.deferred == nil {
		o.deferred = map[string]time.Time{}
	}
	o.deferred[noticeID] = until
}

func (o *observations) forgetNotice(noticeID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.deferred, noticeID)
}

// progressKnown answers whether this exact progress.json body was already
// settled for this task — taken, or refused for good. Re-reading an unchanged
// file is an observation; only a body not settled before is offered to the
// store.
func (o *observations) progressKnown(taskID, body string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.progress[taskID] == body && body != ""
}

// settleProgress remembers a body as settled. It is called only for outcomes
// that another read would reach again — accepted, already held, refused by
// secret or length — and never after a store error, so a note is not lost to
// a moment the store could not answer.
func (o *observations) settleProgress(taskID, body string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.progress == nil {
		o.progress = map[string]string{}
	}
	o.progress[taskID] = body
}

// acceptedKnown and settleAccepted are progressKnown and settleProgress for
// accepted.json: a receipt body refused once is not re-checked every pass.
func (o *observations) acceptedKnown(taskID, secret string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.accepted[taskID]
	return ok && v == secret
}

func (o *observations) settleAccepted(taskID, secret string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.accepted == nil {
		o.accepted = map[string]string{}
	}
	o.accepted[taskID] = secret
}

// Observed reads the beat's current observations.
func (b *Broker) Observed() Observations {
	o := &b.observed
	o.mu.Lock()
	defer o.mu.Unlock()
	out := Observations{
		Generation: o.generation, At: o.at, Complete: o.complete, Sessions: o.sessions,
		Executors: make(map[string]Executor, len(o.executors)), Deferred: len(o.deferred),
		Sources: make(map[string]bool, len(o.sources)),
	}
	for name, complete := range o.sources {
		out.Sources[name] = complete
	}
	for id, e := range o.executors {
		out.Executors[id] = e
	}
	return out
}

// ExecutorOf is the live observation for one task, if the beat has one.
func (b *Broker) ExecutorOf(taskID string) (Executor, bool) {
	b.observed.mu.Lock()
	defer b.observed.mu.Unlock()
	e, ok := b.observed.executors[taskID]
	return e, ok
}

// reading is the one reading of the machine a pass is decided against.
type reading struct {
	sessions map[string]session.Session
	complete bool
	// sources is each source's own completeness, by backend name ("tmux",
	// "iterm", "ps"). A question about one tab is answered by the source that
	// owns it (D05 ③); nil means the reading did not say, which answers no
	// such question.
	sources map[string]bool
	at      time.Time
}

func (r reading) session(terminalID string) (session.Session, bool) {
	s, ok := r.sessions[terminalID]
	return s, ok
}

// sourceComplete is whether the source that owns backend answered completely.
// A backend the reading has no word about is not complete: unknown is not
// "listed, and absent".
func (r reading) sourceComplete(backend string) bool {
	if backend == "" {
		return false
	}
	complete, said := r.sources[backend]
	return said && complete
}

// read takes one reading for a pass. Completeness is the reading's own claim;
// a fallback reading with no sessions at all is never complete, because "a
// reading with no terminals in it at all" is not allowed to decide anybody is
// gone (the Swift app's `3a7adb8e`). A fallback also carries no per-source
// completeness, so nothing about one tab's absence is decided from it.
func (b *Broker) read(ctx context.Context) reading {
	out := reading{sessions: map[string]session.Session{}, at: b.now()}
	var rows []session.Session
	switch {
	case b.Reading != nil:
		inv := b.Reading(ctx)
		rows, out.complete = inv.Sessions, inv.Complete
		out.sources = inv.Sources
	case b.Live != nil:
		rows = b.Live(ctx)
		out.complete = true
	}
	if len(rows) == 0 {
		out.complete = false
	}
	for _, s := range rows {
		out.sessions[s.ID] = s
	}
	return out
}

// observe updates the in-memory executor reading for every live task, and
// forgets the tasks that are no longer live. It writes nothing unless a
// child's status crosses into or out of executor_missing — a conclusion the
// broker reached, which is a fact — and then writes one event, never the
// record.
func (b *Broker) observe(ctx context.Context, rd reading, live []Record) {
	o := &b.observed
	type crossing struct {
		task, from, to string
		e              Executor
	}
	crossings := []crossing{}
	o.mu.Lock()
	o.generation++
	o.at = rd.at
	o.complete = rd.complete
	o.sources = rd.sources
	o.sessions = len(rd.sessions)
	o.seen = presenceOf(rd)
	if o.executors == nil {
		o.executors = map[string]Executor{}
	}
	keep := map[string]bool{}
	for _, r := range live {
		if r.ChildTerminalID == "" || (r.State != StateSpawning && r.State != StateBriefed) {
			continue
		}
		keep[r.ID] = true
		prev, had := o.executors[r.ID]
		next := prev
		next.TerminalID = r.ChildTerminalID
		if s, ok := rd.session(r.ChildTerminalID); ok && s.IsAssistant() {
			next.Status = ExecutorObserved
			next.SessionState = string(s.State)
			next.ObservedAt = rd.at
			next.Generation = o.generation
			next.FirstUnseenAt = time.Time{}
		} else if rd.complete {
			next.SessionState = ""
			if next.FirstUnseenAt.IsZero() {
				next.FirstUnseenAt = rd.at
			}
			if rd.at.Sub(next.FirstUnseenAt) >= missingAfter {
				next.Status = ExecutorMissing
			} else if next.Status != ExecutorMissing {
				next.Status = ExecutorNotSeen
			}
		} else if !had {
			// An incomplete reading decides nothing, and a first look that
			// cannot see is not a look.
			next.Status = ExecutorUnknown
		}
		o.executors[r.ID] = next
		if (prev.Status == ExecutorMissing) != (next.Status == ExecutorMissing) {
			crossings = append(crossings, crossing{r.ID, prev.Status, next.Status, next})
		}
	}
	for id := range o.executors {
		if !keep[id] {
			delete(o.executors, id)
		}
	}
	for id := range o.progress {
		if !keep[id] {
			delete(o.progress, id)
		}
	}
	for id := range o.accepted {
		if !keep[id] {
			delete(o.accepted, id)
		}
	}
	o.mu.Unlock()

	for _, c := range crossings {
		payload, _ := json.Marshal(map[string]any{
			"task": c.task, "from": c.from, "to": c.to, "terminal": c.e.TerminalID,
			"first_unseen_at": c.e.FirstUnseenAt.Unix(),
		})
		_ = b.Store.Append(ctx, store.Event{Kind: "task.executor." + c.to, Subject: c.task, Payload: payload})
	}
}

// progressBus hands each accepted note to every open stream.
//
// A subscriber that does not keep up loses notes rather than slowing the
// broker: the send is non-blocking, and the note is still in the store, where
// the stream's own reconnect snapshot and GET /tasks/:id both find it. A
// stream is a view; the store is the record.
type progressBus struct {
	mu   sync.Mutex
	next int
	subs map[int]chan store.BrokerNote
}

func (p *progressBus) publish(n store.BrokerNote) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ch := range p.subs {
		select {
		case ch <- n:
		default:
		}
	}
}

// SubscribeProgress opens a feed of accepted notes. The caller must call the
// returned function when it stops reading.
func (b *Broker) SubscribeProgress() (<-chan store.BrokerNote, func()) {
	p := &b.progress
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.subs == nil {
		p.subs = map[int]chan store.BrokerNote{}
	}
	p.next++
	id := p.next
	ch := make(chan store.BrokerNote, 64)
	p.subs[id] = ch
	return ch, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.subs, id)
	}
}

// RecentProgress is the newest notes of every live task, oldest first, for a
// stream that has just connected: a page that reconnects is level without
// replaying anything, as the Swift app's stream is on attach.
func (b *Broker) RecentProgress(ctx context.Context) ([]store.BrokerNote, error) {
	live, err := b.liveTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := []store.BrokerNote{}
	for _, r := range live {
		notes, err := b.Store.BrokerNotes(ctx, r.ID, progressKept)
		if err != nil {
			return nil, err
		}
		out = append(out, notes...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}
