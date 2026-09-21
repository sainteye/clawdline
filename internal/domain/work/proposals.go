package work

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Where a person takes part: step 4 of board-redesign §9, T4 of
// docs/design-decisions.md §6 (D31, DG-10).
//
// Few and exact. Everything a session does is first its own to-do, and nobody
// is asked about it (§4.1). A person is brought in at three points only:
//
//   - a proposal: the rules say a line of work is worth following — it sent
//     out a second session (I1), a to-do of it has been owed for a day (I2), or
//     it has an effect outside this machine (I3) — and the person answers
//     track, later or no. Whether the person is asked in the conversation or
//     finds it in the "to confirm" area is the server's answer, never the
//     agent's (§4.3, §4.4);
//   - a decision: a session needs a person's answer. Only one that blocks work
//     is pushed; every decision names the answer that stands if nobody gives
//     one, and when (DG-10);
//   - the digest: once a day, and once a week for the Backlog, the automatic
//     moves and what waits are written down in one place (§8, D2).
//
// These are the rules and nothing else: they read the facts they are handed
// and answer what happens. The application applies them inside the
// transaction that holds the rows they were decided from.

// SignalExternalEffect is I3: the work has, or will have, an effect outside
// this machine — declared by the session in the closed vocabulary, or
// observed by the broker as a landing on a default branch.
const SignalExternalEffect Signal = "external_effect"

// ProposalSignals is I1–I3 in their order: the only signals a proposal rests
// on. T2's repeated_failure is not one of them — §6 makes a second failure a
// decision on a tracked item, or a proposal through I1–I3, not a reason of
// its own.
var ProposalSignals = []Signal{SignalCrossSession, SignalLongLived, SignalExternalEffect}

// Effect is one word of I3's closed vocabulary.
type Effect string

const (
	EffectDeploy      Effect = "deploy"
	EffectPublish     Effect = "publish"
	EffectPushDefault Effect = "push_default_branch"
	EffectSpend       Effect = "spend"
	EffectEmail       Effect = "email"
)

// Effects is the vocabulary, in the order it is documented.
var Effects = []Effect{EffectDeploy, EffectPublish, EffectPushDefault, EffectSpend, EffectEmail}

// defaultBranches are the landing targets the broker's record counts as a
// default branch for I3. It is a name, not a reading of the repository's
// configured default: a repository whose default is called anything else
// needs the session to declare push_default_branch.
var defaultBranches = map[string]bool{"main": true, "master": true}

// ParseEffects reads the declared effects, refusing a word outside the
// vocabulary by name: an effect this broker ignored would be a reason to ask
// that silently was not one.
func ParseEffects(words []string) ([]Effect, error) {
	seen := map[Effect]bool{}
	out := []Effect{}
	for _, w := range words {
		e := Effect(strings.TrimSpace(w))
		known := false
		for _, v := range Effects {
			if v == e {
				known = true
			}
		}
		if !known {
			names := make([]string, len(Effects))
			for i, v := range Effects {
				names[i] = string(v)
			}
			return nil, refuse(400, "unknown_effect", "%q is not an effect; the vocabulary is %s.", w,
				strings.Join(names, ", "))
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return effectIndex(out[i]) < effectIndex(out[j]) })
	return out, nil
}

func effectIndex(e Effect) int {
	for i, v := range Effects {
		if v == e {
			return i
		}
	}
	return len(Effects)
}

// Ignored is a task the signals did not count, and why: a step of other work,
// or a task nobody's session owns (§4.2's "Never proposed").
type Ignored struct {
	Task string `json:"task_id"`
	Why  string `json:"why"`
}

// Why a task does not count toward a proposal.
const (
	IgnoredAuxiliary = "auxiliary" // review, test, correction, question, clarification
	IgnoredNoRoot    = "no_root"   // detached automation, and every scheduled run (D07)
)

// SignalsOf answers the signals a line of work carries now, from the broker's
// facts about its tasks, the to-dos those tasks left, and the effects the
// session declared. The session supplies only the effects; I1 and I2 are read
// from facts, never from what an agent says of itself (§4.2, D7).
func SignalsOf(tasks []TaskFacts, todos []Todo, effects []Effect, now time.Time) ([]Signal, []Ignored) {
	has := map[Signal]bool{}
	ignored := []Ignored{}
	counted := map[string]TaskFacts{}
	for _, t := range tasks {
		switch {
		case t.Owner == "":
			ignored = append(ignored, Ignored{Task: t.Task, Why: IgnoredNoRoot})
			continue
		case auxiliary(t.Kind):
			ignored = append(ignored, Ignored{Task: t.Task, Why: IgnoredAuxiliary})
			continue
		}
		counted[t.Task] = t
		// I1: a root sent a child for this work — it needed a second session.
		has[SignalCrossSession] = true
		if OutcomeOf(t) == OutcomeLanded && t.Landing == "landed" && defaultBranches[t.LandingTarget] {
			has[SignalExternalEffect] = true
		}
	}
	for _, td := range todos {
		if _, ok := counted[td.Task]; !ok {
			continue
		}
		if td.State.Outstanding() && !td.CreatedAt.IsZero() && now.Sub(td.CreatedAt) > LongLived {
			has[SignalLongLived] = true
		}
	}
	if len(effects) > 0 {
		has[SignalExternalEffect] = true
	}
	out := []Signal{}
	for _, s := range ProposalSignals {
		if has[s] {
			out = append(out, s)
		}
	}
	return out, ignored
}

// ——— Proposals ———

// ProposalState is where a proposal is.
type ProposalState string

const (
	// ProposalPending waits for a person's answer.
	ProposalPending ProposalState = "pending"
	// ProposalAnswered has a person's answer.
	ProposalAnswered ProposalState = "answered"
	// ProposalExpired went unanswered for ProposalPolicy.Expiry: its safe
	// default stood — the work stays with its to-dos, and nothing was put on
	// a person's board (board-redesign §10 #4).
	ProposalExpired ProposalState = "expired"
	// ProposalWithdrawn was taken back by the server because the question
	// stopped being a question — its subject settled, or was tracked by
	// another route, or the rule that made it would not make it now. It is a
	// statement about the subject, not about the person: nobody failed to
	// answer, so it is neither a decline nor a default, and the row stays
	// with WithdrawnReason saying which fact ended it.
	ProposalWithdrawn ProposalState = "withdrawn"
)

// ProposalStates is every state a proposal can be read in, in the order the
// counts are listed.
var ProposalStates = []ProposalState{ProposalPending, ProposalAnswered, ProposalExpired, ProposalWithdrawn}

// Why a pending proposal was withdrawn. Each is a fact about the subject,
// read again on the sweep from the same rows the proposal was made from.
const (
	// WithdrawnSubjectTracked: a work item for this line exists now — the
	// person, or a dispatch that named it, put it on the board or the
	// Backlog. GateProposal already refuses a new proposal in this case
	// (RefuseAlreadyTracked); this is the same fact reaching one already
	// recorded.
	WithdrawnSubjectTracked = "subject_tracked"
	// WithdrawnSubjectSettled: every to-do of the line is over — it was
	// collected and landed, or it ended owing nothing. PT-3 required an owed
	// to-do to propose the line at all; this is that requirement read again.
	WithdrawnSubjectSettled = "subject_settled"
	// WithdrawnRuleSpent: the rules made this one, and the rules would not
	// make it now. Only a rule-made proposal can end this way: a person's
	// counterpart asked a question of their own, and no change of rule
	// withdraws that.
	WithdrawnRuleSpent = "rule_no_longer_applies"
)

// Answer is a person's answer to a proposal: Track / Later (backlog) / No.
type Answer string

const (
	AnswerTrack Answer = "track" // onto the board
	AnswerLater Answer = "later" // into the Backlog
	AnswerNo    Answer = "no"    // stays with its to-dos
)

// Answers is every answer, in the order a question offers them.
var Answers = []Answer{AnswerTrack, AnswerLater, AnswerNo}

// Channel is where a proposal is put in front of a person.
type Channel string

const (
	// ChannelSession is the conversation: the session asks at the end of its
	// turn, in the server's sentence, without blocking.
	ChannelSession Channel = "session"
	// ChannelToConfirm is the board's "to confirm" area, and one line in the
	// daily digest. Nothing is pushed.
	ChannelToConfirm Channel = "to_confirm"
)

// Why a proposal is, or is not, asked in the conversation. Only the first
// asks; the other three put it in the "to confirm" area.
const (
	AskHumanPresent = "human_present"
	AskHumanAbsent  = "human_absent"
	// AskBudgetExhausted: this session already asked once this turn, or the
	// day's asks are spent.
	AskBudgetExhausted = "proposal_budget_exhausted"
	// AskFromChild: a child proposed. The line of work is its root's; the
	// proposal is recorded in the root's "to confirm" area and the child asks
	// nobody.
	AskFromChild = "proposal_from_child"
	// AskByRule: the rules made it, from the broker's facts, with no session
	// in a turn to ask (§6's "Session to-do → board (escalation)"). It waits
	// in the "to confirm" area.
	AskByRule = "made_by_rule"
)

// The refusals of §4.4: nothing is recorded, nobody is asked.
const (
	RefuseBelowThreshold = "proposal_below_threshold"
	RefuseDuplicate      = "proposal_duplicate"
	RefuseAlreadyTracked = "proposal_already_tracked"
)

// Source is who made a proposal: the proposing root, one of its children, or
// the rules. It never changes who the proposal belongs to — always the root.
const (
	SourceSession = "session"
	SourceChild   = "child:" // + the child's task id
	SourceRule    = "rule"
)

// Proposal is one proposal, as the rules see it and the store keeps it.
type Proposal struct {
	ID string
	// WorkID is the line of work proposed: the id its dispatches carry, and
	// the id the work item takes if a person says track or later.
	WorkID string
	// TaskID is the dispatch the proposal was made about, when it named one.
	TaskID string
	// Session is the root the line of work belongs to — the conversation that
	// is asked, and that holds the item if it is tracked.
	Session string
	Source  string
	Project string
	Title   string
	Signals []Signal
	Effects []Effect
	// SubjectStatus is the current answer to whether the subject's to-dos
	// can settle this question. It is derived when a proposal is read, not
	// stored with it: the sweep and the screen therefore speak from the same
	// current to-dos rather than from the facts at proposal time.
	SubjectStatus SubjectStatus
	// Ask is the server's answer to "may the session ask in the
	// conversation", AskReason why, and Channel where it went.
	Ask       bool
	AskReason string
	Channel   Channel
	// Question is the one sentence the session asks with, when Ask.
	Question string

	State      ProposalState
	Answer     Answer
	AnsweredBy string
	AnsweredAt time.Time
	CreatedAt  time.Time
	ExpiresAt  time.Time
	// WithdrawnReason and WithdrawnAt are why the server took the question
	// back, and when. Empty in every other state.
	WithdrawnReason string
	WithdrawnAt     time.Time
	// AskedInlineAt is when the session reported it asked in the
	// conversation: the measurement of whether ask:false was obeyed (§4.4).
	AskedInlineAt time.Time
	Version       int64
}

// Unprompted says the session reported asking in the conversation when the
// server had said not to: the briefing was not followed.
func (p Proposal) Unprompted() bool { return !p.AskedInlineAt.IsZero() && !p.Ask }

// ProposalPolicy is the clocks and budgets of §4.3–§4.4 and §10 #4, all of
// them settings.
type ProposalPolicy struct {
	// Present is how recently a person must have sent this session a message
	// for the person to count as there (30 minutes).
	Present time.Duration
	// Expiry is how long a proposal waits before its safe default stands
	// (7 days).
	Expiry time.Duration
	// TurnAsks and DayAsks are the budget: at most this many asks in the
	// conversation per session per turn (1), and per day on this machine (3).
	TurnAsks int
	DayAsks  int
	// RuleAfter is how long a line of work is its root's to propose before
	// the rules propose it themselves (30 minutes, the same as Present). The
	// root may ask in the conversation while the person is there; a rule's
	// proposal first would take that ask away (it would be a duplicate), and
	// once Present has passed the root's own proposal would only have gone to
	// the "to confirm" area too. Work that is over by then — landed, or owing
	// nothing — is never proposed at all.
	RuleAfter time.Duration
	Location  *time.Location
}

// DefaultProposalPolicy is board-redesign §4.3, §4.4 and §10 #4.
func DefaultProposalPolicy() ProposalPolicy {
	return ProposalPolicy{Present: 30 * time.Minute, Expiry: 7 * 24 * time.Hour, TurnAsks: 1, DayAsks: 3,
		RuleAfter: 30 * time.Minute, Location: time.Local}
}

// RuleDue says the rules may propose a line whose first task was dispatched
// at first: its root has had RuleAfter to propose it.
func RuleDue(first time.Time, p ProposalPolicy, now time.Time) bool {
	return !first.IsZero() && now.Sub(first) >= p.RuleAfter
}

func (p ProposalPolicy) loc() *time.Location {
	if p.Location == nil {
		return time.UTC
	}
	return p.Location
}

// DayStart is the start of the calendar day now is in: the day budget's
// window.
func (p ProposalPolicy) DayStart(now time.Time) time.Time {
	y, m, d := now.In(p.loc()).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, p.loc())
}

// ProposalFacts is everything the gate reads, gathered inside the
// transaction that will record its answer.
type ProposalFacts struct {
	// FromChild says the caller authenticated as a child of the root; ByRule
	// that no caller did — the rules made it.
	FromChild bool
	ByRule    bool
	// Item is the work item under WorkID, and HasItem whether there is one.
	Item    Item
	HasItem bool
	// Prior is every earlier proposal of the same line of work.
	Prior []Proposal
	// Signals are the line's signals now (SignalsOf).
	Signals []Signal
	// HeardAt is when a person last sent the proposing session a message
	// through this daemon; zero when nobody has, or nobody knows.
	HeardAt time.Time
	// AskedThisTurn is the session's asks since HeardAt; AskedToday is the
	// machine's asks since the day began.
	AskedThisTurn int
	AskedToday    int
}

// Verdict is the gate's answer to a proposal it records.
type Verdict struct {
	Ask     bool
	Reason  string
	Channel Channel
}

// GateProposal is §4.4: whether a proposal is recorded at all, and whether it
// is asked in the conversation. The order is the table's: a proposal that
// changes nothing is refused before anybody's budget is spent on it.
func GateProposal(f ProposalFacts, p ProposalPolicy, now time.Time) (Verdict, error) {
	if f.HasItem {
		if f.Item.Place == PlaceBoard || f.Item.Place == PlaceBacklog {
			return Verdict{}, refuse(409, RefuseAlreadyTracked,
				"This line of work is already a work item (%s, %s): dispatch with its work_id, nobody needs to be asked.",
				f.Item.Place, f.Item.State)
		}
		// A person said this needs no following (untrack). That is an
		// answer, and the proposal would ask it again.
		return Verdict{}, refuse(409, RefuseDuplicate,
			"A person already said this line of work needs no following; it stays with its to-dos.")
	}
	declined := 0
	seen := map[Signal]bool{}
	for _, prior := range f.Prior {
		switch {
		case prior.State == ProposalWithdrawn:
			// Nobody answered it and nobody let it lapse: the server took
			// the question back because its subject had moved on. It is
			// neither a duplicate to refuse nor a decline to count, so a
			// line that comes back to life may be proposed again.
			continue
		case prior.State == ProposalPending:
			return Verdict{}, refuse(409, RefuseDuplicate,
				"This line of work is already proposed and waits for an answer (%s).", prior.ID)
		case prior.State == ProposalAnswered && prior.Answer != AnswerNo:
			return Verdict{}, refuse(409, RefuseAlreadyTracked,
				"A person already answered %s for this line of work.", prior.Answer)
		}
		declined++
		for _, s := range prior.Signals {
			seen[s] = true
		}
	}
	if len(f.Signals) == 0 {
		return Verdict{}, refuse(422, RefuseBelowThreshold,
			"Nothing here is worth a person's attention yet: no child was sent for it, no to-do of it is a day old, and no effect outside this machine was declared or landed. It stays a to-do.")
	}
	if f.ByRule && !RuleWorthy(f.Signals) {
		return Verdict{}, refuse(422, RefuseBelowThreshold, ruleBarRefusal)
	}
	if declined > 0 {
		fresh := false
		for _, s := range f.Signals {
			if !seen[s] {
				fresh = true
			}
		}
		if declined > 1 || !fresh {
			return Verdict{}, refuse(409, RefuseDuplicate,
				"This line of work was proposed and not taken. It is proposed again only once, and only on a signal it did not have then.")
		}
	}
	switch {
	case f.ByRule:
		return Verdict{Reason: AskByRule, Channel: ChannelToConfirm}, nil
	case f.FromChild:
		return Verdict{Reason: AskFromChild, Channel: ChannelToConfirm}, nil
	case f.HeardAt.IsZero() || now.Sub(f.HeardAt) > p.Present:
		return Verdict{Reason: AskHumanAbsent, Channel: ChannelToConfirm}, nil
	case f.AskedThisTurn >= p.TurnAsks || f.AskedToday >= p.DayAsks:
		return Verdict{Reason: AskBudgetExhausted, Channel: ChannelToConfirm}, nil
	}
	return Verdict{Ask: true, Reason: AskHumanPresent, Channel: ChannelSession}, nil
}

// signalWords are the reasons a question gives, in the person's language.
var signalWords = map[Signal]string{
	SignalCrossSession:   "它派出了另一個 session",
	SignalLongLived:      "它的待辦已經開了超過 24 小時",
	SignalExternalEffect: "它有外部效果",
}

// Question is the one sentence a session asks with. It is the server's, so
// that what is asked is what was decided: §4.4 has the session ask
// "using the sentence in the response".
func Question(title string, signals []Signal, effects []Effect) string {
	why := []string{}
	for _, s := range signals {
		w := signalWords[s]
		if s == SignalExternalEffect && len(effects) > 0 {
			names := make([]string, len(effects))
			for i, e := range effects {
				names[i] = string(e)
			}
			w += "（" + strings.Join(names, "、") + "）"
		}
		why = append(why, w)
	}
	return fmt.Sprintf("要不要把「%s」放上看板追蹤？（%s）回覆：追蹤／之後（Backlog）／不用", title, strings.Join(why, "；"))
}

// ParseAnswer reads a person's answer.
func ParseAnswer(s string) (Answer, error) {
	for _, a := range Answers {
		if string(a) == strings.TrimSpace(s) {
			return a, nil
		}
	}
	return "", refuse(400, "invalid_answer", "An answer to a proposal is track, later or no.")
}

// AnswerProposal records a person's answer. A proposal whose wait passed can
// still be answered: the expiry was a default, and a person's word beats a
// default (DG-10). One that was answered is not answered again.
func AnswerProposal(p Proposal, a Answer, actor string, now time.Time) (Proposal, error) {
	if p.State == ProposalAnswered {
		return Proposal{}, refuse(409, "proposal_answered", "This proposal was already answered (%s).", p.Answer)
	}
	if p.State == ProposalWithdrawn {
		// The expiry is a default about a person, and a person's word beats
		// it. A withdrawal is a fact about the subject — the line is already
		// tracked, or already over — and answering `track` would put
		// finished work on a board. What a person wants followed after that
		// is an item they make (POST /v1/work/items), which says so.
		return Proposal{}, refuse(409, "proposal_withdrawn",
			"This proposal was withdrawn (%s): its subject moved on, so there is nothing left to answer. Make an item on the board if you want it followed.",
			p.WithdrawnReason)
	}
	p.State, p.Answer, p.AnsweredBy, p.AnsweredAt = ProposalAnswered, a, actor, now
	return p, nil
}

// ExpireProposal is the safe default of §10 #4: a proposal nobody answered
// for its wait is let go, and the work stays with its to-dos.
func ExpireProposal(p Proposal, now time.Time) (Proposal, bool) {
	if p.State != ProposalPending || p.ExpiresAt.IsZero() || now.Before(p.ExpiresAt) {
		return p, false
	}
	p.State = ProposalExpired
	return p, true
}

const ruleBarRefusal = "A dispatch on its own is not worth asking a person about: on a machine where sending a child is " +
	"how work is done, every dispatch would be a question. The rules propose a line only once it has outlived a day " +
	"of nobody following it, or has an effect outside this machine. Its root may still propose it."

// RuleWorthy is the bar a proposal the rules make for themselves must clear:
// a signal other than I1.
//
// I1 (a root sent a child) was calibrated for a machine where a second
// session was an event. Where dispatching is how the work is done, I1 alone
// is carried by every line there is, so a rule resting on it asks one
// identically-shaped question per dispatch — 23 of them in one day here on
// 2026-09-20, none of which was ever answered. I2 (a to-do of it owed past a
// day) and I3 (an effect outside this machine) are the signals that pick out
// a line worth interrupting somebody for, and each is self-limiting.
//
// It binds the rules only. A root that judges a line worth a person's
// attention still proposes it on I1 alone, and so does a child: a proposal
// somebody chose to make is not the one that floods.
func RuleWorthy(signals []Signal) bool {
	for _, s := range signals {
		if s != SignalCrossSession {
			return true
		}
	}
	return false
}

// SubjectFacts is what the sweep reads back about a pending proposal's
// subject — the line of work it names — to decide whether it is still a
// question. They are the same rows the proposal was made from: the work item
// under its work id, and the to-dos of that line.
type SubjectFacts struct {
	// HasItem says a work item exists for the line now.
	HasItem bool
	// Todos are every to-do on the line. A line with none is not settled:
	// a leftover's subject is a line nobody has dispatched anything for, and
	// "all of nothing is over" would retire the question before it was asked
	// (leftovers.go).
	Todos []Todo
}

// SubjectStatus is the three-way answer to a proposal's to-do evidence. An
// empty list is not an open subject: it is no evidence with which the sweep
// can decide that the subject settled.
type SubjectStatus string

const (
	SubjectUnknown SubjectStatus = "unknown"
	SubjectOwed    SubjectStatus = "owed"
	SubjectSettled SubjectStatus = "settled"
)

// SubjectStatusOf reads the evidence without collapsing "none observed" into
// "still owed". That distinction is shown to the person for proposals whose
// subject the sweep cannot re-decide from to-dos.
func SubjectStatusOf(todos []Todo) SubjectStatus {
	if len(todos) == 0 {
		return SubjectUnknown
	}
	for _, td := range todos {
		if td.State.Outstanding() {
			return SubjectOwed
		}
	}
	return SubjectSettled
}

// WithdrawProposal is the exit the lifecycle was missing: a pending proposal
// whose question has stopped being a question is taken back by the server,
// with the fact that ended it.
//
// A proposal has two exits that need nobody — an answer and the wait — and
// both are about the person. Neither is about the subject, so a line that
// landed an hour after it was proposed went on asking "shall I follow this?"
// for the rest of its seven days. The sweep re-decides every pending
// proposal from the rows it was made from, in the order of what is truest:
// tracked by another route, settled, or made by a rule that no longer
// applies.
//
// Withdrawing is not deleting. The row stays and its state says why, which is
// what makes "it went away" readable afterwards (GET /v1/work/proposals
// ?state=withdrawn) instead of a question a person half-remembers.
func WithdrawProposal(p Proposal, f SubjectFacts, now time.Time) (Proposal, bool) {
	if p.State != ProposalPending {
		return p, false
	}
	reason := ""
	switch {
	case f.HasItem:
		reason = WithdrawnSubjectTracked
	case SubjectStatusOf(f.Todos) == SubjectSettled:
		reason = WithdrawnSubjectSettled
	case p.Source == SourceRule && !RuleWorthy(p.Signals):
		reason = WithdrawnRuleSpent
	default:
		return p, false
	}
	p.State, p.WithdrawnReason, p.WithdrawnAt = ProposalWithdrawn, reason, now
	return p, true
}

// Placing is where a proposal answered track or later puts its work item:
// the item, and the change that records it (a move from `proposal`).
func Placing(p Proposal, tasks []TaskFacts, now time.Time) (Item, Change, bool) {
	var place Place
	switch p.Answer {
	case AnswerTrack:
		place = PlaceBoard
	case AnswerLater:
		place = PlaceBacklog
	default:
		return Item{}, Change{}, false
	}
	it := Item{ID: p.WorkID, Project: p.Project, Title: p.Title, CreatedAt: now, CreatedBy: p.AnsweredBy,
		Place: place, PlacedAt: now}
	c := Change{From: PlaceProposal, To: place, Trigger: "proposal_" + string(p.Answer), Actor: p.AnsweredBy,
		Evidence: map[string]any{"proposal": p.ID, "signals": p.Signals, "answer": string(p.Answer)}}
	if p.Leftover() {
		// Which task said it did not do this. It is the whole provenance of a
		// row that nobody typed: the item's first move names the delivery it
		// came out of, and `GET /v1/work/items/{id}/moves` reads it back.
		c.Evidence["leftover_of_task"] = p.TaskID
	}
	if place == PlaceBoard {
		// The person chose to follow the whole line, so its stay begins with
		// the first task it already sent: a delivery made before the answer
		// is this stay's delivery.
		since := now
		for _, t := range tasks {
			if t.CreatedAt.Before(since) {
				since = t.CreatedAt
			}
		}
		owner := p.Session
		if owner == "" {
			owner = OwnerUser
		}
		it.State, it.Owner, it.Commitment, it.Since, it.EvidenceAt = ItemActive, owner, CommitAssigned, since, now
		c.Evidence["owner"] = owner
	} else {
		it.State, it.ReviewedAt = ItemPlanned, now
	}
	c.State = it.State
	return it, c, true
}

// PlaceProposal is the from of the move a proposal's answer writes.
const PlaceProposal Place = "proposal"

// ——— Decisions ———

// DecisionState is where a decision is.
type DecisionState string

const (
	DecisionOpen      DecisionState = "open"
	DecisionAnswered  DecisionState = "answered"
	DecisionDefaulted DecisionState = "defaulted"
)

// PushState is what happened to a blocking decision's one push.
type PushState string

const (
	PushNone          PushState = "none"    // not blocking: nothing is pushed
	PushPending       PushState = "pending" // recorded, not yet sent
	PushSent          PushState = "sent"
	PushNotSubscribed PushState = "not_subscribed"
	PushOverBudget    PushState = "over_budget"
	PushFailed        PushState = "failed"
	// PushUnknown: the daemon stopped between recording the push and
	// recording its outcome. It is not sent again — a person pushed twice is
	// worse than one who reads it on the board (DG-7).
	PushUnknown PushState = "unknown"
)

// Option is one answer a decision offers.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Decision is one thing a session needs a person to decide ("Waiting on you").
type Decision struct {
	ID       string
	Session  string
	WorkID   string
	TaskID   string
	Project  string
	Question string
	Options  []Option
	// Default is the option that stands if nobody answers by DueAt.
	Default string
	// Blocking says the work stops until it is answered. Only a blocking
	// decision is pushed.
	Blocking bool

	State      DecisionState
	Answer     string
	AnsweredBy string
	AnsweredAt time.Time
	CreatedAt  time.Time
	DueAt      time.Time
	Push       PushState
	PushedAt   time.Time
	Version    int64
}

// DecisionPolicy is a decision's clock.
type DecisionPolicy struct {
	// Due is how long an unanswered decision waits before its default stands
	// when the session names no due (7 days, as a proposal's and a closure's
	// wait, board-redesign §10 #4, #5); MinDue and MaxDue bound what a session
	// may name.
	Due    time.Duration
	MinDue time.Duration
	MaxDue time.Duration
}

// DefaultDecisionPolicy is 7 days, bounded to 1 hour … 7 days.
func DefaultDecisionPolicy() DecisionPolicy {
	return DecisionPolicy{Due: 7 * 24 * time.Hour, MinDue: time.Hour, MaxDue: 7 * 24 * time.Hour}
}

// Field bounds of a decision a session asks.
const (
	decisionQuestionLimit = 500
	optionLabelLimit      = 80
	optionIDLimit         = 32
	decisionOptionsMin    = 2
	decisionOptionsLimit  = 4
)

// NewDecision is a session's question, admitted, or the refusal. A decision
// without a safe default is refused: nobody answering is the ordinary case,
// and what happens then is decided now, not when it happens (DG-10).
func NewDecision(d Decision, due time.Duration, p DecisionPolicy, now time.Time) (Decision, error) {
	d.Question = strings.TrimSpace(d.Question)
	if d.Question == "" || utf8.RuneCountInString(d.Question) > decisionQuestionLimit {
		return Decision{}, refuse(400, "invalid_question", "question is 1 to %d characters.", decisionQuestionLimit)
	}
	if len(d.Options) < decisionOptionsMin || len(d.Options) > decisionOptionsLimit {
		return Decision{}, refuse(400, "invalid_options", "A decision offers %d to %d options.", decisionOptionsMin,
			decisionOptionsLimit)
	}
	ids := map[string]bool{}
	for i, o := range d.Options {
		o.ID, o.Label = strings.TrimSpace(o.ID), strings.TrimSpace(o.Label)
		if !optionID(o.ID) || ids[o.ID] {
			return Decision{}, refuse(400, "invalid_options",
				"Each option has a distinct id: 1 to %d lowercase letters, digits, _ or -.", optionIDLimit)
		}
		if o.Label == "" || utf8.RuneCountInString(o.Label) > optionLabelLimit {
			return Decision{}, refuse(400, "invalid_options", "Each option has a label of 1 to %d characters.", optionLabelLimit)
		}
		ids[o.ID] = true
		d.Options[i] = o
	}
	switch {
	case d.Default == "":
		return Decision{}, refuse(400, "decision_default_required",
			"A decision names the option that stands if nobody answers: default.")
	case !ids[d.Default]:
		return Decision{}, refuse(400, "decision_default_unknown", "default must be the id of one of the options.")
	}
	if due == 0 {
		due = p.Due
	}
	if due < p.MinDue || due > p.MaxDue {
		return Decision{}, refuse(400, "invalid_due", "due_in_minutes is between %d and %d.",
			int(p.MinDue/time.Minute), int(p.MaxDue/time.Minute))
	}
	d.State, d.CreatedAt, d.DueAt = DecisionOpen, now, now.Add(due)
	d.Push = PushNone
	if d.Blocking {
		d.Push = PushPending
	}
	return d, nil
}

func optionID(s string) bool {
	if s == "" || len(s) > optionIDLimit {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// AnswerDecision records a person's answer. A decision whose default already
// stood is closed: the session may have acted on it, and a later answer would
// contradict what was done.
func AnswerDecision(d Decision, option, actor string, now time.Time) (Decision, error) {
	if d.State != DecisionOpen {
		return Decision{}, refuse(409, "decision_closed", "This decision is closed (%s: %s).", d.State, d.Answer)
	}
	known := false
	for _, o := range d.Options {
		if o.ID == option {
			known = true
		}
	}
	if !known {
		return Decision{}, refuse(400, "invalid_answer", "The answer is the id of one of the decision's options.")
	}
	d.State, d.Answer, d.AnsweredBy, d.AnsweredAt = DecisionAnswered, option, actor, now
	return d, nil
}

// DefaultDecision is the default standing when nobody answered by the due.
func DefaultDecision(d Decision, now time.Time) (Decision, bool) {
	if d.State != DecisionOpen || now.Before(d.DueAt) {
		return d, false
	}
	d.State, d.Answer, d.AnsweredBy, d.AnsweredAt = DecisionDefaulted, d.Default, ActorRule, now
	return d, true
}

// DecisionChange is the move a closed decision writes on the board item it is
// about: nothing moves, and a person's answer — or the default standing — is
// the item's newest fact, so its quiet clock starts again from here (§5.1).
func DecisionChange(d Decision, it Item, now time.Time) Change {
	trigger := "decision_answered"
	if d.State == DecisionDefaulted {
		trigger = "decision_defaulted"
	}
	return Change{From: it.Place, To: it.Place, State: it.State, Trigger: trigger, Actor: d.AnsweredBy,
		EvidenceAt: now, Evidence: map[string]any{"decision": d.ID, "answer": d.Answer}}
}

// ——— The digest ———

// DigestKind is which digest.
type DigestKind string

const (
	DigestDaily  DigestKind = "daily"
	DigestWeekly DigestKind = "weekly"
)

// DigestPolicy is the digest's clocks.
type DigestPolicy struct {
	// ClosureAsk is how long a delivery waits to be closed before the daily
	// digest asks a person about it, once (3 days, §5.1). ClosureWait (Policy)
	// then runs from that ask.
	ClosureAsk time.Duration
	// BacklogStale is how long a Backlog item may go unreviewed before the
	// weekly digest asks whether to keep it (30 days, §5.3).
	BacklogStale time.Duration
	Location     *time.Location
}

// DefaultDigestPolicy is 3 days and 30 days.
func DefaultDigestPolicy() DigestPolicy {
	return DigestPolicy{ClosureAsk: 3 * 24 * time.Hour, BacklogStale: 30 * 24 * time.Hour, Location: time.Local}
}

func (p DigestPolicy) loc() *time.Location {
	if p.Location == nil {
		return time.UTC
	}
	return p.Location
}

// DigestWindow is the digest of kind due at now: its key and the window it
// covers — for the daily, the calendar day before now's; for the weekly, the
// seven days before now's week began on Monday.
func DigestWindow(kind DigestKind, now time.Time, p DigestPolicy) (key string, from, to time.Time) {
	y, m, d := now.In(p.loc()).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, p.loc())
	if kind == DigestWeekly {
		back := (int(today.Weekday()) + 6) % 7 // days since Monday
		monday := today.AddDate(0, 0, -back)
		wy, ww := monday.ISOWeek()
		return fmt.Sprintf("weekly:%04d-W%02d", wy, ww), monday.AddDate(0, 0, -7), monday
	}
	return "daily:" + today.AddDate(0, 0, -1).Format("2006-01-02"), today.AddDate(0, 0, -1), today
}

// ClosureAskDue says the daily digest asks a person about this delivery now:
// it has waited ClosureAsk since the fact that delivered it, and nobody has
// been asked yet.
func ClosureAskDue(it Item, d Derived, p DigestPolicy, now time.Time) bool {
	return it.Place == PlaceBoard && it.State == ItemAwaitingClosure && d.State == ItemAwaitingClosure &&
		it.AskedAt.IsZero() && !d.LastEvidenceAt.IsZero() && now.Sub(d.LastEvidenceAt) >= p.ClosureAsk
}

// ClosureAsked is the change that records the ask: from here the closure
// wait runs (ruleUnconfirmed).
func ClosureAsked(it Item, digest string) Change {
	return Change{From: it.Place, To: it.Place, State: it.State, Asked: true, Trigger: TriggerClosureAsked,
		Actor: ActorRule, Evidence: map[string]any{"digest": digest}}
}

// TriggerClosureAsked is the move a digest's closure question writes.
const TriggerClosureAsked = "closure_asked"

// BacklogStale says a planned item has not been looked at for the window.
func BacklogStale(it Item, p DigestPolicy, now time.Time) bool {
	if it.Place != PlaceBacklog || it.State != ItemPlanned {
		return false
	}
	seen := it.ReviewedAt
	if seen.IsZero() || it.PlacedAt.After(seen) {
		seen = it.PlacedAt
	}
	return now.Sub(seen) >= p.BacklogStale
}

// Automatic says a move was made by a rule or by the broker's facts rather
// than by a person. D2 is why that matters: "an automatic move always
// appears in the daily digest".
func Automatic(actor string) bool {
	return actor == ActorBroker || actor == ActorRule || strings.HasPrefix(actor, "root:")
}
