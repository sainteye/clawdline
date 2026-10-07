package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/session"
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
		Store:           s.store,
		MachineStateDir: s.cfg.Dir,
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
		c.Read = s.freshReading
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
		c := s.coordinator()
		c.Read = s.freshReading
		st, moved, err := c.Rebind(ctx, body.ExpectedCoordinatorID, body.ExpectedGeneration,
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
		writeNoSuchRoute(w, r)
	}
}

// startCoordinator opens a machine Session through the same bounded,
// receipted terminal path as an ordinary start. Its cwd is never an input or
// a Project. The Session must be observed and registered before it wears the
// role; opening a terminal alone is not a role receipt.
func (s *Server) startCoordinator(w http.ResponseWriter, r *http.Request, assistant string) {
	release, ok := admitOpening(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c := s.coordinator()
	c.Read = s.freshReading
	st, err := c.State(ctx)
	if err != nil {
		writeCoordinatorError(w, err)
		return
	}
	if code := machineStartBlock(st, s.cfg.Dir); code != "" {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusConflict, Code: code,
			Message: "A machine Session cannot be started until the existing role and Sessions are fully accounted for."})
		return
	}
	workspace := projects.MachineWorkspace(s.cfg.Dir)
	if err := ensureMachineWorkspace(s.cfg.Dir); err != nil {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusConflict, Code: "machine_workspace_invalid",
			Message: "The machine workspace or its instructions could not be prepared; nothing was started."})
		return
	}
	starter := s.starter(startReadingFrom(st.Seen.Inventory))
	made, err := starter.Start(ctx, projects.Place{Path: workspace}, assistant, "", "", "")
	if err != nil {
		writeStartRefusal(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": made.ID, "backend": made.Backend, "assistant": assistant,
		"model": made.Model, "cwd": filepath.Clean(workspace), "attach": made.Attach,
		"scope": "machine", "registration": "pending", "at": time.Now().Unix()})
}

func machineStartBlock(st app.State, stateDir string) string {
	if st.Status == store.CoordinatorCorrupt || st.Status == store.CoordinatorUnsupported {
		return "coordinator_store_invalid"
	}
	if st.Record != nil && st.Liveness != coordinator.Offline {
		if st.Liveness == coordinator.Unknown {
			return "coordinator_liveness_unknown"
		}
		return "coordinator_online"
	}
	// A missing workspace Session in a partial inventory is not proof that it
	// does not exist. Keep a pending, unregistered Session from duplicating.
	if !st.Seen.Inventory.Complete {
		return "coordinator_liveness_unknown"
	}
	for _, live := range st.Seen.Sessions {
		if projects.IsMachineWorkspace(stateDir, live.CWD) {
			return "coordinator_session_exists"
		}
	}
	return ""
}

const machineInstructions = `# Clawdfather machine workspace

This directory is managed by Clawdline and is outside every Project. Work here as a machine steward: inspect and report on Sessions, tasks, waits and landings with source and observation time; use Clawdline's supported setting and project import/export operations when authorized.

Do not edit source code in any Project, including Clawdline. When the person requests engineering work through Clawdline, create a Board item in the target Project first, then delegate it to a Project Session with ` + "`clawdline item add --project <id> --kind feature --title <title> --assign-new`" + `. The created item's owner handles implementation, child dispatch, verification, landing and deployment. If the person has not explicitly asked for an item, file a proposal for them to accept; do not create an item or dispatch code work on your own. Never dispatch a code task directly from this machine Session or mark another owner's work complete. This directory is an organizational boundary, not an operating system sandbox.

Opening this Session does not register the machine role. After your conversation ID is available, run ` + "`clawdline coordinator bind`" + ` in this Session. If the command is not on PATH, use the daemon's installed binary at ` + "`../bin/clawdline`" + ` from this workspace (` + "`../bin/clawdline.exe`" + ` on Windows). The command uses this conversation ID and rebinds an older role only if it is proven offline. Read ` + "`clawdline guide coordination`" + ` for the receipt and limits. Do not treat an unknown reading as offline.
`

func ensureMachineWorkspace(stateDir string) error {
	workspace := projects.MachineWorkspace(stateDir)
	if err := os.Mkdir(workspace, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(workspace)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !projects.IsMachineWorkspace(stateDir, workspace) {
		return errors.New("the workspace is not a plain directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errors.New("the workspace is readable or writable by other accounts")
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(workspace, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			st, statErr := os.Lstat(path)
			if statErr != nil || !st.Mode().IsRegular() {
				return errors.New("a workspace instruction file is not regular")
			}
			if runtime.GOOS != "windows" && st.Mode().Perm()&0o022 != 0 {
				return errors.New("a workspace instruction file is writable by other accounts")
			}
			continue
		}
		if err != nil {
			return err
		}
		if _, err = file.WriteString(machineInstructions); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
	}
	return nil
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
			Message: "Unknown field(s): " + strings.Join(sortedStrings(unknown), ", ") + ".", RawMessage: true})
		return false
	}
	encoded, _ := json.Marshal(raw)
	if err := json.Unmarshal(encoded, into); err != nil {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusBadRequest, Code: "bad_request",
			Message: "A field has the wrong type: " + err.Error(), RawMessage: true})
		return false
	}
	return true
}

func writeCoordinatorError(w http.ResponseWriter, err error) {
	var ref app.RoleRefusal
	if errors.As(err, &ref) {
		writeBrokerRefusal(w, orchestrator.Refusal{Status: ref.Status, Code: ref.Code, Message: ref.Message, RawMessage: ref.RawMessage, Extra: ref.Extra})
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
		return contract.BearingsSource{ObservedAt: at, Provenance: provenance,
			Freshness: contract.SourceFreshness(freshness)}
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
				Freshness: contract.SourceFreshness(b.SessionsFresh)},
			Tasks: src(b.TasksFresh, "broker"),
			// Its own word, not the task records'. The records can be read in
			// full and still say nothing about whether the work they describe
			// reached its target; that is `unverified`, and it used to be
			// `current` here because this line was a copy of the one above it.
			Landings: src(b.LandingsFresh, "broker"),
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
