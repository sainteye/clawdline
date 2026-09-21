package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// The broker's routes.
//
// Three credentials meet here and the split is the whole security model:
//
//   - the **orchestrator token** — this Mac's own 0600 file — opens dispatch,
//     the acknowledgement, a settled landing, a relayed message and whoami. A
//     paired phone never holds it; through a tunnel every request arrives from
//     127.0.0.1, so loopback proves nothing and is given no exemption.
//   - a **task secret** opens the four routes a child reports on. The gate
//     (gate.go, `taskSecretRoute`) lets those through without a device, and the
//     handler here is what actually checks them.
//   - nothing else opens anything.

// orchestratorRoute dispatches everything under /v1/orchestrator/tasks/.
//
// One handler rather than a route each, because the id is a path segment and
// Go's mux matches prefixes rather than patterns — and because the order the
// suffixes are tested in is a correctness property: `/completion/ack` has to be
// recognised before anything that treats the rest of the path as an id.
func (s *Server) orchestratorTaskRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/orchestrator/tasks/")
	if rest == "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "that is not a task action")
		return
	}
	id, action := rest, ""
	if i := strings.Index(rest, "/"); i >= 0 {
		id, action = rest[:i], rest[i+1:]
	}
	id = decodeSegment(id)
	if id == "" {
		writeBrokerRefusal(w, orchestrator.Refusal{
			Status: http.StatusBadRequest, Code: "bad_request", Message: "The route must name one task id."})
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.brokerTaskDetail(w, r, id)
	case action == "inflight" && r.Method == http.MethodGet:
		s.brokerInflightForTask(w, r, id)
	case action == "progress" && r.Method == http.MethodPost:
		s.brokerProgress(w, r, id)
	case action == "accepted" && r.Method == http.MethodPost:
		s.brokerAccepted(w, r, id)
	case action == "complete" && r.Method == http.MethodPost:
		s.brokerComplete(w, r, id)
	case action == "completion/ack" && r.Method == http.MethodPost:
		s.brokerAck(w, r, id)
	case action == "landing" && r.Method == http.MethodPost:
		s.brokerLanding(w, r, id)
	case action == "notify" && r.Method == http.MethodPost:
		s.brokerNotify(w, r, id)
	case action == "respawn" && r.Method == http.MethodPost:
		s.brokerRespawn(w, r, id)
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "that is not a task action")
	}
}

// taskSecret is the credential a child presents. Header only on the two routes
// where the Swift app accepts nothing else; the body is read by the caller for
// the two where it does.
func taskSecret(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Clawdline-Task-Secret"))
}

// brokerInventory answers what is already going on in a repository.
func (s *Server) brokerInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "the inventory is read with GET")
		return
	}
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = q.Get("project_dir")
	}
	inv, err := s.broker.ReadInventory(r.Context(), project, orchestrator.ParseClaims(q.Get("claims")))
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, orchestrator.InventoryPayload(inv))
}

// brokerDispatch accepts one task and starts it.
func (s *Server) brokerDispatch(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Dispatching needs the orchestrator token.")
		return
	}
	var body struct {
		TaskID     string           `json:"task_id"`
		Secret     string           `json:"secret"`
		Generation *json.RawMessage `json:"inventory_generation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "task_id and secret are required.")
		return
	}
	if body.TaskID == "" || body.Secret == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "task_id and secret are required.")
		return
	}
	req := orchestrator.DispatchRequest{TaskID: body.TaskID, Secret: body.Secret}
	if body.Generation != nil {
		// A non-string value fails the cast and is treated exactly as absent,
		// as in the Swift app: a caller that sent a number did not read the
		// inventory either.
		var generation string
		if json.Unmarshal(*body.Generation, &generation) == nil {
			req.Generation = generation
			req.Offered = true
		}
	}
	// Opening a session and typing into it is slower than a request should wait
	// on a shared deadline, so this one carries its own.
	// The request's own context dies when the caller hangs up, and a dispatch
	// that stopped halfway leaves a tab nobody recorded. This one outlives it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()

	out, err := s.broker.Dispatch(ctx, req)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerDispatchResult{
		OK:       true,
		Task:     s.brokerTaskRow(ctx, out.Record),
		Replayed: out.Replayed,
		Warnings: brokerWarnings(out.Warnings),
	})
}

func brokerWarnings(in []orchestrator.Warning) []contract.BrokerWarning {
	if len(in) == 0 {
		return nil
	}
	out := make([]contract.BrokerWarning, 0, len(in))
	for _, w := range in {
		out = append(out, contract.BrokerWarning{
			Code: w.Code, Message: w.Message, Task: w.Task,
			Paths: w.Paths, AgeSeconds: int64(w.Age), RootKey: w.RootKey,
		})
	}
	return out
}

func (s *Server) brokerTaskDetail(w http.ResponseWriter, r *http.Request, id string) {
	record, _, err := s.broker.Record(r.Context(), id)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerTaskEnvelope{Task: s.brokerTaskRow(r.Context(), record)})
}

func (s *Server) brokerProgress(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	record, err := s.broker.Progress(r.Context(), id, taskSecret(r), body.Note)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerProgressResult{OK: true, Task: s.brokerTaskRow(r.Context(), record)})
}

// brokerAccepted is the child signing for its briefing (D10). Header only: it
// is a new route, and the Swift app's body-secret fallback exists for routes
// older children already call that way.
func (s *Server) brokerAccepted(w http.ResponseWriter, r *http.Request, id string) {
	record, err := s.broker.Accept(r.Context(), id, taskSecret(r))
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerProgressResult{OK: true, Task: s.brokerTaskRow(r.Context(), record)})
}

// brokerComplete asks for the task's result.json to be collected now (D15).
// A body is read only for the secret an older child may put there; `status`
// and `summary` in it are ignored, because the file is the result.
func (s *Server) brokerComplete(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	secret := taskSecret(r)
	if secret == "" {
		secret = body.Secret
	}
	if err := s.broker.Complete(r.Context(), id, secret); err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerAccepted{OK: true})
}

func (s *Server) brokerAck(w http.ResponseWriter, r *http.Request, id string) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Acknowledging completion needs the orchestrator token.")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"The closed ACK schema requires only notice_id.")
		return
	}
	noticeID, ok := body["notice_id"].(string)
	if !ok {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"The closed ACK schema requires only notice_id.")
		return
	}
	changed, err := s.broker.Acknowledge(r.Context(), id, noticeID)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerAckResult{
		OK: true, Acknowledged: true, Changed: changed, NoticeID: strings.ToLower(noticeID),
	})
}

func (s *Server) brokerLanding(w http.ResponseWriter, r *http.Request, id string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"state must be pending, landed, abandoned, or nothing_to_land.")
		return
	}
	unknown := []string{}
	for key := range body {
		switch key {
		case "state", "target", "delivery", "commit", "note":
		default:
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"Unknown landing field(s): "+strings.Join(sortedStrings(unknown), ", ")+".")
		return
	}
	str := func(key string) string {
		v, _ := body[key].(string)
		return strings.TrimSpace(v)
	}
	record, err := s.broker.Land(r.Context(), id, orchestrator.LandingRequest{
		State:    str("state"),
		Target:   str("target"),
		Delivery: str("delivery"),
		Commit:   str("commit"),
		Note:     str("note"),
		Machine:  machineAuthed(r),
		Secret:   taskSecret(r),
	})
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerProgressResult{OK: true, Task: s.brokerTaskRow(r.Context(), record)})
}

func (s *Server) brokerNotify(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	secret := taskSecret(r)
	if secret == "" {
		secret = body.Secret
	}
	out, err := s.broker.AgentNotify(r.Context(), id, secret, body.Title, body.Body)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerNotifyResult{OK: true, Sent: int64(out.Sent), Failed: int64(out.Failed)})
}

// brokerRespawn retries a task whose tab never opened. Local credential only,
// like dispatch itself: this opens a session.
func (s *Server) brokerRespawn(w http.ResponseWriter, r *http.Request, id string) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Respawning a task needs the orchestrator token.")
		return
	}
	// The body is optional and has one key; anything unreadable is read as
	// "no secret supplied", as the Swift app reads it.
	var body struct {
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	// Opening a tab outlives the caller's patience, as a dispatch does.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	out, err := s.broker.Respawn(ctx, id, body.Secret)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerRespawnResult{
		OK:           true,
		Task:         s.brokerTaskRow(ctx, out.Record),
		Replayed:     out.Replayed,
		Warnings:     brokerWarnings(out.Warnings),
		Secret:       out.Secret,
		RespawnOf:    out.From,
		OriginalTask: out.Original,
	})
}

// brokerInflightForTask is the per-task form: the repository comes from the
// task, so a child cannot ask about one it was not sent to.
func (s *Server) brokerInflightForTask(w http.ResponseWriter, r *http.Request, id string) {
	repo, rows, err := s.broker.InflightFor(r.Context(), id, taskSecret(r))
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, s.inflightPayload(r, repo, rows))
}

// brokerInflight is the repository-wide form.
func (s *Server) brokerInflight(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = q.Get("project_dir")
	}
	inv, err := s.broker.ReadInventory(r.Context(), project, nil)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	rows, err := s.broker.Inflight(r.Context(), inv.Repository, "")
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, s.inflightPayload(r, inv.Repository, rows))
}

func (s *Server) inflightPayload(r *http.Request, repo string, rows []orchestrator.InventoryRow) contract.BrokerInflight {
	out := contract.BrokerInflight{
		Repository: repo,
		Inflight:   make([]contract.BrokerInflightRow, 0, len(rows)),
		At:         time.Now().Unix(),
	}
	for _, row := range rows {
		// A row the inventory could not decode is listed from what the
		// inventory has of it, never skipped: skipping it here is how it would
		// vanish from the one answer a child is told to read before it starts
		// (D05 ②).
		if row.Section == orchestrator.VisibilityUnreadable {
			out.Inflight = append(out.Inflight, contract.BrokerInflightRow{
				ID:         row.Task,
				State:      contract.TaskState(orchestrator.StateUnreadable),
				Visibility: string(row.Section),
				Assistant:  contract.Assistant(row.Assistant),
				ProjectDir: row.Project,
				Created:    row.Created.Unix(),
				AgeSeconds: int64(row.Age),
				Claims:     []string{},
			})
			continue
		}
		record, _, err := s.broker.Record(r.Context(), row.Task)
		if err != nil {
			// Read a moment ago and unreadable now: still not absent.
			out.Inflight = append(out.Inflight, contract.BrokerInflightRow{
				ID: row.Task, State: contract.TaskState(orchestrator.StateUnreadable),
				Visibility: string(orchestrator.VisibilityUnreadable), Assistant: contract.Assistant(row.Assistant),
				AgeSeconds: int64(row.Age), Claims: []string{},
			})
			continue
		}
		item := contract.BrokerInflightRow{
			ID:             row.Task,
			Title:          row.Title,
			State:          contract.TaskState(row.State),
			Visibility:     string(row.Section),
			Assistant:      contract.Assistant(row.Assistant),
			ProjectDir:     record.ProjectDir,
			Created:        record.CreatedAt.Unix(),
			AgeSeconds:     int64(row.Age),
			Claims:         row.Claims,
			ClaimsDeclared: record.Claims != nil,
			RootLabel:      row.RootLabel,
			RootKey:        row.RootKey,
			LeaseScope:     record.Scope(),
		}
		if declared, known := record.DeclaredWrites(); known {
			item.DeclaredWrites = declared
		}
		if record.Worktree != nil {
			item.Worktree = brokerWorktree(record)
		}
		if record.Landing != nil {
			item.Landing = brokerLanding(record.Landing, s.broker.Obligation(record))
		}
		out.Inflight = append(out.Inflight, item)
	}
	return out
}

// brokerMessages relays one session's message into another's composer.
func (s *Server) brokerMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a message is sent with POST")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Relaying a session message needs the orchestrator token.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
		return
	}
	var body struct {
		From string `json:"from_session"`
		To   string `json:"to_session"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"The closed body needs only from_session, to_session, 0…100000 characters of text and optional images.")
		return
	}
	// The key is a receipt (D03): the same key and body answers what the
	// first request answered, and types nothing a second time.
	//
	// The request's context is passed, and it decides only how long this
	// handler waits. The typing itself is an effect with a life of its own:
	// a browser closed mid-send leaves the message owed, typed and recorded,
	// so the resend gets the answer rather than `request_in_progress` for
	// ever (orchestrator/effects.go).
	sent, err := s.broker.Relay(r.Context(), orchestrator.Message{From: body.From, To: body.To, Text: body.Text}, key)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	if sent.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	// Two facts, two fields: when it was accepted, and when it was typed.
	// They were the same number twice, which is how "the bytes went in" and
	// "we owe this" stopped being told apart.
	accepted := sent.AcceptedAt
	if accepted.IsZero() {
		accepted = sent.At
	}
	writeJSON(w, contract.BrokerMessageResult{OK: true, AcceptedAt: accepted.Unix(), At: sent.At.Unix(),
		Stage: contract.DeliveryStage(sent.Stage)})
}

// brokerWhoAmI answers which tab a conversation is in.
func (s *Server) brokerWhoAmI(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Resolving session identity needs the orchestrator token.")
		return
	}
	q := r.URL.Query()
	if len(q) != 1 || len(q["conversation_id"]) != 1 {
		writeAuthRefusal(w, http.StatusBadRequest, "conversation_id_required",
			"The closed query needs exactly one conversation_id.")
		return
	}
	id, err := s.broker.WhoAmI(r.Context(), q.Get("conversation_id"))
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerWhoAmI{
		ConversationID: id.ConversationID,
		TerminalID:     id.TerminalID,
		Assistant:      contract.Assistant(id.Assistant),
		Provenance: contract.BrokerProvenance{
			Source:             "live_session_registry",
			RegistryComplete:   id.Complete,
			RegistryObservedAt: id.ObservedAt.Unix(),
			Consistency:        "single_snapshot_revalidated",
		},
		At: id.ObservedAt.Unix(),
	})
}

// brokerSessionRoute is POST /v1/orchestrator/sessions/<terminal>/complete.
func (s *Server) brokerSessionRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/orchestrator/sessions/")
	terminal, action := rest, ""
	if i := strings.Index(rest, "/"); i >= 0 {
		terminal, action = rest[:i], rest[i+1:]
	}
	// The to-do list is named by conversation id, not by terminal (todos.go).
	if action == "todos" && r.Method == http.MethodGet {
		s.sessionTodos(w, r, decodeSegment(terminal))
		return
	}
	// The run a session relays its person's newest message under (runs.go).
	if action == "run" {
		s.sessionRun(w, r, decodeSegment(terminal))
		return
	}
	// The retired per-message workflow step (workflow.go).
	if action == "workflow" && decodeSegment(terminal) != "" {
		s.brokerSessionWorkflow(w, r)
		return
	}
	if action != "complete" || r.Method != http.MethodPost {
		writeRefusal(w, http.StatusNotFound, "not_found", "that is not a session action")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Reporting root delivery needs the orchestrator token.")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"The body must contain only string summary.")
		return
	}
	summary, ok := body["summary"].(string)
	if !ok {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"The body must contain only string summary.")
		return
	}
	out, err := s.broker.ReportSessionDelivery(r.Context(), decodeSegment(terminal), summary)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerSessionDelivery{
		OK:      true,
		Created: out.Created,
		Disposition: contract.BrokerDisposition{
			Scope:     out.Scope,
			Title:     out.Summary,
			Evidence:  out.Evidence,
			ReceiptAt: out.At.Unix(),
		},
	})
}

// brokerAssistants is GET /v1/orchestrator/assistants: what this Mac can say
// about each assistant's account quota, the check a root runs before choosing
// whom to dispatch to. Read-level, as in the Swift app: a paired device reads
// it too. Files only — the status line's cache and Codex's rollouts — and the
// same five-second reading a session's `/info` shows.
//
// Answering runs no assistant and asks no provider anything: it reads files
// they were going to write anyway, when somebody asks. An `unknown` here
// means those files said nothing, and `unknown_reason` says which kind of
// nothing — an assistant that has never run on this machine has no record at
// all, and one dispatch, even a failed one, is what gives it a signal.
func (s *Server) brokerAssistants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "the assistants are read with GET")
		return
	}
	now := time.Now()
	out := contract.AssistantList{At: now.Unix(), Assistants: []contract.AssistantQuota{}}
	for _, a := range limits.Assistants {
		q := quotaReader().Quota(a, now)
		row := contract.AssistantQuota{
			ID:           contract.Assistant(a),
			Label:        assistantLabel(a),
			Installed:    q.Installed,
			Availability: contract.AssistantAvailability(q.Availability),
			// Every reading here is a file the provider wrote.
			Source:        contract.AssistantQuotaSourceObserved,
			ObservedAt:    q.ObservedAt,
			ResetsAt:      q.ResetsAt,
			Stale:         q.Stale,
			Detail:        q.Detail,
			Windows:       wireWindows(q.Windows),
			LastKnown:     contract.AssistantAvailability(q.LastKnown),
			UnknownReason: contract.AssistantUnknownReason(q.Reason),
		}
		if q.ObservedAt != nil {
			row.AgeSeconds = ageSeconds(*q.ObservedAt, now)
		}
		if q.FreshFor > 0 {
			fresh := q.FreshFor
			row.FreshForSeconds = &fresh
		}
		out.Assistants = append(out.Assistants, row)
	}
	writeJSON(w, out)
}

func assistantLabel(id string) string {
	if id == "claude" {
		return "Claude Code"
	}
	return "Codex"
}

// brokerLandings is GET /v1/orchestrator/landings: every pending landing, and
// who has to move each one. Read-level, as in the Swift app.
//
// The sessions are read once and projected once — the same rows /v1/sessions
// publishes — so a landing's owner is placed with the work state the session
// list shows for it, from the same moment.
func (s *Server) brokerLandings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "the landings are read with GET")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	inv := s.reading(ctx)
	snap := s.sessionsPayloadFrom(ctx, inv)
	rd := orchestrator.LandingReading{Processes: inv.Sources["ps"]}
	for _, row := range snap.Sessions {
		rd.Sessions = append(rd.Sessions, orchestrator.LandingSession{
			TerminalID:     row.ID,
			Assistant:      string(row.Assistant),
			ConversationID: row.SessionID,
			WorkState:      string(row.WorkState),
		})
		if row.SessionID == "" {
			rd.Anonymous = true
		}
	}
	rows, err := s.broker.PendingLandings(ctx, rd)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	now := time.Now()
	sessionsFresh := contract.SourceFreshnessCurrent
	if !inv.Complete {
		sessionsFresh = contract.SourceFreshnessStale
	}
	landingsFresh := landingsFreshness(rows)
	sources := contract.BrokerLandingSources{
		Sessions: contract.BearingsSource{ObservedAt: inv.ObservedAt.Unix(), Provenance: inv.Provenance, Freshness: sessionsFresh},
		Tasks:    contract.BearingsSource{ObservedAt: now.Unix(), Provenance: "broker", Freshness: contract.SourceFreshnessCurrent},
		Landings: contract.BearingsSource{ObservedAt: now.Unix(), Provenance: "broker", Freshness: landingsFresh},
	}
	out := contract.BrokerLandingList{Landings: []contract.BrokerPendingLanding{}, Sources: sources, At: now.Unix()}
	for _, p := range rows {
		rec := p.Record
		var rootAssistant *string
		var rootLabel *string
		if rec.Root != nil {
			rootAssistant = optionalString(rec.Root.Assistant)
			rootLabel = optionalString(rec.Root.Label)
		}
		age := int64(now.Sub(p.Since) / time.Second)
		if age < 0 {
			age = 0
		}
		out.Landings = append(out.Landings, contract.BrokerPendingLanding{
			ID:         rec.ID,
			Title:      rec.Title,
			RootKey:    optionalString(p.RootKey),
			RootLabel:  rootLabel,
			Paths:      p.Paths,
			Since:      p.Since.Unix(),
			AgeSeconds: age,
			Target:     optionalString(rec.Landing.Target),
			Settlement: contract.BrokerLandingSettlement(rec.Landing.Settlement),
			Note:       optionalString(rec.Landing.Note),
			Obligation: contract.BrokerLandingObligation(p.Obligation),
			Ownership: contract.BrokerLandingOwnership{
				Version:           1,
				Status:            contract.BrokerLandingOwnershipStatus(p.Ownership.Status),
				Subject:           p.Ownership.Subject,
				Reason:            p.Ownership.Reason,
				TaskID:            rec.ID,
				TaskState:         contract.TaskState(rec.State),
				RootKey:           optionalString(p.RootKey),
				RootAssistant:     rootAssistant,
				ObservedWorkState: optionalString(p.Ownership.WorkState),
				Evidence:          sources,
			},
		})
	}
	writeJSON(w, out)
}

// landingsFreshness is the ledger's own word, decided from the rows it is
// about to send.
//
// Reading every record and finding nothing wrong with the reading is what this
// route used to call `current` — while the rows themselves said that the one
// question a landing ledger exists to answer, *is this work on its target*,
// could not be put at all, because nothing on the record says what the target
// was. That is `unverified`: an answer, and one that may already be wrong.
//
// One such row is enough to qualify the whole block. A count where most of the
// rows can be checked and some cannot is still a count nobody should act on as
// arithmetic, and the alternative — a per-row word the reader has to add up
// themselves — is the shape that made this ledger unreadable in the first
// place.
func landingsFreshness(rows []orchestrator.PendingLanding) contract.SourceFreshness {
	for _, row := range rows {
		if orchestrator.Unverifiable(row.Record.Landing) {
			return contract.SourceFreshnessUnverified
		}
	}
	return contract.SourceFreshnessCurrent
}

// addressRowWire is a row of the address book as it is sent. It exists for
// the key the generator cannot express, as sessionRowWire does: a declared
// `work_person_needed: false` is an answer, and the generated bool would drop
// it.
type addressRowWire struct {
	contract.BrokerAddressRow
	WorkPersonNeeded *bool `json:"work_person_needed,omitempty"`
}

type addressBookWire struct {
	Sessions []addressRowWire `json:"sessions"`
	At       int64            `json:"at"`
}

// brokerAddressBook is GET /v1/orchestrator/sessions: which sessions a wait, a
// relay or a handoff can name, for the caller holding the orchestrator token.
// `GET /v1/sessions` is the paired device's, and this is its row with the
// screen taken out — the same projection, so the two lists cannot disagree
// about a session's state, only about how much of it they show.
func (s *Server) brokerAddressBook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "the sessions are read with GET")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Reading the sessions a wait can name needs the orchestrator token.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	snap := s.sessionsPayload(ctx)
	// The task each tab was opened for: a live task of this broker's naming
	// that terminal as its child's. Only a live one — a finished task's
	// terminal id can have been reused by a later, unrelated pane (tmux
	// numbers panes again after its server restarts), and a stale match would
	// name the wrong task rather than none. Records come newest first.
	opened := map[string]string{}
	if records, _, err := s.broker.Records(ctx); err == nil {
		for _, rec := range records {
			if rec.ChildTerminalID == "" || rec.State.Terminal() {
				continue
			}
			if _, seen := opened[rec.ChildTerminalID]; !seen {
				opened[rec.ChildTerminalID] = rec.ID
			}
		}
	}
	out := addressBookWire{Sessions: []addressRowWire{}, At: snap.At}
	for _, row := range snap.Sessions {
		book := contract.BrokerAddressRow{
			ID:             row.ID,
			Label:          row.Label,
			State:          row.State,
			WorkState:      row.WorkState,
			WorkProvenance: row.WorkProvenance,
			WorkNote:       row.WorkNote,
			WorkSince:      row.WorkSince,
			WorkMovedBy:    row.WorkMovedBy,
			Owed:           row.Owed,
			Closeability:   row.Closeability,
			Assistant:      row.Assistant,
			CWD:            row.CWD,
			TaskID:         opened[row.ID],
			RootAssignment: row.RootAssignment,
			Coordinator:    row.Coordinator,
		}
		if row.Disposition != nil {
			// A completion summary is prose; it stays on the paired-device row.
			d := *row.Disposition
			d.Title = ""
			book.Disposition = &d
		}
		out.Sessions = append(out.Sessions, addressRowWire{BrokerAddressRow: book, WorkPersonNeeded: row.WorkPersonNeeded})
	}
	writeJSON(w, out)
}

// brokerMachineNotify is POST /v1/orchestrator/notify: a root's own push to
// the person, on the orchestrator token (orchestrator.MachineNotify).
func (s *Server) brokerMachineNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a notification is sent with POST")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Agent notification needs the orchestrator token.")
		return
	}
	// Read as the Swift app reads it: a missing or unreadable field is empty,
	// and the empty title or body is what gets refused, by name.
	var body contract.BrokerMachineNotifyRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	out, err := s.broker.MachineNotify(r.Context(), body.Title, body.Body, body.SessionID)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerNotifyResult{OK: true, Sent: int64(out.Sent), Failed: int64(out.Failed)})
}

// brokerDurableReportPromotion is POST /v1/orchestrator/durable-reports/promotions,
// which this daemon does not keep. The Swift app's promotion copies a task
// report into an immutable store that the documents route and the Cloud
// document link then serve; half of that — keeping the bytes without the read
// path behind the link it returns — would hand out a link that opens nothing.
// So it is refused by name, with what to do instead, rather than answered by
// the fallback's generic "not implemented".
func (s *Server) brokerDurableReportPromotion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a promotion is sent with POST")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Promoting a durable report needs the orchestrator token.")
		return
	}
	writeAuthRefusal(w, http.StatusNotImplemented, "durable_report_promotion_unsupported",
		"This Clawdline does not keep durable reports, so nothing was promoted. Cite the task's own "+
			"artifact instead, and copy anything that must outlive the task directory into the "+
			"repository's docs/ first.")
}

// writeBrokerError sends a typed refusal in the Swift app's envelope, with the
// extras merged inside `error` where the Swift app puts them.
func writeBrokerError(w http.ResponseWriter, err error) {
	if ref, ok := err.(orchestrator.Refusal); ok {
		writeBrokerRefusal(w, ref)
		return
	}
	writeAuthRefusal(w, http.StatusInternalServerError, "internal", err.Error())
}

func writeBrokerRefusal(w http.ResponseWriter, ref orchestrator.Refusal) {
	body := map[string]any{
		"code":       ref.Code,
		"message":    ref.Message,
		"request_id": requestID(),
	}
	for k, v := range ref.Extra {
		// The envelope's own three keys are never overwritten by an extra: a
		// refusal that could rename its own code is a refusal a caller cannot
		// branch on.
		if k == "code" || k == "message" || k == "request_id" {
			continue
		}
		body[k] = v
	}
	status := ref.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"error": body})
}

// brokerTaskRow projects a record onto the wire.
func (s *Server) brokerTaskRow(ctx context.Context, r orchestrator.Record) contract.BrokerTask {
	// `claims` is the lease; the declared list travels as declared_writes
	// (D21), so an isolated task's list is never read as "writes nothing".
	claims := r.Lease()
	row := contract.BrokerTask{
		ID:             r.ID,
		State:          contract.TaskState(r.State),
		Kind:           r.Kind,
		Title:          r.Title,
		Assistant:      contract.Assistant(r.Assistant),
		ProjectDir:     r.ProjectDir,
		Repository:     r.Repository,
		DispatchBase:   r.DispatchBase,
		Created:        r.CreatedAt.Unix(),
		Dir:            r.Dir,
		Permission:     r.PermissionMode,
		Isolation:      r.Isolation,
		TimeoutMinutes: int64(r.TimeoutMinutes),
		Claims:         claims,
		ClaimsDeclared: r.Claims != nil,
		Deliverables:   r.Deliverables,
		SpawnError:     r.SpawnError,
		RespawnOf:      r.RespawnOf,
		LeaseScope:     r.Scope(),
		Verdict:        r.Verdict,
	}
	if r.AssistantQuota != nil {
		row.AssistantQuota = brokerAssistantQuotaDecision(r.AssistantQuota)
	}
	if declared, known := r.DeclaredWrites(); known {
		row.DeclaredWrites = declared
	}
	if !r.AcceptedAt.IsZero() {
		row.AcceptedAt = r.AcceptedAt.Unix()
	}
	if r.RespawnGeneration > 0 {
		row.RespawnGeneration = int64(r.RespawnGeneration)
	}
	// What the beat last saw of the child's session: live, from memory, and
	// never from the store (observe.go).
	if e, ok := s.broker.ExecutorOf(r.ID); ok && !r.State.Terminal() {
		row.Executor = brokerExecutor("", e)
	}
	if !r.SpawnedAt.IsZero() {
		row.SpawnedAt = r.SpawnedAt.Unix()
	}
	if !r.FinishedAt.IsZero() {
		row.FinishedAt = r.FinishedAt.Unix()
	}
	if r.ChildTerminalID != "" {
		row.Child = &contract.BrokerChild{
			TerminalID: r.ChildTerminalID,
			Backend:    contract.Backend(r.ChildBackend),
		}
	}
	// What this task's end does to its child's tab, so that "is this tab still
	// being open normal or not" is answered here rather than only inside the
	// child's own CHILD.md (linger.go).
	row.Tab = brokerTab(s.broker.TabPolicy(r))
	if r.Root != nil {
		row.Root = &contract.BrokerRoot{
			SessionID:  r.Root.SessionID,
			Assistant:  contract.Assistant(r.Root.Assistant),
			ProjectDir: r.Root.ProjectDir,
			Label:      r.Root.Label,
			PollOnly:   r.Root.PollOnly,
		}
	}
	if r.Worktree != nil {
		row.Worktree = brokerWorktree(r)
	}
	if r.Landing != nil {
		row.Landing = brokerLanding(r.Landing, s.broker.Obligation(r))
	}
	if r.Notice != nil {
		row.CompletionDelivery = brokerNotice(r.Notice)
	}
	if r.Result != nil {
		row.Result = &contract.BrokerResult{
			Status:     r.Result.Status,
			Summary:    r.Result.Summary,
			Symbols:    r.Result.Symbols,
			Artifacts:  r.Result.Artifacts,
			FinishedAt: r.Result.FinishedAt,
		}
		if v := r.Result.Verify; v != nil {
			row.Result.Verification = &contract.BrokerVerification{
				Runs: int64(v.Runs), Seconds: int64(v.Seconds), Last: v.Last, Scope: v.Scope,
			}
		}
		// What the delivery says it did not do, for the root reading this
		// task at the moment it integrates. They are candidates: raising one
		// is POST /v1/orchestrator/proposals with this task_id and the
		// leftover's title, and only a person's answer makes a row.
		for _, lo := range r.Result.Leftovers {
			row.Result.Leftovers = append(row.Result.Leftovers, contract.BrokerLeftover{
				Title: lo.Title, Why: lo.Why, SuggestedAcceptance: lo.Acceptance,
			})
		}
		row.Summary = r.Result.Summary
	} else if r.Verdict != "" {
		// What the console shows as a finished task's line. The broker's own
		// sentence is shown as that, under `verdict`, and never as a result.
		row.Summary = r.Verdict
	}
	if notes, err := s.broker.Notes(ctx, r.ID); err == nil && len(notes) > 0 {
		for _, n := range notes {
			row.Progress = append(row.Progress, contract.BrokerProgressNote{Note: n.Note, At: n.At.Unix()})
		}
	}
	return row
}

func brokerAssistantQuotaDecision(in *orchestrator.AssistantQuotaDecision) *contract.BrokerAssistantQuotaDecision {
	out := &contract.BrokerAssistantQuotaDecision{
		ReadAt: in.ReadAt.Unix(), Assistants: []contract.AssistantQuota{}, ReadError: in.ReadError,
	}
	for _, row := range in.Assistants {
		windows := make([]contract.SessionLimitWindow, 0, len(row.Windows))
		for _, window := range row.Windows {
			w := contract.SessionLimitWindow{Name: window.Name, Hit: window.Hit}
			if window.UsedPercent != nil {
				w.UsedPercent = *window.UsedPercent
			}
			if window.ResetsAt != nil {
				w.ResetsAt = *window.ResetsAt
			}
			windows = append(windows, w)
		}
		out.Assistants = append(out.Assistants, contract.AssistantQuota{
			ID: contract.Assistant(row.ID), Label: row.Label, Installed: row.Installed,
			Availability: contract.AssistantAvailability(row.Availability),
			Source:       contract.AssistantQuotaSourceObserved, ObservedAt: row.ObservedAt,
			AgeSeconds: row.AgeSeconds, Stale: row.Stale,
			FreshForSeconds: row.FreshForSeconds, ResetsAt: row.ResetsAt,
			Detail: row.Detail, Windows: windows,
			LastKnown:     contract.AssistantAvailability(row.LastKnown),
			UnknownReason: contract.AssistantUnknownReason(row.UnknownReason),
		})
	}
	return out
}

// brokerTab is one task's tab policy on the wire: what each way of ending does
// to the tab, and — once the task has ended — the rule that applied and when
// the close it asks for falls due.
//
// It is projected from orchestrator.TabPolicy, the same value CHILD.md's own
// section is rendered from. That is what keeps the two from drifting: there is
// one description of this policy, read twice, rather than two kept in step.
func brokerTab(p orchestrator.TabPolicy) *contract.BrokerTab {
	out := &contract.BrokerTab{Setting: p.Setting, Value: p.Value}
	for _, e := range p.Ends {
		out.Ends = append(out.Ends, brokerTabEnd(e))
	}
	// Absent while the task is still running: which rule applies is decided by
	// how it ends, and naming one before that would be a guess presented as an
	// answer.
	if p.Decided {
		applied := brokerTabEnd(p.Applied)
		out.Applied = &applied
	}
	if !p.CloseAt.IsZero() {
		out.CloseAt = p.CloseAt.Unix()
	}
	return out
}

func brokerTabEnd(e orchestrator.TabEnd) contract.BrokerTabEnd {
	return contract.BrokerTabEnd{
		End:          contract.TaskState(e.End),
		Rule:         contract.BrokerTabRule(e.Plan.Rule),
		Close:        e.Plan.Close,
		AfterSeconds: int64(e.Plan.After / time.Second),
		What:         orchestrator.TabPlanSentence(e.Plan),
	}
}

func brokerWorktree(r orchestrator.Record) *contract.BrokerWorktree {
	return &contract.BrokerWorktree{
		Repository: r.Worktree.Repository,
		Path:       r.Worktree.Path,
		Branch:     r.Worktree.Branch,
		Base:       r.Worktree.Base,
		Head:       r.Worktree.Head,
	}
}

// brokerLanding is the landing record as the wire carries it, with what a
// pending one means now beside it — derived for this answer, never stored.
func brokerLanding(l *orchestrator.Landing, obligation orchestrator.Obligation) *contract.BrokerLanding {
	out := &contract.BrokerLanding{
		State:        contract.BrokerLandingState(l.State),
		Target:       l.Target,
		Commit:       l.Commit,
		Repo:         l.Repo,
		Note:         l.Note,
		TargetCommit: l.TargetCommit,
		DeliveryHead: l.DeliveryHead,
		Base:         l.Base,
		Settlement:   contract.BrokerLandingSettlement(l.Settlement),
		Obligation:   contract.BrokerLandingObligation(obligation),
	}
	if !l.At.IsZero() {
		out.At = l.At.Unix()
	}
	if l.CorrectedFrom != nil {
		out.CorrectedFrom = brokerLanding(l.CorrectedFrom, "")
	}
	return out
}

func brokerNotice(n *orchestrator.Notice) *contract.BrokerNotice {
	out := &contract.BrokerNotice{
		NoticeID:  n.ID,
		State:     contract.BrokerNoticeState(n.State),
		Attempts:  int64(n.Attempts),
		CreatedAt: n.CreatedAt.Unix(),
		Recipient: n.Recipient,
	}
	stamp := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	out.LastAttemptAt = stamp(n.LastAttemptAt)
	out.NextRetryAt = stamp(n.NextRetryAt)
	out.TransportDeliveredAt = stamp(n.DeliveredAt)
	out.ObservedAt = stamp(n.ObservedAt)
	out.AcknowledgedAt = stamp(n.AcknowledgedAt)
	out.DeadLetterAt = stamp(n.DeadLetterAt)
	if n.LastError != nil {
		out.LastError = &contract.BrokerNoticeError{
			Code: n.LastError.Code, Message: n.LastError.Message, At: n.LastError.At.Unix(),
		}
	}
	return out
}

func sortedStrings(in []string) []string {
	out := append([]string{}, in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
