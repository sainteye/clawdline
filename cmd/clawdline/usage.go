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
	if named > 1 {
		usageCommandUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(showUsage(os.Stdout, os.Stderr, b, usageAsk{Session: *session, Task: *task, Item: *item, JSON: *asJSON}, os.Getenv))
}

func usageCommandUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline usage [--session <conversation> | --task <id> | --item <id>] [--json] [--port n]")
	fmt.Fprintln(os.Stderr, "  what a session, a child task or a Board item spent, by category; this session's own by default")
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
		bill, gaps = s.Bill, s.Gaps
	case "tasks":
		var t contract.UsageTask
		if json.Unmarshal(a.Body, &t) != nil {
			return unreadableUsage(stderr)
		}
		head = usageHead(fmt.Sprintf("task %s (%d sessions)", t.TaskID, len(t.Sessions)), t.Reason, t.Calls, t.PeakContext, t.Bill)
		bill, gaps = t.Bill, t.Gaps
	case "items":
		var it contract.UsageItem
		if json.Unmarshal(a.Body, &it) != nil {
			return unreadableUsage(stderr)
		}
		head = usageHead(fmt.Sprintf("item %s (%d sessions, %d tasks)", it.ItemID, len(it.Sessions), len(it.Tasks)),
			"", it.Calls, it.PeakContext, it.Bill)
		bill, gaps = it.Bill, it.Gaps
	}
	fmt.Fprintln(stdout, head)
	writeUsageLines(stdout, bill)
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
