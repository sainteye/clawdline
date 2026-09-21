package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Where a person takes part (design-decisions T4; board-redesign §4, §8).
//
// A session's side, behind the orchestrator credential:
//
//	POST /v1/orchestrator/proposals                 propose a line of work; the answer says whether to ask
//	POST /v1/orchestrator/proposals {"task_id":…,"leftover":"<title>"}
//	                                                propose one thing a delivery said it did not do
//	GET  /v1/orchestrator/proposals/{id}            one proposal
//	POST /v1/orchestrator/proposals/{id}/asked      "I asked it in the conversation"
//	POST /v1/orchestrator/decisions                 ask a person something
//	GET  /v1/orchestrator/decisions/{id}            its answer, or its default
//
// A child may knock on the first with its own task secret; its proposal is
// recorded in its root's "to confirm" area and it asks nobody.
//
// A person's side, beside the board (a session relays a person's words here
// under their run, as on the board's own routes):
//
//	GET  /v1/work/proposals?state=&project=&cursor= the "to confirm" area (pending by default)
//	GET  /v1/work/proposals/{id}
//	POST /v1/work/proposals/{id}                    {"answer": "track"|"later"|"no"}
//	GET  /v1/work/decisions?state=&cursor=          waiting on you (open by default)
//	GET  /v1/work/decisions/{id}
//	POST /v1/work/decisions/{id}                    {"answer": "<option id>"}
//	GET  /v1/work/digests?kind=&cursor=             the daily and weekly digests
//
// Every POST carries an Idempotency-Key and is answered once: the change and
// its receipt are one transaction (D03).

// participationScope is the receipt scope of these routes.
const participationScope = "participation"

var participationByServer sync.Map // *Server -> *app.Participation

// participation is this server's participation keeper, on its board.
func (s *Server) participation() *app.Participation {
	if p, ok := participationByServer.Load(s); ok {
		return p.(*app.Participation)
	}
	p := app.NewParticipation(s.work())
	p.ProposalLimit = CapacityLimit(capacity.ProposalsOpen)
	p.DecisionLimit = CapacityLimit(capacity.DecisionsOpen)
	p.DigestLimit = CapacityLimit(capacity.WorkDigests)
	// A blocking decision goes the way every push from this daemon goes: the
	// subscription store with its register limit, the sender with its
	// bounded retries (push.go). Tapping it opens the list.
	p.Push = func(ctx context.Context, title, body, tag string) (int, int, error) {
		d, err := s.PushSend(ctx, title, body, "", tag, "")
		return d.Sent, d.Failed, err
	}
	// A proposal or an answer about a task puts it on the line it names, in
	// the broker's own transaction (orchestrator.BindWork).
	if s.broker != nil {
		p.Bind = s.broker.BindWork
	}
	got, _ := participationByServer.LoadOrStore(s, p)
	return got.(*app.Participation)
}

// heardFrom records that a person sent a session a message through this
// daemon: the one reading that says a person is there (board-redesign §4.3).
func (s *Server) heardFrom(sess session.Session) {
	s.participation().Heard.Mark(time.Now(), sess.ConversationID, sess.ID)
}

// participationSweep is the board sweep's second half (app.WorkBoard.Also).
func (s *Server) participationSweep(ctx context.Context) error {
	return s.participation().Sweep(ctx)
}

// participationRoutes adds these routes to the daemon's mux. The mux sends a
// path to its longest registered prefix, so the board's `/v1/work/` keeps
// everything else under it.
func (s *Server) participationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/orchestrator/proposals", s.sessionProposalsRoute)
	mux.HandleFunc("/v1/orchestrator/proposals/", s.sessionProposalsRoute)
	mux.HandleFunc("/v1/orchestrator/decisions", s.sessionDecisionsRoute)
	mux.HandleFunc("/v1/orchestrator/decisions/", s.sessionDecisionsRoute)
	mux.HandleFunc("/v1/work/proposals", s.workProposalsRoute)
	mux.HandleFunc("/v1/work/proposals/", s.workProposalsRoute)
	mux.HandleFunc("/v1/work/decisions", s.workDecisionsRoute)
	mux.HandleFunc("/v1/work/decisions/", s.workDecisionsRoute)
	mux.HandleFunc("/v1/work/digests", s.workDigestsRoute)
}

// proposalDiagnostics is /v1/diagnostics.proposals. A process with no store
// has no proposals to count, and says nothing rather than zero.
func (s *Server) proposalDiagnostics(ctx context.Context) *contract.ProposalDiagnostics {
	if s.store == nil {
		return nil
	}
	c, err := s.participation().Counts(ctx)
	if err != nil {
		return &contract.ProposalDiagnostics{Error: err.Error()}
	}
	out := &contract.ProposalDiagnostics{AskTrue: c.AskTrue, AskFalse: c.AskFalse, AskedInline: c.AskedInline,
		AskedInlineUnprompted: c.Unprompted, Pending: c.Pending, Matched: c.Matched}
	if !c.Oldest.IsZero() {
		out.OldestPendingAt = c.Oldest.Unix()
	}
	return out
}

// ——— Wire ———

type proposalWire struct {
	ID            string   `json:"id"`
	WorkID        string   `json:"work_id"`
	TaskID        *string  `json:"task_id"`
	SessionID     string   `json:"session_id"`
	Source        string   `json:"source"`
	Project       string   `json:"project"`
	Title         string   `json:"title"`
	Signals       []string `json:"signals"`
	Effects       []string `json:"effects"`
	Ask           bool     `json:"ask"`
	Reason        string   `json:"reason"`
	Channel       string   `json:"channel"`
	Question      *string  `json:"question"`
	State         string   `json:"state"`
	Answer        *string  `json:"answer"`
	AnsweredBy    *string  `json:"answered_by"`
	AnsweredAt    *int64   `json:"answered_at"`
	CreatedAt     int64    `json:"created_at"`
	ExpiresAt     int64    `json:"expires_at"`
	AskedInlineAt *int64   `json:"asked_inline_at"`
	Unprompted    bool     `json:"unprompted"`
	// WithdrawnReason and WithdrawnAt say which fact about the subject ended
	// the question, and when; null in every other state.
	WithdrawnReason *string `json:"withdrawn_reason"`
	WithdrawnAt     *int64  `json:"withdrawn_at"`
	Version         int64   `json:"version"`
}

func proposalOf(p work.Proposal) proposalWire {
	signals := make([]string, len(p.Signals))
	for i, v := range p.Signals {
		signals[i] = string(v)
	}
	effects := make([]string, len(p.Effects))
	for i, v := range p.Effects {
		effects[i] = string(v)
	}
	return proposalWire{ID: p.ID, WorkID: p.WorkID, TaskID: optionalString(p.TaskID), SessionID: p.Session,
		Source: p.Source, Project: p.Project, Title: p.Title, Signals: signals, Effects: effects, Ask: p.Ask,
		Reason: p.AskReason, Channel: string(p.Channel), Question: optionalString(p.Question), State: string(p.State),
		Answer: optionalString(string(p.Answer)), AnsweredBy: optionalString(p.AnsweredBy),
		AnsweredAt: optionalUnix(p.AnsweredAt), CreatedAt: p.CreatedAt.Unix(), ExpiresAt: p.ExpiresAt.Unix(),
		AskedInlineAt: optionalUnix(p.AskedInlineAt), Unprompted: p.Unprompted(),
		WithdrawnReason: optionalString(p.WithdrawnReason), WithdrawnAt: optionalUnix(p.WithdrawnAt),
		Version: p.Version}
}

// The one sentence the answer tells the session what to do with it. It is
// the whole of the briefing about asking (§4.4): ask only on ask:true, in the
// server's words.
const (
	askInstructions = "Ask the person at the end of this turn, in the words of `question`, without waiting for the answer. " +
		"Then POST /v1/orchestrator/proposals/{id}/asked. Relay their answer to POST /v1/work/proposals/{id} " +
		"with {\"answer\":\"track\"|\"later\"|\"no\",\"via\":{\"run\":\"<the run that carried it>\"}}, " +
		"the run read from GET /v1/orchestrator/sessions/<your conversation id>/run after their reply arrives."
	holdInstructions = "Do not ask about this in the conversation. It is in the person's \"to confirm\" area and their daily digest; " +
		"if nobody answers by expires_at it stays with its to-dos."
)

type proposalOneWire struct {
	OK           bool           `json:"ok"`
	Proposal     proposalWire   `json:"proposal"`
	Ignored      []work.Ignored `json:"ignored,omitempty"`
	Item         *workItemWire  `json:"item,omitempty"`
	Instructions string         `json:"instructions,omitempty"`
}

func proposalAnswerOf(v app.ProposalView, withInstructions bool) proposalOneWire {
	out := proposalOneWire{OK: true, Proposal: proposalOf(v.Proposal), Ignored: v.Ignored}
	if v.Item != nil {
		item := workItemOf(*v.Item, false)
		out.Item = &item
	}
	if withInstructions {
		out.Instructions = holdInstructions
		if v.Proposal.Ask {
			out.Instructions = strings.ReplaceAll(askInstructions, "{id}", v.Proposal.ID)
		}
	}
	return out
}

type proposalListWire struct {
	OK         bool             `json:"ok"`
	State      *string          `json:"state"`
	Project    *string          `json:"project"`
	Counts     map[string]int64 `json:"counts"`
	Rows       []proposalWire   `json:"rows"`
	NextCursor *string          `json:"next_cursor"`
	PageSize   int              `json:"page_size"`
	// What this reading is worth (`SourceFreshness`), so a screen showing
	// these beside the task list and the landing ledger prints all three the
	// same way. See participationSource.
	Source contract.BearingsSource `json:"source"`
}

type decisionWire struct {
	ID         string        `json:"id"`
	SessionID  string        `json:"session_id"`
	WorkID     *string       `json:"work_id"`
	TaskID     *string       `json:"task_id"`
	Project    *string       `json:"project"`
	Question   string        `json:"question"`
	Options    []work.Option `json:"options"`
	Default    string        `json:"default"`
	Blocking   bool          `json:"blocking"`
	State      string        `json:"state"`
	Answer     *string       `json:"answer"`
	AnsweredBy *string       `json:"answered_by"`
	AnsweredAt *int64        `json:"answered_at"`
	CreatedAt  int64         `json:"created_at"`
	DueAt      int64         `json:"due_at"`
	Push       string        `json:"push"`
	PushedAt   *int64        `json:"pushed_at"`
	Version    int64         `json:"version"`
}

func decisionOf(d work.Decision) decisionWire {
	return decisionWire{ID: d.ID, SessionID: d.Session, WorkID: optionalString(d.WorkID), TaskID: optionalString(d.TaskID),
		Project: optionalString(d.Project), Question: d.Question, Options: d.Options, Default: d.Default,
		Blocking: d.Blocking, State: string(d.State), Answer: optionalString(d.Answer),
		AnsweredBy: optionalString(d.AnsweredBy), AnsweredAt: optionalUnix(d.AnsweredAt), CreatedAt: d.CreatedAt.Unix(),
		DueAt: d.DueAt.Unix(), Push: string(d.Push), PushedAt: optionalUnix(d.PushedAt), Version: d.Version}
}

type decisionOneWire struct {
	OK       bool         `json:"ok"`
	Decision decisionWire `json:"decision"`
}

type decisionListWire struct {
	OK         bool             `json:"ok"`
	State      *string          `json:"state"`
	Counts     map[string]int64 `json:"counts"`
	Rows       []decisionWire   `json:"rows"`
	NextCursor *string          `json:"next_cursor"`
	PageSize   int              `json:"page_size"`
	// As proposalListWire.Source: what this reading is worth.
	Source contract.BearingsSource `json:"source"`
}

// participationSource is the freshness of a proposal or decision listing.
//
// Both lists are read from the store exactly, every time, so "did the read
// happen" is always yes and was never the interesting question. What ages
// these rows is the sweep: a pending proposal leaves on its own when its
// subject settles or when it expires, and an open decision defaults when its
// clock runs out — and both of those are the sweep's doing. With the sweep
// stopped the rows are still there and still say `pending` and `open`, which
// is `unverified`: read in full, and resting on a clock that is not running.
//
// That is why this is not `stale`. Nothing is missing from the answer. What
// may be wrong is the answer itself.
func (s *Server) participationSource() contract.BearingsSource {
	board := s.work()
	pulse, _, running := board.Pulse()
	return contract.BearingsSource{
		ObservedAt: time.Now().Unix(),
		Provenance: "work",
		Freshness:  participationFreshness(running, pulse.At.IsZero(), board.Stalled()),
	}
}

// participationFreshness is that judgement with the board taken out of it, so
// that each of the four ways it can be reached is a case somebody can write
// down rather than a clock somebody has to run.
//
// Three of them are the same fact in three spellings — the clock these rows
// age on is not turning — and all three are `unverified` rather than `stale`,
// because nothing is missing from the answer. What may be wrong is the answer.
func participationFreshness(running, noPassYet, stalled bool) contract.SourceFreshness {
	switch {
	case !running:
		return contract.SourceFreshnessUnverified
	case noPassYet:
		return contract.SourceFreshnessUnverified
	case stalled:
		return contract.SourceFreshnessUnverified
	}
	return contract.SourceFreshnessCurrent
}

type digestWire struct {
	Key       string          `json:"key"`
	Kind      string          `json:"kind"`
	From      int64           `json:"from"`
	To        int64           `json:"to"`
	CreatedAt int64           `json:"created_at"`
	Body      json.RawMessage `json:"body"`
}

type digestListWire struct {
	OK         bool         `json:"ok"`
	Kind       *string      `json:"kind"`
	Rows       []digestWire `json:"rows"`
	NextCursor *string      `json:"next_cursor"`
	PageSize   int          `json:"page_size"`
}

// ——— A session's side ———

type proposeWire struct {
	SessionID string `json:"session_id"`
	WorkID    string `json:"work_id"`
	TaskID    string `json:"task_id"`
	// Leftover is the title of one row of task_id's `leftovers`: something
	// that delivery said it did not do. With it, task_id is where the
	// candidate came from rather than the line of work being proposed, and
	// the title and project are the delivery's own.
	Leftover string   `json:"leftover"`
	Title    string   `json:"title"`
	Project  string   `json:"project"`
	Effects  []string `json:"effects"`
}

type askedWire struct {
	SessionID string `json:"session_id"`
}

type openDecisionWire struct {
	SessionID    string        `json:"session_id"`
	WorkID       string        `json:"work_id"`
	TaskID       string        `json:"task_id"`
	Project      string        `json:"project"`
	Question     string        `json:"question"`
	Options      []work.Option `json:"options"`
	Default      string        `json:"default"`
	Blocking     bool          `json:"blocking"`
	DueInMinutes int64         `json:"due_in_minutes"`
}

// idAfter reads `<prefix><id>[/<rest>]`, where id is a work-shaped id.
func idAfter(p, prefix string) (id, rest string, ok bool) {
	tail, found := strings.CutPrefix(p, prefix)
	if !found {
		return "", "", false
	}
	id, rest, _ = strings.Cut(tail, "/")
	return id, rest, workID(id)
}

func (s *Server) sessionProposalsRoute(w http.ResponseWriter, r *http.Request) {
	p := routePath(r)
	if p == "/v1/orchestrator/proposals" {
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A proposal is made with POST.")
			return
		}
		s.propose(w, r)
		return
	}
	id, rest, ok := idAfter(p, "/v1/orchestrator/proposals/")
	if !ok || !machineAuthed(r) {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such proposal route.")
		return
	}
	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.proposalRead(w, r, id)
	case rest == "asked" && r.Method == http.MethodPost:
		var body askedWire
		if _, ok := readWorkBody(w, r, &body); !ok {
			return
		}
		v, err := s.participation().ReportAsked(r.Context(), id, strings.TrimSpace(body.SessionID))
		if err != nil {
			writeWorkError(w, err)
			return
		}
		writeJSON(w, proposalAnswerOf(v, false))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"A proposal is read with GET; POST …/asked reports that it was asked.")
	}
}

// propose is the one door a proposal comes in by. The session is the
// orchestrator credential; a child is its own task secret, and whatever it
// proposes is its root's.
func (s *Server) propose(w http.ResponseWriter, r *http.Request) {
	var body proposeWire
	raw, ok := readWorkBody(w, r, &body)
	if !ok {
		return
	}
	req := app.ProposalRequest{Session: strings.TrimSpace(body.SessionID), WorkID: strings.TrimSpace(body.WorkID),
		TaskID: strings.TrimSpace(body.TaskID), Leftover: body.Leftover, Title: body.Title, Project: body.Project,
		Effects: body.Effects}
	principal := "machine"
	// seenTask is the task whose completion notice this proposal proves its root
	// read (orchestrator.NoticeSeen). A leftover is a row of a delivery's
	// result.json, and the line this broker types into a root is what tells it
	// this route exists and what those rows are called — so a root proposing one
	// of its own child's leftovers has read the notice, and typing that line at
	// it again is noise. Only from the root's own session: the same route takes
	// a child's proposal *for* its root, and a child cannot observe on its
	// behalf.
	seenTask := ""
	switch {
	case machineAuthed(r):
		if s.broker != nil && req.Leftover != "" && req.Session != "" && orchestrator.IsTaskID(req.TaskID) {
			if rec, _, err := s.broker.Record(r.Context(), req.TaskID); err == nil &&
				rec.Root != nil && rec.Root.SessionID == req.Session {
				seenTask = req.TaskID
			}
		}
	case taskSecret(r) != "":
		if s.broker == nil || !orchestrator.IsTaskID(req.TaskID) {
			writeRefusal(w, http.StatusUnauthorized, "unauthorized", "A child proposes with its task_id and its task secret.")
			return
		}
		rec, _, err := s.broker.Authenticate(r.Context(), req.TaskID, taskSecret(r))
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		if rec.Root == nil || rec.Root.SessionID == "" || rec.Root.PollOnly {
			writeRefusal(w, http.StatusConflict, "no_root",
				"This task has no root session to propose to; its work is nobody's line to follow.")
			return
		}
		// The line of work is the root's: the proposal is about the child's
		// own dispatch, for the root, and in the root's "to confirm" area.
		req.Session, req.FromChild, req.ChildTask = rec.Root.SessionID, true, rec.ID
		principal = "child:" + rec.ID
	case accessOf(r).verdict.Allowed:
		writeRefusal(w, http.StatusForbidden, "proposal_is_for_sessions",
			"A person does not propose to themselves: make the item on the board (POST /v1/work/items).")
		return
	default:
		writeRefusal(w, http.StatusUnauthorized, "unauthorized", "This needs the orchestrator token.")
		return
	}
	k, ok := workKey(w, r, principal)
	if !ok {
		return
	}
	k.Scope = participationScope
	s.participationWrite(w, r, k, raw, func(file func(any) (store.ReceiptKey, store.ReceiptAnswer, bool)) error {
		_, err := s.participation().Propose(r.Context(), req, func(v app.ProposalView) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			return file(proposalAnswerOf(v, true))
		})
		if err == nil && seenTask != "" {
			s.broker.NoticeSeen(r.Context(), seenTask, orchestrator.SeenByProposal)
		}
		return err
	})
}

func (s *Server) proposalRead(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := workQuery(w, r); !ok {
		return
	}
	p, err := s.participation().Proposal(r.Context(), id)
	if err != nil {
		writeWorkError(w, err)
		return
	}
	writeJSON(w, proposalAnswerOf(app.ProposalView{Proposal: p}, false))
}

func (s *Server) sessionDecisionsRoute(w http.ResponseWriter, r *http.Request) {
	p := routePath(r)
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "decision_is_for_sessions",
			"A session asks a person here with the orchestrator token; a person answers at /v1/work/decisions.")
		return
	}
	if p == "/v1/orchestrator/decisions" {
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A decision is asked with POST.")
			return
		}
		var body openDecisionWire
		raw, ok := readWorkBody(w, r, &body)
		if !ok {
			return
		}
		k, ok := workKey(w, r, "machine")
		if !ok {
			return
		}
		k.Scope = participationScope
		req := app.DecisionRequest{Session: strings.TrimSpace(body.SessionID), WorkID: strings.TrimSpace(body.WorkID),
			TaskID: strings.TrimSpace(body.TaskID), Project: body.Project, Question: body.Question,
			Options: body.Options, Default: strings.TrimSpace(body.Default), Blocking: body.Blocking,
			Due: time.Duration(body.DueInMinutes) * time.Minute}
		s.participationWrite(w, r, k, raw, func(file func(any) (store.ReceiptKey, store.ReceiptAnswer, bool)) error {
			_, err := s.participation().OpenDecision(r.Context(), req, func(d work.Decision) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				return file(decisionOneWire{OK: true, Decision: decisionOf(d)})
			})
			return err
		})
		return
	}
	id, rest, ok := idAfter(p, "/v1/orchestrator/decisions/")
	if !ok || rest != "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such decision route.")
		return
	}
	s.decisionRead(w, r, id)
}

func (s *Server) decisionRead(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := workQuery(w, r); !ok {
		return
	}
	d, err := s.participation().Decision(r.Context(), id)
	if err != nil {
		writeWorkError(w, err)
		return
	}
	writeJSON(w, decisionOneWire{OK: true, Decision: decisionOf(d)})
}

// participationWrite runs one write under its receipt, as the board's routes
// do (workWrite): the answer is filed inside the change's transaction, and a
// refusal gives the key back.
func (s *Server) participationWrite(w http.ResponseWriter, r *http.Request, k store.ReceiptKey, raw []byte,
	run func(file func(any) (store.ReceiptKey, store.ReceiptAnswer, bool)) error) {
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
	status := http.StatusOK
	if p := routePath(r); p == "/v1/orchestrator/proposals" || p == "/v1/orchestrator/decisions" {
		status = http.StatusCreated
	}
	var answer []byte
	err := run(func(v any) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer, _ = json.Marshal(v)
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

// ——— A person's side ———

type answerWire struct {
	Answer string       `json:"answer"`
	Via    *workViaWire `json:"via"`
}

func (s *Server) workProposalsRoute(w http.ResponseWriter, r *http.Request) {
	p := routePath(r)
	if p == "/v1/work/proposals" {
		q, ok := workQuery(w, r, "state", "project", "cursor")
		if !ok {
			return
		}
		state := work.ProposalState(q["state"])
		switch state {
		case "":
			state = work.ProposalPending
		case "all":
			state = ""
		case work.ProposalPending, work.ProposalAnswered, work.ProposalExpired, work.ProposalWithdrawn:
		default:
			writeRefusal(w, http.StatusBadRequest, "invalid_state",
				"state is pending, answered, expired, withdrawn or all.")
			return
		}
		page, err := s.participation().ProposalList(r.Context(), state, q["project"], q["cursor"])
		if err != nil {
			writeWorkError(w, err)
			return
		}
		out := proposalListWire{OK: true, State: optionalString(string(state)), Project: optionalString(q["project"]),
			Counts: map[string]int64{}, Rows: []proposalWire{}, NextCursor: optionalString(page.Next),
			PageSize: app.WorkPageSize, Source: s.participationSource()}
		for _, st := range work.ProposalStates {
			out.Counts[string(st)] = page.Counts[st]
		}
		for _, row := range page.Rows {
			out.Rows = append(out.Rows, proposalOf(row))
		}
		writeJSON(w, out)
		return
	}
	id, rest, ok := idAfter(p, "/v1/work/proposals/")
	if !ok || rest != "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such proposal route.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.proposalRead(w, r, id)
	case http.MethodPost:
		var body answerWire
		raw, ok := readWorkBody(w, r, &body)
		if !ok {
			return
		}
		answer, err := work.ParseAnswer(body.Answer)
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
		k.Scope = participationScope
		s.participationWrite(w, r, k, raw, func(file func(any) (store.ReceiptKey, store.ReceiptAnswer, bool)) error {
			run, err := s.relayRun(r.Context(), relay)
			if err != nil {
				return err
			}
			_, err = s.participation().Answer(r.Context(), id, answer, actor, principal, run,
				func(v app.ProposalView) (store.ReceiptKey, store.ReceiptAnswer, bool) {
					return file(proposalAnswerOf(v, false))
				})
			return err
		})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A proposal is read with GET and answered with POST.")
	}
}

func (s *Server) workDecisionsRoute(w http.ResponseWriter, r *http.Request) {
	p := routePath(r)
	if p == "/v1/work/decisions" {
		q, ok := workQuery(w, r, "state", "session_id", "cursor")
		if !ok {
			return
		}
		state := work.DecisionState(q["state"])
		switch state {
		case "":
			state = work.DecisionOpen
		case "all":
			state = ""
		case work.DecisionOpen, work.DecisionAnswered, work.DecisionDefaulted:
		default:
			writeRefusal(w, http.StatusBadRequest, "invalid_state", "state is open, answered, defaulted or all.")
			return
		}
		page, err := s.participation().DecisionList(r.Context(), state, q["session_id"], q["cursor"])
		if err != nil {
			writeWorkError(w, err)
			return
		}
		out := decisionListWire{OK: true, State: optionalString(string(state)), Counts: map[string]int64{},
			Rows: []decisionWire{}, NextCursor: optionalString(page.Next), PageSize: app.WorkPageSize,
			Source: s.participationSource()}
		for _, st := range []work.DecisionState{work.DecisionOpen, work.DecisionAnswered, work.DecisionDefaulted} {
			out.Counts[string(st)] = page.Counts[st]
		}
		for _, row := range page.Rows {
			out.Rows = append(out.Rows, decisionOf(row))
		}
		writeJSON(w, out)
		return
	}
	id, rest, ok := idAfter(p, "/v1/work/decisions/")
	if !ok || rest != "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such decision route.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.decisionRead(w, r, id)
	case http.MethodPost:
		var body answerWire
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
		k.Scope = participationScope
		option := strings.TrimSpace(body.Answer)
		s.participationWrite(w, r, k, raw, func(file func(any) (store.ReceiptKey, store.ReceiptAnswer, bool)) error {
			run, err := s.relayRun(r.Context(), relay)
			if err != nil {
				return err
			}
			_, err = s.participation().AnswerDecision(r.Context(), id, option, actor, principal, run,
				func(d work.Decision) (store.ReceiptKey, store.ReceiptAnswer, bool) {
					return file(decisionOneWire{OK: true, Decision: decisionOf(d)})
				})
			return err
		})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A decision is read with GET and answered with POST.")
	}
}

func (s *Server) workDigestsRoute(w http.ResponseWriter, r *http.Request) {
	q, ok := workQuery(w, r, "kind", "cursor")
	if !ok {
		return
	}
	kind := work.DigestKind(q["kind"])
	if kind != "" && kind != work.DigestDaily && kind != work.DigestWeekly {
		writeRefusal(w, http.StatusBadRequest, "invalid_kind", "kind is daily or weekly.")
		return
	}
	page, err := s.participation().DigestList(r.Context(), kind, q["cursor"])
	if err != nil {
		writeWorkError(w, err)
		return
	}
	out := digestListWire{OK: true, Kind: optionalString(string(kind)), Rows: []digestWire{},
		NextCursor: optionalString(page.Next), PageSize: app.WorkPageSize}
	for _, d := range page.Rows {
		out.Rows = append(out.Rows, digestWire{Key: d.Key, Kind: string(d.Kind), From: d.From.Unix(), To: d.To.Unix(),
			CreatedAt: d.CreatedAt.Unix(), Body: d.Body})
	}
	writeJSON(w, out)
}
