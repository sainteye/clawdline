package http

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

var usageByServer sync.Map // *Server -> *app.UsageLedger

// usageLedger is this server's token ledger (docs/token-ledger.md): the
// reader StartUsage runs, and what a usage route will answer from.
func (s *Server) usageLedger() *app.UsageLedger {
	if u, ok := usageByServer.Load(s); ok {
		return u.(*app.UsageLedger)
	}
	home, _ := os.UserHomeDir()
	got, _ := usageByServer.LoadOrStore(s, app.NewUsageLedger(s.store, home))
	return got.(*app.UsageLedger)
}

// StartUsage runs the token ledger's reading passes. Without a home
// directory there is nothing to read, and the daemon says so and goes on.
func (s *Server) StartUsage(ctx context.Context) {
	u := s.usageLedger()
	if u.Home == "" {
		log.Printf("usage: no home directory; the token ledger is not read")
		return
	}
	go u.Run(ctx)
}

// usageDiagnostics is /v1/diagnostics.usage: the reading loop's own account.
func (s *Server) usageDiagnostics() *contract.UsageDiagnostics {
	p := s.usageLedger().Pulse()
	out := &contract.UsageDiagnostics{
		Running:           p.Running,
		Due:               int64(p.Pass.Due),
		Fed:               int64(p.Pass.Fed),
		Limited:           p.Pass.Limited,
		Missing:           int64(p.Pass.Missing),
		Unreadable:        int64(p.Pass.Unreadable),
		Error:             p.Err,
		EverySeconds:      int64(p.Every / time.Second),
		StallAfterSeconds: int64(p.StallAfter / time.Second),
		Stalled:           p.Stalled,
	}
	if !p.Started.IsZero() {
		out.Started = p.Started.Unix()
	}
	if !p.At.IsZero() {
		out.At = p.At.Unix()
	}
	return out
}

// usageID is what a usage route accepts as an id: a conversation, a task or
// an item id is letters, digits and `-`, `_`, `.`. Anything else is refused
// before it reaches a query or a file name.
var usageID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

// usageRoute is GET /v1/usage/{sessions,tasks,items}/<id> (docs/token-ledger.md
// "What a person and a session see") and GET /v1/usage/compare-compaction. The gate lets a paired device and this
// machine's orchestrator token through (machineScoped); both only read.
func (s *Server) usageRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The token ledger is read with GET.")
		return
	}
	if routePath(r) == "/v1/usage/compare-compaction" {
		s.usageCompareCompaction(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/usage/"), "/")
	if len(parts) != 2 || parts[1] == "" {
		writeRefusal(w, http.StatusNotFound, "not_found",
			"Ask for /v1/usage/sessions/<conversation>, /v1/usage/tasks/<id> or /v1/usage/items/<id>.")
		return
	}
	kind, id := parts[0], parts[1]
	if !usageID.MatchString(id) || strings.Contains(id, "..") {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"An id is letters, digits, '-', '_' and '.', at most 200 of them.")
		return
	}
	ctx := r.Context()
	u := s.usageLedger()
	switch kind {
	case "sessions":
		s.usageSession(ctx, w, u, id)
	case "tasks":
		s.usageTask(ctx, w, u, id)
	case "items":
		got, err := u.ForItem(ctx, id)
		if errors.Is(err, app.ErrUsageNoItem) {
			writeRefusal(w, http.StatusNotFound, "unknown_item", "No Board item has this id.")
			return
		}
		if err != nil {
			usageStoreRefusal(w, err)
			return
		}
		writeJSON(w, usageItem(got, s.usageWindows(ctx)))
	default:
		writeRefusal(w, http.StatusNotFound, "not_found",
			"Ask for /v1/usage/sessions/<conversation>, /v1/usage/tasks/<id> or /v1/usage/items/<id>.")
	}
}

// usageCompareCompaction is GET /v1/usage/compare-compaction?since=…
// (docs/token-ledger.md "Did compacting early pay"): the child tasks created
// since then, grouped by the compaction window they were launched with. A
// query it does not read is refused by name rather than ignored.
func (s *Server) usageCompareCompaction(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, values := range q {
		if key != "since" || len(values) != 1 {
			writeRefusal(w, http.StatusBadRequest, "bad_request",
				"The comparison reads one query field, since: `14d`, `36h` or a Unix time in seconds.")
			return
		}
	}
	got, err := s.usageLedger().CompareCompactionSince(r.Context(), q.Get("since"))
	if errors.Is(err, app.ErrCompareSince) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "since: "+err.Error()+".")
		return
	}
	if err != nil {
		usageStoreRefusal(w, err)
		return
	}
	writeJSON(w, usageComparison(got))
}

func usageStoreRefusal(w http.ResponseWriter, err error) {
	log.Printf("usage: the ledger could not be read: %v", err)
	writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The token ledger could not be read.")
}

// usageSession answers one session. A conversation with no row is one the
// ledger has not read yet when its transcript is there, and unknown when it
// is not: an empty total is never the answer for either.
func (s *Server) usageSession(ctx context.Context, w http.ResponseWriter, u *app.UsageLedger, id string) {
	rows, err := u.Store.UsageRowsForConversations(ctx, []string{id})
	if err != nil {
		usageStoreRefusal(w, err)
		return
	}
	own := false
	parent := ""
	for _, row := range rows {
		if row.Parent == "" {
			own = true
		} else {
			parent = row.Parent
		}
	}
	if !own && parent != "" {
		writeRefusal(w, http.StatusNotFound, "unknown_session",
			"That conversation is a subagent; its bill is in its session's delegate: /v1/usage/sessions/"+parent)
		return
	}
	if !own && !usageTranscriptExists(u.Home, id) {
		writeRefusal(w, http.StatusNotFound, "unknown_session",
			"The ledger has no reading of this conversation and no transcript names it.")
		return
	}
	got, err := u.ForSession(ctx, id)
	if err != nil {
		usageStoreRefusal(w, err)
		return
	}
	writeJSON(w, usageSession(got, s.usageWindows(ctx)))
}

// usageTranscriptExists is whether a transcript of this conversation is on
// disk: Claude's under any project, or a Codex rollout.
func usageTranscriptExists(home, id string) bool {
	if home == "" {
		return false
	}
	if found, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", id+".jsonl")); len(found) > 0 {
		return true
	}
	return transcript.CodexPath(home, id) != ""
}

// usageTask answers one child task: known when a session names it or the
// broker has its record.
func (s *Server) usageTask(ctx context.Context, w http.ResponseWriter, u *app.UsageLedger, id string) {
	got, err := u.ForTask(ctx, id)
	if err != nil {
		usageStoreRefusal(w, err)
		return
	}
	if len(got.Sessions) == 0 {
		_, err := u.Store.BrokerTask(ctx, id)
		if errors.Is(err, store.ErrNoTask) {
			writeRefusal(w, http.StatusNotFound, "unknown_task",
				"No child task has this id and no session's first message names it.")
			return
		}
		if err != nil {
			usageStoreRefusal(w, err)
			return
		}
	}
	writeJSON(w, usageTask(got, s.usageWindows(ctx)))
}

// ---------- the wire shapes ----------

func usageTokens(t transcript.Tokens) contract.UsageTokens {
	return contract.UsageTokens{
		Input: t.Input, CacheWrite1h: t.CacheWrite1h, CacheWrite5m: t.CacheWrite5m, CacheRead: t.CacheRead,
		Output: t.Output, Total: t.Total(), Cost: t.Cost, CostKnown: t.CostKnown(), Unpriced: t.Unpriced,
	}
}

// usageBill is every category in the ledger's order with its share of the
// cost, or of the tokens when the cost is not whole.
func usageBill(t app.UsageTotals) contract.UsageBill {
	out := contract.UsageBill{Total: usageTokens(t.Measured), ShareOf: contract.UsageShareOfCost,
		Categories: []contract.UsageCategory{}}
	whole := t.Measured.Cost
	if !t.CostKnown() {
		out.ShareOf, whole = contract.UsageShareOfTokens, t.Measured.Total()
	}
	for _, c := range transcript.Categories {
		v := t.Categories[c]
		part := v.Cost
		if out.ShareOf == contract.UsageShareOfTokens {
			part = v.Total()
		}
		share := 0.0
		if whole > 0 {
			share = part / whole
		}
		out.Categories = append(out.Categories, contract.UsageCategory{
			Name: contract.UsageCategoryName(c), Share: share, Tokens: usageTokens(v),
			UpperBound: c == transcript.CategoryRules,
		})
	}
	return out
}

func usageGaps(gaps []app.UsageGap) []contract.UsageGap {
	out := make([]contract.UsageGap, 0, len(gaps))
	for _, g := range gaps {
		out = append(out, contract.UsageGap{Kind: g.Kind, ID: g.ID, Reason: contract.UsageReason(g.Reason), Counted: g.Counted})
	}
	return out
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// windowOf answers the compaction window a session was launched with, from
// the task or Root Assignment it names, or nil when that is not known.
type windowOf func(taskID, assignment string) *int64

// usageWindows reads the broker's records for windowOf, each one at most once
// an answer: an item's bill names the same task in every session of it.
func (s *Server) usageWindows(ctx context.Context) windowOf {
	if s.broker == nil {
		return func(string, string) *int64 { return nil }
	}
	tasks := map[string]*int64{}
	assignments := map[string]*int64{}
	return func(taskID, assignment string) *int64 {
		switch {
		case taskID != "":
			if w, ok := tasks[taskID]; ok {
				return w
			}
			var w *int64
			if r, _, err := s.broker.Record(ctx, taskID); err == nil {
				w = r.AutoCompactWindow
			}
			tasks[taskID] = w
			return w
		case assignment != "":
			if w, ok := assignments[assignment]; ok {
				return w
			}
			var w *int64
			if a, err := s.broker.RootAssignmentByID(ctx, assignment); err == nil && a.Executor != nil {
				w = a.Executor.AutoCompactWindow
			}
			assignments[assignment] = w
			return w
		}
		return nil
	}
}

func usageSession(s app.SessionUsage, window windowOf) contract.UsageSession {
	out := contract.UsageSession{
		Conversation: s.Conversation, Assistant: s.Assistant, TaskID: s.TaskID, RootAssignment: s.RootAssignment,
		Reason: contract.UsageReason(s.Reason), ReadAt: unixOrZero(s.ReadAt), More: s.More,
		Bill: usageBill(s.Totals), Calls: s.Calls, PeakContext: s.PeakContext, Compactions: s.Compactions,
		CallsAbove: s.CallsAbove, Above: usageTokens(s.Above),
		Subagents: []contract.UsageSubagent{}, Gaps: usageGaps(s.Gaps),
		AutoCompactWindow: window(s.TaskID, s.RootAssignment),
	}
	if c := s.Composition; c != nil {
		out.Composition = &contract.UsageComposition{
			Measured: c.Measured, SystemPrompt: c.SystemPrompt, Tools: usageNamed(c.Tools),
			SkillListing: c.SkillListing, Instructions: usageNamed(c.Instructions), McpInstructions: c.MCP, Other: c.Other,
		}
	}
	for _, sub := range s.Subagents {
		out.Subagents = append(out.Subagents, contract.UsageSubagent{
			Conversation: sub.Conversation, Reason: contract.UsageReason(sub.Reason), Calls: sub.Calls,
			Measured: usageTokens(sub.Measured),
		})
	}
	return out
}

func usageNamed(in []transcript.NamedSize) []contract.UsageNamedSize {
	out := make([]contract.UsageNamedSize, 0, len(in))
	for _, n := range in {
		out = append(out, contract.UsageNamedSize{Name: n.Name, Tokens: n.Tokens})
	}
	return out
}

// usageSessions is the sessions' wire shapes, their calls added up and their
// largest peak.
func usageSessions(in []app.SessionUsage, window windowOf) (out []contract.UsageSession, calls, peak int64) {
	out = make([]contract.UsageSession, 0, len(in))
	for _, s := range in {
		out = append(out, usageSession(s, window))
		calls += s.Calls
		peak = max(peak, s.PeakContext)
	}
	return out, calls, peak
}

func usageTask(t app.TaskUsage, window windowOf) contract.UsageTask {
	sessions, calls, peak := usageSessions(t.Sessions, window)
	out := contract.UsageTask{TaskID: t.TaskID, Bill: usageBill(t.Totals), Calls: calls, PeakContext: peak,
		Sessions: sessions, Gaps: usageGaps(t.Gaps)}
	read := false
	for _, s := range t.Sessions {
		read = read || !s.ReadAt.IsZero()
	}
	if !read {
		out.Reason = contract.UsageReasonNotYetRead
	}
	return out
}

func usageItem(it app.ItemUsage, window windowOf) contract.UsageItem {
	sessions, calls, peak := usageSessions(it.Sessions, window)
	out := contract.UsageItem{ItemID: it.ItemID, Bill: usageBill(it.Totals), Sessions: sessions,
		Owners: []contract.UsageItemOwner{}, Tasks: []contract.UsageTask{}, Gaps: usageGaps(it.Gaps)}
	for _, o := range it.Owners {
		out.Owners = append(out.Owners, contract.UsageItemOwner{Session: o.Session, RootAssignment: o.RootAssignment,
			From: unixOrZero(o.From), To: unixOrZero(o.To), Current: o.Current})
	}
	for _, t := range it.Tasks {
		task := usageTask(t, window)
		out.Tasks = append(out.Tasks, task)
		calls += task.Calls
		peak = max(peak, task.PeakContext)
	}
	out.Calls, out.PeakContext = calls, peak
	return out
}

func usageComparison(c app.CompactionComparison) contract.UsageCompactionComparison {
	out := contract.UsageCompactionComparison{
		Since: unixOrZero(c.Since), Until: unixOrZero(c.Until), MinTasks: int64(c.MinTasks),
		Groups: []contract.UsageCompactionGroup{}, Excluded: int64(c.Excluded),
		ExcludedTasks: []contract.UsageCompareExcluded{}, ExcludedTruncated: c.ExcludedTruncated,
		Truncated: c.Truncated, NotRecorded: []contract.UsageCompareMissing{},
	}
	for _, g := range c.Groups {
		out.Groups = append(out.Groups, contract.UsageCompactionGroup{
			Group: app.CompareGroupName(g.Window), Window: g.Window,
			Tasks: int64(g.Tasks), ReadTasks: int64(g.Read), Sessions: int64(g.Sessions), TooFew: g.TooFew,
			CostTotal: g.CostTotal, CostMedianPerTask: g.CostMedian, CostKnown: g.CostKnown,
			CallsPerTask: g.CallsPerTask, CompactionsPerTask: g.CompactionsPerTask,
			PeakContextMedian: g.PeakMedian, PeakContextMax: g.PeakMax, Above200kShare: g.AboveShare,
			Ended: int64(g.Ended), Running: int64(g.Running), Success: int64(g.Success), Failure: int64(g.Failure),
			Timeout: int64(g.Timeout), Cancelled: int64(g.Cancelled), Stalled: int64(g.Stalled), Lost: int64(g.Lost),
			SuccessRate: g.SuccessRate, FailureRate: g.FailureRate, TimeoutRate: g.TimeoutRate, StalledRate: g.StalledRate,
			Respawns: int64(g.Respawns),
		})
	}
	for _, e := range c.ExcludedTasks {
		out.ExcludedTasks = append(out.ExcludedTasks, contract.UsageCompareExcluded{TaskID: e.TaskID,
			Reason: contract.UsageCompareExcludedReason(e.Reason)})
	}
	for _, m := range c.NotRecorded {
		out.NotRecorded = append(out.NotRecorded, contract.UsageCompareMissing{Name: m.Name, Why: m.Why})
	}
	return out
}
