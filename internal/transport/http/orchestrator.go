package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
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
	case action == "settle" && r.Method == http.MethodPost:
		s.settleRoute(w, r)
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
			item.Landing = brokerLanding(record.Landing)
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
	sent, err := s.broker.Relay(r.Context(), orchestrator.Message{From: body.From, To: body.To, Text: body.Text}, key)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	if sent.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, contract.BrokerMessageResult{OK: true, AcceptedAt: sent.At.Unix(), At: sent.At.Unix()})
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
		row.Landing = brokerLanding(r.Landing)
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

func brokerWorktree(r orchestrator.Record) *contract.BrokerWorktree {
	return &contract.BrokerWorktree{
		Repository: r.Worktree.Repository,
		Path:       r.Worktree.Path,
		Branch:     r.Worktree.Branch,
		Base:       r.Worktree.Base,
		Head:       r.Worktree.Head,
	}
}

func brokerLanding(l *orchestrator.Landing) *contract.BrokerLanding {
	out := &contract.BrokerLanding{
		State:  contract.BrokerLandingState(l.State),
		Target: l.Target,
		Commit: l.Commit,
		Repo:   l.Repo,
		Note:   l.Note,
	}
	if !l.At.IsZero() {
		out.At = l.At.Unix()
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
