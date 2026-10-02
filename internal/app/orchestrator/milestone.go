package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/privacy"
)

// A milestone handoff is a long-running Root handing its line of work to a
// fresh Session at a milestone, so the work after it is not paid for by
// re-reading everything before it on every call (docs/token-ledger.md,
// "Long-running sessions"). Its handoff.md is a short summary with a fixed
// shape and a byte limit, checked before anything is opened; the daemon adds
// obligations.md, what the sender still holds, from its own records, so the
// summary cannot leave a running child or an owed landing out
// (docs/handoff.md, "Milestone handoffs").

// MilestoneSummaryLimit is the most bytes a milestone summary may have. Six
// KiB is a page: enough for a goal, a dozen decisions and blockers and their
// links, and small enough that the receiver's first call reads it whole.
const MilestoneSummaryLimit = 6 << 10

// milestoneObligationRows is the most rows obligations.md lists per kind;
// the rest are counted, with the command that lists them.
const milestoneObligationRows = 40

// milestoneVerbatimLines is the longest quoted or fenced block a summary may
// carry. Longer is a pasted transcript or log; the summary links it instead.
const milestoneVerbatimLines = 12

// MilestoneSections are the summary's headings, each a `## ` heading, all
// required and no others: what the work is for, what has been verified, what
// stops it, where the evidence is, and what comes next.
var MilestoneSections = []string{"Goal", "Verified decisions", "Blockers", "Evidence", "Next step"}

// MilestoneProblem is one way a summary is not a milestone summary. Detail
// never repeats a credential it found: it is masked.
type MilestoneProblem struct {
	Rule   string `json:"rule"`
	Line   int    `json:"line,omitempty"`
	Detail string `json:"detail"`
}

var (
	// A line that begins a speaker's turn in a pasted conversation.
	transcriptSpeaker = regexp.MustCompile(`(?i)^\s*(?:human|user|assistant|claude|codex|system)\s*:`)
	// Markup an assistant's transcript carries and a summary never needs.
	transcriptMarkup = regexp.MustCompile(`<(?:system-reminder|clawdline-notice|command-name|command-message|antml:[a-z_]+|function_results|tool_use|tool_result)\b|"role"\s*:\s*"(?:user|assistant)"`)
	// Something a reader can follow: a URL, a path, a commit, a UUID, or a
	// command to run.
	evidenceLink = regexp.MustCompile("https?://|(?:^|[\\s(`])(?:\\.{0,2}/|~/)?[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+|\\b[0-9a-f]{7,40}\\b|\\b[0-9a-f]{8}-[0-9a-f]{4}-|`clawdline [a-z]")
)

// CheckMilestoneSummary is every reason data is not a milestone summary; none
// means it is one. It is pure, so the command checks a draft with the same
// rules the daemon refuses with.
func CheckMilestoneSummary(data []byte) []MilestoneProblem {
	var out []MilestoneProblem
	if len(data) > MilestoneSummaryLimit {
		out = append(out, MilestoneProblem{Rule: "too_long",
			Detail: fmt.Sprintf("%d bytes; a milestone summary is at most %d. Link the detail instead of copying it.", len(data), MilestoneSummaryLimit)})
	}
	if !utf8.Valid(data) {
		return append(out, MilestoneProblem{Rule: "not_text", Detail: "the summary is not UTF-8 text"})
	}
	bodies := map[string]int{}
	seen := map[string]bool{}
	current := ""
	quoted, fenced, fence := 0, false, 0
	for i, line := range strings.Split(string(data), "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if fenced && fence > milestoneVerbatimLines {
				out = append(out, MilestoneProblem{Rule: "verbatim", Line: n,
					Detail: fmt.Sprintf("a %d-line code block; link the file or log instead", fence)})
			}
			fenced, fence = !fenced, 0
			continue
		}
		if fenced {
			fence++
		}
		if strings.HasPrefix(trimmed, ">") {
			quoted++
			if quoted == milestoneVerbatimLines+1 {
				out = append(out, MilestoneProblem{Rule: "verbatim", Line: n,
					Detail: fmt.Sprintf("more than %d quoted lines; link the source instead", milestoneVerbatimLines)})
			}
		} else {
			quoted = 0
		}
		if !fenced && strings.HasPrefix(trimmed, "## ") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			current = ""
			for _, s := range MilestoneSections {
				if strings.EqualFold(name, s) {
					current = s
				}
			}
			switch {
			case current == "":
				out = append(out, MilestoneProblem{Rule: "section_unknown", Line: n,
					Detail: fmt.Sprintf("%q is not one of the sections (%s)", name, strings.Join(MilestoneSections, ", "))})
			case seen[current]:
				out = append(out, MilestoneProblem{Rule: "section_repeated", Line: n, Detail: current + " appears twice"})
			}
			if current != "" {
				seen[current] = true
			}
			continue
		}
		if current != "" && trimmed != "" {
			bodies[current]++
			if current == "Evidence" && evidenceLink.MatchString(line) {
				bodies["evidence-link"]++
			}
		}
		if transcriptSpeaker.MatchString(line) || transcriptMarkup.MatchString(line) {
			out = append(out, MilestoneProblem{Rule: "transcript", Line: n,
				Detail: "conversation text; say what was decided and link where, not who said what"})
		}
	}
	for _, f := range privacy.New(nil).Scan("milestone.md", data) {
		if f.Rule == "credential" {
			out = append(out, MilestoneProblem{Rule: "credential", Line: f.Line,
				Detail: "a credential (" + f.Match + "); name where it is kept, never its value"})
		}
	}
	for _, s := range MilestoneSections {
		switch {
		case !seen[s]:
			out = append(out, MilestoneProblem{Rule: "section_missing", Detail: "## " + s + " is required"})
		case bodies[s] == 0:
			out = append(out, MilestoneProblem{Rule: "section_empty", Detail: "## " + s + " is empty; write \"None.\" when there is nothing"})
		}
	}
	if seen["Evidence"] && bodies["Evidence"] > 0 && bodies["evidence-link"] == 0 {
		out = append(out, MilestoneProblem{Rule: "evidence_unlinked",
			Detail: "## Evidence names no path, URL, commit, id or clawdline command the receiver can open"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// MilestoneHandoffLine is the line typed into a milestone handoff's receiver.
func MilestoneHandoffLine(dir string) string {
	return "You are picking up a Clawdline milestone handoff. Read " + filepath.Join(dir, "handoff.md") +
		" (the sender's summary: Goal, Verified decisions, Blockers, Evidence, Next step) and " +
		filepath.Join(dir, MilestoneObligationsFile) + " (what the sender still holds) before anything else. " +
		"Open an Evidence link when the next step needs it, not before; do not ask the person what the summary " +
		"or its evidence answers. The Board items listed there become yours; the child tasks, notices and " +
		"landings stay with the sender until it closes, so do not acknowledge or land them. Continue from Next step."
}

// MilestoneObligationsFile is the file the daemon writes beside handoff.md.
const MilestoneObligationsFile = "obligations.md"

// milestoneObligations is what the sender holds when it hands over.
type milestoneObligations struct {
	Items        []milestoneItem
	Running      []milestoneTask
	Unacked      []milestoneTask
	Landings     []milestoneTask
	ItemsUnread  bool
	TasksUnread  bool
	NoticeUnread bool
}

type milestoneItem struct{ ID, Title, Phase, Condition, Decision string }

type milestoneTask struct{ ID, Title, State string }

// gatherMilestoneObligations reads the sender's tasks, notices and landings.
// A source that cannot be read is said to be unread in the file, never left
// out: an empty list and an unread one are not the same thing to a receiver.
func (b *Broker) gatherMilestoneObligations(ctx context.Context, from string, items []string) milestoneObligations {
	var o milestoneObligations
	for _, id := range items {
		item, err := b.Store.WorkV2Item(ctx, id)
		if err != nil {
			o.ItemsUnread = true
			continue
		}
		o.Items = append(o.Items, milestoneItem{ID: item.ID, Title: item.Title, Phase: string(item.Phase),
			Condition: string(item.Condition), Decision: item.DecisionID})
	}
	records, bad, err := b.Records(ctx)
	if err != nil || len(bad) > 0 {
		o.TasksUnread = true
	}
	for _, r := range records {
		if r.Root == nil || r.Root.SessionID != from {
			continue
		}
		t := milestoneTask{ID: r.ID, Title: r.Title, State: string(r.State)}
		if !r.State.Terminal() {
			o.Running = append(o.Running, t)
		}
		if r.Landing != nil && r.Landing.State == LandingPending {
			o.Landings = append(o.Landings, t)
		}
	}
	completions, err := b.RootCompletions(ctx, from)
	if err != nil {
		o.NoticeUnread = true
	}
	for _, c := range completions {
		o.Unacked = append(o.Unacked, milestoneTask{ID: c.Record.ID, Title: c.Record.Title, State: string(c.Record.State)})
	}
	return o
}

// Carried is every task the obligations name, for the handoff's record.
func (o milestoneObligations) Carried() []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]milestoneTask{o.Running, o.Unacked, o.Landings} {
		for _, t := range list {
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t.ID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// renderMilestoneObligations is obligations.md. It names records by id and
// the command that reads each; it copies no result, brief or transcript.
func renderMilestoneObligations(from string, o milestoneObligations) string {
	var s strings.Builder
	s.WriteString("# What the sender still holds\n\n")
	fmt.Fprintf(&s, "Written by the daemon when Session `%s` handed over. Read each record when you need it; this file is not a copy of any of them.\n\n", from)

	s.WriteString("## Board items (moving to you)\n\n")
	s.WriteString("Each becomes yours once you have read this handoff. `clawdline item steps <id>` reads one.\n\n")
	if o.ItemsUnread {
		s.WriteString("- Some items could not be read when this was written; `clawdline item steps` on each id in the handoff record.\n")
	}
	if len(o.Items) == 0 && !o.ItemsUnread {
		s.WriteString("None.\n")
	}
	for i, it := range o.Items {
		if i == milestoneObligationRows {
			fmt.Fprintf(&s, "- … and %d more\n", len(o.Items)-i)
			break
		}
		line := fmt.Sprintf("- `%s` %s — %s", it.ID, oneLine(it.Title), it.Phase)
		if it.Condition != "" {
			line += ", " + it.Condition
		}
		if it.Decision != "" {
			line += fmt.Sprintf(", waiting on decision `%s` (the person's answer still reaches the sender's Session)", it.Decision)
		}
		s.WriteString(line + "\n")
	}

	s.WriteString("\n## Staying with the sender until it closes\n\n")
	s.WriteString("The sender keeps these until `clawdline session close` finds it safe: it acknowledges each notice and records each landing. Do not do either yourself. `clawdline task show <id>` reads one.\n")
	section := func(title string, unread bool, list []milestoneTask) {
		fmt.Fprintf(&s, "\n### %s\n\n", title)
		if unread {
			s.WriteString("- Could not be read when this was written; ask the sender's Session, or `clawdline landings`.\n")
		}
		if len(list) == 0 && !unread {
			s.WriteString("None.\n")
		}
		for i, t := range list {
			if i == milestoneObligationRows {
				fmt.Fprintf(&s, "- … and %d more\n", len(list)-i)
				break
			}
			fmt.Fprintf(&s, "- `%s` %s — %s\n", t.ID, oneLine(t.Title), t.State)
		}
	}
	section("Child tasks still running", o.TasksUnread, o.Running)
	section("Completion notices not yet acknowledged", o.NoticeUnread, o.Unacked)
	section("Landings still owed", o.TasksUnread, o.Landings)
	return s.String()
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 120 {
		r := []rune(s)
		s = string(r[:119]) + "…"
	}
	return s
}

// writeMilestoneObligations writes obligations.md beside the summary,
// replacing one a sender may have left there: the daemon's is the account.
func writeMilestoneObligations(dir, body string) error {
	path := filepath.Join(dir, MilestoneObligationsFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readBounded reads at most limit bytes of path: enough to know a file is
// over a limit without reading all of it.
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

// ParseMilestoneSummary is each section's body, by its name in
// MilestoneSections; a receiver's reading of the summary, and the test's.
func ParseMilestoneSummary(data []byte) map[string]string {
	out := map[string]string{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "## ") {
			current = ""
			name := strings.TrimSpace(strings.TrimPrefix(t, "## "))
			for _, s := range MilestoneSections {
				if strings.EqualFold(name, s) {
					current = s
				}
			}
			continue
		}
		if current != "" {
			out[current] += line + "\n"
		}
	}
	for k, v := range out {
		out[k] = strings.TrimSpace(v)
	}
	return out
}
