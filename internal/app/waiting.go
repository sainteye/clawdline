package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Waiting tells the person when a session has stopped on a question to them
// and nobody has answered it (docs/push.md "有人在等你回答").
//
// Every other push about a person being waited on is sent by whoever is
// waiting — a child's /notify, a root's /v1/orchestrator/notify, a blocking
// decision — and it is the sending that gets forgotten. This one is the
// machine's own: the reading already knows a session is `waiting` (the
// assistant's own registry says so, or its screen draws a menu; it is what
// the lists draw as waiting_you), so nobody has to remember anything for the
// person to hear about it. It is the retired app's one state-change push
// (StateHook.swift:341-360), with the two things it did not have.
//
//   - **Not at once.** It pushed the moment a session went waiting. Here a
//     stop is pushed only once it has stood for maxUnseenWait: measured on
//     this machine over seven days, half the questions were answered inside
//     two minutes by a person who was already there, and a push for those
//     is a push about something they were looking at.
//   - **Once per stop, and written down.** A stop runs from the first reading
//     that saw the session waiting to the first that saw it doing anything
//     else. Whatever was decided about it — pushed, over the budget, or a
//     finished task's tab that says nothing — is recorded with the push in
//     one transaction (the outbox, D08), so a restart reads it back rather
//     than deciding again. A stop whose end nobody saw (the daemon was down,
//     or the reading could not see that terminal) stays spent: a person
//     pushed twice is worse than one who finds it on the list (DG-7).
//
// Only a reading ends a stop. `unknown` is not "not waiting", and a row
// missing from a reading that could not see its terminal is not a row gone.
type Waiting struct {
	Store WaitingStore
	// Run runs the pushes a sweep recorded (orchestrator.Broker.RunEffects).
	Run func(ctx context.Context, ids []int64)
	// Role says whose a terminal is: a dispatched child's tab, and whether
	// its task is still going. Asked only when a stop is due, never per
	// sweep. Nil, or not found, is a session a person opened for themselves.
	Role func(ctx context.Context, terminal string) (WaitingRole, bool)
	// Enabled is the person's switch. Nil is on.
	Enabled func() bool
	// After is how long a question stands before it is pushed, and HourLimit
	// how many this machine pushes in an hour. Zero is the default; an
	// override may only lower them.
	After     time.Duration
	HourLimit int
	Now       func() time.Time

	mu    sync.Mutex
	stops map[string]*waitingStop
	sent  []time.Time
}

// WaitingStore is the part of the store a stop is written down in.
type WaitingStore interface {
	LatestEvents(ctx context.Context, kind string, subjects []string) (map[string]store.Event, error)
	RecordIntent(ctx context.Context, events []store.Event, effects []store.Effect) ([]int64, error)
	Append(ctx context.Context, e store.Event) error
}

// WaitingRole is a terminal's place in the broker's work.
type WaitingRole struct {
	// Child is a dispatched task's own tab; Live is that task still going.
	Child bool
	Live  bool
	// Title is the task's, and Deadline when it times out; zero is none.
	Title    string
	Deadline time.Time
}

const (
	// maxUnseenWait is how long a question stands before the person is told.
	// Ten minutes is where this machine's answers run out: over seven days,
	// of 45 questions 34 were answered inside ten minutes and 11 were not,
	// and of those eleven nine went past fifteen and five past half an hour.
	// A shorter wait catches people who were about to answer; a longer one
	// only delays the ones who were never going to. docs/push.md has the run.
	maxUnseenWait = 10 * time.Minute
	// waitingPushHourLimit is how many stops this machine pushes in an hour,
	// counted apart from the agents' 30 and the decisions' 30. The week
	// measured never had more than three in a day; the budget is for the
	// afternoon a whole wave of children stops on one permission prompt,
	// which is one fact and must not be ten notifications. A stop over it is
	// recorded over_budget and not pushed later.
	waitingPushHourLimit = 6
	// waitingForget is how long a stop no reading has seen is remembered in
	// this process. Its record is in the store; this only lets the memory go.
	waitingForget = 24 * time.Hour
)

// WaitingEvent is the kind a stop is written down under, its subject the
// stop's key and its phase what was decided.
const WaitingEvent = "session.waiting"

// What was decided about a stop.
const (
	WaitingPushed     = "pushed"
	WaitingOverBudget = "over_budget"
	// WaitingSilent is a finished task's tab: nobody is behind it to be
	// blocked (StateHook.swift:282).
	WaitingSilent = "silent"
	WaitingEnded  = "ended"
)

// waitingStop is one session standing on a question, as this process saw it.
type waitingStop struct {
	since   time.Time
	seen    time.Time
	backend session.Backend
	// settled is a decision about this stop written down, by this process or
	// an earlier one.
	settled bool
}

// WaitingKey is a stop's subject: the terminal, and the conversation in it
// when there is one, so a tmux pane handed to a new session is a new subject.
func WaitingKey(s session.Session) string {
	if s.ConversationID == "" {
		return s.ID
	}
	return s.ID + "|" + s.ConversationID
}

func (w *Waiting) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Waiting) after() time.Duration {
	if w.After > 0 && w.After < maxUnseenWait {
		return w.After
	}
	return maxUnseenWait
}

func (w *Waiting) hourLimit() int {
	if w.HourLimit > 0 && w.HourLimit < waitingPushHourLimit {
		return w.HourLimit
	}
	return waitingPushHourLimit
}

// Observe reads one session reading: it starts the stops it sees, ends the
// ones it sees are over, and records — and runs — the push of every stop that
// has stood long enough. It answers how many it pushed.
//
// It holds its lock throughout, the store's reads and writes included: its
// one caller is the sweep, and nothing else touches these stops.
func (w *Waiting) Observe(ctx context.Context, r session.Inventory) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stops == nil {
		w.stops = map[string]*waitingStop{}
	}
	now := w.now()
	seen := map[string]bool{}
	var ended []string
	type due struct {
		key string
		s   session.Session
		st  *waitingStop
	}
	var owed []due
	for _, s := range r.Sessions {
		if !s.IsAssistant() {
			continue
		}
		key := WaitingKey(s)
		st := w.stops[key]
		switch s.State {
		case session.StateWaiting:
			seen[key] = true
			if st == nil {
				st = &waitingStop{since: now, backend: s.Backend}
				w.stops[key] = st
			}
			st.seen = now
			if !st.settled && now.Sub(st.since) >= w.after() {
				owed = append(owed, due{key, s, st})
			}
		case session.StateUnknown:
			// Nothing is known about it, which ends nothing.
			seen[key] = true
		default:
			seen[key] = true
			if st != nil {
				ended = append(ended, key)
			}
		}
	}
	for key, st := range w.stops {
		if seen[key] {
			continue
		}
		switch {
		case r.Complete || r.Sources[string(st.backend)]:
			// The reading could see where it was, and it is not there.
			ended = append(ended, key)
		case now.Sub(st.seen) > waitingForget:
			delete(w.stops, key)
		}
	}
	var errs []error
	for _, key := range ended {
		st := w.stops[key]
		if st.settled {
			payload, _ := json.Marshal(map[string]any{"phase": WaitingEnded, "since": st.since.Unix()})
			if err := w.Store.Append(ctx, store.Event{Kind: WaitingEvent, Subject: key, Payload: payload}); err != nil {
				// Kept, so the next sweep writes the end again: without it the
				// next stop in this terminal would read as this one.
				errs = append(errs, err)
				continue
			}
		}
		delete(w.stops, key)
	}
	pushed := 0
	for _, d := range owed {
		ok, err := w.settle(ctx, d.key, d.s, d.st, now)
		if err != nil {
			errs = append(errs, err)
		}
		if ok {
			pushed++
		}
	}
	if len(errs) > 0 {
		return pushed, errs[0]
	}
	return pushed, nil
}

// settle decides one due stop and writes the decision down. It answers
// whether a push was recorded.
func (w *Waiting) settle(ctx context.Context, key string, s session.Session, st *waitingStop, now time.Time) (bool, error) {
	if w.Enabled != nil && !w.Enabled() {
		// Left undecided: turned back on while it still stands, it is pushed.
		return false, nil
	}
	latest, err := w.Store.LatestEvents(ctx, WaitingEvent, []string{key})
	if err != nil {
		return false, err
	}
	if e, ok := latest[key]; ok && waitingPhase(e) != WaitingEnded {
		// Decided by a daemon before this one, and no reading since has
		// seen it end.
		st.settled = true
		return false, nil
	}
	role, hasRole := WaitingRole{}, false
	if w.Role != nil {
		role, hasRole = w.Role(ctx, s.ID)
	}
	record := func(phase string) error {
		payload, _ := json.Marshal(map[string]any{"phase": phase, "since": st.since.Unix(), "terminal": s.ID})
		if err := w.Store.Append(ctx, store.Event{Kind: WaitingEvent, Subject: key, Payload: payload}); err != nil {
			return err
		}
		st.settled = true
		return nil
	}
	if hasRole && role.Child && !role.Live {
		return false, record(WaitingSilent)
	}
	cut := now.Add(-time.Hour)
	kept := w.sent[:0]
	for _, at := range w.sent {
		if at.After(cut) {
			kept = append(kept, at)
		}
	}
	w.sent = kept
	if len(w.sent) >= w.hourLimit() {
		return false, record(WaitingOverBudget)
	}
	title, body := waitingText(s, role, hasRole, now.Sub(st.since), now)
	push, err := json.Marshal(orchestrator.WaitingPush{Key: key, Terminal: s.ID, Title: title, Body: body,
		Tag: "waiting-" + s.ID})
	if err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]any{"phase": WaitingPushed, "since": st.since.Unix(), "terminal": s.ID,
		"push": orchestrator.EffectWaitingPush})
	ids, err := w.Store.RecordIntent(ctx, []store.Event{{Kind: WaitingEvent, Subject: key, Payload: payload}},
		[]store.Effect{{Kind: orchestrator.EffectWaitingPush, Subject: key, Payload: push}})
	if err != nil {
		return false, err
	}
	st.settled = true
	w.sent = append(w.sent, now)
	if w.Run != nil {
		w.Run(ctx, ids)
	}
	return true, nil
}

func waitingPhase(e store.Event) string {
	var p struct {
		Phase string `json:"phase"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	return p.Phase
}

// waitingText is one stop's notification, its title and its body, inside the
// push's own bounds (80 and 500 characters). The title names the session; the
// body says how long it has stood, and for a child how long its task has left,
// and then the question when the screen showed one.
func waitingText(s session.Session, role WaitingRole, hasRole bool, stood time.Duration, now time.Time) (string, string) {
	label := strings.TrimSpace(s.Label)
	if hasRole && role.Child && strings.TrimSpace(role.Title) != "" {
		label = strings.TrimSpace(role.Title)
	}
	if label == "" {
		label = s.ID
	}
	title := clip("等你回答："+label, 80)
	body := fmt.Sprintf("停在一個問題上 %d 分鐘了", int(stood/time.Minute))
	if hasRole && role.Child && !role.Deadline.IsZero() {
		if left := int(role.Deadline.Sub(now) / time.Minute); left > 0 {
			body += fmt.Sprintf("，這個 task 的時限還剩 %d 分鐘", left)
		}
	}
	body += "。"
	if s.Menu != nil {
		if q := strings.TrimSpace(s.Menu.Question); q != "" {
			body += "\n" + q
		}
	}
	return title, clip(body, 500)
}

// clip is s cut to n characters, the last of them an ellipsis when cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
