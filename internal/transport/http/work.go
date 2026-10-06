package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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

// Shared work route helpers and the participation board keeper. The v1 board,
// Backlog and item HTTP routes were retired; the v2 dispatcher and the
// proposals, decisions and digests routes still use these helpers.

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

// StartWork remains as the daemon wiring boundary. Work-system v2 is advanced
// explicitly by its owning Agent and by broker evidence at that command; the
// retired v1 sweep must not manufacture digests or proposals after cutover.
func (s *Server) StartWork(ctx context.Context) {
	_ = ctx
}

// V2 has no background sweep whose pulse can stall.
func (s *Server) workHealth(h *contract.Health) {
	_ = h
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

// ——— Routes ———

// workRoute dispatches the v2 board. The old v1 board, Backlog and item
// paths have no handler; participation routes are registered separately.
func (s *Server) workRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/work/")
	if strings.HasPrefix(rest, "v2/") {
		s.workV2Route(w, r)
		return
	}
	writeNoSuchRoute(w, r)
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

// workViaWire is a session relaying what a person said to it (U4).
type workViaWire struct {
	Run string `json:"run"`
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
