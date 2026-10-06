package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// An agent closing a finished Session: `clawdline session close`.
//
// The paired-device routes (`/v1/sessions`, `…/close`) stay a person's. This
// is the machine credential's way to the same guarded close, and it is
// narrower in two ways: a Session may close only itself, or a Feature Root its
// Epic owner opened with `--assign-new`; and `force` does not exist here. A
// person may know something the obligations do not; an agent closing its own
// Session is exactly the one who must not override them.
//
//	GET  /v1/work/v2/agent/sessions/<terminal>/closeability?session_id=<caller>
//	POST /v1/work/v2/agent/sessions/<terminal>/close
//	     {"session_id": "<caller>", "expected_closeability_version": "…"}

// agentCloseRequest is the body of an agent's close.
type agentCloseRequest struct {
	SessionID                   string `json:"session_id"`
	ExpectedCloseabilityVersion string `json:"expected_closeability_version"`
	Force                       bool   `json:"force"`
}

// agentCloseAudit is everything the close looked at, as the agent reads it.
type agentCloseAudit struct {
	TerminalID string                 `json:"terminal_id"`
	State      string                 `json:"state"`
	Version    string                 `json:"version"`
	Reasons    []contract.CloseReason `json:"reasons"`
	// Authority is why this caller may close it: self, or epic_owner.
	Authority string `json:"authority"`
}

// agentCloseRefusal is a refusal before anything was closed.
type agentCloseRefusal struct {
	status        int
	code, message string
}

func (s *Server) agentSessions(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) != 3 {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such Agent work-system route.")
		return
	}
	// The route reads the escaped path (routePath), and a tmux pane's name
	// `%84` arrives as `%2584`: the segment is a name, so it is decoded.
	terminal := decodeSegment(parts[1])
	switch {
	case parts[2] == "closeability" && r.Method == http.MethodGet:
		audit, refusal := s.agentAuditClose(r.Context(), r.URL.Query().Get("session_id"), terminal)
		if refusal != nil {
			writeRefusal(w, refusal.status, refusal.code, refusal.message)
			return
		}
		writeJSON(w, audit)
	case parts[2] == "close" && r.Method == http.MethodPost:
		s.agentCloseSession(w, r, terminal)
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such Agent work-system route.")
	}
}

func (s *Server) agentCloseSession(w http.ResponseWriter, r *http.Request, terminal string) {
	var body agentCloseRequest
	if _, ok := readWorkV2Body(w, r, &body); !ok {
		return
	}
	if body.Force {
		writeRefusal(w, http.StatusUnprocessableEntity, "force_refused",
			"An agent cannot force a close. Clear what the Session still owes, or ask the person to close it.")
		return
	}
	audit, refusal := s.agentAuditClose(r.Context(), body.SessionID, terminal)
	if refusal != nil {
		writeRefusal(w, refusal.status, refusal.code, refusal.message)
		return
	}
	if audit.Authority == "self" && onlyThisTurn(audit) {
		s.scheduleOwnClose(w, r, body, audit)
		return
	}
	switch {
	case audit.State == string(contract.CloseabilityStateBlocked):
		markFixedRefusalKey(w, fixedRefusalKey("This Session still has unfinished work."))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(closeRefusalWire("close_blocked", "This Session still has unfinished work.", audit.Reasons))
		return
	case audit.State != string(contract.CloseabilityStateSafe):
		markFixedRefusalKey(w, fixedRefusalKey("What this Session owes could not be read clearly enough to close it."))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(closeRefusalWire("closeability_unknown", "What this Session owes could not be read clearly enough to close it.", audit.Reasons))
		return
	case body.ExpectedCloseabilityVersion != "" && body.ExpectedCloseabilityVersion != audit.Version:
		writeRefusal(w, http.StatusConflict, "close_not_proven",
			"The Session changed since it was read. Read its closeability again before closing.")
		return
	}
	ctx, more := context.WithTimeout(r.Context(), closeBudget)
	defer more()
	if _, err := s.closeActions(r).Close(ctx, terminal, false); err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, contract.ActionResult{OK: true, ID: terminal, Action: "closed"})
}

// scheduleOwnClose is a Session's close of itself while the one thing left is
// the turn it is running: recorded, and carried out when that turn ends
// (agent_close_schedule.go). An Epic owner closes only an idle Session and is
// never scheduled.
func (s *Server) scheduleOwnClose(w http.ResponseWriter, r *http.Request, body agentCloseRequest, audit agentCloseAudit) {
	if body.ExpectedCloseabilityVersion != "" && body.ExpectedCloseabilityVersion != audit.Version {
		writeRefusal(w, http.StatusConflict, "close_not_proven",
			"The Session changed since it was read. Read its closeability again before closing.")
		return
	}
	sc, added, err := s.closes.add(audit.TerminalID, body.SessionID, personPrincipal(r))
	if err != nil {
		writeRefusal(w, http.StatusTooManyRequests, "close_schedule_full",
			"Too many Sessions are already waiting to close when their turn ends. Run the close again after this turn.")
		return
	}
	if added {
		s.recordCloseEvent(r.Context(), "session.close_scheduled", sc.Terminal,
			map[string]any{"conversation": sc.Caller, "reasons": audit.Reasons})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(contract.ActionResult{OK: true, ID: sc.Terminal, Action: "close_scheduled"})
}

// agentAuditClose reads one Session's closeability from a fresh inventory,
// checks the caller may close it, and adds what the Session list does not
// carry: completion notices of its children it has not acknowledged.
func (s *Server) agentAuditClose(ctx context.Context, caller, terminal string) (agentCloseAudit, *agentCloseRefusal) {
	if caller == "" {
		return agentCloseAudit{}, &agentCloseRefusal{http.StatusBadRequest, "session_id_required",
			"Name the calling Session's conversation as session_id."}
	}
	inv := s.freshReading(ctx)
	var target session.Session
	found := false
	for _, item := range inv.Sessions {
		if item.ID == terminal {
			target, found = item, true
			break
		}
	}
	snapshot := s.sessionsPayloadFrom(ctx, inv)
	var row *sessionRowWire
	for i := range snapshot.Sessions {
		if snapshot.Sessions[i].ID == terminal {
			row = &snapshot.Sessions[i]
			break
		}
	}
	if !found || row == nil {
		if snapshot.Scan.Complete {
			return agentCloseAudit{}, &agentCloseRefusal{http.StatusNotFound, "session_not_found",
				"That Session is not on this machine; it may already be closed."}
		}
		return agentCloseAudit{}, &agentCloseRefusal{http.StatusConflict, "close_inventory_unavailable",
			"A fresh Session inventory did not complete. Try again after the next scan."}
	}
	authority := ""
	switch {
	case target.ConversationID != "" && target.ConversationID == caller:
		authority = "self"
	case row.EpicParent != nil && row.EpicParent.OwnerSessionID == caller:
		// A Feature Root opened by this Epic owner. Whether its item is done,
		// or now held by another Session, is the closeability's to say: an
		// item it still owns and has not finished blocks below.
		authority = "epic_owner"
	default:
		return agentCloseAudit{}, &agentCloseRefusal{http.StatusForbidden, "close_not_yours",
			"A Session may close only itself, or a Feature Root its Epic opened with --assign-new."}
	}
	c := row.Closeability
	reasons := append([]contract.CloseReason{}, c.Reasons...)
	reasons = append(reasons, s.unacknowledgedCloseReasons(ctx, target.ConversationID)...)
	reasons = append(reasons, s.childTaskCloseReasons(ctx, target.ConversationID)...)
	return agentCloseAudit{
		TerminalID: terminal, State: string(agentCloseState(c.State, reasons)), Version: c.Version,
		Reasons: reasons, Authority: authority,
	}, nil
}

// unacknowledgedCloseReasons is every child completion the Session has not
// acknowledged, read through the same ledger its turn boundary reads
// (addUnacknowledgedCompletions), so a change to what counts as owed reaches
// the close too. An unreadable ledger is evidence, never an empty list.
func (s *Server) unacknowledgedCloseReasons(ctx context.Context, conversation string) []contract.CloseReason {
	answer := map[string]any{}
	s.addUnacknowledgedCompletions(ctx, answer, conversation)
	if unknown, _ := answer["unacknowledged_completions_unknown"].(bool); unknown || conversation == "" {
		return []contract.CloseReason{{Code: "completion_notices_unreadable", Kind: "evidence",
			Mover: contract.CloseMover{Kind: "broker"}}}
	}
	out := []contract.CloseReason{}
	list, _ := answer["unacknowledged_completions"].([]unacknowledgedCompletionWire)
	for _, c := range list {
		out = append(out, contract.CloseReason{Code: "completion_unacknowledged", Kind: "obligation",
			SubjectKind: "task", SubjectID: c.TaskID, Mover: contract.CloseMover{Kind: "session", Self: true}})
	}
	return out
}

// childTaskCloseReasons is what the Session list does not carry about this
// Session's own children: a task that has not finished, and a finished one
// whose worktree the broker's inventory still counts as unlanded
// (docs/worktrees.md) — the same classification `tools/check-worktrees.sh`
// keeps a checkout for. A ledger or inventory that cannot be read is
// evidence: the close then reads unknown, never safe.
func (s *Server) childTaskCloseReasons(ctx context.Context, conversation string) []contract.CloseReason {
	unreadable := func(code string) []contract.CloseReason {
		return []contract.CloseReason{{Code: code, Kind: "evidence", Mover: contract.CloseMover{Kind: "broker"}}}
	}
	if s.broker == nil || s.broker.Store == nil || conversation == "" {
		return unreadable("child_tasks_unreadable")
	}
	records, bad, err := s.broker.Records(ctx)
	if err != nil || len(bad) > 0 {
		return unreadable("child_tasks_unreadable")
	}
	out := []contract.CloseReason{}
	own := map[string]bool{}
	repos := []string{}
	seen := map[string]bool{}
	for _, r := range records {
		if r.Root == nil || r.Root.SessionID != conversation {
			continue
		}
		if !r.State.Terminal() {
			out = append(out, contract.CloseReason{Code: "child_task_running", Kind: "obligation",
				SubjectKind: "task", SubjectID: r.ID, Mover: contract.CloseMover{Kind: "task", TaskID: r.ID}})
			continue
		}
		if r.Worktree == nil || r.Worktree.Repository == "" {
			continue
		}
		own[r.ID] = true
		if !seen[r.Worktree.Repository] {
			seen[r.Worktree.Repository] = true
			repos = append(repos, r.Worktree.Repository)
		}
	}
	for _, repo := range repos {
		inv, err := s.broker.ReadInventory(ctx, repo, nil)
		if err != nil {
			return append(out, unreadable("worktrees_unreadable")...)
		}
		for _, row := range inv.Unreadable {
			if own[row.Task] {
				return append(out, unreadable("worktrees_unreadable")...)
			}
		}
		for _, row := range inv.Unlanded {
			if own[row.Task] {
				out = append(out, contract.CloseReason{Code: "worktree_unlanded", Kind: "obligation",
					SubjectKind: "task", SubjectID: row.Task, Mover: contract.CloseMover{Kind: "session", Self: true}})
			}
		}
	}
	return out
}

// agentCloseState is the list's state once the agent's own reasons are added:
// unreadable evidence makes it unknown, an extra obligation makes a safe row
// blocked, and nothing ever turns blocked or unknown into safe.
func agentCloseState(listed contract.CloseabilityState, reasons []contract.CloseReason) contract.CloseabilityState {
	obligations := false
	for _, r := range reasons {
		if r.Kind == "evidence" {
			return contract.CloseabilityStateUnknown
		}
		if r.Kind == "obligation" {
			obligations = true
		}
	}
	if listed == contract.CloseabilityStateSafe && obligations {
		return contract.CloseabilityStateBlocked
	}
	return listed
}
