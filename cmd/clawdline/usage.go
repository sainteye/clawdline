package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline usage`: what a session, a child task or a Board item spent, by
// what each token was spent on (docs/token-ledger.md "What a person and a
// session see"). One GET to /v1/usage/…; with no flag it is the calling
// session's own bill, named the way `session report` names it.

func usageCommand(args []string) {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	session := fs.String("session", "", "a conversation id (default: this assistant's own)")
	task := fs.String("task", "", "a child task id")
	item := fs.String("item", "", "a Board item id")
	compare := fs.Bool("compare-compaction", false, "child tasks grouped by the compaction window they were launched with")
	since := fs.String("since", "", "with --compare-compaction: `14d`, `36h` or a Unix time (default 14d)")
	asJSON := fs.Bool("json", false, "print the daemon's answer as JSON")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		usageCommandUsage()
	}
	named := 0
	for _, v := range []string{*session, *task, *item} {
		if v != "" {
			named++
		}
	}
	if named > 1 || (*compare && named > 0) || (*since != "" && !*compare) {
		usageCommandUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	if *compare {
		os.Exit(showCompactionComparison(os.Stdout, os.Stderr, b, *since, *asJSON))
	}
	os.Exit(showUsage(os.Stdout, os.Stderr, b, usageAsk{Session: *session, Task: *task, Item: *item, JSON: *asJSON}, os.Getenv))
}

func usageCommandUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline usage [--session <conversation> | --task <id> | --item <id>] [--json] [--port n]")
	fmt.Fprintln(os.Stderr, "  what a session, a child task or a Board item spent, by category; this session's own by default")
	fmt.Fprintln(os.Stderr, "       clawdline usage --compare-compaction [--since 14d] [--json] [--port n]")
	fmt.Fprintln(os.Stderr, "  child tasks grouped by the compaction window they were launched with: what they cost and how they ended")
	os.Exit(2)
}

// usageAsk is what the command was asked for: at most one of the three.
type usageAsk struct {
	Session, Task, Item string
	JSON                bool
}

// showUsage is the command, answering its exit status.
func showUsage(stdout, stderr io.Writer, b *broker, ask usageAsk, getenv func(string) string) int {
	kind, id := "sessions", ask.Session
	switch {
	case ask.Task != "":
		kind, id = "tasks", ask.Task
	case ask.Item != "":
		kind, id = "items", ask.Item
	case id == "":
		for _, name := range conversationEnv {
			if v := strings.TrimSpace(getenv(name)); v != "" {
				id = v
				break
			}
		}
		if id == "" {
			fmt.Fprintf(stderr, "clawdline usage: cannot tell which conversation this is: none of %s is set. "+
				"Pass --session <conversation id>, --task <id> or --item <id>.\n", strings.Join(conversationEnv, ", "))
			return 2
		}
	}
	a, err := b.request(http.MethodGet, "/v1/usage/"+kind+"/"+url.PathEscape(id), nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline usage:", err)
		return 1
	}
	if ask.JSON || !a.ok() {
		return report(stdout, stderr, "usage", a)
	}
	var head string
	var bill contract.UsageBill
	var gaps []contract.UsageGap
	var sessions []contract.UsageSession
	switch kind {
	case "sessions":
		var s contract.UsageSession
		if json.Unmarshal(a.Body, &s) != nil {
			return unreadableUsage(stderr)
		}
		head = usageHead("session "+s.Conversation, s.Reason, s.Calls, s.PeakContext, s.Bill)
		if s.CallsAbove > 0 {
			head += fmt.Sprintf(", %d calls above 200k context (%s)", s.CallsAbove, usageCost(s.Above))
		}
		if s.Compactions > 0 {
			head += fmt.Sprintf(", %d compactions", s.Compactions)
		}
		if w := usageWindow(s.AutoCompactWindow); w != "" {
			head += ", " + w
		}
		bill, gaps = s.Bill, s.Gaps
	case "tasks":
		var t contract.UsageTask
		if json.Unmarshal(a.Body, &t) != nil {
			return unreadableUsage(stderr)
		}
		head = usageHead(fmt.Sprintf("task %s (%d sessions)", t.TaskID, len(t.Sessions)), t.Reason, t.Calls, t.PeakContext, t.Bill)
		bill, gaps, sessions = t.Bill, t.Gaps, t.Sessions
	case "items":
		var it contract.UsageItem
		if json.Unmarshal(a.Body, &it) != nil {
			return unreadableUsage(stderr)
		}
		head = usageHead(fmt.Sprintf("item %s (%d sessions, %d tasks)", it.ItemID, len(it.Sessions), len(it.Tasks)),
			"", it.Calls, it.PeakContext, it.Bill)
		bill, gaps, sessions = it.Bill, it.Gaps, it.Sessions
		for _, t := range it.Tasks {
			sessions = append(sessions, t.Sessions...)
		}
	}
	fmt.Fprintln(stdout, head)
	writeUsageLines(stdout, bill)
	// Each session's compaction window where Clawdline launched it, so an
	// experiment can be grouped by it; one it did not launch says nothing.
	for _, s := range sessions {
		if w := usageWindow(s.AutoCompactWindow); w != "" {
			fmt.Fprintf(stdout, "session %s: %s, peak context %s, %d compactions\n",
				s.Conversation, w, usageCount(float64(s.PeakContext)), s.Compactions)
		}
	}
	for _, g := range gaps {
		counted := "nothing of it counted"
		if g.Counted {
			counted = "an earlier reading counted"
		}
		fmt.Fprintf(stdout, "gap: %s %s %s (%s)\n", g.Kind, g.ID, g.Reason, counted)
	}
	return 0
}

func unreadableUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "clawdline usage: the daemon's answer could not be read; --json prints it as it came")
	return 1
}

// usageHead is the header line: what it is, its calls, its peak context and
// its cost — or, for a reading the ledger does not have, why.
func usageHead(what string, reason contract.UsageReason, calls, peak int64, bill contract.UsageBill) string {
	if reason == contract.UsageReasonNotYetRead {
		return what + ": not_yet_read — the ledger has no reading of it yet"
	}
	head := fmt.Sprintf("%s: %d calls, peak context %s, %s", what, calls, usageCount(float64(peak)), usageCost(bill.Total))
	if reason != "" {
		head += " — " + string(reason) + ", from the last reading"
	}
	return head
}

// writeUsageLines is one line per category with anything in it, by cost, or
// by tokens when the cost is not whole: name, share, tokens, cost.
func writeUsageLines(w io.Writer, bill contract.UsageBill) {
	lines := append([]contract.UsageCategory(nil), bill.Categories...)
	byTokens := bill.ShareOf == contract.UsageShareOfTokens
	sort.SliceStable(lines, func(i, j int) bool {
		if byTokens {
			return lines[i].Tokens.Total > lines[j].Tokens.Total
		}
		return lines[i].Tokens.Cost > lines[j].Tokens.Cost
	})
	of := "of cost"
	if byTokens {
		of = "of tokens"
	}
	for _, c := range lines {
		if c.Tokens.Total == 0 {
			continue
		}
		line := fmt.Sprintf("  %-10s %5.1f%% %s  %8s tokens  %s", c.Name, c.Share*100, of, usageCount(c.Tokens.Total), usageCost(c.Tokens))
		if c.UpperBound {
			line += "  (upper bound)"
		}
		fmt.Fprintln(w, line)
	}
}

// usageCount is a token count a person reads at a glance.
func usageCount(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

// usageCost is a cost, saying so when part of it has no price.
func usageCost(t contract.UsageTokens) string {
	if !t.CostKnown {
		if t.Cost == 0 {
			return "cost unknown"
		}
		return fmt.Sprintf("$%.2f + unpriced %s tokens", t.Cost, usageCount(t.Unpriced))
	}
	return fmt.Sprintf("$%.2f", t.Cost)
}

// usageWindow is the compaction window a session was launched with, in words,
// or empty when Clawdline did not launch it or cannot say.
func usageWindow(w *int64) string {
	switch {
	case w == nil:
		return ""
	case *w == 0:
		return "launched with no compaction window"
	}
	return "launched to compact at " + usageCount(float64(*w))
}

// showCompactionComparison is `usage --compare-compaction`: one GET to
// /v1/usage/compare-compaction and one table, a row per group, then what the
// answer left out (docs/token-ledger.md "Did compacting early pay").
func showCompactionComparison(stdout, stderr io.Writer, b *broker, since string, asJSON bool) int {
	path := "/v1/usage/compare-compaction"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	a, err := b.request(http.MethodGet, path, nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline usage:", err)
		return 1
	}
	if asJSON || !a.ok() {
		return report(stdout, stderr, "usage", a)
	}
	var c contract.UsageCompactionComparison
	if json.Unmarshal(a.Body, &c) != nil {
		return unreadableUsage(stderr)
	}
	writeCompactionComparison(stdout, c)
	return 0
}

// writeCompactionComparison is the comparison as a table and the lines that
// qualify it; `clawdline verify show` prints a record's data with it too.
func writeCompactionComparison(stdout io.Writer, c contract.UsageCompactionComparison) {
	fmt.Fprintf(stdout, "child tasks created %s – %s, by the compaction window they were launched with\n",
		time.Unix(c.Since, 0).UTC().Format("2006-01-02 15:04Z"), time.Unix(c.Until, 0).UTC().Format("2006-01-02 15:04Z"))
	if len(c.Groups) == 0 {
		fmt.Fprintln(stdout, "no child task in this range was launched with a known window")
	} else {
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(tw, "group\tsessions\ttasks\tcost/task\tcalls/task\tcompactions/task\tabove-200k share\tsuccess rate\tstalled\trespawns\t")
		for _, g := range c.Groups {
			cost := "—"
			if g.ReadTasks > 0 {
				cost = fmt.Sprintf("$%.2f", g.CostMedianPerTask)
				if !g.CostKnown {
					cost += "+"
				}
			}
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%.1f\t%.2f\t%s\t%s\t%d\t%d\t\n", g.Group, g.Sessions, g.Tasks, cost,
				g.CallsPerTask, g.CompactionsPerTask, comparePercent(g.Above200kShare, g.TooFew),
				comparePercent(g.SuccessRate, g.TooFew), g.Stalled, g.Respawns)
		}
		_ = tw.Flush()
		fmt.Fprintln(stdout, "cost/task is the median over the tasks the ledger has read; + means part of it has no price.")
		for _, g := range c.Groups {
			if g.TooFew {
				fmt.Fprintf(stdout, "%s: %d tasks, fewer than %d — too few to compare, so no percentage is shown\n",
					g.Group, g.Tasks, c.MinTasks)
			}
			if g.Running > 0 || g.ReadTasks < g.Tasks {
				fmt.Fprintf(stdout, "%s: %d still running, %d not read by the ledger yet\n", g.Group, g.Running, g.Tasks-g.ReadTasks)
			}
		}
	}
	if c.Excluded > 0 {
		reasons := map[contract.UsageCompareExcludedReason]int{}
		for _, e := range c.ExcludedTasks {
			reasons[e.Reason]++
		}
		var parts []string
		for _, r := range contract.UsageCompareExcludedReasonValues {
			if reasons[r] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", reasons[r], r))
			}
		}
		line := fmt.Sprintf("excluded: %d tasks with no known window", c.Excluded)
		if len(parts) > 0 {
			line += " (" + strings.Join(parts, ", ")
			if c.ExcludedTruncated {
				line += fmt.Sprintf(" among the newest %d", len(c.ExcludedTasks))
			}
			line += ")"
		}
		fmt.Fprintln(stdout, line)
	}
	if c.Truncated {
		fmt.Fprintln(stdout, "truncated: the range held more tasks than one answer reads; these are the newest")
	}
	for _, m := range c.NotRecorded {
		fmt.Fprintf(stdout, "not recorded: %s — %s\n", m.Name, m.Why)
	}
}

// comparePercent is a share as a percentage, or why there is none.
func comparePercent(p *float64, tooFew bool) string {
	switch {
	case tooFew:
		return "too few"
	case p == nil:
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *p*100)
}
