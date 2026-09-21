package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// The hand-over plane's routes (docs/design-decisions.md §6 W6): handoffs,
// Root Assignments, task graphs and the reclamation sweep. Every one of them
// is this machine's orchestrator token's, reads included — a handoff names
// another session's conversation, and a sweep names every checkout on the
// machine — as the Swift app's are (RemoteServer.swift:2653-2679).

func (s *Server) machineOnly(w http.ResponseWriter, r *http.Request) bool {
	if machineAuthed(r) {
		return true
	}
	writeAuthRefusal(w, http.StatusForbidden, "forbidden", "That needs the orchestrator token.")
	return false
}

// handoffsRoute is /v1/orchestrator/handoffs and /v1/orchestrator/handoffs/<id>.
func (s *Server) handoffsRoute(w http.ResponseWriter, r *http.Request) {
	if !s.machineOnly(w, r) {
		return
	}
	id := strings.Trim(strings.TrimPrefix(routePath(r), "/v1/orchestrator/handoffs"), "/")
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := s.broker.Handoffs(r.Context())
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := contract.BrokerHandoffList{Handoffs: []contract.BrokerHandoff{}, PackageRoot: s.broker.HandoffRoot(),
			At: time.Now().Unix()}
		for _, h := range list {
			out.Handoffs = append(out.Handoffs, wireHandoff(h))
		}
		writeJSON(w, out)
	case id == "" && r.Method == http.MethodPost:
		var body orchestrator.HandoffRequest
		if !decodeClosed(w, r, &body, "handoff_id", "project_dir", "assistant", "model", "title",
			"from_session", "coordinator_plain_handoff") {
			return
		}
		// Opening a tab and waiting for its prompt outlives an impatient
		// caller; a handoff stopped halfway is a tab nobody recorded.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
		defer cancel()
		h, replayed, err := s.broker.OpenHandoff(ctx, body)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.BrokerHandoffResult{OK: true, Replayed: replayed, Handoff: wireHandoff(h)})
	case id != "" && r.Method == http.MethodGet:
		h, err := s.broker.HandoffByID(r.Context(), decodeSegment(id))
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.BrokerHandoffEnvelope{Handoff: wireHandoff(h)})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Handoffs are read with GET and opened with POST.")
	}
}

// rootAssignmentsRoute is /v1/orchestrator/root-assignments[/<id>].
func (s *Server) rootAssignmentsRoute(w http.ResponseWriter, r *http.Request) {
	if !s.machineOnly(w, r) {
		return
	}
	id := strings.Trim(strings.TrimPrefix(routePath(r), "/v1/orchestrator/root-assignments"), "/")
	switch {
	case id == "" && r.Method == http.MethodGet:
		list, err := s.broker.RootAssignments(r.Context())
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := contract.BrokerRootAssignmentList{RootAssignments: []contract.BrokerRootAssignment{}, At: time.Now().Unix()}
		for _, a := range list {
			out.RootAssignments = append(out.RootAssignments, wireAssignment(a))
		}
		writeJSON(w, out)
	case id == "" && r.Method == http.MethodPost:
		var body orchestrator.RootAssignmentRequest
		if !decodeClosed(w, r, &body, "request_id", "assistant", "model", "project_dir", "label", "assignment") {
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
		defer cancel()
		a, replayed, err := s.broker.OpenRootAssignment(ctx, r.Header.Get("Idempotency-Key"), body)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.BrokerRootAssignmentResult{OK: true, Replayed: replayed, RootAssignment: wireAssignment(a)})
	case id != "" && r.Method == http.MethodGet:
		a, err := s.broker.RootAssignmentByID(r.Context(), decodeSegment(id))
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.BrokerRootAssignmentEnvelope{RootAssignment: wireAssignment(a)})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"Root Assignments are read with GET and opened with POST.")
	}
}

// graphsRoute is GET /v1/orchestrator/graphs: every graph this broker's tasks
// name, with each node's state as its tasks say it is now.
func (s *Server) graphsRoute(w http.ResponseWriter, r *http.Request) {
	if !s.machineOnly(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Graphs are read with GET.")
		return
	}
	graphs, err := s.broker.Graphs(r.Context())
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	out := contract.BrokerGraphList{Graphs: []contract.BrokerGraph{}, At: time.Now().Unix()}
	for _, g := range graphs {
		var wire contract.BrokerGraph
		recast(g, &wire)
		out.Graphs = append(out.Graphs, wire)
	}
	writeJSON(w, out)
}

// reclaimRoute is /v1/orchestrator/reclaim: GET is the last sweep and every
// standing decision; POST runs one sweep — a dry run unless the body says
// `"dry_run": false`, because the request that removes files is the one that
// has to say so.
func (s *Server) reclaimRoute(w http.ResponseWriter, r *http.Request) {
	if !s.machineOnly(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := s.broker.Reclaims(r.Context())
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		out := contract.BrokerReclaimState{Decisions: []contract.BrokerReclaimStanding{},
			WorktreeRoot: s.broker.WorktreeRoot(), ReclaimedRoot: s.broker.ReclaimedDir(), At: time.Now().Unix()}
		if last := s.broker.LastReclaim(); last != nil {
			rep := wireReclaim(*last)
			out.Last = &rep
		}
		for _, row := range rows {
			var evidence map[string]any
			_ = json.Unmarshal(row.Detail, &evidence)
			out.Decisions = append(out.Decisions, contract.BrokerReclaimStanding{
				Task: row.Task, Subject: contract.BrokerReclaimSubject(row.Subject),
				Outcome: contract.BrokerReclaimOutcome(row.Outcome), Reason: row.Reason, Bytes: row.Bytes,
				Since: row.FirstAt.Unix(), LastSeen: row.LastAt.Unix(), Evidence: wireEvidence(evidence),
			})
		}
		writeJSON(w, out)
	case http.MethodPost:
		body := struct {
			DryRun *bool `json:"dry_run"`
		}{}
		if r.ContentLength != 0 && !decodeClosed(w, r, &body, "dry_run") {
			return
		}
		dry := body.DryRun == nil || *body.DryRun
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
		defer cancel()
		rep, err := s.broker.Reclaim(ctx, dry)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, wireReclaim(rep))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The sweep is read with GET and run with POST.")
	}
}

// recast moves a value into its contract type through the JSON both spell
// the same way. The broker's types carry the wire names as their tags; the
// test beside this file holds each pair to losing nothing on the way.
func recast(from, into any) {
	body, err := json.Marshal(from)
	if err == nil {
		_ = json.Unmarshal(body, into)
	}
}

func wireHandoff(h orchestrator.Handoff) contract.BrokerHandoff {
	var out contract.BrokerHandoff
	recast(h, &out)
	return out
}

func wireAssignment(a orchestrator.RootAssignment) contract.BrokerRootAssignment {
	var out contract.BrokerRootAssignment
	recast(a, &out)
	return out
}

func wireReclaim(rep orchestrator.ReclaimReport) contract.BrokerReclaimReport {
	out := contract.BrokerReclaimReport{
		At: rep.At.Unix(), DryRun: rep.DryRun, Decisions: []contract.BrokerReclaimDecision{},
		Foreign: rep.Foreign, ForeignCount: int64(rep.ForeignCount), Removed: int64(rep.Removed),
		Preserved: int64(rep.Preserved), Kept: int64(rep.Kept), BytesFreed: rep.BytesFreed,
		Deferred: int64(rep.Deferred), Unreadable: int64(rep.Unreadable),
	}
	if out.Foreign == nil {
		out.Foreign = []string{}
	}
	for _, d := range rep.Decisions {
		out.Decisions = append(out.Decisions, contract.BrokerReclaimDecision{
			Task: d.Task, Subject: contract.BrokerReclaimSubject(d.Subject), Path: d.Path,
			Outcome: contract.BrokerReclaimOutcome(d.Outcome), Reason: d.Reason, Bytes: d.Bytes,
			Evidence: wireEvidence(d.Evidence),
		})
	}
	return out
}

// problemKeys are the evidence keys that hold the error which left a question
// unanswered; the wire has one name for all of them.
var problemKeys = []string{"worktree_list", "snapshot", "walk", "attributes", "preserve", "remove", "intent",
	"second_snapshot", "landing_proof"}

// wireEvidence is a decision's evidence in its contract type: the owner's
// answers lifted beside the rest, and whichever error there was as `problem`.
func wireEvidence(ev map[string]any) *contract.BrokerReclaimEvidence {
	if len(ev) == 0 {
		return nil
	}
	flat := map[string]any{}
	for k, v := range ev {
		flat[k] = v
	}
	if owner, ok := ev["owner"].(map[string]any); ok {
		for k, v := range owner {
			if _, taken := flat[k]; !taken {
				flat[k] = v
			}
		}
	}
	if nested, ok := ev["evidence"].(map[string]any); ok {
		for k, v := range nested {
			if _, taken := flat[k]; !taken {
				flat[k] = v
			}
		}
	}
	for _, k := range problemKeys {
		if v, ok := ev[k].(string); ok && v != "" {
			flat["problem"] = k + ": " + v
			break
		}
	}
	var out contract.BrokerReclaimEvidence
	recast(flat, &out)
	return &out
}
