package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/logs"
	adapterpush "github.com/sainteye/clawdline-go/internal/adapters/push"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
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
	measure  map[string]func() capacity.Reading
	record   func(context.Context, store.Event) error
	since    time.Time

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
}

var capacityByServer sync.Map // *Server -> *capacityBeat

// capacity is this server's beat, built on first use. Building it measures
// nothing.
func (s *Server) capacity() *capacityBeat {
	if b, ok := capacityByServer.Load(s); ok {
		return b.(*capacityBeat)
	}
	resolved, problems := capacityOverrides()
	b, _ := capacityByServer.LoadOrStore(s, &capacityBeat{
		resolved: resolved,
		problems: problems,
		tracker:  capacity.NewTracker(),
		measure:  s.capacityMeasures(),
		record:   s.store.Append,
		since:    time.Now(),
		poke:     make(chan struct{}, 1),
	})
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
// own and each cheap: a stat, a length, a count kept in memory.
func (s *Server) capacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.LogDaemon: func() capacity.Reading {
			w, ok := daemonLogs.Load(s.cfg.Dir)
			if !ok {
				return capacity.Unmeasured("this process writes its log to stderr, not to a file")
			}
			return w.(*logs.Writer).Reading()
		},
		capacity.DevicesList: func() capacity.Reading {
			g := s.gate()
			if g.files == nil {
				return capacity.Unmeasured(fmt.Sprint(g.err))
			}
			return g.files.DevicesReading()
		},
		capacity.PushSubscriptions: func() capacity.Reading {
			// Measuring does not make the push directory: a machine nobody
			// has asked to notify them holds no subscriptions, and that is a
			// known zero rather than a store opened to find out.
			if _, held := pushStores.Load(s.cfg.Dir); !held {
				_, err := os.Lstat(filepath.Join(s.cfg.Dir, adapterpush.DirName, adapterpush.SubscriptionsFile))
				if errors.Is(err, os.ErrNotExist) {
					return capacity.Reading{Known: true, Note: "nothing has subscribed on this machine"}
				}
			}
			store, err := s.push()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return store.Reading()
		},
		capacity.CacheTranscriptUsage: func() capacity.Reading {
			if s.ledger == nil {
				return capacity.Unmeasured("this server keeps no usage ledger")
			}
			return s.ledger.Reading()
		},
		capacity.CacheTranscriptTitles: func() capacity.Reading {
			h, ok := s.inventory.Identity.(*transcript.Host)
			if !ok {
				return capacity.Unmeasured("this inventory does not keep conversation titles")
			}
			return h.Titles().Reading()
		},
		capacity.AuditSecurity: func() capacity.Reading {
			g := s.gate()
			if g.files == nil {
				return capacity.Unmeasured(fmt.Sprint(g.err))
			}
			return g.files.AuditReading()
		},
		// The file's size and the disk's room, and this handle's own account
		// of the writes the database refused (limits N2).
		capacity.StoreDB: func() capacity.Reading { return s.store.Reading(s.cfg.Dir) },
		capacity.StoreReceipts: func() capacity.Reading {
			uses, err := s.store.ReceiptUses(context.Background(), store.ReceiptWindow, time.Now())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			r := capacity.Reading{Known: true}
			tombstones := 0
			for _, u := range uses {
				if int64(u.Live) > r.Used {
					r.Used = int64(u.Live)
					r.Note = "fullest scope: " + u.Scope
				}
				tombstones += u.Tombstones
			}
			if tombstones > 0 {
				r.Note = strings.TrimPrefix(r.Note+"; ", "; ") + strconv.Itoa(tombstones) + " expired key(s) kept as tombstones"
			}
			return r
		},
		capacity.WorkOpen: func() capacity.Reading {
			n, err := s.store.WorkOpenCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n}
		},
		capacity.BoardReceipts: func() capacity.Reading { return s.boardReceiptReading() },
		capacity.CloudRelayQueue: func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud line is off: there is no queue"}
			}
			q, ok := line.(interface {
				RelayQueue() (waiting, depth int, counters capacity.Counters, ok bool)
			})
			if !ok {
				return capacity.Unmeasured("this Cloud line does not report its queue")
			}
			waiting, depth, counters, ok := q.RelayQueue()
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud line was never built: there is no queue"}
			}
			r := capacity.Reading{Known: true, Used: int64(waiting), Counters: counters}
			if limit := CapacityLimit(capacity.CloudRelayQueue); int64(depth) != limit {
				r.Note = fmt.Sprintf("the queue holds %d, not the register's %d", depth, limit)
			}
			return r
		},
		capacity.SSEScreenPending: func() capacity.Reading {
			if s.screenBus == nil {
				return capacity.Unmeasured("this server publishes no event stream")
			}
			return s.screenBus.reading()
		},
		capacity.ArtifactsImages: func() capacity.Reading {
			if s.pictures.store == nil {
				return capacity.Unmeasured("this server keeps no picture store")
			}
			count, _ := s.pictures.store.Readings(time.Now())
			return count
		},
		capacity.ArtifactsImageSize: func() capacity.Reading {
			if s.pictures.store == nil {
				return capacity.Unmeasured("this server keeps no picture store")
			}
			_, bytes := s.pictures.store.Readings(time.Now())
			return bytes
		},
		capacity.ArtifactsDrops: func() capacity.Reading {
			if s.pictures.drops == nil {
				return capacity.Unmeasured("this server keeps no drop cache")
			}
			return s.pictures.drops.Reading()
		},
		// W5's three rows, read from the store the broker writes them to.
		capacity.LeasesQueue: func() capacity.Reading {
			leases, err := s.store.Leases(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			r := capacity.Reading{Known: true}
			for _, l := range leases {
				if n := int64(len(l.Waiters)); n > r.Used {
					r.Used, r.Note = n, "longest line: "+l.Resource
				}
			}
			return r
		},
		capacity.CoordinatorAliases: func() capacity.Reading {
			rec, _, err := s.store.Coordinator(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			r := capacity.Reading{Known: true}
			if rec != nil {
				r.Used = int64(len(rec.Aliases))
			}
			return r
		},
		capacity.WaitsOpen: func() capacity.Reading {
			n, err := s.store.OpenWaitCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(n)}
		},
	}
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
	now := time.Now()
	rows := make([]capacity.Status, len(b.resolved))
	var events []capacity.Event
	for i, res := range b.resolved {
		measure := b.measure[res.Entry.Name]
		r := capacity.Unmeasured("nothing measures this row")
		if measure != nil {
			r = measure()
		}
		st, ev := b.tracker.Observe(res, r, now)
		rows[i] = st
		events = append(events, ev...)
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
	b.mu.Lock()
	b.rows = rows
	b.passes++
	b.at = time.Now()
	b.eventErrs += failed
	if lastErr != "" {
		b.eventErr = lastErr
	}
	b.mu.Unlock()
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
		out.Entries = append(out.Entries, CapacityEntry(st))
	}
	return out, !exhausted && !stalled
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
