package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Linger: closing a finished child's tab a little after it finished
// (broker-design #26, E5; docs/design-decisions.md D12).
//
// The Swift app kept this deadline in memory, and a restart forgot it:
// "Seventeen of the eighteen tabs left standing had a restart inside their
// three minutes" (`3a7adb8e`). Its fix is carried whole:
//
//   - the deadline is a row in the store, written by the settlement that owes
//     it, in the same transaction (store/handover.go);
//   - a deadline that passed while no broker was running is given twenty
//     seconds from this broker's first pass, so the first reading of the
//     machine is in before anything is decided — D12 puts that grace here,
//     and nowhere near the task's own timeout;
//   - "a reading with no terminals in it at all is no longer allowed to
//     decide": a tab is dropped as already gone only on a reading that saw
//     terminals and whose source for that tab answered completely. Anything
//     else waits.
//
// Both backends are lingered, by one rule (lingerStepFor). A tmux child is
// closed only while the pane it recorded is still one of the panes of the
// session named for the task, and an iTerm2 child by the session id iTerm2
// gave back, which it never gives to another session — after the child's own
// job in it has been ended, because iTerm2 asks a person before it closes a
// tab with a job running (runCloseChild, terminal/iterm_close_darwin.go). Until
// this change only tmux was: a linger was decided on a reading that answered
// completely for the tab's source, and on a Mac with a window iTerm2 will not
// list that source never does — so every finished iTerm2 child stayed open,
// and the session list only grew. A close needs no complete answer, only the
// tab itself, seen, at rest; it is the tab's *absence* that needs one.

// LingerDefault is the Swift app's `orchestrator_child_linger`, 180 seconds
// (Config.swift:478).
const LingerDefault = 180 * time.Second

// lingerRestartGrace is the Swift app's `restartGrace` (Orchestrator.swift:6567).
const lingerRestartGrace = 20 * time.Second

// itermCloseQuiet is how long no iTerm2 close is made after one iTerm2 did not
// answer. Such a close has already held the beat for its whole limit, and
// while iTerm2 is that busy — or holding a dialog for a person — the next
// close would only do the same.
const itermCloseQuiet = time.Minute

// lingerGiveUp is how long past its deadline a linger nothing could decide is
// still asked about. An iTerm2 tab a person closed is never seen again, and on
// a Mac where one window will not list, never seen gone either; after a day
// the row is dropped with the reason, and the tab — if it is there at all — is
// left as it is.
const lingerGiveUp = 24 * time.Hour

func (b *Broker) childLinger() time.Duration {
	if b.ChildLinger == nil {
		return LingerDefault
	}
	return b.ChildLinger()
}

// The tab policy: what a task's end does to its child's tab, decided in one
// place (tabPolicy) and named by one of these rules. The rule is readable
// where it matters: CHILD.md states it for the task before it starts
// (tabPolicyBrief), the task's own answer carries it as `tab`
// (GET /v1/orchestrator/tasks/{id}) so that reading the child's files is not
// the only way to know, the settlement's event carries the plan that applied,
// and a close still owed is a row in the store.
const (
	// TabRuleLinger: an unscheduled task that ended in success or failure is
	// closed `orchestrator_child_linger` after it ended.
	TabRuleLinger = "child_linger"
	// TabRuleLingerOff: `orchestrator_child_linger` is negative, and every
	// finished unscheduled child is left open, a session of its own.
	TabRuleLingerOff = "child_linger_off"
	// TabRuleUnfinished: an unscheduled task that timed out or was cancelled
	// is left open — its child may still be working, and a person may want to
	// see why it did not finish.
	TabRuleUnfinished = "unfinished_left_open"
	// The three a scheduled task's `close_tab` names, the Swift app's
	// `scheduledCloseAt`: closed as soon as it may be, after a success only,
	// after any end, or never.
	TabRuleScheduleOnSuccess = "schedule_on_success"
	TabRuleScheduleAlways    = "schedule_always"
	TabRuleScheduleNever     = "schedule_never"
	// TabRuleSpawnFailed: a child that never started is closed at once when
	// its tab is there (closeChild, D11), whatever else is set.
	TabRuleSpawnFailed = "spawn_failed"
)

// The three values of a schedule's close_tab (domain/schedule.CloseTab), as a
// scheduled task's record keeps them.
const (
	closeTabOnSuccess = "on_success"
	closeTabAlways    = "always"
	closeTabNever     = "never"
)

// TabPlan is what one end does to a task's child's tab: the rule that decided
// it, whether the tab is closed, and how long after the end.
type TabPlan struct {
	Rule  string
	Close bool
	After time.Duration
}

// TabEnd is one way a task can end, and what that end does to its tab.
type TabEnd struct {
	End  State
	Plan TabPlan
}

// The two settings a task's tab rules come from, named wherever the policy is
// read so that a reader knows which one to change.
const (
	TabSettingLinger   = "orchestrator_child_linger"
	TabSettingCloseTab = "close_tab"
)

// tabEndStates are the ends a task that started can reach, in the order both
// CHILD.md and the task's answer state them. spawn_failed is not one of them:
// it is an end that happened instead of the work, and it reaches
// TabPolicy.Applied on its own.
var tabEndStates = []State{StateSuccess, StateFailure, StateTimeout, StateCancelled}

// TabPolicy is one task's whole tab policy: which setting decided it, what
// each way of ending does, and — once the task has ended — the rule that
// applied and when the close it asks for falls due.
//
// One value, two readers, and that is the point. CHILD.md's section is
// rendered from it before the work starts (tabPolicyBrief) and the task's
// answer on the wire is projected from it (contract.BrokerTab, brokerTab):
// neither is a description of the other, so there is no second description to
// drift. What they could still both be is wrong together — a guard comparing
// two texts never caught that either — and the only place that can be fixed is
// tabPolicy's own table.
type TabPolicy struct {
	// Setting is `orchestrator_child_linger` or a schedule's `close_tab`, and
	// Value is what it was set to: the linger's seconds, "-1" when it is off,
	// or one of close_tab's three words.
	Setting string
	Value   string
	// Ends is what each of tabEndStates does to this tab.
	Ends []TabEnd
	// Decided says the task has ended, and Applied is the end that happened
	// with the rule it chose. Before that there is no single rule to name:
	// which one applies is still the task's to decide by how it ends.
	Decided bool
	Applied TabEnd
	// CloseAt is when the close Applied asks for falls due, zero when none is
	// owed. It is the rule's deadline and not an observation: whether the
	// close was made is the linger's own events (task.child.linger.*), and a
	// tab still open well past this is the thing worth looking into.
	CloseAt time.Time
}

// scheduleCloseTab is a scheduled task's close_tab. A record written before
// the field existed reads as the schedule default, on_success.
func (r Record) scheduleCloseTab() string {
	switch r.ScheduleCloseTab {
	case closeTabAlways, closeTabNever:
		return r.ScheduleCloseTab
	}
	return closeTabOnSuccess
}

// tabPolicy is the whole policy. A close it asks for is still made only while
// the tab is the child's own, at rest, and nobody has used it since the task
// ended (lingerStepFor): the rule says when a close is owed, never that it
// may be forced.
func tabPolicy(r Record, end State, linger time.Duration) TabPlan {
	if end == StateSpawnFailed {
		return TabPlan{Rule: TabRuleSpawnFailed, Close: true}
	}
	if r.ScheduleID != "" {
		switch r.scheduleCloseTab() {
		case closeTabNever:
			return TabPlan{Rule: TabRuleScheduleNever}
		case closeTabAlways:
			return TabPlan{Rule: TabRuleScheduleAlways, Close: true}
		}
		return TabPlan{Rule: TabRuleScheduleOnSuccess, Close: end == StateSuccess}
	}
	if linger < 0 {
		return TabPlan{Rule: TabRuleLingerOff}
	}
	if end == StateSuccess || end == StateFailure {
		return TabPlan{Rule: TabRuleLinger, Close: true, After: linger}
	}
	return TabPlan{Rule: TabRuleUnfinished}
}

// tabPlanPayload is a plan as a settlement's event carries it.
func tabPlanPayload(p TabPlan) map[string]any {
	return map[string]any{"rule": p.Rule, "close": p.Close, "after_seconds": int64(p.After / time.Second)}
}

// tabPolicyOf answers one task's whole policy, the value CHILD.md and the
// task's answer are both made of.
func tabPolicyOf(r Record, linger time.Duration) TabPolicy {
	p := TabPolicy{Setting: TabSettingLinger, Value: strconv.FormatInt(int64(linger/time.Second), 10)}
	if linger < 0 {
		p.Value = "-1"
	}
	if r.ScheduleID != "" {
		p.Setting, p.Value = TabSettingCloseTab, r.scheduleCloseTab()
	}
	for _, end := range tabEndStates {
		p.Ends = append(p.Ends, TabEnd{End: end, Plan: tabPolicy(r, end, linger)})
	}
	if r.State.Terminal() {
		p.Decided = true
		p.Applied = TabEnd{End: r.State, Plan: tabPolicy(r, r.State, linger)}
		if p.Applied.Plan.Close && !r.FinishedAt.IsZero() {
			p.CloseAt = r.FinishedAt.Add(p.Applied.Plan.After)
		}
	}
	return p
}

// TabPolicy is this task's tab policy under the linger this broker is running
// with: what CHILD.md tells the child, and what GET /v1/orchestrator/tasks/{id}
// answers, from the one value.
func (b *Broker) TabPolicy(r Record) TabPolicy {
	return tabPolicyOf(r, b.childLinger())
}

// TabPlanSentence is one plan in words — the same words in CHILD.md and in
// anything else that reads a plan back to a person.
func TabPlanSentence(p TabPlan) string {
	switch {
	case p.Close && p.After > 0:
		return fmt.Sprintf("closed about %d seconds after it ends", int64(p.After/time.Second))
	case p.Close:
		return "closed as soon as it is at rest"
	}
	return "left open"
}

// tabPolicyBrief is the policy as CHILD.md states it for one task: which rule,
// and what each way of ending does to this tab. Every line of it is rendered
// from tabPolicyOf, so the section cannot say something the task's answer does
// not.
func tabPolicyBrief(r Record, linger time.Duration) []string {
	policy := tabPolicyOf(r, linger)
	intro := "For this task the rule is "
	switch {
	case policy.Setting == TabSettingCloseTab:
		intro += fmt.Sprintf("its schedule's `close_tab: %s`:", policy.Value)
	case policy.Value == "-1":
		intro += "`orchestrator_child_linger` = -1, which keeps every finished child open:"
	default:
		intro += fmt.Sprintf("`%s` = %s seconds:", policy.Setting, policy.Value)
	}
	lines := []string{intro, ""}
	for i, e := range policy.Ends {
		stop := ";"
		if i == len(policy.Ends)-1 {
			stop = "."
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (`%s`)%s", e.End, TabPlanSentence(e.Plan), e.Plan.Rule, stop))
	}
	return lines
}

// lingerFor is the close a settlement owes its child's tab, if any: what
// tabPolicy asks for, of a tab this broker can prove it opened and can close.
// A spawn_failed child is closed by the settlement itself (closeChild), and
// owes no linger.
func (b *Broker) lingerFor(r Record, now time.Time) (store.Linger, bool) {
	plan := tabPolicy(r, r.State, b.childLinger())
	if !plan.Close || r.State == StateSpawnFailed || r.ChildTerminalID == "" || b.Launcher == nil {
		return store.Linger{}, false
	}
	l := store.Linger{Task: r.ID, Backend: r.ChildBackend, Pane: r.ChildTerminalID,
		Deadline: now.Add(plan.After), CreatedAt: now}
	switch r.ChildBackend {
	case "tmux":
		l.Session = ChildSessionName(r.ID)
	case "iterm":
		if _, ok := b.Launcher.(itermCloser); !ok {
			return store.Linger{}, false
		}
	default:
		return store.Linger{}, false
	}
	return l, true
}

// lingerWatch is what the beat has seen of one lingering tab since its task
// ended. It is how "nobody has used it since" is told: the child's last turn
// ends at rest, so a turn that begins after the tab was seen at rest, or a
// different conversation in it, is somebody else's — a person who went on
// working in the tab, a root that sent it a message. Such a tab is left to
// them. Waiting is not counted: an assistant can put a question or a notice on
// its own screen without anybody having typed, and it only holds the close
// back. It is kept in memory: a restart forgets it, and the next close is then
// decided on the tab's state alone, as before.
type lingerWatch struct {
	idle         bool
	conversation string
	takenOver    string
}

func (w *lingerWatch) see(s session.Session) {
	if s.ConversationID != "" {
		switch {
		case w.conversation == "":
			w.conversation = s.ConversationID
		case s.ConversationID != w.conversation && w.takenOver == "":
			w.takenOver = "another conversation is running in it"
		}
	}
	switch s.State {
	case session.StateIdle:
		w.idle = true
	case session.StateWorking:
		if w.idle && w.takenOver == "" {
			w.takenOver = "a turn began in it after the child's last one had ended"
		}
	}
}

// lingerMemory is what closeLingers keeps between passes: each lingering tab's
// lingerWatch, and when an iTerm2 close may next be made. It is locked because
// a pass that overlaps another is counted, not refused (observe.go), and two
// passes writing one map at once would stop the daemon.
type lingerMemory struct {
	mu         sync.Mutex
	watch      map[string]*lingerWatch
	quietUntil time.Time
}

// observe records what this reading shows of every lingering tab, due or not,
// and forgets the tabs no longer owed a close.
func (m *lingerMemory) observe(rows []store.Linger, rd reading) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.watch == nil {
		m.watch = map[string]*lingerWatch{}
	}
	owed := make(map[string]bool, len(rows))
	for _, l := range rows {
		owed[l.Task] = true
		w := m.watch[l.Task]
		if w == nil {
			w = &lingerWatch{}
			m.watch[l.Task] = w
		}
		if s, present := rd.session(l.Pane); present {
			w.see(s)
		}
	}
	for task := range m.watch {
		if !owed[task] {
			delete(m.watch, task)
		}
	}
}

// seen is a copy of what has been seen of one tab.
func (m *lingerMemory) seen(task string) lingerWatch {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w := m.watch[task]; w != nil {
		return *w
	}
	return lingerWatch{}
}

func (m *lingerMemory) forget(task string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.watch, task)
}

// quiet is whether an iTerm2 close must wait at now; quietFor starts the wait.
func (m *lingerMemory) quiet(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return now.Before(m.quietUntil)
}

func (m *lingerMemory) quietFor(now time.Time, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quietUntil = now.Add(d)
}

// lingerStep is what one due linger becomes on one reading.
type lingerStep int

const (
	lingerWait  lingerStep = iota // decide on a later reading
	lingerGone                    // nothing of the child's is left to close
	lingerLeave                   // somebody else is using it; it is theirs
	lingerClose
)

// lingerStepFor decides one due linger, for either backend.
//
//   - Absent is decided only by a reading that saw terminals and whose source
//     for this tab answered completely. On a Mac where iTerm2 will not list
//     one window, an iTerm2 tab not seen is not a tab gone, and it waits.
//   - Present needs no complete answer: the tab was seen, by its own id. It is
//     left alone for good when a different assistant is running in it or it
//     has been used since the task ended (lingerWatch), and closed only while
//     it is plainly at rest. Working is the child still finishing a turn;
//     waiting is a question somebody may be about to answer; unknown is a
//     screen this daemon could not read — and a close on unknown is the
//     removal on unknown DG-7 forbids.
func lingerStepFor(l store.Linger, rd reading, w lingerWatch, assistant string) (lingerStep, string) {
	s, present := rd.session(l.Pane)
	if !present {
		if len(rd.sessions) > 0 && rd.sourceComplete(l.Backend) {
			return lingerGone, "its terminal answered completely and the tab is not there"
		}
		return lingerWait, ""
	}
	if s.IsAssistant() && assistant != "" && string(s.Assistant) != assistant {
		return lingerLeave, "a different assistant is running in it"
	}
	if w.takenOver != "" {
		return lingerLeave, w.takenOver
	}
	if s.State != session.StateIdle {
		return lingerWait, ""
	}
	return lingerClose, ""
}

// closeLingers closes the tabs whose linger is over, and answers how many
// closes it recorded. It runs on the beat, against the pass's one reading.
//
// At most one iTerm2 close is made per pass, and none for itermCloseQuiet
// after one iTerm2 did not answer: an iTerm2 close holds the beat for as long
// as iTerm2 takes, and a busy iTerm2 would otherwise hold it once per tab.
func (b *Broker) closeLingers(ctx context.Context, rd reading) int {
	now := b.now()
	if b.lingerStarted.IsZero() {
		b.lingerStarted = now
	}
	rows, err := b.Store.Lingers(ctx)
	if err != nil {
		return 0
	}
	b.lingers.observe(rows, rd)
	n := 0
	itermTaken := false
	for _, l := range rows {
		due := l.Deadline
		if !due.After(b.lingerStarted) {
			// It passed while no broker was watching: the first reading of
			// this one decides, not the clock that ran out in the dark.
			due = b.lingerStarted.Add(lingerRestartGrace)
		}
		if now.Before(due) {
			continue
		}
		iterm := l.Backend == "iterm"
		// The record is read only for a tab that is there: a row nothing can
		// decide yet costs the pass nothing but this lookup.
		var r Record
		_, present := rd.session(l.Pane)
		if present {
			if held, _, err := b.Record(ctx, l.Task); err == nil {
				r = held
			}
		}
		step, why := lingerStepFor(l, rd, b.lingers.seen(l.Task), r.Assistant)
		if step == lingerClose && iterm && (itermTaken || b.lingers.quiet(now)) {
			continue
		}
		body := map[string]any{"task": l.Task, "backend": l.Backend, "pane": l.Pane, "session": l.Session,
			"deadline": l.Deadline.Unix(), "present": present}
		if why != "" {
			body["why"] = why
		}
		var effects []store.Effect
		var kind string
		switch step {
		case lingerWait:
			if now.Sub(l.Deadline) < lingerGiveUp {
				continue
			}
			kind = "task.child.linger.expired"
			body["why"] = "nothing decided it within a day of its deadline; the tab is left as it is"
		case lingerGone:
			kind = "task.child.linger.gone"
		case lingerLeave:
			kind = "task.child.linger.left"
		case lingerClose:
			kind = "task.child.linger.due"
			c := closeChildEffect{Pane: l.Pane, Session: l.Session}
			if iterm {
				c = closeChildEffect{Backend: "iterm", Pane: l.Pane}
				if !r.FinishedAt.IsZero() {
					c.Before = r.FinishedAt.Unix()
				}
			}
			raw, _ := json.Marshal(c)
			effects = []store.Effect{{Kind: EffectCloseChild, Subject: l.Task, Payload: raw}}
		}
		payload, _ := json.Marshal(body)
		ids, err := b.Store.TakeLinger(ctx, l.Task, []store.Event{{Kind: kind, Subject: l.Task, Payload: payload}}, effects)
		if err != nil {
			continue
		}
		b.lingers.forget(l.Task)
		if iterm && step == lingerClose {
			itermTaken = true
		}
		for _, res := range b.runRecorded(ctx, ids) {
			if res.state == store.EffectDone && res.outcome == "closed" {
				n++
			}
			if iterm && res.state == store.EffectUnknown {
				b.lingers.quietFor(b.now(), itermCloseQuiet)
			}
		}
	}
	return n
}
