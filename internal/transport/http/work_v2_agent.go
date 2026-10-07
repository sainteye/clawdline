package http

import (
	"context"
	"encoding/json"
	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
	"net/http"
	"strings"
	"time"
)

func (s *Server) workV2Agent(w http.ResponseWriter, r *http.Request, parts []string) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusUnauthorized, "machine_required", "Only an authenticated Session may use this route.")
		return
	}
	if len(parts) >= 1 && parts[0] == "sessions" {
		s.agentSessions(w, r, parts)
		return
	}
	if len(parts) == 1 && parts[0] == "human-interventions" && r.Method == http.MethodPost {
		s.agentCreateHumanIntervention(w, r)
		return
	}
	if len(parts) == 1 && parts[0] == "items" && r.Method == http.MethodPost {
		s.agentCreateItem(w, r)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "gate-decision" {
		s.workV2GateDecision(w, r, parts[1], false)
		return
	}
	if len(parts) == 1 && parts[0] == "proposals" && r.Method == http.MethodPost {
		var body struct {
			ProjectID           string `json:"project_id"`
			Kind                string `json:"kind"`
			Title               string `json:"title"`
			Description         string `json:"description"`
			Reason              string `json:"reason"`
			SuggestedAcceptance string `json:"suggested_acceptance"`
			SessionID           string `json:"session_id"`
			SourceWorkID        string `json:"source_work_id"`
			SourceTodoID        string `json:"source_todo_id"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		project, ok := s.workV2Project(r.Context(), body.ProjectID)
		if !ok {
			writeRefusal(w, http.StatusUnprocessableEntity, "project_not_found", "Choose a Project from the current Project catalog.")
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		p, err := s.workV2().Propose(r.Context(), work.ProposalV2{ProjectID: project.ID, ProjectPath: project.Path,
			Kind: work.Kind(body.Kind), Title: body.Title, Description: body.Description, Reason: body.Reason,
			SuggestedAcceptance: body.SuggestedAcceptance, SessionID: body.SessionID,
			SourceWorkID: body.SourceWorkID, SourceTodoID: body.SourceTodoID})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		answer, _ := json.Marshal(map[string]any{"ok": true, "proposal": proposalV2Of(p)})
		if err := s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}); err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "children" && r.Method == http.MethodPost {
		s.agentCreateEpicChild(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "assign" && r.Method == http.MethodPost {
		s.agentAssignEpicChild(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "claim" && r.Method == http.MethodPost {
		s.agentClaimItem(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "session-name" && r.Method == http.MethodPost {
		s.agentNameItemSession(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "edit" && r.Method == http.MethodPatch {
		s.workV2Edit(w, r, parts[1], false)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "acceptance-revision" && r.Method == http.MethodPost {
		s.agentReviseAcceptance(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "reopen" && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			SessionID       string `json:"session_id"`
			Reason          string `json:"reason"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		itemOf := s.workV2ItemProjector(r.Context())
		var answer []byte
		_, err := s.workV2().ReopenIncomplete(r.Context(), parts[1], app.AgentReopenWorkV2{
			ExpectedVersion: body.ExpectedVersion, SessionID: body.SessionID, Reason: body.Reason,
		}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(itemOf(v))
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "phase" && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion    int64                   `json:"expected_version"`
			SessionID          string                  `json:"session_id"`
			Next               string                  `json:"next"`
			Verification       string                  `json:"verification"`
			Candidate          *workV2CandidateRequest `json:"candidate"`
			Landing            *workV2LandingRequest   `json:"landing"`
			NoLandingReason    string                  `json:"no_landing_reason"`
			Deployment         string                  `json:"deployment"`
			NoDeploymentReason string                  `json:"no_deployment_reason"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		catalog := s.workV2Projects(r.Context())
		item, err := s.workV2().Item(r.Context(), parts[1])
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		var candidate *contract.WorkGateCandidateReceipt
		roundID, attemptID := "", ""
		if item.Item.Phase == work.PhaseImplementing && work.Phase(body.Next) == work.PhaseVerifying && item.Item.VerifyGate {
			candidate, err = verifyWorkV2Candidate(r.Context(), gitadapter.New(), item, body.SessionID,
				body.Candidate, time.Now().Truncate(time.Second))
			if err != nil {
				_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
				s.writeWorkV2Error(w, err)
				return
			}
			roundID = newWorkV2UUID()
			attemptID = deterministicWorkGateID(roundID + ":attempt:0")
		}
		if refusal := noLandingWithLanding(body.NoLandingReason, body.Landing); refusal != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, refusal)
			return
		}
		var detections []orchestrator.LandingDetection
		if work.Phase(body.Next) == work.PhaseDeploying {
			// A merge made a moment ago is recorded now, not at the beat's
			// next look (R6): the step below reads what this writes.
			if _, detections, err = s.detectItemLandings(r.Context(), parts[1]); err != nil {
				_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
				s.writeWorkV2Error(w, err)
				return
			}
		}
		repo := item.Item.ProjectPath
		if body.Landing != nil && strings.TrimSpace(body.Landing.Project) != "" {
			other, ok := catalog[strings.TrimSpace(body.Landing.Project)]
			if !ok {
				_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
				writeRefusal(w, http.StatusUnprocessableEntity, "landing_project_not_found",
					"landing.project names no Project in the current catalog.")
				return
			}
			repo = other.Path
		}
		landing, landingErr := verifyWorkV2DirectLanding(r.Context(), gitadapter.New(), item, body.SessionID, repo, body.Landing)
		if landingErr != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, landingErr)
			return
		}
		var effects []store.Effect
		if work.Phase(body.Next) == work.PhaseDone {
			effects = []store.Effect{workV2CompletionEffect(item, body.SessionID, s.productLanguage())}
		}
		var answer []byte
		changed, err := s.workV2().Advance(r.Context(), parts[1], app.AdvanceWorkV2{ExpectedVersion: app.AgentExpectedVersion(body.ExpectedVersion),
			SessionID: body.SessionID, Next: work.Phase(body.Next), Verification: body.Verification,
			Landing: landing, NoLandingReason: body.NoLandingReason, Deployment: body.Deployment,
			NoDeploymentReason: body.NoDeploymentReason,
			Actor:              body.SessionID, Effects: effects, Candidate: candidate, RoundID: roundID, AttemptID: attemptID},
			func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = workV2Answer(s.workV2ItemOf(catalog, v))
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, withDetections(err, detections))
			return
		}
		s.broker.RunEffects(r.Context(), changed.EffectIDs)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "finish" && r.Method == http.MethodPost {
		s.agentFinishItem(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) &&
		(parts[2] == "documents" || parts[2] == "steps") && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			SessionID       string `json:"session_id"`
			Role            string `json:"role"`
			Title           string `json:"title"`
			Body            string `json:"body"`
			Reference       string `json:"reference"`
			Position        int64  `json:"position"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		itemOf := s.workV2ItemProjector(r.Context())
		var answer []byte
		file := func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(itemOf(v))
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		}
		var err error
		if parts[2] == "documents" {
			_, err = s.workV2().AddDocument(r.Context(), parts[1], app.AddDocumentV2{ExpectedVersion: app.AgentExpectedVersion(body.ExpectedVersion),
				SessionID: body.SessionID, Role: body.Role, Title: body.Title, Body: body.Body,
				Reference: body.Reference, Position: body.Position}, file)
		} else {
			_, err = s.workV2().AddStep(r.Context(), parts[1], app.AddStepV2{ExpectedVersion: app.AgentExpectedVersion(body.ExpectedVersion),
				SessionID: body.SessionID, Title: body.Title, Position: body.Position}, file)
		}
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 5 && parts[0] == "items" && workID(parts[1]) && parts[2] == "steps" &&
		parts[4] == "complete" && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			SessionID       string `json:"session_id"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		itemOf := s.workV2ItemProjector(r.Context())
		var answer []byte
		_, err := s.workV2().CompleteStep(r.Context(), parts[1], parts[3], body.SessionID, app.AgentExpectedVersion(body.ExpectedVersion),
			func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = workV2Answer(itemOf(v))
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(answer)
		return
	}
	if len(parts) >= 2 && parts[0] == "session-todos" {
		sessionID := parts[1]
		if len(parts) == 2 && r.Method == http.MethodGet {
			rows, truncated, err := s.workV2().DirectTodos(r.Context(), sessionID, false, true)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			out := make([]directTodoV2Wire, 0, len(rows))
			for _, td := range rows {
				images, imageErr := s.workV2().DirectTodoImages(r.Context(), td.ID)
				if imageErr != nil {
					s.writeWorkV2Error(w, imageErr)
					return
				}
				out = append(out, directTodoWire(td, images))
			}
			items, itemTruncated, err := s.workV2().List(r.Context(), "", sessionID, "open", "")
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			recent, recentTruncated, err := s.workV2().RecentlyCompleted(r.Context(), sessionID)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			itemOf := func(v app.WorkV2View) workV2ItemWire { return s.workV2ItemOf(nil, v) }
			if len(items)+len(recent) > 0 {
				itemOf = s.workV2ItemProjector(r.Context())
			}
			assigned := make([]workV2ItemWire, 0, len(items))
			for _, item := range items {
				assigned = append(assigned, itemOf(item))
			}
			completed := make([]workV2ItemWire, 0, len(recent))
			for _, item := range recent {
				completed = append(completed, itemOf(item))
			}
			answer := map[string]any{"ok": true, "assigned_items": assigned, "recent_items": completed,
				"direct_todos": out, "truncated": truncated || itemTruncated || recentTruncated}
			s.addUnacknowledgedCompletions(r.Context(), answer, sessionID)
			writeJSON(w, answer)
			return
		}
		if len(parts) == 2 && r.Method == http.MethodPost {
			s.agentAddSessionTodos(w, r, sessionID)
			return
		}
		if len(parts) == 4 && parts[3] == "complete" && r.Method == http.MethodPost {
			var body struct{}
			raw, ok := readWorkV2Body(w, r, &body)
			if !ok {
				return
			}
			k, ok := s.beginWorkV2Write(w, r, sessionID, raw)
			if !ok {
				return
			}
			var answer []byte
			_, err := s.workV2().CompleteDirectTodo(r.Context(), parts[2], sessionID, sessionID, false,
				func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
					answer = directTodoAnswer(td, nil)
					return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
				})
			if err != nil {
				_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
				s.writeWorkV2Error(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(answer)
			return
		}
	}
	writeNoSuchRoute(w, r)
}

// unacknowledgedCompletionWire is one completion notice a root has not
// acknowledged, on the list that root reads at every turn boundary.
type unacknowledgedCompletionWire struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	// Kind is the notice's own kind: task_finished, or task_stalled for a
	// child that sat idle after its briefing and was ended spawn_failed.
	Kind        string                    `json:"kind"`
	ResultPath  string                    `json:"result_path"`
	NoticeID    string                    `json:"notice_id"`
	NoticeState string                    `json:"notice_state"`
	LastError   *orchestrator.NoticeError `json:"last_error,omitempty"`
	AckPath     string                    `json:"ack_path"`
}

// addUnacknowledgedCompletions puts on a root's own to-do answer every child
// completion it has not acknowledged — pending or dead-lettered — so a root
// that never saw the typed line learns of it at its next turn boundary
// (orchestrator.RootCompletions). A ledger that cannot be read says so with
// `unacknowledged_completions_unknown` rather than failing the to-dos, which
// are the other half of the answer, or answering an empty list, which would
// read as "nothing of yours finished".
func (s *Server) addUnacknowledgedCompletions(ctx context.Context, answer map[string]any, conversation string) {
	list := []unacknowledgedCompletionWire{}
	if s.broker == nil || s.broker.Store == nil {
		answer["unacknowledged_completions"] = list
		answer["unacknowledged_completions_unknown"] = true
		return
	}
	rows, err := s.broker.RootCompletions(ctx, conversation)
	if err != nil {
		answer["unacknowledged_completions"] = list
		answer["unacknowledged_completions_unknown"] = true
		return
	}
	for _, c := range rows {
		list = append(list, unacknowledgedCompletionWire{
			TaskID: c.Record.ID, Title: c.Record.Title, State: string(c.Record.State),
			Kind:       orchestrator.NoticeKind(c.Record),
			ResultPath: s.broker.ResultPath(c.Record.ID), NoticeID: c.Notice.ID,
			NoticeState: string(c.Notice.State), LastError: c.Notice.LastError,
			AckPath: "/v1/orchestrator/tasks/" + c.Record.ID + "/completion/ack",
		})
	}
	answer["unacknowledged_completions"] = list
}

// agentAddSessionTodos is POST /v1/work/v2/agent/session-todos/<conversation>:
// a Session writing its own to-dos because the person asked it to. The
// conversation must be a live, non-child Session; the batch is written whole
// or not at all, and a replay with the same key answers what was stored.
func (s *Server) agentAddSessionTodos(w http.ResponseWriter, r *http.Request, conversation string) {
	var body struct {
		Todos []struct {
			Text string `json:"text"`
		} `json:"todos"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, conversation, raw)
	if !ok {
		return
	}
	release := func() { _ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k) }
	if s.broker == nil {
		release()
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved",
			"This daemon has no broker to say which Session owns that conversation; nothing was added.")
		return
	}
	if _, err := s.broker.LiveRootSession(r.Context(), conversation); err != nil {
		release()
		if ref, typed := err.(orchestrator.Refusal); typed {
			if ref.RawMessage {
				writeRawRefusal(w, ref.Status, ref.Code, ref.Message)
			} else {
				writeRefusal(w, ref.Status, ref.Code, ref.Message)
			}
			return
		}
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved",
			"Which Session owns that conversation could not be read; nothing was added.")
		return
	}
	texts := make([]string, 0, len(body.Todos))
	for _, td := range body.Todos {
		texts = append(texts, td.Text)
	}
	var answer []byte
	_, err := s.workV2().CreateSessionTodos(r.Context(), app.NewSessionTodosV2{SessionID: conversation, Texts: texts},
		func(rows []work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			wires := make([]directTodoV2Wire, 0, len(rows))
			for _, td := range rows {
				wires = append(wires, directTodoWire(td, nil))
			}
			answer, _ = json.Marshal(map[string]any{"ok": true, "todos": wires})
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
	if err != nil {
		release()
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

// agentCreateItem is POST /v1/work/v2/agent/items: a Session creating a Board
// item because its person told it to, in a message sent through Clawdline
// (work-system-v2 §2, amended 2026-09-25). The body names that message's run;
// the run must exist, be recent and have been said to this Session, the
// Session must be a live non-child one, and one run backs at most
// runItemLimit items. An executable item arrives unassigned with its steps,
// unless the body asks {"assign":{"mode":"self"}}: then it arrives assigned to
// the Session in the same transaction, and nothing is typed into the terminal,
// because the Session asked for it. Every refusal is typed and writes nothing,
// and a replay with the same key answers what was stored.
func (s *Server) agentCreateItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Via       struct {
			Run string `json:"run"`
		} `json:"via"`
		ProjectID          string                   `json:"project_id"`
		Kind               string                   `json:"kind"`
		Title              string                   `json:"title"`
		Description        string                   `json:"description"`
		AcceptanceCriteria string                   `json:"acceptance_criteria"`
		DeploymentPolicy   string                   `json:"deployment_policy"`
		Steps              []string                 `json:"steps"`
		Assign             *workV2EpicAssignRequest `json:"assign"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
	if !ok {
		return
	}
	refuse := s.relayRefuser(w, r, k)
	run, sess, err := s.relaySession(r.Context(), body.Via.Run, body.SessionID,
		"A Session creates a Board item only on a person's message sent through Clawdline; name its run as "+
			"{\"via\":{\"run\":\"…\"}}. Without one, file a proposal (POST /v1/work/v2/agent/proposals) for the person to accept.",
		"nothing was created")
	if err != nil {
		refuse(err)
		return
	}
	machineTriage := projects.IsMachineWorkspace(s.cfg.Dir, sess.CWD)
	if machineTriage {
		c := s.coordinator()
		c.Read = s.freshReading
		st, roleErr := c.State(r.Context())
		if roleErr != nil || !machineMayCreateItem(st, sess) {
			refuse(&app.WorkError{Status: http.StatusForbidden, Code: "coordinator_required",
				Message: "Only the live, registered Clawdfather may create a Project item from the machine workspace."})
			return
		}
	}
	// "self" is the creating Session taking the item it creates. It is its
	// own mode rather than existing_session with the caller's terminal: the
	// daemon already knows the caller, so a terminal id could only disagree
	// with it, and the assignment is written in the create's own transaction
	// instead of by a second, separately failing assign.
	assignSelf := body.Assign != nil && body.Assign.Mode == "self"
	switch {
	case assignSelf && machineTriage:
		refuse(&app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_assignment",
			Message: "Clawdfather delegates a Project item to a Project Session with " +
				"{\"mode\":\"existing_session\",…} or {\"mode\":\"new_session\",…}; it does not take one itself. Nothing was created."})
		return
	case assignSelf:
		if a := body.Assign; a.TerminalID != "" || a.Assistant != "" || a.Model != "" || a.Persona != "" {
			refuse(&app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_assignment",
				Message: "{\"mode\":\"self\"} is the Session making the request and takes no other field; nothing was created."})
			return
		}
	case body.Assign != nil:
		if !machineTriage {
			refuse(&app.WorkError{Status: http.StatusForbidden, Code: "machine_delegation_required",
				Message: "Only Clawdfather may create and delegate a Project item in one request; " +
					"a Session takes the item it creates with {\"assign\":{\"mode\":\"self\"}}."})
			return
		}
		if err := body.Assign.check(); err != nil {
			refuse(err)
			return
		}
	}
	catalog := s.workV2Projects(r.Context())
	project, ok := catalog[body.ProjectID]
	if !ok {
		refuse(&app.WorkError{Status: http.StatusUnprocessableEntity, Code: "project_not_found",
			Message: "Choose a Project from the current Project catalog."})
		return
	}
	sessionProject, _ := projects.CanonicalProjectKey(sess.CWD)
	var answer []byte
	created, err := s.workV2().CreateFromSession(r.Context(), app.NewSessionItemV2{Run: *run, SessionID: sess.ConversationID,
		TerminalID: sess.ID, Assistant: string(sess.Assistant), SessionProject: sessionProject,
		MachineTriage: machineTriage, AssignSelf: assignSelf,
		ProjectID: project.ID, ProjectPath: project.Path, Kind: work.Kind(body.Kind), Title: body.Title,
		Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria,
		DeploymentPolicy: work.DeploymentPolicy(body.DeploymentPolicy), Steps: body.Steps},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			if machineTriage {
				state := "not_requested"
				if body.Assign != nil {
					state = "pending"
				}
				answer, _ = json.Marshal(map[string]any{"ok": true, "item_created": true,
					"assigned": false, "assignment_state": state, "item": s.workV2ItemOf(catalog, v)})
				return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
			}
			item := s.workV2ItemOf(catalog, v)
			assigned := v.Item.OwnerSession != ""
			state := "not_requested"
			if assigned {
				state = "assigned"
			}
			answer, _ = json.Marshal(map[string]any{"ok": true, "assigned": assigned,
				"assignment_state": state, "item": item})
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
	if err != nil {
		refuse(err)
		return
	}
	if machineTriage {
		if body.Assign == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(answer)
			return
		}
		result := map[string]any{"ok": true, "item_created": true, "assigned": false,
			"assignment_state": "not_requested"}
		view := created
		if a := body.Assign; a != nil {
			assignedView, assignErr := s.assignWorkV2By(r.Context(), created.Item.ID, run.Actor(), "", created.Item.Version,
				a.Mode, a.TerminalID, a.Assistant, a.Model, a.Persona, nil)
			if assignErr != nil {
				result["assignment_error"] = assignmentErrorWire(assignErr)
				result["assignment_state"] = "failed"
			} else {
				view = assignedView
				if assignedView.Item.OwnerSession != "" && assignedView.Item.Phase == work.PhaseAssigned {
					result["assigned"] = true
					result["assignment_state"] = "assigned"
				} else {
					result["assignment_state"] = "awaiting_user"
				}
			}
		}
		if fresh, readErr := s.workV2().Item(r.Context(), created.Item.ID); readErr == nil {
			view = fresh
		}
		result["item"] = s.workV2ItemOf(catalog, view)
		answer, _ = json.Marshal(result)
		replace := s.replaceMachineItemReceipt
		if replace == nil {
			replace = s.store.ReplaceReceipt
		}
		if err := replace(context.WithoutCancel(r.Context()), k,
			store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}); err != nil {
			writeRawRefusal(w, http.StatusInternalServerError, "receipt_update_failed",
				"Board item "+created.Item.ID+" was created, but its delegation receipt could not be updated. Retry the same key to recover the item ID, then inspect the Board before assigning again.")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

func machineMayCreateItem(st app.State, sess session.Session) bool {
	return st.Record != nil && st.Liveness == coordinator.Online &&
		st.Record.ConversationID == sess.ConversationID && st.Record.TerminalID == sess.ID
}

// relayRefuser answers a relay route's refusal after releasing its receipt,
// so the same key may be tried again once the cause is gone.
func (s *Server) relayRefuser(w http.ResponseWriter, r *http.Request, k store.ReceiptKey) func(error) {
	return func(err error) {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		if ref, typed := err.(orchestrator.Refusal); typed {
			if ref.RawMessage {
				writeRawRefusal(w, ref.Status, ref.Code, ref.Message)
			} else {
				writeRefusal(w, ref.Status, ref.Code, ref.Message)
			}
			return
		}
		s.writeWorkV2Error(w, err)
	}
}

// relaySession is the person's message a Session relays and the live,
// non-child Session relaying it: the run found and checked as a relay's run,
// the Session resolved through the broker. Whether the run was said to that
// Session is checked where the write happens (work.RelayTo). noRun is the
// refusal for a body that names no run; nothing ends each other refusal.
func (s *Server) relaySession(ctx context.Context, runID, sessionID, noRun, nothing string) (*work.Run, session.Session, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, session.Session{}, &app.WorkError{Status: http.StatusForbidden, Code: "run_unknown", Message: noRun}
	}
	run, err := s.relayRun(ctx, runID)
	if err != nil {
		return nil, session.Session{}, err
	}
	if s.broker == nil {
		return nil, session.Session{}, &app.WorkError{Status: http.StatusServiceUnavailable, Code: "session_unresolved",
			Message: "This daemon has no broker to say which Session owns that conversation; " + nothing + "."}
	}
	sess, err := s.broker.LiveRootSession(ctx, sessionID)
	if err != nil {
		if _, typed := err.(orchestrator.Refusal); !typed {
			err = &app.WorkError{Status: http.StatusServiceUnavailable, Code: "session_unresolved",
				Message: "Which Session owns that conversation could not be read; " + nothing + "."}
		}
		return nil, session.Session{}, err
	}
	return run, sess, nil
}

// agentClaimItem is POST /v1/work/v2/agent/items/<id>/claim: a Session taking
// a Board item because its person told it to, in a message sent through
// Clawdline. The item goes to the Session that message was said to — the body
// names no other Session or terminal — through the same Assign a person's
// "existing Session" choice uses, so it reads as the person's assignment
// would, plus the message it was claimed on. The run is checked as
// agentCreateItem checks it; a child, another Project, an item already held
// or finished, a planning kind and a spent run are refused by name, and
// nothing is written. Nothing is typed into the terminal: the Session asked.
func (s *Server) agentClaimItem(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		SessionID       string `json:"session_id"`
		Via             struct {
			Run string `json:"run"`
		} `json:"via"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
	if !ok {
		return
	}
	refuse := s.relayRefuser(w, r, k)
	run, sess, err := s.relaySession(r.Context(), body.Via.Run, body.SessionID,
		"A Session claims a Board item only on a person's message sent through Clawdline that tells it to; name "+
			"its run as {\"via\":{\"run\":\"…\"}}. Without one, leave the item for the person to assign.",
		"nothing was claimed")
	if err != nil {
		refuse(err)
		return
	}
	item, err := s.workV2().Item(r.Context(), id)
	if err != nil {
		refuse(err)
		return
	}
	if !sessionInProject(sess, item.Item) {
		refuse(&app.WorkError{Status: http.StatusConflict, Code: "project_mismatch",
			Message: "This Session is not working in that item's Project; nothing was claimed."})
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	_, err = s.workV2().ClaimFromSession(r.Context(), id, app.ClaimWorkV2{Run: *run, ExpectedVersion: app.AgentExpectedVersion(body.ExpectedVersion),
		SessionID: sess.ConversationID, TerminalID: sess.ID, Assistant: string(sess.Assistant)},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(itemOf(v))
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
	if err != nil {
		refuse(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// workV2EpicAssignRequest is how an Epic's owner hands a child item to a
// Session: an existing one by terminal id, or a new one it asks Clawdline to
// open — the same two choices a person has.
type workV2EpicAssignRequest struct {
	Mode       string `json:"mode"`
	TerminalID string `json:"terminal_id"`
	Assistant  string `json:"assistant"`
	Model      string `json:"model"`
	// Persona is the built-in persona a new Session opens as; none when empty.
	Persona string `json:"persona"`
}

// check refuses a malformed choice before anything is written.
func (a workV2EpicAssignRequest) check() error {
	switch {
	case a.Mode == "existing_session" && strings.TrimSpace(a.TerminalID) != "":
	case a.Mode == "new_session" && (a.Assistant == "" || a.Assistant == "codex" || a.Assistant == "claude"):
	default:
		return &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_assignment",
			Message: "assign is {\"mode\":\"existing_session\",\"terminal_id\":…} or " +
				"{\"mode\":\"new_session\",\"assistant\":\"claude\"|\"codex\",\"persona\":…optional}; nothing was created."}
	}
	return checkWorkV2Persona(a.Mode, a.Persona)
}

// agentCreateEpicChild is POST /v1/work/v2/agent/items/<epic id>/children:
// the owner Session of an Epic breaking it into a Feature or Issue item and,
// optionally, assigning that item to a Session (work-system-v2 §6.5). The
// person's assignment of the Epic is the authority, so no run is named; the
// owner, kind, plan-gate and per-Epic bounds are checked where the item is
// written. The item is created first, whole, and then assigned: when the
// assignment fails the item stays, unassigned, and the answer says so with
// the assignment's refusal code rather than pretending nothing happened.
// The receipt is filed only once both are known, so a replay answers the same.
func (s *Server) agentCreateEpicChild(w http.ResponseWriter, r *http.Request, epicID string) {
	var body struct {
		ExpectedVersion    int64                    `json:"expected_version"`
		SessionID          string                   `json:"session_id"`
		Kind               string                   `json:"kind"`
		Title              string                   `json:"title"`
		Description        string                   `json:"description"`
		AcceptanceCriteria string                   `json:"acceptance_criteria"`
		DeploymentPolicy   string                   `json:"deployment_policy"`
		Steps              []string                 `json:"steps"`
		Assign             *workV2EpicAssignRequest `json:"assign"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if !s.requireSquadWorkActor(w, r, body.SessionID) {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
	if !ok {
		return
	}
	refuse := func(err error) {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
	}
	if body.Assign != nil {
		if err := body.Assign.check(); err != nil {
			refuse(err)
			return
		}
	}
	created, err := s.workV2().CreateEpicChild(r.Context(), app.NewEpicChildV2{EpicID: epicID,
		ExpectedVersion: app.AgentExpectedVersion(body.ExpectedVersion), SessionID: body.SessionID, Kind: work.Kind(body.Kind), Title: body.Title,
		Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria,
		DeploymentPolicy: work.DeploymentPolicy(body.DeploymentPolicy), Steps: body.Steps})
	if err != nil {
		refuse(err)
		return
	}
	answer := map[string]any{"ok": true, "assigned": false}
	if a := body.Assign; a != nil {
		_, assignErr := s.assignWorkV2By(r.Context(), created.Item.ID, work.EpicOwnerActor(body.SessionID), body.SessionID,
			created.Item.Version, a.Mode, a.TerminalID, a.Assistant, a.Model, a.Persona, nil)
		if assignErr != nil {
			answer["assignment_error"] = assignmentErrorWire(assignErr)
		} else {
			answer["assigned"] = true
		}
	}
	view := created
	if fresh, err := s.workV2().Item(r.Context(), created.Item.ID); err == nil {
		view = fresh
	}
	answer["item"] = s.workV2ItemProjector(r.Context())(view)
	b, _ := json.Marshal(answer)
	if err := s.store.CompleteReceipt(context.WithoutCancel(r.Context()), k,
		store.ReceiptAnswer{Status: http.StatusCreated, Body: b}); err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(b)
}

// agentAssignEpicChild is POST /v1/work/v2/agent/items/<id>/assign: the
// owner Session of an item's parent Epic (re)assigning that open child to a
// Session, existing or new, through the same assignment a person's choice
// makes. The owner check runs inside the assignment's transaction; an item
// with no parent Epic is refused not_epic_child and stays the person's to
// assign — unless the body names the person's message ({"via":{"run":…}}),
// when it is agentAssignOnRun's.
func (s *Server) agentAssignEpicChild(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		SessionID       string `json:"session_id"`
		workV2EpicAssignRequest
		Via *struct {
			Run string `json:"run"`
		} `json:"via"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if !s.requireSquadWorkActor(w, r, body.SessionID) {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
	if !ok {
		return
	}
	refuse := s.relayRefuser(w, r, k)
	if err := body.check(); err != nil {
		refuse(err)
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	file := func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(itemOf(v))
		return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
	}
	var err error
	expected := app.AgentExpectedVersion(body.ExpectedVersion)
	if body.Via != nil {
		err = s.agentAssignOnRun(r.Context(), id, body.SessionID, body.Via.Run, expected,
			body.workV2EpicAssignRequest, file)
	} else {
		_, err = s.assignWorkV2By(r.Context(), id, work.EpicOwnerActor(body.SessionID), body.SessionID, expected,
			body.Mode, body.TerminalID, body.Assistant, body.Model, body.Persona, file)
	}
	if err != nil {
		refuse(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// agentAssignOnRun is a Session handing an ordinary unassigned Feature or
// Issue to a new Session, opened with the persona it names, because the
// person's message through Clawdline asked it to (app.RunAssignment). The
// run is checked as a claim's is, and the Session must be live, not a child,
// and working in the item's Project; the kind, the target, the item's holder
// and the message's budget are checked inside the assignment's transaction.
// Every refusal writes nothing.
func (s *Server) agentAssignOnRun(ctx context.Context, id, sessionID, runID string, expected int64,
	a workV2EpicAssignRequest, file app.WorkV2Filer) error {
	if a.Mode != "new_session" {
		return &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "new_session_only",
			Message: "On the person's message a Session assigns an item only to a new Session; to take it itself, " +
				"claim it. Nothing was assigned."}
	}
	run, sess, err := s.relaySession(ctx, runID, sessionID,
		"A Session assigns an item that is not its Epic's child only on a person's message sent through Clawdline "+
			"that asks for it; name its run as {\"via\":{\"run\":\"…\"}}. Without one, leave the item for the person to assign.",
		"nothing was assigned")
	if err != nil {
		return err
	}
	item, err := s.workV2().Item(ctx, id)
	if err != nil {
		return err
	}
	if !sessionInProject(sess, item.Item) {
		return &app.WorkError{Status: http.StatusConflict, Code: "project_mismatch",
			Message: "This Session is not working in that item's Project; nothing was assigned."}
	}
	claim, err := app.RunAssignment(*run, sess.ConversationID, a.Persona)
	if err != nil {
		return err
	}
	_, err = s.assignWorkV2Via(ctx, id, run.Actor(), "", expected, a.Mode, "", a.Assistant, a.Model, a.Persona, claim, file)
	return err
}

// sessionInProject is whether a Session works in an item's Project, the check
// a person's "existing Session" assignment and a Session's claim both make.
func sessionInProject(sess session.Session, item work.ItemV2) bool {
	canonical, ok := projects.CanonicalProjectKey(sess.CWD)
	return ok && canonical == item.ProjectPath
}
