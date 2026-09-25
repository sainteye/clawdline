package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

// The token ledger kept by the daemon (docs/token-ledger.md "Storage and
// reading", "Which session is which").
//
// A pass finds the transcripts written within a look-back window, feeds each
// one that changed from the state stored for it, and stores the state and its
// totals again. What each token was spent on is transcript.LedgerState's; this
// file only keeps reading, keeps what was read, and says which sessions a child
// task and a Board item are.

const (
	// usagePassLimit is how many transcripts one pass feeds, one Feed each
	// (at most ledgerFeedLimit bytes and a line, limits N42). The rest are the
	// next pass's, which starts after the last one fed, so every due
	// transcript is reached however many there are.
	usagePassLimit = 32
	// usageWindowLimit is how far back a pass looks: a transcript last
	// written earlier than this is not looked for. A row already stored keeps
	// its totals; the file is simply not visited again until it is written.
	usageWindowLimit = 7 * 24 * time.Hour
	// usageEvery is how often the daemon runs a pass.
	usageEvery = time.Minute
	// usageStallPasses is how many passes' time may go by with none finished
	// before the ledger is said to be stalled (/v1/diagnostics).
	usageStallPasses = 3
)

// Openings of a first message that name what a session is. They are the
// broker's briefing (orchestrator/brief.go) and the Root Assignment's
// (orchestrator/handoffs.go), and the reader's (transcript/ledger.go).
const (
	usageChildOpening          = "You are a Clawdline CHILD agent for task "
	usageRootAssignmentOpening = "You are an independently owned Clawdline Feature Root for Root Assignment "
)

// UsageLedger reads transcripts into the store and answers what a session, a
// child task and a Board item spent.
type UsageLedger struct {
	Store *store.Store
	// Home is the directory whose .claude and .codex are read.
	Home string
	// PassLimit and Window are usagePassLimit and usageWindowLimit unless a
	// test lowers them.
	PassLimit int
	Window    time.Duration
	Now       func() time.Time
	Log       func(format string, args ...any)

	mu sync.Mutex
	// cursor is the key of the last transcript fed: the next pass starts
	// after it.
	cursor string
	// logged is what was last said about each transcript, so a failure is
	// said once per reason, and again only after the transcript read fine.
	logged  map[string]string
	limited bool
	// said is how many lines this ledger logged, for a test to count.
	said int
	// started is when Run began, and pulse what its last pass did.
	started time.Time
	pulse   *UsagePulse
}

// NewUsageLedger is a ledger over st, reading the transcripts under home.
func NewUsageLedger(st *store.Store, home string) *UsageLedger {
	return &UsageLedger{Store: st, Home: home}
}

func (u *UsageLedger) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *UsageLedger) passLimit() int {
	if u.PassLimit > 0 {
		return u.PassLimit
	}
	return usagePassLimit
}

func (u *UsageLedger) window() time.Duration {
	if u.Window > 0 {
		return u.Window
	}
	return usageWindowLimit
}

func (u *UsageLedger) say(format string, args ...any) {
	u.said++
	if u.Log != nil {
		u.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Run passes every usageEvery until ctx ends, the first at once.
func (u *UsageLedger) Run(ctx context.Context) {
	tick := time.NewTicker(usageEvery)
	defer tick.Stop()
	u.mu.Lock()
	u.started = u.now()
	u.mu.Unlock()
	for {
		pass, err := u.Pass(ctx)
		if err != nil && ctx.Err() == nil {
			u.sayOnce("pass", "the pass could not read the store: "+err.Error())
		}
		if ctx.Err() == nil {
			u.record(pass, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// UsagePass is what one pass did.
type UsagePass struct {
	// Due is how many transcripts had something to read; Fed how many this
	// pass read. Limited says Due was more than the pass limit.
	Due     int
	Fed     int
	Limited bool
	// Missing and Unreadable count what this pass found gone or could not
	// read.
	Missing    int
	Unreadable int
}

// UsagePulse is what the reading loop says about itself: when it started, its
// last finished pass and what that pass did. A loop that stopped and one with
// nothing to read are both silence from outside; this tells them apart.
type UsagePulse struct {
	// Running says Run has started; Started is when.
	Running bool
	Started time.Time
	// At is when the last pass ended, zero before the first; Pass is what it
	// did and Err why it could not finish.
	At   time.Time
	Pass UsagePass
	Err  string
	// Every is how often a pass runs, and StallAfter how long without one
	// ending is a stall.
	Every, StallAfter time.Duration
	// Stalled says no pass has ended for usageStallPasses intervals, counted
	// from the last one or, before the first, from the start.
	Stalled bool
}

func (u *UsageLedger) record(pass UsagePass, err error) {
	p := UsagePulse{At: u.now(), Pass: pass}
	if err != nil {
		p.Err = pathless(err).Error()
	}
	u.mu.Lock()
	u.pulse = &p
	u.mu.Unlock()
}

// Pulse is the reading loop's account of itself now.
func (u *UsageLedger) Pulse() UsagePulse {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := UsagePulse{Every: usageEvery, StallAfter: usageStallPasses * usageEvery}
	if u.pulse != nil {
		out.At, out.Pass, out.Err = u.pulse.At, u.pulse.Pass, u.pulse.Err
	}
	if u.started.IsZero() {
		return out
	}
	out.Running, out.Started = true, u.started
	since := out.At
	if since.IsZero() {
		since = u.started
	}
	out.Stalled = u.now().Sub(since) > out.StallAfter
	return out
}

// usageFile is one transcript a pass may visit.
type usageFile struct {
	assistant, conversation, path, parent string
	size                                  int64
	modified                              time.Time
	gone                                  bool
}

func (f usageFile) key() string { return f.assistant + "\x00" + f.conversation }

// Pass is one reading: every transcript due, at most the pass limit of them,
// in round robin after the last one fed.
func (u *UsageLedger) Pass(ctx context.Context) (UsagePass, error) {
	var out UsagePass
	now := u.now()
	since := now.Add(-u.window())
	found := u.discover(since)

	stored := map[string]store.UsageRow{}
	var convs []string
	for _, f := range found {
		convs = append(convs, f.conversation)
	}
	rows, err := u.Store.UsageRowsForConversations(ctx, convs)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		stored[r.Assistant+"\x00"+r.Conversation] = r
	}
	// A transcript read within the window and not found now: it is gone, or
	// it is a Codex rollout still being written under a day the window no
	// longer covers, or it simply has not been written since.
	recent, err := u.Store.UsageRowsReadSince(ctx, since)
	if err != nil {
		return out, err
	}
	for _, r := range recent {
		k := r.Assistant + "\x00" + r.Conversation
		if _, ok := found[k]; ok {
			continue
		}
		f := usageFile{assistant: r.Assistant, conversation: r.Conversation, path: r.Path, parent: r.Parent}
		st, err := os.Stat(r.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			f.gone = true
		case err != nil || st.ModTime().Before(since):
			continue
		default:
			f.size, f.modified = st.Size(), st.ModTime()
		}
		stored[k] = r
		found[k] = f
	}

	var due []usageFile
	for k, f := range found {
		if usageDue(f, stored[k], stored[k].Conversation != "") {
			due = append(due, f)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].key() < due[j].key() })
	out.Due = len(due)
	limit := u.passLimit()
	out.Limited = len(due) > limit

	u.mu.Lock()
	cursor := u.cursor
	wasLimited := u.limited
	u.limited = out.Limited
	u.mu.Unlock()
	if out.Limited && !wasLimited {
		u.say("usage: %d transcripts are due and a pass reads %d; the rest wait for the next passes", len(due), limit)
	}

	start := sort.Search(len(due), func(i int) bool { return due[i].key() > cursor })
	for n := 0; n < len(due) && n < limit; n++ {
		if ctx.Err() != nil {
			break
		}
		f := due[(start+n)%len(due)]
		u.mu.Lock()
		u.cursor = f.key()
		u.mu.Unlock()
		prev, had := stored[f.key()]
		row, reason, err := u.read(f, prev, had, now)
		if err := u.Store.SaveUsageRow(ctx, row); err != nil {
			return out, err
		}
		switch reason {
		case store.UsageTranscriptMissing:
			out.Missing++
		case store.UsageTranscriptUnreadable:
			out.Unreadable++
		default:
			out.Fed++
		}
		u.sayAbout(f, reason, err)
	}
	return out, nil
}

// usageDue says a transcript has something a pass should read: it is new, it
// changed since it was read, its last read stopped short, it could not be read
// last time, or it is gone and its row does not say so yet.
func usageDue(f usageFile, prev store.UsageRow, had bool) bool {
	if f.gone {
		return had && prev.Reason != store.UsageTranscriptMissing
	}
	if !had {
		return true
	}
	return prev.More || prev.Reason != "" || prev.Size != f.size || !prev.ModifiedAt.Equal(f.modified) ||
		prev.Path != f.path
}

// read feeds one transcript once and answers its next row. The row keeps the
// last reading's totals whatever went wrong.
func (u *UsageLedger) read(f usageFile, prev store.UsageRow, had bool, now time.Time) (store.UsageRow, string, error) {
	row := prev
	if !had {
		row = store.UsageRow{Assistant: f.assistant, Conversation: f.conversation}
	}
	row.Parent = f.parent
	if f.gone {
		row.Reason = store.UsageTranscriptMissing
		return row, row.Reason, nil
	}
	row.Path = f.path
	var state transcript.LedgerState
	if len(prev.State) > 0 {
		if err := json.Unmarshal(prev.State, &state); err != nil {
			// A state this build cannot read is read again from the start:
			// the totals come back, and nothing is counted twice.
			state = transcript.LedgerState{}
		}
	}
	res, err := state.Feed(f.path)
	if err != nil {
		if errors.Is(err, transcript.ErrNoRecord) {
			row.Reason = store.UsageTranscriptMissing
		} else {
			row.Reason = store.UsageTranscriptUnreadable
		}
		if row.ReadAt.IsZero() && row.Reason == store.UsageTranscriptMissing {
			row.Reason = store.UsageNotYetRead
		}
		return row, row.Reason, err
	}
	spent, measured := state.Totals()
	measured = pricedMeasured(spent, measured)
	stateJSON, err := json.Marshal(&state)
	if err != nil {
		return row, store.UsageTranscriptUnreadable, err
	}
	spentJSON, _ := json.Marshal(spent)
	measuredJSON, _ := json.Marshal(measured)
	row.State, row.Spent, row.Measured = stateJSON, spentJSON, measuredJSON
	row.Size, row.ModifiedAt, row.More = f.size, f.modified, res.More
	row.ReadAt, row.Reason = now, ""
	if !row.OpeningRead && f.parent == "" {
		u.readOpening(&row, f)
	}
	return row, "", nil
}

// readOpening names the child task or Root Assignment the transcript's first
// message names. A transcript with no person's turn yet is looked at again
// when it grows, until the first turn is past what FirstUser reads.
func (u *UsageLedger) readOpening(row *store.UsageRow, f usageFile) {
	text, err := transcript.FirstUser(f.path, f.assistant)
	switch {
	case err == nil:
		row.TaskID, row.RootAssignment = usageOpening(text)
		row.OpeningRead = true
	case errors.Is(err, transcript.ErrNotFound):
		row.OpeningRead = f.size > transcript.ReadBudget
	}
}

// usageOpening is the task id or Root Assignment id a first message names.
func usageOpening(text string) (task, rootAssignment string) {
	text = strings.TrimSpace(text)
	word := func(rest string) string {
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return ""
		}
		return strings.TrimRight(fields[0], ".,;:")
	}
	switch {
	case strings.HasPrefix(text, usageChildOpening):
		return word(text[len(usageChildOpening):]), ""
	case strings.HasPrefix(text, usageRootAssignmentOpening):
		return "", word(text[len(usageRootAssignmentOpening):])
	}
	return "", ""
}

// sayAbout logs a transcript's trouble once per reason, and forgets it once
// the transcript reads fine. The line names the conversation, never the path:
// the path is under the person's home.
func (u *UsageLedger) sayAbout(f usageFile, reason string, err error) {
	msg := reason
	if err != nil {
		msg += ": " + pathless(err).Error()
	}
	if reason == "" {
		u.mu.Lock()
		delete(u.logged, f.key())
		u.mu.Unlock()
		return
	}
	u.sayOnce(f.key(), fmt.Sprintf("usage: %s transcript %s: %s", f.assistant, f.conversation, msg))
}

// pathless is err without the file it names: the operating system's reason
// alone, as transcript.UnreadableError keeps it.
func pathless(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

func (u *UsageLedger) sayOnce(key, line string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.logged[key] == line {
		return
	}
	if u.logged == nil {
		u.logged = map[string]string{}
	}
	u.logged[key] = line
	u.say("%s", line)
}

// discover is every transcript written since since, by key: Claude's
// sessions and their subagents, and Codex's rollouts filed under the days the
// window covers.
func (u *UsageLedger) discover(since time.Time) map[string]usageFile {
	out := map[string]usageFile{}
	add := func(f usageFile) {
		st, err := os.Stat(f.path)
		if err != nil || st.ModTime().Before(since) {
			return
		}
		f.size, f.modified = st.Size(), st.ModTime()
		out[f.key()] = f
	}
	projects := filepath.Join(u.Home, ".claude", "projects")
	sessions, _ := filepath.Glob(filepath.Join(projects, "*", "*.jsonl"))
	for _, p := range sessions {
		add(usageFile{assistant: "claude", conversation: stem(p), path: p})
	}
	agents, _ := filepath.Glob(filepath.Join(projects, "*", "*", "subagents", "*.jsonl"))
	for _, p := range agents {
		parent := filepath.Base(filepath.Dir(filepath.Dir(p)))
		add(usageFile{assistant: "claude", conversation: stem(p), path: p, parent: parent})
	}
	root := filepath.Join(u.Home, ".codex", "sessions")
	end := u.now()
	// Codex files a rollout under the local date it started.
	first := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	for day := first; !day.After(end); day = day.AddDate(0, 0, 1) {
		dir := filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02"))
		rollouts, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		for _, p := range rollouts {
			add(usageFile{assistant: "codex", conversation: codexConversation(p), path: p})
		}
	}
	return out
}

func stem(p string) string { return strings.TrimSuffix(filepath.Base(p), ".jsonl") }

// codexConversation is a rollout's thread id: the uuid its name ends in
// (transcript.CodexPath finds a thread by that suffix).
func codexConversation(p string) string {
	s := stem(p)
	if len(s) > 36 && s[len(s)-37] == '-' {
		return s[len(s)-36:]
	}
	return s
}

// ---------- attribution ----------

// ErrUsageNoItem is a Board item that is not there.
var ErrUsageNoItem = errors.New("no Board item has this id")

// UsageTotals is what some sessions spent: by category, and by part with the
// cost. Measured.Unpriced > 0 means the cost is not the whole cost.
type UsageTotals struct {
	Categories map[transcript.Category]transcript.Tokens `json:"categories"`
	Measured   transcript.Tokens                         `json:"measured"`
}

// CostKnown says whether Measured.Cost is the whole cost.
func (t UsageTotals) CostKnown() bool { return t.Measured.CostKnown() }

func (t *UsageTotals) add(o UsageTotals) {
	if t.Categories == nil {
		t.Categories = map[transcript.Category]transcript.Tokens{}
	}
	for _, c := range transcript.Categories {
		if v, ok := o.Categories[c]; ok {
			t.Categories[c] = addTokens(t.Categories[c], v)
		}
	}
	t.Measured = addTokens(t.Measured, o.Measured)
}

// addDelegate folds a subagent's whole measured cost into delegate.
func (t *UsageTotals) addDelegate(m transcript.Tokens) {
	if t.Categories == nil {
		t.Categories = map[transcript.Category]transcript.Tokens{}
	}
	t.Categories[transcript.CategoryDelegate] = addTokens(t.Categories[transcript.CategoryDelegate], m)
	t.Measured = addTokens(t.Measured, m)
}

func addTokens(a, b transcript.Tokens) transcript.Tokens {
	return transcript.Tokens{
		Input: a.Input + b.Input, CacheWrite1h: a.CacheWrite1h + b.CacheWrite1h,
		CacheWrite5m: a.CacheWrite5m + b.CacheWrite5m, CacheRead: a.CacheRead + b.CacheRead,
		Output: a.Output + b.Output, Cost: a.Cost + b.Cost, Unpriced: a.Unpriced + b.Unpriced,
	}
}

// UsageGap is something an answer could not count as a current reading: a
// session, a subagent, a task or a Root Assignment, and why. Counted says a
// previous reading of it is in the totals.
type UsageGap struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Counted bool   `json:"counted"`
}

// SubagentUsage is one subagent transcript of a session.
type SubagentUsage struct {
	Conversation string            `json:"conversation"`
	Reason       string            `json:"reason,omitempty"`
	Calls        int64             `json:"calls"`
	Measured     transcript.Tokens `json:"measured"`
}

// SessionUsage is one session's ledger, its subagents folded into delegate.
type SessionUsage struct {
	Conversation   string    `json:"conversation"`
	Assistant      string    `json:"assistant,omitempty"`
	TaskID         string    `json:"task_id,omitempty"`
	RootAssignment string    `json:"root_assignment,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	ReadAt         time.Time `json:"read_at"`
	// More says the last pass stopped before the transcript's end.
	More   bool        `json:"more,omitempty"`
	Totals UsageTotals `json:"totals"`
	// The session's own calls; its subagents' are in Subagents.
	Calls       int64                   `json:"calls"`
	Compactions int64                   `json:"compactions"`
	PeakContext int64                   `json:"peak_context"`
	CallsAbove  int64                   `json:"calls_above"`
	Above       transcript.Tokens       `json:"above"`
	Composition *transcript.Composition `json:"composition,omitempty"`
	Subagents   []SubagentUsage         `json:"subagents,omitempty"`
	Gaps        []UsageGap              `json:"gaps,omitempty"`
}

// counted says the session's totals hold a reading.
func (s SessionUsage) counted() bool { return !s.ReadAt.IsZero() }

// TaskUsage is a child task's session or sessions.
type TaskUsage struct {
	TaskID   string         `json:"task_id"`
	Sessions []SessionUsage `json:"sessions"`
	Totals   UsageTotals    `json:"totals"`
	Gaps     []UsageGap     `json:"gaps,omitempty"`
}

// ItemOwner is one stint of a session owning a Board item.
type ItemOwner struct {
	Session        string    `json:"session,omitempty"`
	RootAssignment string    `json:"root_assignment,omitempty"`
	From           time.Time `json:"from"`
	// To is zero while the stint has not ended.
	To      time.Time `json:"to"`
	Current bool      `json:"current,omitempty"`
}

// ItemUsage is a Board item's bill: its owner sessions, whole, and the child
// tasks those sessions dispatched while they owned it.
type ItemUsage struct {
	ItemID   string         `json:"item_id"`
	Owners   []ItemOwner    `json:"owners"`
	Sessions []SessionUsage `json:"sessions"`
	Tasks    []TaskUsage    `json:"tasks"`
	Totals   UsageTotals    `json:"totals"`
	Gaps     []UsageGap     `json:"gaps,omitempty"`
}

// usageLedgerOf decodes a stored state; a row with none is an empty one.
func usageLedgerOf(r store.UsageRow) (transcript.LedgerState, map[transcript.Category]transcript.Tokens, transcript.Tokens) {
	var state transcript.LedgerState
	spent := map[transcript.Category]transcript.Tokens{}
	var measured transcript.Tokens
	if r.ReadAt.IsZero() {
		return state, spent, measured
	}
	_ = json.Unmarshal(r.State, &state)
	_ = json.Unmarshal(r.Spent, &spent)
	_ = json.Unmarshal(r.Measured, &measured)
	return state, spent, pricedMeasured(spent, measured)
}

// pricedMeasured is the measured count with its cost: the reader prices by
// category, so the session's cost is its categories'.
func pricedMeasured(spent map[transcript.Category]transcript.Tokens, measured transcript.Tokens) transcript.Tokens {
	measured.Cost, measured.Unpriced = 0, 0
	for _, c := range transcript.Categories {
		measured.Cost += spent[c].Cost
		measured.Unpriced += spent[c].Unpriced
	}
	return measured
}

func rowReason(r store.UsageRow) string {
	if r.Reason == "" && r.ReadAt.IsZero() {
		return store.UsageNotYetRead
	}
	return r.Reason
}

// FoldSession is one session from its own row and its subagents' rows. With
// no row the session is not yet read.
func FoldSession(conversation string, own []store.UsageRow, subagents []store.UsageRow) SessionUsage {
	out := SessionUsage{Conversation: conversation, Totals: UsageTotals{Categories: map[transcript.Category]transcript.Tokens{}}}
	if len(own) == 0 {
		out.Reason = store.UsageNotYetRead
		out.Gaps = append(out.Gaps, UsageGap{Kind: "session", ID: conversation, Reason: out.Reason})
	} else {
		r := own[0]
		state, spent, measured := usageLedgerOf(r)
		out.Assistant, out.TaskID, out.RootAssignment = r.Assistant, r.TaskID, r.RootAssignment
		out.Reason, out.ReadAt, out.More = rowReason(r), r.ReadAt, r.More
		out.Totals.add(UsageTotals{Categories: spent, Measured: measured})
		out.Calls, out.Compactions, out.PeakContext = state.Calls, state.Compactions, state.PeakContext
		out.CallsAbove, out.Above, out.Composition = state.CallsAbove, state.Above, state.Composition
		if out.Reason != "" {
			out.Gaps = append(out.Gaps, UsageGap{Kind: "session", ID: conversation, Reason: out.Reason, Counted: !r.ReadAt.IsZero()})
		}
	}
	for _, r := range subagents {
		if r.Parent != conversation {
			continue
		}
		state, _, measured := usageLedgerOf(r)
		sub := SubagentUsage{Conversation: r.Conversation, Reason: rowReason(r), Calls: state.Calls, Measured: measured}
		out.Subagents = append(out.Subagents, sub)
		out.Totals.addDelegate(measured)
		if sub.Reason != "" {
			out.Gaps = append(out.Gaps, UsageGap{Kind: "subagent", ID: r.Conversation, Reason: sub.Reason, Counted: !r.ReadAt.IsZero()})
		}
	}
	return out
}

// sessions folds every conversation the rows name, in the order given.
func foldSessions(order []string, rows, subagents []store.UsageRow) []SessionUsage {
	byConv := map[string][]store.UsageRow{}
	for _, r := range rows {
		byConv[r.Conversation] = append(byConv[r.Conversation], r)
	}
	out := make([]SessionUsage, 0, len(order))
	for _, c := range order {
		out = append(out, FoldSession(c, byConv[c], subagents))
	}
	return out
}

// ForSession is one session's ledger.
func (u *UsageLedger) ForSession(ctx context.Context, conversation string) (SessionUsage, error) {
	rows, err := u.Store.UsageRowsForConversations(ctx, []string{conversation})
	if err != nil {
		return SessionUsage{}, err
	}
	subs, err := u.Store.UsageRowsWithParents(ctx, []string{conversation})
	if err != nil {
		return SessionUsage{}, err
	}
	return FoldSession(conversation, ownRows(rows), subs), nil
}

// ownRows are the rows of sessions, not of subagents.
func ownRows(rows []store.UsageRow) []store.UsageRow {
	var out []store.UsageRow
	for _, r := range rows {
		if r.Parent == "" {
			out = append(out, r)
		}
	}
	return out
}

// ForTask is a child task's session or sessions: those whose first message
// names it.
func (u *UsageLedger) ForTask(ctx context.Context, taskID string) (TaskUsage, error) {
	rows, err := u.Store.UsageRowsForTask(ctx, taskID)
	if err != nil {
		return TaskUsage{}, err
	}
	rows = ownRows(rows)
	convs := conversationsOf(rows)
	subs, err := u.Store.UsageRowsWithParents(ctx, convs)
	if err != nil {
		return TaskUsage{}, err
	}
	return FoldTask(taskID, rows, subs), nil
}

// FoldTask is a task from the rows naming it and their subagents' rows.
func FoldTask(taskID string, rows, subagents []store.UsageRow) TaskUsage {
	out := TaskUsage{TaskID: taskID, Totals: UsageTotals{Categories: map[transcript.Category]transcript.Tokens{}}}
	out.Sessions = foldSessions(conversationsOf(rows), rows, subagents)
	for _, s := range out.Sessions {
		out.Totals.add(s.Totals)
		out.Gaps = append(out.Gaps, s.Gaps...)
	}
	if len(out.Sessions) == 0 {
		out.Gaps = append(out.Gaps, UsageGap{Kind: "task", ID: taskID, Reason: store.UsageNotYetRead})
	}
	return out
}

func conversationsOf(rows []store.UsageRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Conversation] {
			seen[r.Conversation] = true
			out = append(out, r.Conversation)
		}
	}
	sort.Strings(out)
	return out
}

// ForItem is a Board item's bill: the sessions that owned it — its current
// owner and every owner in its assignment history — whole, and the child
// tasks each of them dispatched between its assignment and its end.
func (u *UsageLedger) ForItem(ctx context.Context, itemID string) (ItemUsage, error) {
	item, err := u.Store.WorkV2Item(ctx, itemID)
	if errors.Is(err, store.ErrNoWorkV2) || errors.Is(err, sql.ErrNoRows) {
		return ItemUsage{}, ErrUsageNoItem
	}
	if err != nil {
		return ItemUsage{}, err
	}
	assignments, err := u.Store.WorkV2Assignments(ctx, itemID)
	if err != nil {
		return ItemUsage{}, err
	}
	var owners []ItemOwner
	for _, a := range assignments {
		if a.SessionID == "" && a.RootAssignment == "" {
			continue
		}
		to := a.ReleasedAt
		if to.IsZero() {
			to = item.ClosedAt
		}
		owners = append(owners, ItemOwner{Session: a.SessionID, RootAssignment: a.RootAssignment, From: a.CreatedAt, To: to,
			Current: item.OwnerSession != "" && a.SessionID == item.OwnerSession && a.ReleasedAt.IsZero()})
	}
	if item.OwnerSession != "" {
		known := false
		for _, o := range owners {
			known = known || o.Current
		}
		if !known {
			owners = append(owners, ItemOwner{Session: item.OwnerSession, From: item.CreatedAt, To: item.ClosedAt, Current: true})
		}
	}

	// A Root Assignment whose session was never written on the assignment
	// is found by the first message that names it.
	var assignmentIDs []string
	for _, o := range owners {
		if o.Session == "" {
			assignmentIDs = append(assignmentIDs, o.RootAssignment)
		}
	}
	opened, err := u.Store.UsageRowsForRootAssignments(ctx, assignmentIDs)
	if err != nil {
		return ItemUsage{}, err
	}
	for i, o := range owners {
		if o.Session != "" {
			continue
		}
		for _, r := range ownRows(opened) {
			if r.RootAssignment == o.RootAssignment {
				owners[i].Session = r.Conversation
				break
			}
		}
	}

	facts := itemFacts{ItemID: itemID, Owners: owners, Dispatched: map[int][]store.UsageTaskRef{}}
	var sessions []string
	for i, o := range owners {
		if o.Session == "" {
			continue
		}
		sessions = append(sessions, o.Session)
		refs, err := u.Store.BrokerTasksDispatchedBy(ctx, o.Session, o.From, o.To)
		if err != nil {
			return ItemUsage{}, err
		}
		facts.Dispatched[i] = refs
	}
	var taskIDs []string
	for _, refs := range facts.Dispatched {
		for _, r := range refs {
			taskIDs = append(taskIDs, r.ID)
		}
	}
	own, err := u.Store.UsageRowsForConversations(ctx, sessions)
	if err != nil {
		return ItemUsage{}, err
	}
	facts.Rows = ownRows(own)
	for _, id := range dedupe(taskIDs) {
		rows, err := u.Store.UsageRowsForTask(ctx, id)
		if err != nil {
			return ItemUsage{}, err
		}
		facts.Rows = append(facts.Rows, ownRows(rows)...)
	}
	facts.Subagents, err = u.Store.UsageRowsWithParents(ctx, conversationsOf(facts.Rows))
	if err != nil {
		return ItemUsage{}, err
	}
	return FoldItem(facts), nil
}

// itemFacts is everything FoldItem needs, read by ForItem.
type itemFacts struct {
	ItemID string
	Owners []ItemOwner
	// Dispatched is, by index into Owners, the tasks that owner's session
	// dispatched during its stint.
	Dispatched map[int][]store.UsageTaskRef
	// Rows are the owner sessions' rows and every dispatched task's session
	// rows; Subagents are theirs.
	Rows, Subagents []store.UsageRow
}

// FoldItem is the item's bill from what ForItem read. A session is counted
// once however many stints it owned the item or however many tasks name it.
func FoldItem(f itemFacts) ItemUsage {
	out := ItemUsage{ItemID: f.ItemID, Owners: f.Owners, Totals: UsageTotals{Categories: map[transcript.Category]transcript.Tokens{}}}
	counted := map[string]bool{}
	var ownerSessions []string
	for _, o := range f.Owners {
		if o.Session == "" {
			out.Gaps = append(out.Gaps, UsageGap{Kind: "root_assignment", ID: o.RootAssignment, Reason: store.UsageNotYetRead})
			continue
		}
		if !counted[o.Session] {
			counted[o.Session] = true
			ownerSessions = append(ownerSessions, o.Session)
		}
	}
	var ownerRows []store.UsageRow
	for _, r := range f.Rows {
		if counted[r.Conversation] {
			ownerRows = append(ownerRows, r)
		}
	}
	out.Sessions = foldSessions(ownerSessions, ownerRows, f.Subagents)
	for _, s := range out.Sessions {
		out.Totals.add(s.Totals)
		out.Gaps = append(out.Gaps, s.Gaps...)
	}

	seenTask := map[string]bool{}
	for i := range f.Owners {
		for _, ref := range f.Dispatched[i] {
			if seenTask[ref.ID] {
				continue
			}
			seenTask[ref.ID] = true
			var rows []store.UsageRow
			for _, r := range f.Rows {
				if r.TaskID == ref.ID && !counted[r.Conversation] {
					rows = append(rows, r)
				}
			}
			task := FoldTask(ref.ID, rows, f.Subagents)
			for _, s := range task.Sessions {
				counted[s.Conversation] = true
			}
			out.Tasks = append(out.Tasks, task)
			out.Totals.add(task.Totals)
			out.Gaps = append(out.Gaps, task.Gaps...)
		}
	}
	return out
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
