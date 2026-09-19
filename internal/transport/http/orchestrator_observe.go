package http

import (
	"context"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// What the broker says about itself, on the two routes that describe this
// process (docs/broker-design.md §6.6).
//
// The open /v1/health says one thing more than it did: that the broker's beat
// has stopped, when it has. The Swift app's health had no field that could say
// that — measured on 2026-09-18, its top-level keys were auth, authed, build,
// http_reliability, instance, ok, password, protocol, version and write, and it
// answered `ok: true` while its heartbeat was stopped. Everything else is in
// /v1/diagnostics, behind this machine's own token, because the open route
// carries nothing about the work.

// reasonBeatStalled is the one reason health gives today.
const reasonBeatStalled = "broker_beat_stalled"

// brokerHealth turns a stalled beat into `ok: false` with its reason. The
// status stays 200: the answer is the JSON, and a reader that treats a 503 as
// "the daemon is gone" would restart a process whose HTTP side is fine — the
// one self-repair this broker is designed not to do.
func (s *Server) brokerHealth(h *contract.Health) {
	if s.broker == nil {
		return
	}
	if stalled, _ := s.broker.Stalled(); stalled {
		h.OK = false
		h.Reason = reasonBeatStalled
	}
}

// brokerDiagnostics is the broker block of /v1/diagnostics.
func (s *Server) brokerDiagnostics(ctx context.Context) *contract.BrokerDiagnostics {
	if s.broker == nil {
		return nil
	}
	stamp := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	now := time.Now()
	beat := s.broker.Beat()
	out := &contract.BrokerDiagnostics{
		Beat: contract.BrokerBeat{
			Running:        beat.Running,
			TickSeconds:    int64(beat.Tick / time.Second),
			StartedAt:      stamp(beat.Started),
			At:             stamp(beat.At),
			Passes:         beat.Passes,
			InPass:         beat.InPass,
			LastDurationMs: beat.Last.Milliseconds(),
			P99DurationMs:  beat.P99.Milliseconds(),
			Overlaps:       beat.Overlaps,
			Restarts:       beat.Restarts,
			Stalled:        beat.Stalled,
			QuietSeconds:   int64(beat.Quiet / time.Second),
		},
		Pass: contract.BrokerPass{
			At:          stamp(beat.Pulse.At),
			Watched:     int64(beat.Pulse.Watched),
			Settled:     int64(beat.Pulse.Settled),
			Notes:       int64(beat.Pulse.Notes),
			Notices:     int64(beat.Pulse.Notices),
			TimedOut:    int64(beat.Pulse.TimedOut),
			SpawnFailed: int64(beat.Pulse.SpawnFail),
			StoreError:  beat.StoreErr,
			// Three quiet failures made loud (D05 ②, G34, D11): rows the
			// pass could not decode, notes it refused, tabs it closed.
			Unreadable:   int64(beat.Pulse.Unreadable),
			NotesRefused: int64(beat.Pulse.NotesRefused),
			Closed:       int64(beat.Pulse.Closed),
		},
	}
	out.WorkflowRetired = s.retired.diagnostics()
	if s.broker.Lanes != nil {
		st := s.broker.Lanes.Stats()
		out.Lanes = &contract.BrokerLanes{
			Admitted: int64(st.Admitted), Limit: int64(st.Limit), Terminals: int64(st.Keys), Refused: st.Refused,
		}
	}
	if p, ok := s.broker.PolicyRead(); ok {
		out.Policy = &contract.BrokerPolicy{
			Chars: int64(p.Chars), Limit: int64(p.Limit), BaseChars: int64(p.BaseChars),
			LocalChars: int64(p.LocalChars), Cut: p.Cut, NearLimit: p.NearLimit, At: stamp(p.At),
		}
	}
	if beat.InPass {
		out.Beat.PassBeganAt = stamp(beat.Began)
	}
	if beat.Backoff.After(now) {
		out.Beat.BackoffUntil = stamp(beat.Backoff)
	}
	for _, p := range beat.Panics {
		out.Beat.Panics = append(out.Beat.Panics, contract.BrokerBeatPanic{
			At: stamp(p.At), Pass: p.Pass, Value: p.Value, Stack: p.Stack,
		})
	}

	health := s.store.Health(ctx)
	stats := s.store.Stats()
	out.Store = contract.BrokerStoreHealth{
		Status:      contract.BrokerStoreStatusReady,
		Error:       health.Err,
		Tasks:       int64(health.Tasks),
		Writes:      stats.Writes,
		Rows:        stats.Rows,
		Changes:     health.Changes,
		Failures:    stats.Failures,
		Busy:        stats.Busy,
		WriteP99Ms:  stats.P99.Milliseconds(),
		LastWriteAt: stamp(stats.Last),
		LastError:   stats.LastErr,
		LastErrorAt: stamp(stats.ErrAt),
	}
	if !health.Ready {
		out.Store.Status = contract.BrokerStoreStatusUnavailable
	}
	if counts, err := s.store.BrokerNoticeCounts(ctx); err == nil {
		out.Notices = contract.BrokerNoticeCounts{
			Pending:      int64(counts.Pending),
			Delivered:    int64(counts.Delivered),
			DeadLetter:   int64(counts.DeadLetter),
			Acknowledged: int64(counts.Acknowledged),
		}
		if !counts.OldestPending.IsZero() {
			out.Notices.OldestOpenSeconds = int64(now.Sub(counts.OldestPending) / time.Second)
		}
	}

	seen := s.broker.Observed()
	out.Observations = contract.BrokerObservations{
		Generation:      seen.Generation,
		At:              stamp(seen.At),
		Complete:        seen.Complete,
		Sessions:        int64(seen.Sessions),
		DeferredNotices: int64(seen.Deferred),
		Executors:       []contract.BrokerExecutor{},
	}
	sources := make([]string, 0, len(seen.Sources))
	for name := range seen.Sources {
		sources = append(sources, name)
	}
	sort.Strings(sources)
	for _, name := range sources {
		out.Observations.Sources = append(out.Observations.Sources,
			contract.BrokerSourceReading{Source: name, Complete: seen.Sources[name]})
	}
	ids := make([]string, 0, len(seen.Executors))
	for id := range seen.Executors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Observations.Executors = append(out.Observations.Executors, *brokerExecutor(id, seen.Executors[id]))
	}
	return out
}

// brokerExecutor is one live observation on the wire.
func brokerExecutor(taskID string, e orchestrator.Executor) *contract.BrokerExecutor {
	out := &contract.BrokerExecutor{
		TaskID:       taskID,
		Status:       contract.BrokerExecutorStatus(e.Status),
		TerminalID:   e.TerminalID,
		SessionState: e.SessionState,
		Generation:   e.Generation,
	}
	if out.Status == "" {
		out.Status = contract.BrokerExecutorStatusUnknown
	}
	if !e.ObservedAt.IsZero() {
		out.ObservedAt = e.ObservedAt.Unix()
	}
	if !e.FirstUnseenAt.IsZero() {
		out.FirstUnseenAt = e.FirstUnseenAt.Unix()
	}
	return out
}

// beatFault reads CLAWDLINE_NEXT_BEAT_FAULT, the failure-injection seam for
// the broker's beat, so its alarms can be shown to fire on a real daemon:
//
//	panic@N   the beat panics at the top of pass N, once
//	stall@N   pass N never returns — the beat goroutine is, to everything
//	          outside it, dead
//	exit@N    the beat goroutine exits (runtime.Goexit) at pass N, once
//
// Anything else is ignored, loudly. Absent — every real daemon — is nil.
func beatFault() func(pass int64) {
	spec := strings.TrimSpace(os.Getenv("CLAWDLINE_NEXT_BEAT_FAULT"))
	if spec == "" {
		return nil
	}
	kind, at, ok := strings.Cut(spec, "@")
	n, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || n < 1 {
		log.Printf("orchestrator: CLAWDLINE_NEXT_BEAT_FAULT=%q is not kind@pass; ignored", spec)
		return nil
	}
	switch kind {
	case "panic", "stall", "exit":
	default:
		log.Printf("orchestrator: CLAWDLINE_NEXT_BEAT_FAULT kind %q is not panic, stall or exit; ignored", kind)
		return nil
	}
	log.Printf("orchestrator: FAULT INJECTION ARMED — the beat will %s at pass %d (CLAWDLINE_NEXT_BEAT_FAULT)", kind, n)
	return func(pass int64) {
		if pass != n {
			return
		}
		switch kind {
		case "panic":
			panic("injected by CLAWDLINE_NEXT_BEAT_FAULT")
		case "exit":
			runtime.Goexit()
		case "stall":
			log.Printf("orchestrator: injected stall at pass %d; this beat will not return", pass)
			select {}
		}
	}
}
