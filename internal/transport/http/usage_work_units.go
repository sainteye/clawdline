package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

// One unit of work (docs/token-ledger.md "One unit of work"): the cursors a
// child task and a Board item leave in the token ledger at their edges, and
// GET /v1/usage/work-units, which reads them back as deltas.

var workUnitsByServer sync.Map // *Server -> *app.WorkUnitRecorder

// workUnits is this server's cursor recorder, over its token ledger.
func (s *Server) workUnits() *app.WorkUnitRecorder {
	if r, ok := workUnitsByServer.Load(s); ok {
		return r.(*app.WorkUnitRecorder)
	}
	got, _ := workUnitsByServer.LoadOrStore(s, app.NewWorkUnitRecorder(s.usageLedger()))
	return got.(*app.WorkUnitRecorder)
}

// workUnitEdge is the broker's Broker.WorkUnitEdge: it only queues.
func (s *Server) workUnitEdge() func(taskID, edge, outcome string, at time.Time) {
	return func(taskID, edge, outcome string, at time.Time) {
		s.workUnits().TaskEdge(taskID, edge, outcome, at)
	}
}

// startWorkUnits has the Board's committed writes told to the recorder, runs
// its worker, and settles behind cursors after every ledger pass.
func (s *Server) startWorkUnits(ctx context.Context) {
	rec := s.workUnits()
	s.store.ObserveWorkV2(rec.ObserveBoard)
	u := s.usageLedger()
	u.AfterPass = func(ctx context.Context) {
		if err := u.SettleWorkCursors(ctx); err != nil && ctx.Err() == nil {
			log.Printf("usage: behind work cursors could not be settled: %v", err)
		}
	}
	go rec.Run(ctx)
}

// usageWorkUnits is GET /v1/usage/work-units?since=…: a query it does not read
// is refused by name rather than ignored.
func (s *Server) usageWorkUnits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, values := range q {
		if key != "since" || len(values) != 1 {
			writeRefusal(w, http.StatusBadRequest, "bad_request",
				"The work units read one query field, since: `14d`, `36h` or a Unix time in seconds.")
			return
		}
	}
	got, err := s.usageLedger().WorkUnitsSince(r.Context(), q.Get("since"))
	if errors.Is(err, app.ErrCompareSince) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "since: "+err.Error()+".")
		return
	}
	if err != nil {
		usageStoreRefusal(w, err)
		return
	}
	writeJSON(w, usageWorkReport(got))
}

// ---------- the wire shapes ----------

func usageWorkReport(rep app.WorkUnitReport) contract.UsageWorkUnits {
	out := contract.UsageWorkUnits{Since: unixOrZero(rep.Since), Until: unixOrZero(rep.Until),
		Truncated: rep.Truncated, Units: []contract.UsageWorkUnit{}}
	for _, u := range rep.Units {
		out.Units = append(out.Units, usageWorkUnit(u))
	}
	return out
}

func usageWorkTokens(t transcript.Tokens) contract.UsageWorkTokens {
	return contract.UsageWorkTokens{Input: t.Input, CacheWrite1h: t.CacheWrite1h, CacheWrite5m: t.CacheWrite5m,
		CacheRead: t.CacheRead, Output: t.Output, Total: t.Total(), Unpriced: t.Unpriced}
}

func usageWorkModels(in []app.WorkCursorModel) []contract.UsageWorkModel {
	out := make([]contract.UsageWorkModel, 0, len(in))
	for _, m := range in {
		out = append(out, contract.UsageWorkModel{Model: m.Model, Priced: m.Priced})
	}
	return out
}

func usageWorkStates(in []string) []contract.UsageWorkState {
	out := make([]contract.UsageWorkState, 0, len(in))
	for _, s := range in {
		out = append(out, contract.UsageWorkState(s))
	}
	return out
}

func usageWorkUnit(u app.WorkUnitDelta) contract.UsageWorkUnit {
	out := contract.UsageWorkUnit{Kind: contract.UsageWorkKind(u.Kind), ID: u.ID, Cycle: u.Cycle,
		StartedAt: unixOrZero(u.StartedAt), EndedAt: unixOrZero(u.EndedAt), Outcome: u.Outcome,
		States: usageWorkStates(u.States), Tokens: usageWorkTokens(u.Tokens), Cost: u.Cost, CostKnown: u.CostKnown,
		Calls: u.Calls, Compactions: u.Compactions, Models: usageWorkModels(u.Models),
		Sessions: []contract.UsageWorkSession{}, Cursors: []contract.UsageWorkCursor{}}
	for _, s := range u.Sessions {
		out.Sessions = append(out.Sessions, contract.UsageWorkSession{Conversation: s.Conversation,
			States: usageWorkStates(s.States), Counted: s.Counted, Delta: usageWorkTokens(s.Delta), Calls: s.Calls,
			Compactions: s.Compactions, PeakStart: s.PeakStart, PeakEnd: s.PeakEnd, PeakRose: s.PeakRose,
			Models: usageWorkModels(s.Models)})
	}
	for _, c := range u.Cursors {
		wire := contract.UsageWorkCursor{Seq: c.Seq, UnitKind: contract.UsageWorkKind(c.UnitKind), UnitID: c.UnitID,
			Cycle: c.Cycle, Edge: c.Edge, Session: c.Session, At: unixOrZero(c.At), TakenAt: unixOrZero(c.TakenAt),
			Outcome: c.Outcome, State: c.State}
		if c.Session != "" {
			var r app.WorkCursorReading
			if json.Unmarshal(c.Reading, &r) == nil {
				reading := usageWorkReading(r)
				wire.Reading = &reading
			}
		}
		out.Cursors = append(out.Cursors, wire)
	}
	return out
}

func usageWorkReading(r app.WorkCursorReading) contract.UsageWorkReading {
	out := contract.UsageWorkReading{Assistant: r.Assistant, Present: r.Present, Reason: contract.UsageReason(r.Reason),
		ReadAt: unixOrZero(r.ReadAt), More: r.More, Tokens: usageTokens(r.Tokens), Calls: r.Calls,
		Compactions: r.Compactions, PeakContext: r.PeakContext, Models: usageWorkModels(r.Models),
		Files: []contract.UsageWorkFile{}, Behind: r.Behind}
	for _, f := range r.Files {
		out.Files = append(out.Files, contract.UsageWorkFile{Conversation: f.Conversation, LedgerSize: f.LedgerSize,
			LedgerModifiedAt: unixOrZero(f.LedgerModifiedAt), FileSize: f.FileSize,
			FileModifiedAt: unixOrZero(f.FileModifiedAt), Seen: f.Seen, More: f.More,
			Reason: contract.UsageReason(f.Reason)})
	}
	return out
}
