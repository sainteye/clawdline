package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The board and the Backlog (design-decisions T3, D31, D34): the new board's
// own resource, beside the old cards' `/v1/board`, which stays read-only for
// the 787 cards it shows (D32) and gets nothing new (D34).
//
//	GET  /v1/work/board?project=&section=&cursor=   the board, one section's page
//	GET  /v1/work/backlog?project=&cursor=          the planned Backlog, in its order
//	POST /v1/work/items                             a person's new item
//	GET  /v1/work/items/{id}                        one item, its tasks, what they make it
//	POST /v1/work/items/{id}                        a person's command on it
//	GET  /v1/work/items/{id}/moves?cursor=          every change it went through
//
// Pages are a fixed size, with a cursor; there is no size parameter, no byte
// budget and no prefix selector (D34). Every POST carries an Idempotency-Key
// and is answered once: the change and its receipt are one transaction (D03).
//
// Who may write, and who the move says it was:
//
//   - a person — this Mac's own browser, or a paired device that may send — is
//     `user`, with the device in the evidence;
//   - the orchestrator credential is a session, and a session never puts
//     anything on a person's board of its own accord (#1). It may relay what a
//     person said to it (design-decisions U4): the request then names the run
//     that carried the person's message — one this daemon issued when the
//     person sent it (runs.go), and refused by name when it did not — and the
//     move records `user_via_session:<run>` with `principal: machine` and the
//     run's session and time beside it, so whose word it was, where it was
//     said and which credential carried it are all on the record.

// workScope is the receipt scope of these routes.
const workScope = "work"

// workReceiptWait is how long a request whose twin is being answered waits
// for that answer: a command is one short transaction.
const workReceiptWait = 10 * time.Second

// workBodyLimit is one command's body.
const workBodyLimit = 16 << 10

var workByServer sync.Map // *Server -> *app.WorkBoard

// work is this server's board keeper.
func (s *Server) work() *app.WorkBoard {
	if w, ok := workByServer.Load(s); ok {
		return w.(*app.WorkBoard)
	}
	w := app.NewWorkBoard(s.store)
	w.OpenLimit = CapacityLimit(capacity.WorkOpen)
	// The participation points ride the same clock (proposals.go, T4).
	w.Also = s.participationSweep
	got, _ := workByServer.LoadOrStore(s, w)
	return got.(*app.WorkBoard)
}

// StartWork runs the board's sweep: a pass at once, then one per tick
// (CLAWDLINE_NEXT_WORK_TICK, 15 seconds by default). It is started by the
// daemon, because a landing nobody reads the board for has still landed.
func (s *Server) StartWork(ctx context.Context) {
	tick := 15 * time.Second
	if v := os.Getenv("CLAWDLINE_NEXT_WORK_TICK"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			tick = d
		}
	}
	// The board's settings move out of their old document here, once (D37),
	// rather than on the first read of them.
	if err := s.carryBoardSettings(ctx); err != nil {
		log.Printf("board settings: the old document was not carried over: %v", err)
	}
	go s.work().Run(ctx, tick)
	log.Printf("board sweep ticking every %s", tick)
	// A session standing on a question is watched on the same clock. It needs
	// the broker: its push is one of the broker's outbox effects.
	if s.broker != nil {
		go s.watchWaiting(ctx, tick)
	}
}

// workHealth turns /v1/health red when the sweep has stopped (DG-1): the
// board is then not following its facts, and a landing would not close it.
func (s *Server) workHealth(h *contract.Health) {
	if h.OK && s.work().Stalled() {
		h.OK = false
		h.Reason = contract.HealthReasonWorkSweepStalled
	}
}

// ——— Wire ———

type workTaskWire struct {
	TaskID     string  `json:"task_id"`
	Title      string  `json:"title"`
	State      string  `json:"state"`
	Landing    *string `json:"landing"`
	Outcome    string  `json:"outcome"`
	Owner      *string `json:"owner_session"`
	CreatedAt  int64   `json:"created_at"`
	FinishedAt *int64  `json:"finished_at"`
	LandedAt   *int64  `json:"landed_at"`
}

type workDerivedWire struct {
	State          string          `json:"state"`
	Reason         string          `json:"reason"`
	Landed         bool            `json:"landed"`
	Tasks          work.TaskCounts `json:"tasks"`
	LastEvidenceAt *int64          `json:"last_evidence_at"`
	StallAt        *int64          `json:"stall_at"`
	ClosureDueAt   *int64          `json:"closure_due_at"`
}

type workItemWire struct {
	ID           string          `json:"id"`
	Project      string          `json:"project"`
	Title        string          `json:"title"`
	Acceptance   *string         `json:"acceptance"`
	CreatedAt    int64           `json:"created_at"`
	CreatedBy    string          `json:"created_by"`
	Place        string          `json:"place"`
	State        *string         `json:"state"`
	Owner        *string         `json:"owner"`
	Commitment   *string         `json:"commitment"`
	StartOn      *string         `json:"start_on"`
	Rank         *int64          `json:"rank"`
	ClosedReason *string         `json:"closed_reason"`
	ClosedAt     *int64          `json:"closed_at"`
	PlacedAt     int64           `json:"placed_at"`
	Version      int64           `json:"version"`
	Section      *string         `json:"section"`
	Derived      workDerivedWire `json:"derived"`
	UnknownTasks int             `json:"unknown_tasks"`
	Tasks        []workTaskWire  `json:"tasks,omitempty"`
}

func optionalInt(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func workItemOf(v app.WorkView, withTasks bool) workItemWire {
	it, d := v.Item, v.Derived
	out := workItemWire{
		ID: it.ID, Project: it.Project, Title: it.Title, Acceptance: optionalString(it.Acceptance),
		CreatedAt: it.CreatedAt.Unix(), CreatedBy: it.CreatedBy, Place: string(it.Place),
		State: optionalString(string(it.State)), Owner: optionalString(it.Owner),
		Commitment: optionalString(string(it.Commitment)), StartOn: optionalString(it.StartOn),
		Rank: optionalInt(it.Rank), ClosedReason: optionalString(it.ClosedReason), ClosedAt: optionalUnix(it.ClosedAt),
		PlacedAt: it.PlacedAt.Unix(), Version: it.Version, Section: optionalString(string(v.Section)),
		UnknownTasks: v.Unknown,
		Derived: workDerivedWire{State: string(d.State), Reason: d.Reason, Landed: d.Landed, Tasks: d.Tasks,
			LastEvidenceAt: optionalUnix(d.LastEvidenceAt), StallAt: optionalUnix(d.StallAt),
			ClosureDueAt: optionalUnix(d.ClosureDueAt)},
	}
	if withTasks {
		out.Tasks = []workTaskWire{}
		for _, t := range v.Tasks {
			out.Tasks = append(out.Tasks, workTaskWire{TaskID: t.Task, Title: t.Title, State: t.State,
				Landing: optionalString(t.Landing), Outcome: string(work.OutcomeOf(t)), Owner: optionalString(t.Owner),
				CreatedAt: t.CreatedAt.Unix(), FinishedAt: optionalUnix(t.FinishedAt), LandedAt: optionalUnix(t.LandedAt)})
		}
	}
	return out
}

type workSweepWire struct {
	At          *int64 `json:"at"`
	TickSeconds int64  `json:"tick_seconds"`
	Passes      int64  `json:"passes"`
	Considered  int    `json:"considered"`
	Moved       int    `json:"moved"`
	Unknown     int    `json:"unknown"`
	Error       string `json:"error,omitempty"`
	Running     bool   `json:"running"`
	Stalled     bool   `json:"stalled"`
}

func (s *Server) workSweep() workSweepWire {
	p, tick, running := s.work().Pulse()
	return workSweepWire{At: optionalUnix(p.At), TickSeconds: int64(tick / time.Second), Passes: p.Passes,
		Considered: p.Considered, Moved: p.Moved, Unknown: p.Unknown, Error: p.Err, Running: running,
		Stalled: s.work().Stalled()}
}

type workBoardWire struct {
	OK         bool           `json:"ok"`
	Project    *string        `json:"project"`
	Section    *string        `json:"section"`
	Counts     map[string]int `json:"counts"`
	Rows       []workItemWire `json:"rows"`
	NextCursor *string        `json:"next_cursor"`
	PageSize   int            `json:"page_size"`
	Truncated  bool           `json:"truncated"`
	Sweep      workSweepWire  `json:"sweep"`
}

type workBacklogWire struct {
	OK         bool           `json:"ok"`
	Project    *string        `json:"project"`
	Counts     map[string]int `json:"counts"`
	Rows       []workItemWire `json:"rows"`
	NextCursor *string        `json:"next_cursor"`
	PageSize   int            `json:"page_size"`
}

// workMoveWire is one move, with its time in Unix seconds like every other
// time on these routes.
type workMoveWire struct {
	Seq      int64           `json:"seq"`
	WorkID   string          `json:"work_id"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	State    string          `json:"state"`
	Trigger  string          `json:"trigger"`
	Actor    string          `json:"actor"`
	Evidence json.RawMessage `json:"evidence"`
	At       int64           `json:"at"`
}

type workMovesWire struct {
	OK         bool           `json:"ok"`
	WorkID     string         `json:"work_id"`
	Moves      []workMoveWire `json:"moves"`
	NextCursor *string        `json:"next_cursor"`
	PageSize   int            `json:"page_size"`
}

type workOneWire struct {
	OK   bool         `json:"ok"`
	Item workItemWire `json:"item"`
}

// ——— Routes ———

// workRoute is everything under /v1/work/.
func (s *Server) workRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/work/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "board":
		s.workBoard(w, r)
	case rest == "backlog":
		s.workBacklog(w, r)
	case rest == "items":
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A new item is made with POST.")
			return
		}
		s.workCreate(w, r)
	case len(parts) == 2 && parts[0] == "items" && workID(parts[1]):
		switch r.Method {
		case http.MethodGet:
			s.workItem(w, r, parts[1])
		case http.MethodPost:
			s.workCommand(w, r, parts[1])
		default:
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "An item is read with GET and commanded with POST.")
		}
	case len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "moves":
		s.workMoves(w, r, parts[1])
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such board route.")
	}
}

// workID is the shape of a work id: a lowercase UUID, as a dispatch's work_id
// is checked (D36).
func workID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}

// workQuery reads a read route's query, refusing a field it does not know
// (D34: no selector grammar beyond these).
func workQuery(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]string, bool) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "This is read with GET.")
		return nil, false
	}
	known := map[string]bool{}
	for _, k := range allowed {
		known[k] = true
	}
	out := map[string]string{}
	for key, v := range r.URL.Query() {
		if !known[key] || len(v) != 1 || v[0] == "" || len(v[0]) > 1024 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Unknown or repeated query field "+key+".")
			return nil, false
		}
		out[key] = v[0]
	}
	return out, true
}

func writeWorkError(w http.ResponseWriter, err error) {
	var we *app.WorkError
	if errors.As(err, &we) {
		if we.Current != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(we.Status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": we.Code, "detail": we.Message,
				"current": workItemOf(*we.Current, false)})
			return
		}
		if we.Status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		writeRefusal(w, we.Status, we.Code, we.Message)
		return
	}
	writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The board could not be read or written.")
}

func (s *Server) workBoard(w http.ResponseWriter, r *http.Request) {
	q, ok := workQuery(w, r, "project", "section", "cursor")
	if !ok {
		return
	}
	section := work.Section(q["section"])
	if section != "" && sectionUnknown(section) {
		writeRefusal(w, http.StatusBadRequest, "invalid_section", "A section is decide, active, scheduled or done.")
		return
	}
	page, err := s.work().Board(r.Context(), q["project"], section, q["cursor"])
	if err != nil {
		writeWorkError(w, err)
		return
	}
	out := workBoardWire{OK: true, Project: optionalString(q["project"]), Section: optionalString(q["section"]),
		Counts: map[string]int{}, Rows: []workItemWire{}, PageSize: app.WorkPageSize, Truncated: page.Truncated,
		Sweep: s.workSweep()}
	for _, sec := range work.Sections {
		out.Counts[string(sec)] = page.Counts[sec]
	}
	for _, v := range page.Rows {
		out.Rows = append(out.Rows, workItemOf(v, false))
	}
	out.NextCursor = optionalString(page.Next)
	writeJSON(w, out)
}

func sectionUnknown(s work.Section) bool {
	for _, x := range work.Sections {
		if x == s {
			return false
		}
	}
	return true
}

func (s *Server) workBacklog(w http.ResponseWriter, r *http.Request) {
	q, ok := workQuery(w, r, "project", "cursor")
	if !ok {
		return
	}
	page, err := s.work().Backlog(r.Context(), q["project"], q["cursor"])
	if err != nil {
		writeWorkError(w, err)
		return
	}
	out := workBacklogWire{OK: true, Project: optionalString(q["project"]),
		Counts: map[string]int{"planned": page.Counts[work.ItemPlanned], "dropped": page.Counts[work.ItemDropped]},
		Rows:   []workItemWire{}, PageSize: app.WorkPageSize, NextCursor: optionalString(page.Next)}
	for _, v := range page.Rows {
		out.Rows = append(out.Rows, workItemOf(v, false))
	}
	writeJSON(w, out)
}

func (s *Server) workItem(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := workQuery(w, r); !ok {
		return
	}
	v, err := s.work().Item(r.Context(), id)
	if err != nil {
		writeWorkError(w, err)
		return
	}
	writeJSON(w, workOneWire{OK: true, Item: workItemOf(v, true)})
}

func (s *Server) workMoves(w http.ResponseWriter, r *http.Request, id string) {
	q, ok := workQuery(w, r, "cursor")
	if !ok {
		return
	}
	var after int64
	if c := q["cursor"]; c != "" {
		n, err := strconv.ParseInt(c, 10, 64)
		if err != nil || n < 0 {
			writeRefusal(w, http.StatusBadRequest, "bad_cursor", "cursor is not one this route wrote.")
			return
		}
		after = n
	}
	moves, next, err := s.work().Moves(r.Context(), id, after)
	if err != nil {
		writeWorkError(w, err)
		return
	}
	out := workMovesWire{OK: true, WorkID: id, Moves: make([]workMoveWire, 0, len(moves)), PageSize: app.WorkPageSize}
	for _, m := range moves {
		out.Moves = append(out.Moves, workMoveWire{Seq: m.Seq, WorkID: m.WorkID, From: string(m.From), To: string(m.To),
			State: string(m.State), Trigger: m.Trigger, Actor: m.Actor, Evidence: m.Evidence, At: m.At.Unix()})
	}
	if next > 0 {
		c := strconv.FormatInt(next, 10)
		out.NextCursor = &c
	}
	writeJSON(w, out)
}

// ——— Writes ———

// workViaWire is a session relaying what a person said to it (U4).
type workViaWire struct {
	Run string `json:"run"`
}

type workCreateWire struct {
	Title      string       `json:"title"`
	Project    string       `json:"project"`
	Acceptance string       `json:"acceptance"`
	Place      string       `json:"place"`
	Owner      string       `json:"owner"`
	StartOn    string       `json:"start_on"`
	Rank       int64        `json:"rank"`
	Via        *workViaWire `json:"via"`
}

type workCommandWire struct {
	Op      string `json:"op"`
	Owner   string `json:"owner"`
	StartOn string `json:"start_on"`
	Rank    *int64 `json:"rank"`
	// Reason is what a person says about work whose delivery named no item
	// (`done_elsewhere`): what was done and where it landed. It is refused
	// empty rather than recorded empty, so the move is a record.
	Reason          string       `json:"reason"`
	ExpectedVersion *int64       `json:"expected_version"`
	Via             *workViaWire `json:"via"`
}

// workActor is who a write says it was, and which credential carried it;
// for a session's relay, the id of the run it named. ok false means the
// request was answered with a refusal.
//
// The run itself is found and checked by relayRun inside the write, after
// the request's receipt is claimed: a retry of a relay that was answered is
// the stored answer, whatever has happened to the run since.
func workActor(w http.ResponseWriter, r *http.Request, via *workViaWire) (actor, principal, relay string, ok bool) {
	a := accessOf(r)
	if a.machine {
		if via == nil || strings.TrimSpace(via.Run) == "" {
			writeRefusal(w, http.StatusForbidden, "session_cannot_decide",
				"A session does not put anything on a person's board, or change it, of its own accord (#1). "+
					"To relay what a person said in the session, name the run that carried their message: {\"via\":{\"run\":\"…\"}}; "+
					"read it with GET /v1/orchestrator/sessions/<conversation id>/run.")
			return "", "", "", false
		}
		relay = strings.TrimSpace(via.Run)
		return work.ActorViaSession + relay, "machine", relay, true
	}
	if via != nil {
		writeRefusal(w, http.StatusBadRequest, "via_is_for_sessions", "via is how a session relays a person's words; a person writes as themselves.")
		return "", "", "", false
	}
	if !a.verdict.Allowed {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a paired device.")
		return "", "", "", false
	}
	return "user", personPrincipal(r), "", true
}

// readWorkBody reads one command body, refusing an unknown field by name: a
// field this route ignored would be a decision the person believed was
// taken (and "state": "landed" is the one D31 exists to refuse).
func readWorkBody(w http.ResponseWriter, r *http.Request, into any) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, workBodyLimit+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The body could not be read.")
		return nil, false
	}
	if len(raw) > workBodyLimit {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "A board command is at most 16 KiB.")
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		msg := "The body is not a board command."
		if strings.Contains(err.Error(), "unknown field") {
			msg = "The body has a field a board command does not take (" + strings.TrimPrefix(err.Error(), "json: ") +
				"). An item's state is decided by its facts and by the commands, never set."
		}
		writeRefusal(w, http.StatusBadRequest, "invalid_command", msg)
		return nil, false
	}
	return raw, true
}

// workKey reads the request's Idempotency-Key.
func workKey(w http.ResponseWriter, r *http.Request, principal string) (store.ReceiptKey, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		writeRefusal(w, http.StatusBadRequest, "idempotency_key_required",
			"Every board write carries an Idempotency-Key header (at most 200 characters), so a retry is answered, not repeated.")
		return store.ReceiptKey{}, false
	}
	return store.ReceiptKey{Scope: workScope, Actor: principal, Key: key}, true
}

// claimOnce asks the receipt table about a write, and answers every outcome
// but two itself: proceed is true when the request is new and this handler
// now holds its receipt, and replay is the stored answer of a request already
// answered, for the caller to send. Otherwise the request has been answered.
// mismatch is the code a key reused for a different request is refused with.
func (s *Server) claimOnce(w http.ResponseWriter, r *http.Request, k store.ReceiptKey, digest string,
	p store.ReceiptPolicy, mismatch string) (replay *store.ReceiptAnswer, proceed bool) {
	ctx := r.Context()
	deadline := time.Now().Add(workReceiptWait)
	for {
		claim, err := s.store.ClaimReceipt(ctx, k, digest, p, time.Now())
		if err != nil {
			w.Header().Set("Retry-After", "1")
			writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
				"The request's receipt could not be read; nothing was done.")
			return nil, false
		}
		switch claim.Outcome {
		case store.ReceiptNew:
			return nil, true
		case store.ReceiptReplay:
			return &claim.Answer, false
		case store.ReceiptMismatch:
			writeRefusal(w, http.StatusConflict, mismatch,
				"This key was already used for a different request; nothing was done. Use a new key.")
		case store.ReceiptExpired:
			writeRefusal(w, http.StatusConflict, "receipt_expired",
				"This key's window has passed. The request was answered once and is not carried out again.")
		case store.ReceiptFull:
			w.Header().Set("Retry-After", strconv.Itoa(int(claim.RetryAfter/time.Second)+1))
			writeRefusal(w, http.StatusTooManyRequests, "receipts_full",
				"This route holds as many answered requests as it keeps inside their window; nothing was done.")
		case store.ReceiptOrphaned:
			rec := &recorder{header: http.Header{}}
			writeRefusal(rec, http.StatusConflict, "request_outcome_unknown",
				"The daemon stopped while this request was being carried out, so whether it took effect is unknown. "+
					"It was not repeated; read what it changed before asking again under a new key.")
			_ = s.store.CompleteReceipt(context.WithoutCancel(ctx), k, store.ReceiptAnswer{Status: rec.status, Body: rec.body.Bytes()})
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body.Bytes())
		case store.ReceiptPending:
			if time.Now().After(deadline) {
				w.Header().Set("Retry-After", "1")
				writeRefusal(w, http.StatusConflict, "request_in_progress",
					"The same request is still being carried out; ask again for its answer.")
				return nil, false
			}
			select {
			case <-ctx.Done():
				return nil, false
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		return nil, false
	}
}

// workWrite runs one write under its receipt. The change files its own
// answer inside its transaction; a refusal gives the key back, because
// nothing was done and a retry should be decided afresh.
func (s *Server) workWrite(w http.ResponseWriter, r *http.Request, k store.ReceiptKey, raw []byte, status int,
	run func(app.Filer) (app.WorkView, error)) {
	replay, proceed := s.claimOnce(w, r, k, requestDigest([]byte(r.Method), []byte(routePath(r)), raw),
		store.ReceiptPolicy{}, "idempotency_key_reused")
	if replay != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(replay.Status)
		_, _ = w.Write(replay.Body)
		return
	}
	if !proceed {
		return
	}
	var answer []byte
	_, err := run(func(v app.WorkView) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer, _ = json.Marshal(workOneWire{OK: true, Item: workItemOf(v, false)})
		return k, store.ReceiptAnswer{Status: status, Body: answer}, true
	})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		writeWorkError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(answer)
}

func (s *Server) workCreate(w http.ResponseWriter, r *http.Request) {
	var body workCreateWire
	raw, ok := readWorkBody(w, r, &body)
	if !ok {
		return
	}
	actor, principal, relay, ok := workActor(w, r, body.Via)
	if !ok {
		return
	}
	k, ok := workKey(w, r, principal)
	if !ok {
		return
	}
	s.workWrite(w, r, k, raw, http.StatusCreated, func(file app.Filer) (app.WorkView, error) {
		run, err := s.relayRun(r.Context(), relay)
		if err != nil {
			return app.WorkView{}, err
		}
		return s.work().Create(r.Context(), app.NewWork{Title: body.Title, Project: body.Project,
			Acceptance: body.Acceptance, Place: work.Place(body.Place), Owner: body.Owner, StartOn: body.StartOn,
			Rank: body.Rank, Actor: actor, Principal: principal, Via: run}, file)
	})
}

func (s *Server) workCommand(w http.ResponseWriter, r *http.Request, id string) {
	var body workCommandWire
	raw, ok := readWorkBody(w, r, &body)
	if !ok {
		return
	}
	op, err := work.ParseOp(body.Op)
	var refused *work.Refusal
	if errors.As(err, &refused) {
		writeRefusal(w, refused.Status, refused.Code, refused.Message)
		return
	}
	actor, principal, relay, ok := workActor(w, r, body.Via)
	if !ok {
		return
	}
	k, ok := workKey(w, r, principal)
	if !ok {
		return
	}
	s.workWrite(w, r, k, raw, http.StatusOK, func(file app.Filer) (app.WorkView, error) {
		run, err := s.relayRun(r.Context(), relay)
		if err != nil {
			return app.WorkView{}, err
		}
		return s.work().Command(r.Context(), id, app.WorkCommand{
			Command: work.Command{Op: op, Actor: actor, Owner: body.Owner, StartOn: body.StartOn, Rank: body.Rank,
				Reason: body.Reason},
			Principal: principal, Via: run, ExpectedVersion: body.ExpectedVersion}, file)
	})
}
