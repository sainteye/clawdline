package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A milestone handoff's receiver opens with MilestoneHandoffLine, which says
// only that a handoff is being picked up. An assistant that names its
// conversation from its first message (Codex's thread name, Claude's aiTitle)
// then calls every receiver "picking up a milestone handoff". The work it
// continues is in the summary's Goal and Next step, which the daemon has
// already read and validated before opening the receiver — so naming reads
// that same file from the daemon's own record, never a path a line names.

// MilestoneNamingText is what a naming model reads for a milestone handoff:
// the Goal and Next step of the summary its record names. It is false for a
// plain handoff, an unreadable summary, or one with neither section.
func MilestoneNamingText(h Handoff) (string, bool) {
	if !h.Milestone || h.Dir == "" {
		return "", false
	}
	data, err := readBounded(filepath.Join(h.Dir, "handoff.md"), MilestoneSummaryLimit+1)
	if err != nil || len(data) > MilestoneSummaryLimit {
		return "", false
	}
	sections := ParseMilestoneSummary(data)
	goal, next := sections["Goal"], sections["Next step"]
	if goal == "" && next == "" {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("A continuation of earlier work, handed over at a milestone.")
	if goal != "" {
		sb.WriteString("\nGoal: " + goal)
	}
	if next != "" {
		sb.WriteString("\nNext step: " + next)
	}
	return sb.String(), true
}

// MilestoneHandoffForLine is the recorded milestone handoff whose receiver
// was typed this line, by the package directory the daemon chose for it.
func (b *Broker) MilestoneHandoffForLine(ctx context.Context, line string) (Handoff, bool) {
	text := strings.TrimSpace(line)
	if !strings.HasPrefix(text, milestoneLineOpening) {
		return Handoff{}, false
	}
	list, err := b.Handoffs(ctx)
	if err != nil {
		return Handoff{}, false
	}
	for _, h := range list {
		if h.Milestone && h.Dir != "" &&
			strings.HasPrefix(text, milestoneLineOpening+filepath.Join(h.Dir, "handoff.md")+" ") {
			return h, true
		}
	}
	return Handoff{}, false
}

const milestoneLineOpening = "You are picking up a Clawdline milestone handoff. Read "

// NameHandoff gives an untitled handoff the title a naming model chose. A
// title the sender gave, or one already chosen, is never replaced; the bool
// says whether this call set it.
func (b *Broker) NameHandoff(ctx context.Context, id, title string) (Handoff, bool, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > handoffTitleLimit {
		return Handoff{}, false, badHandoff("title is 1 to 200 characters.")
	}
	set := false
	h, err := b.updateHandoff(ctx, id, "handoff.named", func(x *Handoff) {
		if strings.TrimSpace(x.Title) == "" {
			x.Title, set = title, true
		}
	})
	return h, set, err
}
