package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/coordinator"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The machine role's routes, as the Swift app has them
// (`/v1/orchestrator/coordinator`, `…/register`, `…/rebind`, `…/bearings`),
// over this daemon's own store (app.Coordinator).
//
// `/v1/next/coordinator` is kept as a read for the Dashboard, which reads it
// today; it is the same record through the same code, and it no longer takes a
// POST, so the role has one door to be registered and moved through
// (DG-5). A POST there is refused with the name of that door.

func (s *Server) coordinator() *app.Coordinator {
	return &app.Coordinator{
		Store: s.store,
		Read: func(ctx context.Context) session.Inventory {
			return s.reading(ctx)
		},
		ProcessStart: swiftstore.ProcessStart,
		Broker:       s.broker,
	}
}

// coordinatorRoute is the Dashboard's read of the role.
func (s *Server) coordinatorRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusMethodNotAllowed, Code: "route_moved",
			Message: "The role is registered and moved through POST /v1/orchestrator/coordinator/register and " +
				"/rebind; this route only reads it."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	st, err := s.coordinator().State(ctx)
	if err != nil {
		writeCoordinatorError(w, err)
		return
	}
	candidates := []string{}
	for _, row := range st.Seen.Sessions {
		if row.ConversationID != "" {
			candidates = append(candidates, row.ConversationID)
		}
	}
	liveness := contract.Liveness(st.Liveness)
	if st.Record == nil {
		liveness = contract.LivenessUnknown
		if st.Seen.Inventory.Complete {
			liveness = contract.LivenessOffline
		}
	}
	writeJSON(w, contract.CoordinatorSnapshot{
		Registered: st.Record != nil,
		Record:     wireCoordinator(st.Record),
		Liveness:   liveness,
		Candidates: sortedStrings(candidates),
	})
}

// wireCoordinator is the Dashboard's shape of the record. The timestamps are
// integers, like every other time on this daemon.
func wireCoordinator(rec *coordinator.Record) *contract.CoordinatorRecord {
	if rec == nil {
		return nil
	}
	out := contract.CoordinatorRecord{
		ID:             rec.ID,
		Label:          coordinator.Label,
		TerminalID:     rec.TerminalID,
		ConversationID: rec.ConversationID,
		Assistant:      contract.Assistant(rec.Assistant),
		PID:            int64(rec.PID),
		RegisteredAt:   rec.RegisteredAt.Unix(),
		Generation:     rec.Generation,
	}
	if !rec.ReboundAt.IsZero() {
		out.ReboundAt = rec.ReboundAt.Unix()
	}
	return &out
}

// orchestratorCoordinatorRoute is everything under /v1/orchestrator/coordinator.
func (s *Server) orchestratorCoordinatorRoute(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSuffix(routePath(r), "/")
	get := r.Method == http.MethodGet || r.Method == http.MethodHead
	post := r.Method == http.MethodPost
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch {
	case get && p == "/v1/orchestrator/coordinator":
		if !machineAuthed(r) {
			writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Inspecting the role needs the orchestrator token.")
			return
		}
		s.coordinatorInspection(ctx, w)
	case get && p == "/v1/orchestrator/coordinator/bearings":
		// Read-level, as in the Swift app: a paired phone may see where the
		// work stands, never the role's ids.
		s.coordinatorBearings(ctx, w)
	case post && p == "/v1/orchestrator/coordinator/register":
		var body contract.CoordinatorRegisterRequest
		if !decodeClosed(w, r, &body, "session_id") {
			return
		}
		c := s.coordinator()
		st, created, err := c.Register(ctx, strings.TrimSpace(body.SessionID))
		if err != nil {
			writeCoordinatorError(w, err)
			return
		}
		writeJSON(w, contract.CoordinatorRegisterResult{OK: true, Created: created, Coordinator: coordinatorMetadata(st)})
	case post && p == "/v1/orchestrator/coordinator/rebind":
		var body contract.CoordinatorRebindRequest
		if !decodeClosed(w, r, &body, "expected_coordinator_id", "expected_generation", "session_id") {
			return
		}
		if body.ExpectedGeneration < 1 || body.ExpectedCoordinatorID == "" {
			writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusBadRequest, Code: "bad_request",
				Message: "expected_coordinator_id and a positive expected_generation are required: read the role first."})
			return
		}
		st, moved, err := s.coordinator().Rebind(ctx, body.ExpectedCoordinatorID, body.ExpectedGeneration,
			strings.TrimSpace(body.SessionID))
		if err != nil {
			writeCoordinatorError(w, err)
			return
		}
		writeJSON(w, contract.CoordinatorRebindResult{OK: true, Rebound: moved, Coordinator: coordinatorMetadata(st)})
	case strings.HasPrefix(p, "/v1/orchestrator/coordinator/successions"):
		// Not implemented, said by name rather than answered 404, so a caller
		// following the Swift guide learns why.
		//
		// What this used to say was wrong twice over. Handoffs arrived (W6)
		// and the sentence still said they had not; and "move the role with
		// /rebind once the bound session is offline" was offered to the one
		// caller who cannot use it — `succession_required` is raised while the
		// holder is **live**, and a live holder asking for /rebind is told
		// `coordinator_online`. The truthful answer is that a live session
		// cannot give the role away on this build at all, so the message says
		// that, and says who can do what instead. The same words are in the
		// remedy table (orchestrator/remedy.go), which is what the refusal
		// carries; this route keeps its own copy because a caller reaching it
		// directly never sees that refusal.
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusNotImplemented, Code: "succession_unavailable",
			Message: "Succession — moving the machine role together with the work — is not implemented on this " +
				"daemon, and nothing else moves the role while its session is live: /v1/orchestrator/coordinator/rebind " +
				"answers coordinator_online until a reading proves the bound session offline. What does work: the " +
				"work itself goes over as an ordinary handoff from a session that does not hold the role, and once " +
				"the holder is gone the receiver moves the role with POST /v1/orchestrator/coordinator/rebind, using " +
				"the id and generation from GET /v1/orchestrator/coordinator.",
			Extra: map[string]any{"implemented": false}})
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such coordinator route.")
	}
}

// decodeClosed reads a closed JSON body: every key must be one of allowed.
func decodeClosed(w http.ResponseWriter, r *http.Request, into any, allowed ...string) bool {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusBadRequest, Code: "bad_request",
			Message: "The body must be one JSON object."})
		return false
	}
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k] = true
	}
	unknown := []string{}
	for k := range raw {
		if !ok[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusBadRequest, Code: "bad_request",
			Message: "Unknown field(s): " + strings.Join(sortedStrings(unknown), ", ") + "."})
		return false
	}
	encoded, _ := json.Marshal(raw)
	if err := json.Unmarshal(encoded, into); err != nil {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusBadRequest, Code: "bad_request",
			Message: "A field has the wrong type: " + err.Error()})
		return false
	}
	return true
}

func writeCoordinatorError(w http.ResponseWriter, err error) {
	var ref app.RoleRefusal
	if errors.As(err, &ref) {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: ref.Status, Code: ref.Code, Message: ref.Message, Extra: ref.Extra})
		return
	}
	writeBrokerError(w, err)
}

// coordinatorMetadata is Coordinator.swift's coordinatorMetadata.
func coordinatorMetadata(st app.State) contract.CoordinatorMetadata {
	out := contract.CoordinatorMetadata{
		Scope: coordinator.Scope, Label: coordinator.Label,
		Status: contract.CoordinatorStatusUnregistered, Lifecycle: contract.CoordinatorLifecycle(app.Lifecycle(st)),
	}
	rec := st.Record
	if rec == nil {
		return out
	}
	out.Configured = true
	out.ID = rec.ID
	out.RegisteredAt = rec.RegisteredAt.Unix()
	if !rec.ReboundAt.IsZero() {
		out.ReboundAt = rec.ReboundAt.Unix()
	}
	out.Generation = rec.Generation
	out.Status = contract.CoordinatorStatus(st.Liveness)
	sess := contract.CoordinatorSession{
		SessionID: rec.ConversationID, TerminalID: rec.TerminalID,
		Assistant: contract.Assistant(rec.Assistant), Label: rec.SessionLabel, CWD: rec.CWD,
	}
	// The live row's own label and place, when the bound session is on screen.
	if row, ok := st.Seen.Sessions[rec.TerminalID]; ok && row.ConversationID == rec.ConversationID {
		if row.Label != "" {
			sess.Label = row.Label
		}
		if row.CWD != "" {
			sess.CWD = row.CWD
		}
	}
	out.Session = &sess
	return out
}

func (s *Server) coordinatorInspection(ctx context.Context, w http.ResponseWriter) {
	c := s.coordinator()
	st, err := c.State(ctx)
	if err != nil {
		writeCoordinatorError(w, err)
		return
	}
	registration := "available"
	switch {
	case st.Record != nil:
		registration = "configured"
	case st.Status != "absent":
		registration = "blocked"
	}
	writeJSON(w, contract.CoordinatorInspection{
		Version:      1,
		ObservedAt:   time.Now().Unix(),
		Store:        contract.CoordinatorStoreState{Status: string(st.Status)},
		Registration: contract.CoordinatorRegistration{State: registration},
		Coordinator:  coordinatorMetadata(st),
		Bearings:     wireBearings(c.Bearings(ctx, st), st),
	})
}

func (s *Server) coordinatorBearings(ctx context.Context, w http.ResponseWriter) {
	c := s.coordinator()
	st, err := c.State(ctx)
	if err != nil {
		writeCoordinatorError(w, err)
		return
	}
	writeJSON(w, wireBearings(c.Bearings(ctx, st), st))
}

func wireBearings(b app.Bearings, st app.State) contract.CoordinatorBearings {
	at := b.ObservedAt.Unix()
	src := func(freshness, provenance string) contract.BearingsSource {
		return contract.BearingsSource{ObservedAt: at, Provenance: provenance, Freshness: freshness}
	}
	sessionsAt := st.Seen.Inventory.ObservedAt
	out := contract.CoordinatorBearings{
		ObservedAt:           at,
		CoordinatorLifecycle: contract.CoordinatorLifecycle(b.Lifecycle),
		ActiveTaskCount:      int64(b.ActiveTasks),
		PendingLandingCount:  int64(len(b.PendingLandings)),
		PendingLandings:      []contract.BearingsLanding{},
		OpenWaitCount:        int64(b.OpenWaits),
		DeadLetterCount:      int64(b.DeadLetters),
		HeldLeases:           int64(b.HeldLeases),
		Unknown:              append([]string{}, b.Unknown...),
		Sources: contract.BearingsSources{
			Sessions: contract.BearingsSource{ObservedAt: sessionsAt.Unix(), Provenance: st.Seen.Inventory.Provenance,
				Freshness: b.SessionsFresh},
			Tasks:    src(b.TasksFresh, "broker"),
			Landings: src(b.TasksFresh, "broker"),
			Waits:    src(b.WaitsFresh, "broker"),
		},
	}
	for _, l := range b.PendingLandings {
		out.PendingLandings = append(out.PendingLandings, contract.BearingsLanding{
			TaskID: l.Task, Title: l.Title, Obligation: l.Obligation, AgeSeconds: int64(l.Age / time.Second),
		})
	}
	return out
}

// ownCrown is the crown this daemon's own role draws on one row, or nil. The
// matching rule and the command table are Coordinator.sessionProjection's,
// which the Swift store's reader already ports; handing it this store's
// record keeps one rule for both sources rather than a second copy of it.
func ownCrown(rec *coordinator.Record, live swiftstore.Live) *contract.SessionCoordinator {
	if rec == nil {
		return nil
	}
	pid := int64(rec.PID)
	start := swiftstore.Seconds(rec.ProcessStart.Unix())
	conversation := rec.ConversationID
	snap := swiftstore.Snapshot{Coordinator: &swiftstore.Coordinator{
		Version: 1, ID: rec.ID, Label: coordinator.Label, SessionID: rec.TerminalID, Assistant: rec.Assistant,
		TTY: rec.TTY, PID: &pid, ProcessStart: &start, ConversationID: &conversation, CWD: rec.CWD,
	}}
	return snap.CoordinatorFor(live)
}
