package http

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Runs: the proof that a person said something to a session (design-decisions
// U4; internal/app/runs.go, internal/domain/work/runs.go).
//
//	POST /v1/sessions/{id}/send                      a person's message; issues its run
//	GET  /v1/orchestrator/sessions/{session}/run     the run of the newest message a person sent that session
//	GET  /v1/orchestrator/runs/{run}                 what a run was: which session, when, sent by whom
//
// A run is issued by the send route alone — the one route a person's device,
// and never the orchestrator credential, may type into a session with
// (maySend) — once the bytes are typed. A session relays the person's words
// to the board, a proposal or a decision by naming it: {"via":{"run":"…"}}.
// The relay is refused, by name, when the run was never issued
// (run_unknown), is more than a day old (run_expired), was a message to a
// different session than the proposal or decision it answers
// (run_other_session), or was sent before that question was put
// (run_before_question). It is checked inside the write, after the request's
// receipt is claimed, so a retry of an answered relay is its stored answer.
// Before runs had an issuer a relay named any string of the right letters;
// such a relay is now refused as run_unknown, and the moves it wrote before
// stay as they were written.
//
// What a run does not prove is which session relays it: the orchestrator
// credential is the whole machine's, and the session route below answers
// whoever holds it (internal/domain/work/runs.go).

var runsByServer sync.Map // *Server -> *app.Runs

// runs is this server's run keeper.
func (s *Server) runs() *app.Runs {
	if r, ok := runsByServer.Load(s); ok {
		return r.(*app.Runs)
	}
	got, _ := runsByServer.LoadOrStore(s, &app.Runs{Store: s.store})
	return got.(*app.Runs)
}

// personPrincipal is the credential a person wrote with: this Mac's own
// browser, or a paired device.
func personPrincipal(r *http.Request) string {
	v := accessOf(r).verdict
	if v.Local {
		return "local"
	}
	return "device:" + v.Device
}

// issueRun records the run of a message a person just had typed into sess.
// The bytes are already typed, so a run that could not be recorded is
// logged and the send still succeeds; the session then has no run to relay
// that message under, which is the refusal side, never the believing one.
func (s *Server) issueRun(ctx context.Context, r *http.Request, sess session.Session) {
	if s.store == nil {
		return
	}
	if _, err := s.runs().Issue(context.WithoutCancel(ctx), sess, personPrincipal(r)); err != nil {
		log.Printf("runs: the run of a message to %s was not recorded: %v", sess.ID, err)
	}
}

type runWire struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	TerminalID string `json:"terminal_id"`
	Assistant  string `json:"assistant,omitempty"`
	Principal  string `json:"principal"`
	At         int64  `json:"at"`
	// RelayUntil is when this run stops carrying a relay (work.RelayWindow).
	RelayUntil int64 `json:"relay_until"`
}

type runOneWire struct {
	OK  bool    `json:"ok"`
	Run runWire `json:"run"`
}

func runOf(r work.Run) runWire {
	return runWire{ID: r.ID, SessionID: r.Session, TerminalID: r.Terminal, Assistant: r.Assistant,
		Principal: r.Principal, At: r.At.Unix(), RelayUntil: r.At.Add(work.RelayWindow).Unix()}
}

// sessionRun is GET /v1/orchestrator/sessions/{session}/run: the run a
// session relays its person's newest message under. The session is named by
// its conversation id — the root.session_id a dispatch sends — or its
// terminal id.
func (s *Server) sessionRun(w http.ResponseWriter, r *http.Request, id string) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "A session reads its run with the orchestrator token.")
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A run is read with GET.")
		return
	}
	id = strings.TrimSpace(id)
	if id == "" || s.store == nil {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such session.")
		return
	}
	run, ok, err := s.runs().Latest(r.Context(), id)
	switch {
	case err != nil:
		w.Header().Set("Retry-After", "1")
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The runs could not be read.")
	case !ok:
		writeRefusal(w, http.StatusNotFound, "no_run",
			"Nobody has sent this session a message through Clawdline, so it has no run to relay a person's words under. "+
				"A person typing straight into the terminal is not a run; ask them to answer in Clawdline.")
	default:
		writeJSON(w, runOneWire{OK: true, Run: runOf(run)})
	}
}

// runRoute is GET /v1/orchestrator/runs/{run}: what a run was. A person
// reads it too, to see whose words a relayed move rests on.
func (s *Server) runRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A run is read with GET.")
		return
	}
	if !machineAuthed(r) && !accessOf(r).verdict.Allowed {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This needs the orchestrator token or a paired device.")
		return
	}
	id := strings.TrimPrefix(routePath(r), "/v1/orchestrator/runs/")
	if !work.RunShaped(id) || s.store == nil {
		writeRefusal(w, http.StatusNotFound, "run_unknown", "No run has that id.")
		return
	}
	run, ok, err := s.runs().Find(r.Context(), id)
	switch {
	case err != nil:
		w.Header().Set("Retry-After", "1")
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The runs could not be read.")
	case !ok:
		writeRefusal(w, http.StatusNotFound, "run_unknown", "No run has that id.")
	default:
		writeJSON(w, runOneWire{OK: true, Run: runOf(run)})
	}
}

// relayRun is the run a session's relay names, found and checked as a
// relay's run; nil for a write a person made as themselves. Its refusals are
// the board's (*app.WorkError), answered by the route that asked.
func (s *Server) relayRun(ctx context.Context, id string) (*work.Run, error) {
	if id == "" {
		return nil, nil
	}
	if s.store == nil {
		return nil, &app.WorkError{Status: http.StatusServiceUnavailable, Code: "store_unavailable",
			Message: "This daemon keeps no runs."}
	}
	run, err := s.runs().Relay(ctx, id)
	if err != nil {
		return nil, err
	}
	return &run, nil
}
