package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/logs"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// The capacity register on this daemon (docs/limits.md §4, design-decisions
// D27): a beat that measures every registered row, `/v1/diagnostics.capacity`
// with the whole reading, and `/v1/health` saying `capacity_exhausted` — the
// reason only, never a name or a number — when an evidence or security-audit
// row is full with nothing making room.
//
// Both routes read what the last pass saw. A read measures nothing and writes
// nothing: the transitions and notices a reading causes are events in the
// store, and a GET that wrote them would be a read with a write's effect. The
// pass runs at start and on the scheduler's tick; `beat.at` says how old the
// reading is, and a beat that stops turns health red within three ticks, as
// the broker's does.
//
// A notice about a row whose register line tells a person by notice owes a
// push (C4). The `capacity.notify` event and that push are recorded in one
// transaction — the outbox's, so the broker that runs every other effect runs
// this one, and a daemon that dies between the commit and the push leaves a
// row the next one settles exactly once. The push is sent after the pass, off
// the beat, so a push service having a slow afternoon never makes the reading
// late. Its budget is the tracker's one notice per row a day, read back from
// the store when the beat starts so that a restart does not spend it again,
// and it is counted apart from everything the agents' notifications spend.

// capacityPushDeadline bounds one pass's pushes, retries and all, like the
// dead letter's.
const capacityPushDeadline = 2 * time.Minute

func (s *Server) terminalCapacity(name string) capacity.Reading {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		return capacity.Reading{Known: true, Note: "Cloud terminal line is off"}
	}
	reader, ok := line.(interface{ TerminalCapacity(string) capacity.Reading })
	if !ok {
		return capacity.Unmeasured("Cloud line does not report terminal capacity")
	}
	return reader.TerminalCapacity(name)
}

func (s *Server) peerStoreCapacity(name string) capacity.Reading {
	usage, err := peerstore.ReadUsage(context.Background(), filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	var used int64
	switch name {
	case capacity.CloudPeerPairs:
		used = usage.Pairs
	case capacity.CloudPeerGrants:
		used = usage.Grants
	case capacity.CloudPeerInbox:
		used = usage.Inbox
	case capacity.CloudPeerOutbox:
		used = usage.Outbox
	}
	return capacity.Reading{Known: true, Used: used}
}

// capacityOverrides is the register resolved against CLAWDLINE_NEXT_CAPACITY,
// once per process: every adapter that enforces a row's limit and the beat
// that measures it read the same numbers.
var capacityOverrides = sync.OnceValues(func() ([]capacity.Resolved, []string) {
	resolved, problems := capacity.Resolve(capacity.Register(), os.Getenv(capacity.OverrideEnv))
	for _, p := range problems {
		log.Printf("capacity: %s refused: %s", capacity.OverrideEnv, p)
	}
	for _, r := range resolved {
		if r.Overridden {
			// Said once, where a person reading why something filled early
			// will look: this limit is lower than the one the register ships.
			log.Printf("capacity: %s is overridden to %d %s (default %d)", r.Entry.Name, r.Limit, r.Entry.Unit, r.Entry.Limit)
		}
	}
	return resolved, problems
})

// CapacityLimit is the limit this process runs a registered row at: the
// register's default, or lower when CLAWDLINE_NEXT_CAPACITY says so.
func CapacityLimit(name string) int64 {
	resolved, _ := capacityOverrides()
	return capacity.Limit(resolved, name)
}

// capacityBeat is the loop that measures the register, and what its last pass
// saw.
type capacityBeat struct {
	resolved []capacity.Resolved
	problems []string
	tracker  *capacity.Tracker
	// measure builds one pass's readers, so a reading several rows share is
	// taken once per pass and never carried into the next.
	measure func() map[string]func() capacity.Reading
	record  func(context.Context, store.Event) error
	since   time.Time

	// intent records a notice's event with the push it owes, in one
	// transaction; run sends pushes so recorded; latest reads the last push
	// each row owed. With no intent or run a notice is only an event.
	intent func(context.Context, []store.Event, []store.Effect) ([]int64, error)
	run    func(context.Context, []int64)
	latest func(context.Context) ([]store.Effect, error)
	// loc is where a notice writes the day a row will be full.
	loc *time.Location
	// language is read when a push intent is recorded, then its text is fixed.
	language func() string
	// diskSources measures the largest disk users for a low free-space
	// notice, only when that notice is recorded (at most once per cooldown).
	diskSources func(context.Context) []capacity.DiskSource
	// sending is the pushes sent off the beat and not yet finished.
	sending sync.WaitGroup

	// poke asks for a pass now rather than at the next tick: a write that
	// failed, a picture stored. One slot, because two pokes before a pass are
	// one pass.
	poke chan struct{}
	// passMu is held for the length of one pass, so a poke and a tick never
	// measure at once and never record one transition twice.
	passMu sync.Mutex

	mu        sync.Mutex
	started   time.Time
	tick      time.Duration
	passes    int64
	at        time.Time
	rows      []capacity.Status
	eventErrs int64
	eventErr  string
	// seeded says the tracker has been told what earlier processes last
	// said about each row; pushes is the last push each row owed, by name.
	seeded bool
	pushes map[string]*contract.CapacityLastPush
}

var capacityByServer sync.Map // *Server -> *capacityBeat

// capacity is this server's beat, built on first use. Building it measures
// nothing.
func (s *Server) capacity() *capacityBeat {
	if b, ok := capacityByServer.Load(s); ok {
		return b.(*capacityBeat)
	}
	resolved, problems := capacityOverrides()
	beat := &capacityBeat{
		resolved:    resolved,
		problems:    problems,
		tracker:     capacity.NewTracker(),
		measure:     s.capacityMeasures,
		record:      s.store.Append,
		since:       time.Now(),
		poke:        make(chan struct{}, 1),
		loc:         time.Local,
		language:    s.productLanguage,
		diskSources: s.lowDiskSources,
		latest: func(ctx context.Context) ([]store.Effect, error) {
			return s.store.LatestEffects(ctx, orchestrator.EffectCapacityPush)
		},
	}
	if s.broker != nil {
		beat.intent, beat.run = s.store.RecordIntent, s.broker.RunEffects
	}
	b, _ := capacityByServer.LoadOrStore(s, beat)
	return b.(*capacityBeat)
}

// nudge asks the beat to measure now. It never blocks: a pass already asked
// for is the pass this one wanted. A beat that is not running is not started
// by it; the next pass, whenever it comes, sees what happened.
func (b *capacityBeat) nudge() {
	select {
	case b.poke <- struct{}{}:
	default:
	}
}

// daemonLogs is the log file each state directory's daemon writes, set by
// `clawdline serve` once it has moved the log there. A process that never did
// (a test, a CLI command) has none, and the row says so.
var daemonLogs sync.Map // dir -> *logs.Writer

// SetDaemonLog records the log file this process writes for dir, so the
// register can measure it.
func SetDaemonLog(dir string, w *logs.Writer) { daemonLogs.Store(dir, w) }

// capacityMeasures is one measurement per registered row, each the adapter's
// own and each cheap: a stat, a length, a count kept in memory. A pass builds
// them anew, so a reading shared by several rows is taken once per pass.
func (s *Server) capacityMeasures() map[string]func() capacity.Reading {
	measures := map[string]func() capacity.Reading{}
	for _, a := range capacityMeasureAreas {
		for name, m := range a.measures(s) {
			measures[name] = m
		}
	}
	return measures
}

func countedCapacityReading(n int64, err error, note string) capacity.Reading {
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	return capacity.Reading{Known: true, Used: n, Note: note}
}

func (s *Server) workV2CapacityReading(name string) capacity.Reading {
	counts, err := s.store.WorkV2CapacityCounts(context.Background())
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	return capacity.Reading{Known: true, Used: counts[name]}
}

func (s *Server) workV2DocumentCapacityReading() capacity.Reading {
	counts, err := s.store.WorkV2CapacityCounts(context.Background())
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	r := capacity.Reading{Known: true, Used: counts["documents_per_item"]}
	for _, group := range []struct {
		closed bool
		count  int64
		label  string
	}{
		{false, r.Used, "Writable items at the document limit"},
		{true, counts["closed_documents_per_item"], "Closed items retained at the document limit"},
	} {
		if group.count < store.WorkV2DocumentLimit {
			continue
		}
		ids, err := s.store.WorkV2ItemsAtDocumentLimit(context.Background(), group.closed)
		if err != nil {
			return capacity.Unmeasured(err.Error())
		}
		if len(ids) == 0 {
			continue
		}
		more := ""
		if len(ids) > 3 {
			ids, more = ids[:3], ", and more"
		}
		if r.Note != "" {
			r.Note += "; "
		}
		r.Note += group.label + ": " + strings.Join(ids, ", ") + more
	}
	if r.Used >= store.WorkV2DocumentLimit {
		r.Note += ". Revise an existing document or split distinct work into another item."
	}
	return r
}

func (s *Server) humanInterventionCapacityReading(open bool) capacity.Reading {
	maxOpen, total, err := s.store.HumanInterventionCapacityCounts(context.Background())
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	if open {
		return capacity.Reading{Known: true, Used: maxOpen}
	}
	return capacity.Reading{Known: true, Used: total}
}

// spoolReadings are the Cloud spool's two rows. A line that is off, or was
// never built, has no spool: that is a known zero, said in the note.
func (s *Server) spoolReadings() (rows, bytes capacity.Reading) {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		off := capacity.Reading{Known: true, Note: "the Cloud line is off: there is no spool"}
		return off, off
	}
	q, ok := line.(interface {
		SpoolReadings() (rows, bytes capacity.Reading, ok bool)
	})
	if !ok {
		unknown := capacity.Unmeasured("this Cloud line does not report its spool")
		return unknown, unknown
	}
	rows, bytes, ok = q.SpoolReadings()
	if !ok {
		never := capacity.Reading{Known: true, Note: "the Cloud line was never built: there is no spool"}
		return never, never
	}
	return rows, bytes
}

// spoolChannelReadings are the same spool's other two rows, answered the same
// way as the two above: a line that is off, or was never built, is a known
// zero said in the note rather than an unmeasured row.
func (s *Server) spoolChannelReadings() (channelBytes, receipts capacity.Reading) {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		off := capacity.Reading{Known: true, Note: "the Cloud line is off: there is no spool"}
		return off, off
	}
	q, ok := line.(interface {
		SpoolChannelReadings() (channelBytes, receipts capacity.Reading, ok bool)
	})
	if !ok {
		unknown := capacity.Unmeasured("this Cloud line does not report its spool")
		return unknown, unknown
	}
	channelBytes, receipts, ok = q.SpoolChannelReadings()
	if !ok {
		never := capacity.Reading{Known: true, Note: "the Cloud line was never built: there is no spool"}
		return never, never
	}
	return channelBytes, receipts
}

func (s *Server) spoolRefusalReading() capacity.Reading {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		return capacity.Reading{Known: true, Note: "the Cloud line is off: there is no spool"}
	}
	q, ok := line.(interface {
		SpoolRefusalReading() (capacity.Reading, bool)
	})
	if !ok {
		return capacity.Unmeasured("this Cloud line does not report its spool")
	}
	r, ok := q.SpoolRefusalReading()
	if !ok {
		return capacity.Reading{Known: true, Note: "the Cloud line was never built: there is no spool"}
	}
	return r
}

// StartCapacity runs the register's beat on the scheduler's tick, the first
// pass at once.
func (s *Server) StartCapacity(ctx context.Context) {
	b := s.capacity()
	tick := schedulerTick()
	b.mu.Lock()
	if !b.started.IsZero() {
		b.mu.Unlock()
		return
	}
	b.started, b.tick = time.Now(), tick
	b.mu.Unlock()
	// A write the store refused is measured at once, not a tick later: the
	// store.db row turns failing, and /v1/health says so, on the pass this
	// starts (limits N2). A picture stored is measured at once for the same
	// reason, so the warning about a store that is filling comes before the
	// write that makes it let the oldest go (limits N15, N16).
	s.store.ObserveFailing(func(bool) { b.nudge() })
	if s.pictures.store != nil && s.pictures.drops != nil {
		s.pictures.changed(b.nudge)
	}
	go func() {
		b.pass(ctx)
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				b.pass(ctx)
			case <-b.poke:
				b.pass(ctx)
			}
		}
	}()
	log.Printf("capacity: measuring %d rows every %s", len(b.resolved), tick)
}

// pass measures every row once and records what that caused. A pass that
// panics is logged and does not count as one: the next tick tries again, and
// a beat that keeps panicking is a stalled beat, which health says.
func (b *capacityBeat) pass(ctx context.Context) {
	b.passMu.Lock()
	defer b.passMu.Unlock()
	defer func() {
		if v := recover(); v != nil {
			log.Printf("capacity: a pass panicked: %v", v)
		}
	}()
	b.readPushes(ctx)
	now := time.Now()
	rows := make([]capacity.Status, len(b.resolved))
	var events []capacity.Event
	var notices []owedNotice
	measures := b.measure()
	for i, res := range b.resolved {
		measure := measures[res.Entry.Name]
		r := capacity.Unmeasured("nothing measures this row")
		if measure != nil {
			r = measure()
		}
		st, ev := b.tracker.Observe(res, r, now)
		rows[i] = st
		for _, e := range ev {
			if e.Kind == capacity.EventNotify && b.intent != nil && b.run != nil && capacity.Pushes(res.Entry) {
				notices = append(notices, owedNotice{status: st, event: e})
				continue
			}
			events = append(events, e)
		}
	}
	var failed int64
	var lastErr string
	for _, e := range events {
		payload, err := json.Marshal(e.Payload)
		if err == nil {
			err = b.record(ctx, store.Event{Kind: e.Kind, Subject: e.Subject, Payload: payload})
		}
		if err != nil {
			failed++
			lastErr = err.Error()
			log.Printf("capacity: could not record %s for %s: %v", e.Kind, e.Subject, err)
		}
		if e.Kind == capacity.EventNotify {
			log.Printf("capacity: %s is %v (%v of %v)", e.Subject, e.Payload["state"], e.Payload["used"], e.Payload["limit"])
		}
	}
	var owed []int64
	for _, n := range notices {
		id, err := b.recordNotice(ctx, n)
		if err != nil {
			// Nothing is sent for a notice that could not be written down:
			// a push the store has no record of is one a restart would send
			// again, and one nobody could account for.
			failed++
			lastErr = err.Error()
			log.Printf("capacity: could not record the notice for %s: %v", n.event.Subject, err)
			continue
		}
		owed = append(owed, id)
		log.Printf("capacity: %s is %v (%v of %v); a push is owed", n.event.Subject,
			n.event.Payload["state"], n.event.Payload["used"], n.event.Payload["limit"])
	}
	b.mu.Lock()
	b.rows = rows
	b.passes++
	b.at = time.Now()
	b.eventErrs += failed
	if lastErr != "" {
		b.eventErr = lastErr
	}
	b.mu.Unlock()
	if len(owed) > 0 {
		b.send(owed)
	}
}

// owedNotice is a notice that owes a push, and the reading that caused it.
type owedNotice struct {
	status capacity.Status
	event  capacity.Event
}

// recordNotice writes one notice's `capacity.notify` event and the push it
// owes in one transaction, and answers the push's outbox id. The words are
// fixed here, from the reading that crossed the line.
func (b *capacityBeat) recordNotice(ctx context.Context, n owedNotice) (int64, error) {
	e, st := n.event, n.status
	state := capacity.State(fmt.Sprint(e.Payload["state"]))
	language := "en"
	if b.language != nil {
		language = productcopy.Resolve(b.language())
	}
	title, body := capacity.NoticeTextLanguage(language, st.Entry, state, st.Reading.Used, st.Limit, st.ProjectedFullAt, b.loc)
	if st.Entry.Name == capacity.MachineDiskLow && state != capacity.OK {
		var sources []capacity.DiskSource
		if b.diskSources != nil {
			sources = b.diskSources(ctx)
		}
		title, body = capacity.LowDiskNoticeText(language, st.Reading.DiskFree, st.Limit, sources)
	}
	push, err := json.Marshal(orchestrator.CapacityPush{
		Name: e.Subject, State: string(state), From: fmt.Sprint(e.Payload["from"]),
		Used: st.Reading.Used, Limit: st.Limit,
		Title: title, Body: body, Tag: capacity.PushTag(e.Subject),
	})
	if err != nil {
		return 0, err
	}
	e.Payload["push"] = orchestrator.EffectCapacityPush
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return 0, err
	}
	ids, err := b.intent(ctx,
		[]store.Event{{Kind: e.Kind, Subject: e.Subject, Payload: payload}},
		[]store.Effect{{Kind: orchestrator.EffectCapacityPush, Subject: e.Subject, Payload: push}})
	if err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, fmt.Errorf("the notice recorded %d pushes, not one", len(ids))
	}
	return ids[0], nil
}

// send runs the pushes a pass recorded, off the beat, and reads back how they
// ended.
func (b *capacityBeat) send(ids []int64) {
	b.sending.Add(1)
	go func() {
		defer b.sending.Done()
		ctx, cancel := context.WithTimeout(context.Background(), capacityPushDeadline)
		defer cancel()
		b.run(ctx, ids)
		b.readPushes(ctx)
	}()
}

// readPushes reads the last push each row owed. The first read that succeeds
// also seeds the tracker with it, so that the one-a-day budget and a row's
// owed "back to ok" outlive the process that spent or owed them. A read that
// fails is logged and tried again on the next pass; until one succeeds the
// budget is this process's own.
func (b *capacityBeat) readPushes(ctx context.Context) {
	if b.latest == nil {
		return
	}
	effects, err := b.latest(ctx)
	if err != nil {
		log.Printf("capacity: the last pushes could not be read: %v", err)
		return
	}
	pushes := make(map[string]*contract.CapacityLastPush, len(effects))
	var seeds []orchestrator.CapacityPush
	var at []time.Time
	for _, e := range effects {
		var p orchestrator.CapacityPush
		if json.Unmarshal(e.Payload, &p) != nil || p.Name == "" {
			continue
		}
		pushes[p.Name] = lastPush(e, p)
		seeds, at = append(seeds, p), append(at, e.CreatedAt)
	}
	b.mu.Lock()
	first := !b.seeded
	b.seeded, b.pushes = true, pushes
	b.mu.Unlock()
	if first {
		for i, p := range seeds {
			b.tracker.Seed(p.Name, at[i], capacity.State(p.State))
		}
	}
}

// lastPush is one outbox row as the wire says it.
func lastPush(e store.Effect, p orchestrator.CapacityPush) *contract.CapacityLastPush {
	out := &contract.CapacityLastPush{At: unix(e.CreatedAt), State: contract.CapacityState(p.State),
		FinishedAt: unix(e.FinishedAt)}
	switch e.State {
	case store.EffectPending:
		out.Push = contract.CapacityPushStatePending
	case store.EffectStarted:
		out.Push = contract.CapacityPushStateSending
	case store.EffectDone:
		out.Push = contract.CapacityPushStatePushed
		if e.Outcome == "not_subscribed" {
			out.Push = contract.CapacityPushStateNotSubscribed
		}
	case store.EffectUnknown:
		out.Push, out.Detail = contract.CapacityPushStateUnknown, e.Outcome
	default:
		out.Push, out.Detail = contract.CapacityPushStateFailed, e.Outcome
	}
	return out
}

// verdict is what the open health route may say about capacity: whether a row
// is exhausted, and whether the beat has stopped.
func (b *capacityBeat) verdict(now time.Time) (exhausted, stalled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.rows {
		if r.Exhausted {
			exhausted = true
		}
	}
	return exhausted, capacity.BeatStalled(b.started, b.at, b.tick, now)
}

// capacityHealth adds capacity to an answer that is still ok. The broker's
// reason, when there is one, is given first.
func (s *Server) capacityHealth(h *contract.Health) {
	if !h.OK {
		return
	}
	exhausted, stalled := s.capacity().verdict(time.Now())
	switch {
	case exhausted:
		h.OK, h.Reason = false, contract.HealthReasonCapacityExhausted
	case stalled:
		h.OK, h.Reason = false, contract.HealthReasonCapacityBeatStalled
	}
}

// capacityDiagnostics is /v1/diagnostics.capacity, and whether it lets the
// daemon call itself ok.
func (s *Server) capacityDiagnostics() (contract.CapacityDiagnostics, bool) {
	b := s.capacity()
	now := time.Now()
	exhausted, stalled := b.verdict(now)
	b.mu.Lock()
	defer b.mu.Unlock()
	out := contract.CapacityDiagnostics{
		CountingSince:    b.since.Unix(),
		OverrideProblems: b.problems,
		Beat: contract.CapacityBeat{
			Running:        !b.started.IsZero(),
			TickSeconds:    int64(b.tick / time.Second),
			Passes:         b.passes,
			Stalled:        stalled,
			EventErrors:    b.eventErrs,
			LastEventError: b.eventErr,
			StartedAt:      unix(b.started),
			At:             unix(b.at),
		},
		Entries: make([]contract.CapacityEntry, 0, len(b.resolved)),
	}
	if b.rows == nil {
		// No pass yet: every row, and nothing known about any of them.
		why := "the capacity beat has not finished a pass"
		if b.started.IsZero() {
			why = "the capacity beat is not running in this process"
		}
		for _, res := range b.resolved {
			out.Entries = append(out.Entries, CapacityEntry(capacity.Status{
				Resolved: res, Reading: capacity.Unmeasured(why), State: capacity.Unknown,
			}))
		}
		return out, !stalled
	}
	for _, st := range b.rows {
		row := CapacityEntry(st)
		row.LastPush = b.pushes[st.Entry.Name]
		out.Entries = append(out.Entries, row)
	}
	return out, !exhausted && !stalled
}

// capacityRoute is GET /v1/capacity: the Settings page's capacity block (C4,
// web/console/src/pages/settings/CapacityBlock.tsx), which every capacity push
// names. Clawdline Cloud carries it as the `capacity` word, so the phone that
// got the push can open it.
//
// /v1/diagnostics is this machine's own token's, because it carries where this
// daemon keeps its state and which ports it holds. The panel is a person's
// screen, and a person reads it from a browser that is a paired device — so
// the register's rows are here for any paired device, with the completion
// notices that went unanswered, and nothing about this disk's layout. Like
// /v1/diagnostics it reads what the last pass saw and writes nothing.
func (s *Server) capacityRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The capacity panel is read with GET.")
		return
	}
	panel, _ := s.capacityDiagnostics()
	out := contract.CapacityPanel{At: time.Now().Unix(), Capacity: panel}
	if counts, err := s.store.BrokerNoticeCounts(r.Context()); err != nil {
		out.CompletionsError = err.Error()
	} else {
		wire := brokerNoticeCountsWire(counts, time.Unix(out.At, 0))
		out.Completions = &wire
	}
	writeJSON(w, out)
}

func brokerNoticeCountsWire(counts store.NoticeCounts, now time.Time) contract.BrokerNoticeCounts {
	wire := contract.BrokerNoticeCounts{
		Pending: int64(counts.Pending), Delivered: int64(counts.Delivered),
		DeadLetter: int64(counts.DeadLetter), Acknowledged: int64(counts.Acknowledged),
		DeadLetterDelivery: &contract.BrokerDeadLetterDelivery{
			Delivered:  int64(counts.DeadLetterDelivered),
			Unrecorded: int64(counts.DeadLetter - counts.DeadLetterDelivered),
		},
	}
	if !counts.OldestPending.IsZero() {
		wire.OldestOpenSeconds = int64(now.Sub(counts.OldestPending) / time.Second)
	}
	return wire
}

// CapacityEntry is one row on the wire, as /v1/diagnostics.capacity and
// `clawdline doctor capacity --drill` both print it.
func CapacityEntry(st capacity.Status) contract.CapacityEntry {
	e := st.Entry
	out := contract.CapacityEntry{
		Name: e.Name, Class: contract.CapacityClass(e.Class), Unit: contract.CapacityUnit(e.Unit),
		Limit: st.Limit, WarnAt: e.Warn(), AtLimit: contract.CapacityAction(e.AtLimit),
		Deviation: e.Deviation, EvictedBy: contract.CapacityDecider(e.EvictedBy),
		Overridden: st.Overridden, State: contract.CapacityState(st.State),
		Exhausted: st.Exhausted, Failing: st.Reading.Failing,
		Error: st.Reading.Err, Note: st.Reading.Note,
		Refused: st.Reading.Counters.Refused, Evicted: st.Reading.Counters.Evicted,
		Expired: st.Reading.Counters.Expired, Rotated: st.Reading.Counters.Rotated,
		Dropped: st.Reading.Counters.Dropped, WriteErrors: st.Reading.Counters.WriteErrors,
		Coalesced: st.Reading.Counters.Coalesced, Disconnected: st.Reading.Counters.Disconnected,
		LastActionAt:    unix(st.Reading.Counters.LastActionAt),
		CountersDurable: st.Reading.DurableCounters,
		OldestAt:        unix(st.Reading.OldestAt),
		WindowSeconds:   st.Reading.WindowSeconds,
		MeasuredAt:      unix(st.MeasuredAt),
		Notices:         st.Notices, NoticesSuppressed: st.Suppressed,
		LastNoticeAt:    unix(st.LastNoticeAt),
		ProjectedFullAt: unix(st.ProjectedFullAt),
		Told:            make([]contract.CapacityChannel, 0, len(e.Told)),
	}
	for _, c := range e.Told {
		out.Told = append(out.Told, contract.CapacityChannel(c))
	}
	if st.Reading.Known {
		used, ratio := st.Reading.Used, st.Ratio
		out.Used, out.Ratio = &used, &ratio
	}
	if st.Reading.HasDiskFree {
		out.DiskFreeBytes = st.Reading.DiskFree
	}
	if st.HasGrowth {
		out.GrowthPerDay = st.GrowthPerDay
	}
	return out
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
