package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Participation keeps the points where a person takes part in the board
// (design-decisions T4; the rules are internal/domain/work/proposals.go).
//
// Few and exact, and the server decides, never the agent:
//
//   - a proposal comes in through one door (Propose). The rules decide
//     whether it is recorded at all — the refusals of board-redesign §4.4 —
//     and whether the session may ask in the conversation, from facts the
//     session cannot supply: the broker's tasks and to-dos, and when a person
//     last sent that session a message through this daemon (Heard). The
//     session reports when it did ask (ReportAsked), so what was decided and
//     what was done can be counted against each other;
//   - a person answers it (Answer): track or later makes the work item, and
//     the move that records it, in the answer's own transaction;
//   - a decision (OpenDecision) is a session's question to a person. It names
//     the answer that stands if nobody gives one. Only a blocking one is
//     pushed, once, after its row is written, and the push's outcome is a
//     second write (DG-4: sent and delivered are two records);
//   - the sweep (Sweep, run on the board's own clock) lets each safe default
//     stand when its wait is over, and writes the daily and weekly digest.
type Participation struct {
	Board     *WorkBoard
	Proposals work.ProposalPolicy
	Decisions work.DecisionPolicy
	Digests   work.DigestPolicy
	// Heard is when a person last sent a session a message through this
	// daemon. Nil, or nothing heard, is a person not known to be there.
	Heard *Heard
	// Push sends one notification to every device that asked for them. Nil
	// is a daemon with no push path: a blocking decision is then recorded as
	// not_subscribed.
	Push func(ctx context.Context, title, body, tag string) (sent, failed int, err error)
	// The register's limits: proposals.open, decisions.open, work.digests.
	// Zero is the store's default; an override may only lower them.
	ProposalLimit int64
	DecisionLimit int64
	DigestLimit   int64
	// Bind puts a broker task on a line of work (orchestrator.BindWork). Nil
	// is a daemon with no broker: a proposal about a task still records, and
	// a task still owed is bound when a broker next starts on the same store.
	Bind func(ctx context.Context, task, workID, from string) error
}

// NewParticipation is the participation points on a board, with the default
// clocks.
func NewParticipation(b *WorkBoard) *Participation {
	loc := b.Policy.Location
	p := &Participation{Board: b, Proposals: work.DefaultProposalPolicy(), Decisions: work.DefaultDecisionPolicy(),
		Digests: work.DefaultDigestPolicy(), Heard: NewHeard(30 * time.Minute)}
	if loc != nil {
		p.Proposals.Location, p.Digests.Location = loc, loc
	}
	return p
}

// Bounds of what a session sends, and of what one digest lists.
const (
	// decisionPushHourLimit is how many decisions this machine pushes in an
	// hour: the agent notifications' 30 (board-redesign §4.3), counted apart
	// from them.
	decisionPushHourLimit = 30
	// stalePush is how long a push recorded as pending may go without an
	// outcome before it is recorded as unknown, never sent twice.
	stalePush = 10 * time.Minute
	// digestLineLimit is the most lines one section of a digest lists; its
	// total is always there.
	digestLineLimit = 50
	// digestMoveLimit is the most moves one digest reads.
	digestMoveLimit = 2_000
	// weeklyLookback is how many earlier weekly digests are read to ask about
	// a stale Backlog item once in its thirty days.
	weeklyLookback = 5
	// ruleLineLimit is the most named lines one pass proposes by rule; the
	// rest are the next pass's.
	ruleLineLimit = 50
	// withdrawLimit is the most pending proposals one pass re-decides against
	// their subjects; the rest are the next pass's. It is above the register's
	// pending cap for a single project's worth of questions, so a day's pile
	// clears in one pass rather than 15 seconds at a time.
	withdrawLimit = 200
)

func (p *Participation) now() time.Time { return seconds(p.Board.now()) }

func limitOr(v, def int64) int64 {
	if v > 0 && v < def {
		return v
	}
	return def
}

// ——— Hearing a person ———

// Heard is when a person last sent each session a message through this
// daemon — an observation, kept in memory and never stored (D04): after a
// restart nobody has been heard, and a proposal goes to the "to confirm"
// area, which is the side that interrupts nobody. It holds only what is
// inside its window; older entries are let go on every write, so it is
// bounded by how many sessions a person wrote to in that window.
type Heard struct {
	window time.Duration
	mu     sync.Mutex
	at     map[string]time.Time
}

// NewHeard is a ledger that remembers a message for window.
func NewHeard(window time.Duration) *Heard {
	return &Heard{window: window, at: map[string]time.Time{}}
}

// Mark records that a person sent a message to the session known by each of
// ids — its conversation id, and its terminal id.
func (h *Heard) Mark(at time.Time, ids ...string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, when := range h.at {
		if at.Sub(when) > h.window {
			delete(h.at, id)
		}
	}
	for _, id := range ids {
		if id != "" {
			h.at[id] = at
		}
	}
}

// At is when a person last sent the session a message; zero when not
// inside the window.
func (h *Heard) At(id string) time.Time {
	if h == nil {
		return time.Time{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.at[id]
}

// ——— Refusals ———

// participationRefusal turns a failure into the refusal its route answers.
func participationRefusal(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrProposalsFull):
		return workRefusal(429, "proposals_full",
			"The \"to confirm\" area holds as many proposals as this machine keeps waiting; nothing was recorded. The work stays with its to-dos.")
	case errors.Is(err, store.ErrDecisionsFull):
		return workRefusal(429, "decisions_full",
			"As many decisions wait for a person as this machine keeps; nothing was recorded. Decide it in the session, or wait for one to be answered.")
	case errors.Is(err, store.ErrNoProposal):
		return workRefusal(404, "proposal_not_found", "No proposal has that id.")
	case errors.Is(err, store.ErrNoDecision):
		return workRefusal(404, "decision_not_found", "No decision has that id.")
	}
	return storeRefusal(err)
}

func checkSession(session string) error {
	if session == "" || utf8.RuneCountInString(session) > workOwnerLimit || strings.Contains(session, "/") {
		return workRefusal(400, "session_required",
			"session_id names the root conversation the work belongs to (at most "+strconv.Itoa(workOwnerLimit)+" characters).")
	}
	return nil
}

// ——— Proposals ———

// ProposalRequest is one proposal as its route decoded it.
type ProposalRequest struct {
	// Session is the root conversation the line of work belongs to. For a
	// child it is the child's own root, read from the broker's record.
	Session string
	WorkID  string
	TaskID  string
	Title   string
	Project string
	Effects []string
	// Leftover is the title of one row of TaskID's `result.json` leftovers:
	// something a child reported it did not do (work.Leftover). It makes the
	// proposal's subject that leftover rather than TaskID's own line of work,
	// so TaskID is provenance here — which delivery said so — and never a
	// dispatch of the work being proposed.
	Leftover string
	// FromChild says the caller authenticated with a child's task secret;
	// ChildTask is that task. ByRule says the rules made it (RuleProposals).
	FromChild bool
	ChildTask string
	ByRule    bool
}

// ProposalView is a proposal as its routes answer it.
type ProposalView struct {
	Proposal work.Proposal
	// Ignored is the tasks the signals did not count, and why.
	Ignored []work.Ignored
	// Item is the work item an answer made, or found already there.
	Item *WorkView
}

// ProposalFiler files a route's answer inside the change's transaction.
type ProposalFiler func(ProposalView) (store.ReceiptKey, store.ReceiptAnswer, bool)

func mergeRows(rows []store.BrokerRow, more []store.BrokerRow) []store.BrokerRow {
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.ID] = true
	}
	for _, r := range more {
		if !seen[r.ID] {
			seen[r.ID] = true
			rows = append(rows, r)
		}
	}
	return rows
}

// Propose is the one door a proposal comes in by (board-redesign §4.4). It is
// recorded, or refused and nothing is written; and whether the session may
// ask in the conversation is this answer's, decided from facts.
func (p *Participation) Propose(ctx context.Context, req ProposalRequest, file ProposalFiler) (ProposalView, error) {
	req.Title, req.Project = strings.TrimSpace(req.Title), strings.TrimSpace(req.Project)
	req.Leftover = strings.TrimSpace(req.Leftover)
	// A leftover takes its title, and its project, from the delivery that
	// named it: what is proposed is what the child wrote, not a sentence the
	// proposing session made up about it.
	leftover := req.Leftover != ""
	switch {
	case checkSession(req.Session) != nil:
		return ProposalView{}, checkSession(req.Session)
	case leftover && req.TaskID == "":
		return ProposalView{}, workRefusal(400, "subject_required",
			"A leftover is proposed with the task_id of the delivery whose result.json named it.")
	case leftover && req.WorkID != "":
		return ProposalView{}, workRefusal(400, "invalid_work_id",
			"A leftover has no line of work yet: one is named when a person answers, so work_id is not given here.")
	case leftover && len(req.Effects) > 0:
		return ProposalView{}, workRefusal(400, "invalid_leftover",
			"A leftover has no effects yet: nothing has been done towards it. Declare effects on the line of work that had them.")
	case leftover && utf8.RuneCountInString(req.Leftover) > work.LeftoverTitleLimit:
		return ProposalView{}, workRefusal(400, "invalid_leftover",
			"leftover is the title of one row of that task's leftovers, at most "+
				strconv.Itoa(work.LeftoverTitleLimit)+" characters.")
	case !leftover && (req.Title == "" || utf8.RuneCountInString(req.Title) > workTitleLimit):
		return ProposalView{}, workRefusal(400, "invalid_title", "title is 1 to "+strconv.Itoa(workTitleLimit)+" characters.")
	case !leftover && (req.Project == "" || utf8.RuneCountInString(req.Project) > workProjectLimit):
		return ProposalView{}, workRefusal(400, "project_required", "project names the work's project.")
	case utf8.RuneCountInString(req.Project) > workProjectLimit:
		return ProposalView{}, workRefusal(400, "project_required", "project names the work's project.")
	case req.WorkID != "" && !orchestrator.IsTaskID(req.WorkID):
		return ProposalView{}, workRefusal(400, "invalid_work_id", "work_id is a lowercase UUID, the one the line's dispatches carry.")
	case req.TaskID != "" && !orchestrator.IsTaskID(req.TaskID):
		return ProposalView{}, workRefusal(400, "invalid_task_id", "task_id is a broker task id.")
	case req.WorkID == "" && req.TaskID == "":
		return ProposalView{}, workRefusal(400, "subject_required",
			"A proposal names its line of work: work_id, or the task_id of the dispatch it is about.")
	}
	effects, err := work.ParseEffects(req.Effects)
	if err != nil {
		return ProposalView{}, participationRefusal(err)
	}
	heard := p.Heard.At(req.Session)
	now := p.now()
	source := work.SourceSession
	switch {
	case req.ByRule:
		source, heard = work.SourceRule, time.Time{}
	case req.FromChild:
		source = work.SourceChild + req.ChildTask
	}
	var out ProposalView
	// bind is the task this proposal puts on its line, when the broker had
	// not: a task stored before lines were bound at admission, or a step of
	// other work the root chose to propose. It is written after the proposal,
	// and only if the proposal was recorded (bindAfter).
	var bind *lineBinding
	err = p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		bind = nil
		workID := req.WorkID
		rows := []store.BrokerRow{}
		title, project, question := req.Title, req.Project, ""
		var signals []work.Signal
		var ignored []work.Ignored
		var prior []work.Proposal
		subjectStatus := work.SubjectUnknown
		if leftover {
			sub, err := leftoverSubject(tx, req)
			if err != nil {
				return err
			}
			// A line of its own, named now. Nothing is dispatched on it yet,
			// and the task that raised it stays on its own line: it is where
			// this came from, not work done towards it.
			workID, title, question = newWorkID(), sub.leftover.Title, work.LeftoverQuestion(sub.leftover, req.TaskID)
			project, signals, prior = sub.project, []work.Signal{work.SignalLeftover}, sub.prior
			if req.Project != "" {
				project = req.Project
			}
		} else if req.TaskID != "" {
			row, err := tx.BrokerTask(req.TaskID)
			if errors.Is(err, store.ErrNoTask) {
				return workRefusal(404, "task_not_found", "No broker task has that id.")
			}
			if err != nil {
				return err
			}
			r, err := orchestrator.Decode(row.Record)
			if err != nil {
				return workRefusal(409, "facts_unknown", "That task's record could not be read; nothing was recorded.")
			}
			facts, _ := Facts([]store.BrokerRow{row})
			f := facts[0]
			if f.Owner != req.Session {
				return workRefusal(409, "not_the_root",
					"A line of work is proposed for the root it belongs to, and that task's root is another session.")
			}
			line, from, bound := orchestrator.LineOf(r)
			switch {
			case bound && workID == "":
				workID = line
			case bound && line != workID:
				return workRefusal(409, "work_id_mismatch", "That task is on another line of work ("+line+").")
			case bound:
			case workID != "":
				// The root names the line this unbound task is part of.
				bind = &lineBinding{task: r.ID, workID: workID, from: work.WorkNamed}
			default:
				// The line an earlier proposal about it named, or else the
				// one the broker's rules put it on (lines.go); a step of
				// other work the root chose to propose begins its own.
				earlier, err := tx.PriorProposals("", r.ID)
				if err != nil {
					return err
				}
				if named := work.ProposedLine(earlier, r.ID); named != "" {
					line, from = named, work.WorkProposal
				}
				if line == "" {
					line, from = r.ID, work.WorkDispatch
				}
				workID = line
				bind = &lineBinding{task: r.ID, workID: workID, from: from}
			}
			rows = append(rows, row)
		}
		if !leftover {
			if workID == "" {
				// No task and no line named: the proposal names a new line,
				// and the root's dispatches for it carry it.
				workID = newWorkID()
			} else {
				bound, err := tx.Tasks(workID)
				if err != nil {
					return err
				}
				rows = mergeRows(rows, bound)
			}
			facts, unknown := Facts(rows)
			if unknown > 0 {
				return workRefusal(409, "facts_unknown",
					"A task of this line of work could not be read, so its signals are unknown; nothing was recorded.")
			}
			ids := make([]string, 0, len(facts))
			for _, f := range facts {
				ids = append(ids, f.Task)
			}
			todos, err := tx.TodosOf(ids)
			if err != nil {
				return err
			}
			subjectStatus = work.SubjectStatusOf(todos)
			signals, ignored = work.SignalsOf(facts, todos, effects, now)
			if prior, err = tx.PriorProposals(workID, req.TaskID); err != nil {
				return err
			}
		}
		item, err := tx.Item(workID)
		hasItem := err == nil
		if err != nil && !errors.Is(err, store.ErrNoWork) {
			return err
		}
		inTurn, inDay, err := tx.AskCounts(req.Session, heard, p.Proposals.DayStart(now))
		if err != nil {
			return err
		}
		verdict, err := work.GateProposal(work.ProposalFacts{FromChild: req.FromChild, ByRule: req.ByRule,
			Item: item, HasItem: hasItem,
			Prior: prior, Signals: signals, HeardAt: heard, AskedThisTurn: inTurn, AskedToday: inDay}, p.Proposals, now)
		if err != nil {
			return err
		}
		prop := work.Proposal{ID: newWorkID(), WorkID: workID, TaskID: req.TaskID, Session: req.Session,
			Source: source, Project: project, Title: title, Signals: signals, Effects: effects,
			SubjectStatus: subjectStatus,
			Ask:           verdict.Ask, AskReason: verdict.Reason, Channel: verdict.Channel, State: work.ProposalPending,
			CreatedAt: now, ExpiresAt: now.Add(p.Proposals.Expiry)}
		switch {
		case leftover:
			// A leftover's sentence is recorded whether or not it may be
			// asked: it is what a person reads in the "to confirm" area, and
			// what the child wrote is the whole of the question.
			prop.Question = question
		case prop.Ask:
			prop.Question = work.Question(prop.Title, signals, effects)
		}
		if err := tx.PutProposal(prop, nil, limitOr(p.ProposalLimit, store.ProposalOpenLimit)); err != nil {
			return err
		}
		out = ProposalView{Proposal: prop, Ignored: ignored}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	if err == nil {
		p.bindAfter(ctx, bind)
	}
	return out, participationRefusal(err)
}

// leftoverOf is the subject of a leftover's proposal, read inside the
// transaction that will record it.
type leftoverSubjectRow struct {
	leftover work.Leftover
	project  string
	// prior is every earlier proposal of **this** leftover — the same task
	// and the same title — and nothing else. A delivery that named three
	// leftovers is three subjects, not one proposed three times, so the
	// duplicate rules must not read its siblings as earlier attempts.
	prior []work.Proposal
}

// leftoverSubject reads the delivery a leftover was named in, and the leftover
// itself. The task belongs to the proposing root or nothing is read: a
// proposal is put to one root, and a delivery of another root's is not this
// root's to raise.
func leftoverSubject(tx *store.WorkTx, req ProposalRequest) (leftoverSubjectRow, error) {
	row, err := tx.BrokerTask(req.TaskID)
	if errors.Is(err, store.ErrNoTask) {
		return leftoverSubjectRow{}, workRefusal(404, "task_not_found", "No broker task has that id.")
	}
	if err != nil {
		return leftoverSubjectRow{}, err
	}
	r, err := orchestrator.Decode(row.Record)
	if err != nil {
		return leftoverSubjectRow{}, workRefusal(409, "facts_unknown",
			"That task's record could not be read; nothing was recorded.")
	}
	facts, _ := Facts([]store.BrokerRow{row})
	if facts[0].Owner != req.Session {
		return leftoverSubjectRow{}, workRefusal(409, "not_the_root",
			"A leftover is raised for the root the delivery belongs to, and that task's root is another session.")
	}
	var named []work.Leftover
	if r.Result != nil {
		named = r.Result.Leftovers
	}
	// Read again here rather than trusted from the file: the child's own
	// validator refuses a malformed list (taskdir.ValidateResult), and a
	// result that reached the record another way is still not allowed to put
	// an unbounded string in front of a person.
	list, err := work.ParseLeftovers(named)
	if err != nil {
		return leftoverSubjectRow{}, err
	}
	lo, ok := work.FindLeftover(list, req.Leftover)
	if !ok {
		return leftoverSubjectRow{}, workRefusal(404, "leftover_not_found",
			"That delivery's result.json names no leftover with that title.")
	}
	all, err := tx.PriorProposals("", req.TaskID)
	if err != nil {
		return leftoverSubjectRow{}, err
	}
	return leftoverSubjectRow{leftover: lo, project: r.ProjectDir, prior: work.PriorLeftovers(all, lo.Title)}, nil
}

// lineBinding is a task to be put on a line of work by the broker.
type lineBinding struct{ task, workID, from string }

// bindAfter puts a task on the line a recorded proposal or answer named, in
// the broker's own transaction (orchestrator.BindWork), after the change that
// named it committed. Nothing is refused for it: the proposal stands, and the
// same binding is asked again when the proposal is answered and, for a task
// still owed, when the daemon starts — each time to the same line, so a
// binding that did not happen once is not a different one later.
func (p *Participation) bindAfter(ctx context.Context, b *lineBinding) {
	if b == nil || p.Bind == nil {
		return
	}
	if err := p.Bind(context.WithoutCancel(ctx), b.task, b.workID, b.from); err != nil {
		log.Printf("proposals: task %s was not put on line %s: %v", b.task, b.workID, err)
	}
}

// ReportAsked records that the session asked a proposal in the
// conversation. It is recorded whatever the server had said — refusing it
// would hide exactly what the count is for — and once: a second report
// changes nothing.
func (p *Participation) ReportAsked(ctx context.Context, id, session string) (ProposalView, error) {
	now := p.now()
	var out ProposalView
	err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		prev, err := tx.Proposal(id)
		if err != nil {
			return err
		}
		subject, err := tx.SubjectOf(prev.WorkID)
		if err != nil {
			return err
		}
		prev.SubjectStatus = work.SubjectStatusOf(subject.Todos)
		if session != "" && prev.Session != session {
			return workRefusal(409, "not_your_proposal", "That proposal belongs to another session.")
		}
		out = ProposalView{Proposal: prev}
		if !prev.AskedInlineAt.IsZero() {
			return nil
		}
		next := prev
		next.AskedInlineAt = now
		if err := tx.PutProposal(next, &prev, 0); err != nil {
			return err
		}
		next.Version++
		out.Proposal = next
		return nil
	})
	return out, participationRefusal(err)
}

// Answer is a person's answer to a proposal. Track and later make the work
// item where the answer put it, with the move that records it, in this
// transaction; no leaves the work with its to-dos.
//
// via is the run a session relayed the answer under (runs.go), already found
// and checked as a run; nil when the person answered themselves. A proposal
// is a question put to one root, so only a message to that root answers it.
func (p *Participation) Answer(ctx context.Context, id string, answer work.Answer, actor, principal string,
	via *work.Run, file ProposalFiler) (ProposalView, error) {
	if err := checkRelay(actor, via); err != nil {
		return ProposalView{}, err
	}
	now := p.now()
	var out ProposalView
	var bind *lineBinding
	err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		bind = nil
		prev, err := tx.Proposal(id)
		if err != nil {
			return err
		}
		subject, err := tx.SubjectOf(prev.WorkID)
		if err != nil {
			return err
		}
		prev.SubjectStatus = work.SubjectStatusOf(subject.Todos)
		if via != nil {
			if err := work.RelayTo(*via, prev.Session, prev.CreatedAt); err != nil {
				return err
			}
		}
		next, err := work.AnswerProposal(prev, answer, actor, now)
		if err != nil {
			return err
		}
		out = ProposalView{Proposal: next}
		if answer != work.AnswerNo {
			rows, err := tx.Tasks(next.WorkID)
			if err != nil {
				return err
			}
			facts, unknown := Facts(rows)
			existing, err := tx.Item(next.WorkID)
			switch {
			case err == nil:
				// Somebody made the work item since: the answer is recorded,
				// and the item is where it already is.
				v := p.Board.view(existing, facts, unknown)
				out.Item = &v
			case errors.Is(err, store.ErrNoWork):
				if unknown > 0 {
					return workRefusal(409, "facts_unknown",
						"A task of this line of work could not be read; nothing was done.")
				}
				item, change, _ := work.Placing(next, facts, now)
				if principal != "" {
					change.Evidence["principal"] = principal
				}
				if via != nil {
					change.Evidence["run"] = via.Evidence()
				}
				if err := tx.Create(item, store.MoveOf(item.ID, change, now), p.Board.openLimit()); err != nil {
					return err
				}
				v := p.Board.view(item, facts, 0)
				out.Item = &v
			default:
				return err
			}
			// The task the proposal was about is on the line the item now
			// holds; asked again here in case the proposal's own binding
			// did not happen (bindAfter). A leftover's task is not that: it
			// is the delivery that reported the work was **not** done, and
			// binding it here would move a finished task onto a line nothing
			// has been done on.
			if next.TaskID != "" && !next.Leftover() {
				from := work.WorkProposal
				if next.TaskID == next.WorkID {
					from = work.WorkDispatch
				}
				bind = &lineBinding{task: next.TaskID, workID: next.WorkID, from: from}
			}
		}
		if err := tx.PutProposal(next, &prev, 0); err != nil {
			return err
		}
		out.Proposal.Version = prev.Version + 1
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	if err == nil {
		p.bindAfter(ctx, bind)
	}
	return out, participationRefusal(err)
}

// ResolveProposal closes a pending proposal after somebody inspected its
// subject and recorded both the conclusion and its source. A person may do
// that directly; a root may do it for a proposal belonging to that root,
// because the claim is made reviewable by evidence rather than made true by
// a person's authority. session is empty for the person and the owning root's
// conversation id for a root.
func (p *Participation) ResolveProposal(ctx context.Context, id, resolution, evidence, actor, session string,
	file ProposalFiler) (ProposalView, error) {
	now := p.now()
	var out ProposalView
	err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		prev, err := tx.Proposal(id)
		if err != nil {
			return err
		}
		if session != "" && prev.Session != session {
			return workRefusal(409, "not_your_proposal", "That proposal belongs to another session.")
		}
		next, err := work.ResolveProposal(prev, resolution, evidence, actor, now)
		if err != nil {
			return err
		}
		out = ProposalView{Proposal: next}
		if err := tx.PutProposal(next, &prev, 0); err != nil {
			return err
		}
		out.Proposal.Version = prev.Version + 1
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, participationRefusal(err)
}

// ProposalPage is one read of the proposals — the "to confirm" area when
// narrowed to pending.
type ProposalPage struct {
	Counts map[work.ProposalState]int64
	Rows   []work.Proposal
	Next   string
}

// ProposalList reads one page of proposals, newest first.
func (p *Participation) ProposalList(ctx context.Context, state work.ProposalState, project, cursor string) (ProposalPage, error) {
	q := store.ProposalQuery{State: state, Project: project, Limit: WorkPageSize + 1}
	if cursor != "" {
		at, id, err := parseCreatedCursor(cursor)
		if err != nil {
			return ProposalPage{}, err
		}
		q.AfterCreated, q.AfterID = at, id
	}
	rows, err := p.Board.Store.Proposals(ctx, q)
	if err != nil {
		return ProposalPage{}, participationRefusal(err)
	}
	tally, err := p.Board.Store.ProposalTally(ctx)
	if err != nil {
		return ProposalPage{}, participationRefusal(err)
	}
	page := ProposalPage{Counts: tally.ByState, Rows: rows}
	if len(rows) > WorkPageSize {
		page.Rows = rows[:WorkPageSize]
		last := page.Rows[len(page.Rows)-1]
		page.Next = strconv.FormatInt(last.CreatedAt.Unix(), 10) + ":" + last.ID
	}
	if err := p.subjectStatuses(ctx, page.Rows); err != nil {
		return ProposalPage{}, participationRefusal(err)
	}
	return page, nil
}

// Proposal reads one proposal.
func (p *Participation) Proposal(ctx context.Context, id string) (work.Proposal, error) {
	prop, err := p.Board.Store.ProposalRow(ctx, id)
	if err != nil {
		return prop, participationRefusal(err)
	}
	rows := []work.Proposal{prop}
	if err := p.subjectStatuses(ctx, rows); err != nil {
		return work.Proposal{}, participationRefusal(err)
	}
	return rows[0], nil
}

// subjectStatuses adds the current three-way reading of each proposal's
// to-dos. It is deliberately derived on read: persisting it would make the
// screen repeat the proposal-time answer after the subject changed.
func (p *Participation) subjectStatuses(ctx context.Context, rows []work.Proposal) error {
	for i := range rows {
		todos, err := p.Board.Store.TodosOfWork(ctx, rows[i].WorkID)
		if err != nil {
			return err
		}
		rows[i].SubjectStatus = work.SubjectStatusOf(todos)
	}
	return nil
}

func parseCreatedCursor(cursor string) (int64, string, error) {
	created, id, ok := strings.Cut(cursor, ":")
	at, err := strconv.ParseInt(created, 10, 64)
	if !ok || err != nil || id == "" {
		return 0, "", errCursor
	}
	return at, id, nil
}

// ——— Decisions ———

// DecisionRequest is a session's question to a person, as its route decoded
// it.
type DecisionRequest struct {
	Session  string
	WorkID   string
	TaskID   string
	Project  string
	Question string
	Options  []work.Option
	Default  string
	Blocking bool
	// Due is how long it waits for an answer; zero is the policy's.
	Due time.Duration
}

// DecisionFiler files a route's answer inside the change's transaction.
type DecisionFiler func(work.Decision) (store.ReceiptKey, store.ReceiptAnswer, bool)

// OpenDecision records a session's question. A blocking one is pushed after
// the row is written, once, and the push's outcome is recorded apart: the
// answer this returns — and files — says `pending` for it, which is what was
// true when the question was admitted.
func (p *Participation) OpenDecision(ctx context.Context, req DecisionRequest, file DecisionFiler) (work.Decision, error) {
	switch {
	case checkSession(req.Session) != nil:
		return work.Decision{}, checkSession(req.Session)
	case req.WorkID != "" && !orchestrator.IsTaskID(req.WorkID):
		return work.Decision{}, workRefusal(400, "invalid_work_id", "work_id is a lowercase UUID.")
	case req.TaskID != "" && !orchestrator.IsTaskID(req.TaskID):
		return work.Decision{}, workRefusal(400, "invalid_task_id", "task_id is a broker task id.")
	case utf8.RuneCountInString(req.Project) > workProjectLimit:
		return work.Decision{}, workRefusal(400, "invalid_project", "project is at most "+strconv.Itoa(workProjectLimit)+" characters.")
	}
	now := p.now()
	d, err := work.NewDecision(work.Decision{ID: newWorkID(), Session: req.Session, WorkID: req.WorkID,
		TaskID: req.TaskID, Project: strings.TrimSpace(req.Project), Question: req.Question, Options: req.Options,
		Default: req.Default, Blocking: req.Blocking}, req.Due, p.Decisions, now)
	if err != nil {
		return work.Decision{}, participationRefusal(err)
	}
	err = p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		if err := tx.PutDecision(d, nil, limitOr(p.DecisionLimit, store.DecisionOpenLimit)); err != nil {
			return err
		}
		if file != nil {
			if k, a, ok := file(d); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	if err != nil {
		return work.Decision{}, participationRefusal(err)
	}
	if d.Blocking {
		p.PushDecision(context.WithoutCancel(ctx), d.ID)
	}
	return d, nil
}

// pushText is a blocking decision's notification, its title and its body,
// inside the push's own bounds (80 and 500 characters).
func pushText(d work.Decision, loc *time.Location) (string, string) {
	title := "等你決定：" + d.Question
	if utf8.RuneCountInString(title) > 80 {
		title = string([]rune(title)[:79]) + "…"
	}
	labels := []string{}
	fallback := d.Default
	for _, o := range d.Options {
		labels = append(labels, o.Label)
		if o.ID == d.Default {
			fallback = o.Label
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	body := "選項：" + strings.Join(labels, "／") + "。沒有回答的話，" + d.DueAt.In(loc).Format("01-02 15:04") +
		" 起照「" + fallback + "」做。"
	if utf8.RuneCountInString(body) > 500 {
		body = string([]rune(body)[:499]) + "…"
	}
	return title, body
}

// PushDecision pushes a blocking decision recorded as pending, once, and
// records what happened. The budget is read, and the outcome written, each in
// its own transaction; the push itself is outside both (D08).
func (p *Participation) PushDecision(ctx context.Context, id string) work.PushState {
	now := p.now()
	var d work.Decision
	outcome := work.PushState("")
	err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		prev, err := tx.Decision(id)
		if err != nil {
			return err
		}
		d = prev
		if prev.Push != work.PushPending {
			outcome = prev.Push
			return nil
		}
		n, err := tx.PushesSince(now.Add(-time.Hour))
		if err != nil {
			return err
		}
		switch {
		case n >= decisionPushHourLimit:
			outcome = work.PushOverBudget
		case p.Push == nil:
			outcome = work.PushNotSubscribed
		default:
			return nil
		}
		next := prev
		next.Push = outcome
		return tx.PutDecision(next, &prev, 0)
	})
	if err != nil || outcome != "" {
		return outcome
	}
	title, body := pushText(d, p.Proposals.Location)
	sent, failed, perr := p.Push(ctx, title, body, "decision-"+d.ID)
	switch {
	case perr != nil || (failed > 0 && sent == 0):
		outcome = work.PushFailed
	case sent == 0:
		outcome = work.PushNotSubscribed
	default:
		outcome = work.PushSent
	}
	_ = p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		prev, err := tx.Decision(id)
		if err != nil || prev.Push != work.PushPending {
			return err
		}
		next := prev
		next.Push = outcome
		if outcome == work.PushSent {
			next.PushedAt = p.now()
		}
		return tx.PutDecision(next, &prev, 0)
	})
	return outcome
}

// AnswerDecision is a person's answer. On the board item the decision is
// about, it is the item's newest fact, written as a move in this
// transaction. via is as Answer's: a decision is one root's question, and
// only a message to that root answers it.
func (p *Participation) AnswerDecision(ctx context.Context, id, option, actor, principal string,
	via *work.Run, file DecisionFiler) (work.Decision, error) {
	if err := checkRelay(actor, via); err != nil {
		return work.Decision{}, err
	}
	now := p.now()
	var out work.Decision
	err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		prev, err := tx.Decision(id)
		if err != nil {
			return err
		}
		if via != nil {
			if err := work.RelayTo(*via, prev.Session, prev.CreatedAt); err != nil {
				return err
			}
		}
		next, err := work.AnswerDecision(prev, option, actor, now)
		if err != nil {
			return err
		}
		if err := tx.PutDecision(next, &prev, 0); err != nil {
			return err
		}
		next.Version = prev.Version + 1
		out = next
		if err := p.decisionMove(tx, next, principal, via, now); err != nil {
			return err
		}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, participationRefusal(err)
}

// decisionMove writes a closed decision's move on the open board item it is
// about, if there is one.
func (p *Participation) decisionMove(tx *store.WorkTx, d work.Decision, principal string, via *work.Run,
	now time.Time) error {
	if d.WorkID == "" {
		return nil
	}
	it, err := tx.Item(d.WorkID)
	if errors.Is(err, store.ErrNoWork) {
		return nil
	}
	if err != nil {
		return err
	}
	if it.Place != work.PlaceBoard || !(it.State == work.ItemActive || it.State == work.ItemAwaitingClosure) {
		return nil
	}
	c := work.DecisionChange(d, it, now)
	if principal != "" {
		c.Evidence["principal"] = principal
	}
	if via != nil {
		c.Evidence["run"] = via.Evidence()
	}
	return tx.Put(it, c.Apply(it, now), store.MoveOf(it.ID, c, now))
}

// DecisionPage is one read of the decisions.
type DecisionPage struct {
	Counts map[work.DecisionState]int64
	Rows   []work.Decision
	Next   string
}

// DecisionList reads one page of decisions, newest first.
func (p *Participation) DecisionList(ctx context.Context, state work.DecisionState, session, cursor string) (DecisionPage, error) {
	q := store.DecisionQuery{State: state, Session: session, Limit: WorkPageSize + 1}
	if cursor != "" {
		at, id, err := parseCreatedCursor(cursor)
		if err != nil {
			return DecisionPage{}, err
		}
		q.AfterCreated, q.AfterID = at, id
	}
	rows, err := p.Board.Store.Decisions(ctx, q)
	if err != nil {
		return DecisionPage{}, participationRefusal(err)
	}
	counts, err := p.Board.Store.DecisionCounts(ctx)
	if err != nil {
		return DecisionPage{}, participationRefusal(err)
	}
	page := DecisionPage{Counts: counts, Rows: rows}
	if len(rows) > WorkPageSize {
		page.Rows = rows[:WorkPageSize]
		last := page.Rows[len(page.Rows)-1]
		page.Next = strconv.FormatInt(last.CreatedAt.Unix(), 10) + ":" + last.ID
	}
	return page, nil
}

// Decision reads one decision.
func (p *Participation) Decision(ctx context.Context, id string) (work.Decision, error) {
	d, err := p.Board.Store.DecisionRow(ctx, id)
	return d, participationRefusal(err)
}

// ——— The sweep ———

// Sweep lets every safe default whose wait is over stand — a proposal
// nobody answered stays with its to-dos, a decision nobody answered takes
// its default, a push that never recorded an outcome is unknown — and writes
// the digests that are due. Each change is decided again inside its write.
func (p *Participation) Sweep(ctx context.Context) error {
	now := p.now()
	var errs []error
	errs = append(errs, p.WithdrawProposals(ctx))
	due, err := p.Board.Store.DueProposals(ctx, now)
	errs = append(errs, err)
	for _, id := range due {
		errs = append(errs, p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
			prev, err := tx.Proposal(id)
			if err != nil {
				return err
			}
			next, ok := work.ExpireProposal(prev, now)
			if !ok {
				return nil
			}
			return tx.PutProposal(next, &prev, 0)
		}))
	}
	owed, err := p.Board.Store.DueDecisions(ctx, now)
	errs = append(errs, err)
	for _, id := range owed {
		errs = append(errs, p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
			prev, err := tx.Decision(id)
			if err != nil {
				return err
			}
			next, ok := work.DefaultDecision(prev, now)
			if !ok {
				return nil
			}
			if err := tx.PutDecision(next, &prev, 0); err != nil {
				return err
			}
			return p.decisionMove(tx, next, "", nil, now)
		}))
	}
	stale, err := p.Board.Store.StalePushes(ctx, now.Add(-stalePush))
	errs = append(errs, err)
	for _, id := range stale {
		errs = append(errs, p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
			prev, err := tx.Decision(id)
			if err != nil || prev.Push != work.PushPending {
				return err
			}
			next := prev
			next.Push = work.PushUnknown
			return tx.PutDecision(next, &prev, 0)
		}))
	}
	_, err = p.RuleProposals(ctx)
	errs = append(errs, err)
	for _, kind := range []work.DigestKind{work.DigestDaily, work.DigestWeekly} {
		_, err := p.WriteDigest(ctx, kind)
		errs = append(errs, err)
	}
	for _, e := range errs {
		if e != nil && !errors.Is(e, store.ErrConflict) {
			return e
		}
	}
	return nil
}

// WithdrawProposals is the exit a proposal was missing: every pending one is
// re-decided against its own subject, and the ones whose question has stopped
// being a question are taken back with the fact that ended it
// (work.WithdrawProposal).
//
// It runs first in the sweep, before the expiry, so a question that is over
// leaves as what it is — withdrawn, with a reason — rather than waiting out
// seven days to be recorded as one a person ignored. Each row is decided
// again inside its own write, from the rows it was made from.
func (p *Participation) WithdrawProposals(ctx context.Context) error {
	ids, err := p.Board.Store.PendingProposals(ctx, withdrawLimit)
	if err != nil {
		return err
	}
	now := p.now()
	for _, id := range ids {
		err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
			prev, err := tx.Proposal(id)
			if err != nil {
				return err
			}
			facts, err := tx.SubjectOf(prev.WorkID)
			if err != nil {
				return err
			}
			next, ok := work.WithdrawProposal(prev, facts, now)
			if !ok {
				return nil
			}
			return tx.PutProposal(next, &prev, 0)
		})
		if err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	}
	return nil
}

// RuleProposals is §6's "session to-do → board (promotion)": a line of work the
// rules find worth following is proposed without anybody calling anything —
// a design that waits for an agent to remember a call misses what 43.6% of
// runs missed (board-redesign §3.1). Such a proposal is never asked in a
// conversation: it waits in the "to confirm" area and the digest.
//
// A line is one whose tasks carry a work_id — every dispatch with a root is
// on one from its admission (lines.go) — that has no work item yet and whose
// to-do is still owed. Work that is over, landed or owing nothing, is never
// put in front of a person. A line is proposed only once its root has had
// the policy's RuleAfter to propose it itself: the root may ask in the
// conversation while the person is there, and a rule's proposal first would
// be the duplicate that takes that ask away. The gate's refusals stand: a
// line with no signal, or one already proposed, is left alone and nothing is
// written.
func (p *Participation) RuleProposals(ctx context.Context) (int, error) {
	lines, err := p.Board.Store.UnproposedLines(ctx, ruleLineLimit)
	if err != nil {
		return 0, err
	}
	if len(lines) == 0 {
		return 0, nil
	}
	ids := make([]string, len(lines))
	for i, l := range lines {
		ids[i] = l.WorkID
	}
	bound, err := p.Board.Store.WorkTasks(ctx, ids)
	if err != nil {
		return 0, err
	}
	now := p.now()
	made := 0
	for _, l := range lines {
		// Decided from a read first, so a line with nothing to say costs no
		// write; decided again inside the write.
		facts, unknown := Facts(bound[l.WorkID])
		if unknown > 0 || l.Project == "" || l.Title == "" {
			continue
		}
		first := time.Time{}
		for _, f := range facts {
			if first.IsZero() || f.CreatedAt.Before(first) {
				first = f.CreatedAt
			}
		}
		if !work.RuleDue(first, p.Proposals, now) {
			continue
		}
		todos, err := p.Board.Store.TodosOfWork(ctx, l.WorkID)
		if err != nil {
			return made, err
		}
		if signals, _ := work.SignalsOf(facts, todos, nil, now); !work.RuleWorthy(signals) {
			// I1 on its own is every dispatch there is; the gate refuses it
			// too, and this saves the write that would be rolled back.
			continue
		}
		title := l.Title
		if utf8.RuneCountInString(title) > workTitleLimit {
			title = string([]rune(title)[:workTitleLimit])
		}
		if _, err := p.Propose(ctx, ProposalRequest{Session: l.Owner, WorkID: l.WorkID, TaskID: l.Task,
			Title: title, Project: l.Project, ByRule: true}, nil); err == nil {
			made++
		}
	}
	return made, nil
}

// ——— The digest ———

// DigestLine is one line of a digest: a work item, a proposal or a decision,
// what happened to it, and when.
type DigestLine struct {
	WorkID string `json:"work_id,omitempty"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title"`
	What   string `json:"what"`
	At     int64  `json:"at"`
}

// DigestSection is a total and at most digestLineLimit of its lines.
type DigestSection struct {
	Total int          `json:"total"`
	Lines []DigestLine `json:"lines"`
}

func (s *DigestSection) add(l DigestLine) {
	s.Total++
	if len(s.Lines) < digestLineLimit {
		s.Lines = append(s.Lines, l)
	}
}

func section() DigestSection { return DigestSection{Lines: []DigestLine{}} }

// DigestBody is what one digest says (board-redesign §8's Periodic and Planning
// rows): what finished, what went back to the Backlog, every automatic move
// (D2), what waits for a person, and — weekly — the Backlog nobody has looked
// at for thirty days, each asked about once, kept unless a person says so.
type DigestBody struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
	From int64  `json:"from"`
	To   int64  `json:"to"`

	Completed   DigestSection `json:"completed"`
	Landed      int           `json:"landed"`
	Stalled     DigestSection `json:"stalled"`
	FromBacklog DigestSection `json:"from_backlog"`
	Automatic   DigestSection `json:"automatic"`
	// MovesTruncated says the window held more moves than one digest reads;
	// the totals are then of the ones read.
	MovesTruncated bool `json:"moves_truncated"`

	ProposalsPending int64         `json:"proposals_pending"`
	ProposalsExpired DigestSection `json:"proposals_expired"`
	// ProposalsWithdrawn is what the server took back that day and why — the
	// only place a question that left on its own is said to have left.
	ProposalsWithdrawn DigestSection `json:"proposals_withdrawn"`
	DecisionsOpen      int64         `json:"decisions_open"`
	DecisionsDefaulted DigestSection `json:"decisions_defaulted"`
	AwaitingClosure    int           `json:"awaiting_closure"`
	// ClosureAsked is the deliveries this digest asks a person about, once;
	// seven days from here each closes as unconfirmed.
	ClosureAsked   DigestSection `json:"closure_asked"`
	HandedOffTodos int64         `json:"handed_off_todos"`

	// BacklogStale is the weekly digest's question: keep these? Kept unless a
	// person drops them.
	BacklogStale DigestSection `json:"backlog_stale"`
}

var errDigestWritten = errors.New("the digest is already written")

// WriteDigest writes the digest of kind due now, once: false when it is
// already written. The daily digest asks about each delivery that has waited
// three days to be closed, in the transaction that writes the digest, so a
// delivery is asked about exactly when a digest says it was.
func (p *Participation) WriteDigest(ctx context.Context, kind work.DigestKind) (bool, error) {
	now := p.now()
	key, from, to := work.DigestWindow(kind, now, p.Digests)
	if has, err := p.Board.Store.HasDigest(ctx, key); err != nil || has {
		return false, err
	}
	body := DigestBody{Kind: string(kind), Key: key, From: from.Unix(), To: to.Unix(),
		Stalled: section(), FromBacklog: section(), Automatic: section(), Completed: section(),
		ProposalsExpired: section(), ProposalsWithdrawn: section(), DecisionsDefaulted: section(),
		ClosureAsked: section(), BacklogStale: section()}
	candidates := []string{}
	if kind == work.DigestDaily {
		if err := p.daily(ctx, &body, from, to); err != nil {
			return false, err
		}
		items, _, err := p.Board.Store.WorkBoard(ctx, "", now, workScanLimit)
		if err != nil {
			return false, err
		}
		for _, it := range items {
			if it.State == work.ItemAwaitingClosure {
				body.AwaitingClosure++
				candidates = append(candidates, it.ID)
			}
		}
	} else if err := p.weekly(ctx, &body, now); err != nil {
		return false, err
	}
	wrote := false
	err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		for _, id := range candidates {
			it, err := tx.Item(id)
			if err != nil {
				return err
			}
			rows, err := tx.Tasks(id)
			if err != nil {
				return err
			}
			facts, unknown := Facts(rows)
			if unknown > 0 {
				continue
			}
			d := work.Derive(it, facts, p.Board.Policy, now)
			if !work.ClosureAskDue(it, d, p.Digests, now) {
				continue
			}
			c := work.ClosureAsked(it, key)
			if err := tx.Put(it, c.Apply(it, now), store.MoveOf(it.ID, c, now)); err != nil {
				return err
			}
			body.ClosureAsked.add(DigestLine{WorkID: it.ID, Title: it.Title, What: work.TriggerClosureAsked, At: now.Unix()})
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		ok, err := tx.PutDigest(store.DigestRow{Key: key, Kind: kind, From: from, To: to, Body: raw, CreatedAt: now},
			limitOr(p.DigestLimit, store.DigestKeepLimit))
		if err != nil {
			return err
		}
		if !ok {
			return errDigestWritten
		}
		wrote = true
		return nil
	})
	if errors.Is(err, errDigestWritten) {
		return false, nil
	}
	return wrote, err
}

// daily fills the day's facts: the moves made in the window, the proposals
// that expired and the decisions that took their default in it, and what
// waits now.
func (p *Participation) daily(ctx context.Context, b *DigestBody, from, to time.Time) error {
	st := p.Board.Store
	moves, total, err := st.MovesBetween(ctx, from, to, digestMoveLimit)
	if err != nil {
		return err
	}
	b.MovesTruncated = total > len(moves)
	for _, m := range moves {
		line := DigestLine{WorkID: m.WorkID, Title: m.Title, What: m.Trigger, At: m.At.Unix()}
		if work.Automatic(m.Actor) {
			b.Automatic.add(line)
		}
		switch {
		case m.Trigger == work.TriggerStalled || m.Trigger == work.TriggerMissed:
			b.Stalled.add(line)
		case m.From == work.PlaceBacklog && m.To == work.PlaceBoard:
			b.FromBacklog.add(line)
		}
		// Where it ended up, not where it came from: an item a person closes
		// as done elsewhere (BD-17) comes onto the board in the same move it
		// closes in, and it did finish that day.
		if m.State == work.ItemDone && m.To == work.PlaceBoard {
			b.Completed.add(line)
			if m.Trigger == work.TriggerLanded {
				b.Landed++
			}
		}
	}
	expired, err := st.ProposalsClosedBetween(ctx, work.ProposalExpired, from, to, digestLineLimit+1)
	if err != nil {
		return err
	}
	for _, e := range expired {
		b.ProposalsExpired.add(DigestLine{WorkID: e.WorkID, ID: e.ID, Title: e.Title, What: "expired",
			At: e.ExpiresAt.Unix()})
	}
	withdrawn, err := st.ProposalsClosedBetween(ctx, work.ProposalWithdrawn, from, to, digestLineLimit+1)
	if err != nil {
		return err
	}
	for _, e := range withdrawn {
		b.ProposalsWithdrawn.add(DigestLine{WorkID: e.WorkID, ID: e.ID, Title: e.Title, What: e.WithdrawnReason,
			At: e.WithdrawnAt.Unix()})
	}
	defaulted, err := st.DecisionsClosedBetween(ctx, work.DecisionDefaulted, from, to, digestLineLimit+1)
	if err != nil {
		return err
	}
	for _, d := range defaulted {
		b.DecisionsDefaulted.add(DigestLine{WorkID: d.WorkID, ID: d.ID, Title: d.Question, What: d.Answer,
			At: d.AnsweredAt.Unix()})
	}
	tally, err := st.ProposalTally(ctx)
	if err != nil {
		return err
	}
	b.ProposalsPending = tally.ByState[work.ProposalPending]
	counts, err := st.DecisionCounts(ctx)
	if err != nil {
		return err
	}
	b.DecisionsOpen = counts[work.DecisionOpen]
	b.HandedOffTodos, err = st.HandedOffTodos(ctx)
	return err
}

// weekly fills the Backlog question: planned items nobody has looked at for
// the window, less those an earlier weekly digest already asked about inside
// it.
func (p *Participation) weekly(ctx context.Context, b *DigestBody, now time.Time) error {
	st := p.Board.Store
	asked := map[string]bool{}
	earlier, err := st.Digests(ctx, work.DigestWeekly, 0, weeklyLookback)
	if err != nil {
		return err
	}
	for _, d := range earlier {
		if now.Sub(d.CreatedAt) >= p.Digests.BacklogStale {
			continue
		}
		var prev DigestBody
		if json.Unmarshal(d.Body, &prev) == nil {
			for _, l := range prev.BacklogStale.Lines {
				asked[l.WorkID] = true
			}
		}
	}
	items, err := st.StaleBacklog(ctx, now.Add(-p.Digests.BacklogStale), int(store.WorkOpenLimit))
	if err != nil {
		return err
	}
	for _, it := range items {
		if asked[it.ID] || !work.BacklogStale(it, p.Digests, now) {
			continue
		}
		b.BacklogStale.add(DigestLine{WorkID: it.ID, Title: it.Title, What: "keep?", At: it.PlacedAt.Unix()})
	}
	return nil
}

// DigestPage is one read of the digests.
type DigestPage struct {
	Rows []store.DigestRow
	Next string
}

// DigestList reads one page of digests, newest first.
func (p *Participation) DigestList(ctx context.Context, kind work.DigestKind, cursor string) (DigestPage, error) {
	var before int64
	if cursor != "" {
		n, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil || n <= 0 {
			return DigestPage{}, errCursor
		}
		before = n
	}
	rows, err := p.Board.Store.Digests(ctx, kind, before, WorkPageSize+1)
	if err != nil {
		return DigestPage{}, participationRefusal(err)
	}
	page := DigestPage{Rows: rows}
	if len(rows) > WorkPageSize {
		page.Rows = rows[:WorkPageSize]
		page.Next = strconv.FormatInt(page.Rows[len(page.Rows)-1].From.Unix(), 10)
	}
	return page, nil
}

// ——— Diagnostics ———

// ProposalCounts is /v1/diagnostics.proposals: what the server decided and
// what the sessions did, counted from the rows. Matched says every proposal
// the server let be asked was reported asked and none it said not to ask
// was; a difference is a briefing not followed, or an ask not yet reported.
type ProposalCounts struct {
	AskTrue     int64
	AskFalse    int64
	AskedInline int64
	Unprompted  int64
	Pending     int64
	Matched     bool
	Oldest      time.Time
}

// Counts reads the proposal counts.
func (p *Participation) Counts(ctx context.Context) (ProposalCounts, error) {
	t, err := p.Board.Store.ProposalTally(ctx)
	if err != nil {
		return ProposalCounts{}, err
	}
	return ProposalCounts{AskTrue: t.AskTrue, AskFalse: t.AskFalse, AskedInline: t.AskedInline,
		Unprompted: t.Unprompted, Pending: t.ByState[work.ProposalPending],
		Matched: t.AskedInline == t.AskTrue && t.Unprompted == 0, Oldest: t.OldestPending}, nil
}
