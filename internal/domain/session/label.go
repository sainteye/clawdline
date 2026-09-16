package session

import "strings"

// LabelRungs are the places a session's name can come from, highest first.
//
// The order is the Swift app's `TargetSession.preferredDisplayLabel`, and so is
// the thing it leaves out: **no rung carries a terminal title.** A tab or pane
// title is a place a name is displayed, not a place one is kept — anything in
// the terminal may overwrite it, Claude Code clears it, and nothing announces
// either — so a terminal renamed to `Default`, or to nothing, cannot change a
// row. There is deliberately nowhere to pass one.
type LabelRungs struct {
	// Manual is the name a person typed for this conversation in Clawdline.
	Manual string
	// Orchestrator is the title of the task Clawdline opened the tab for. It
	// was known before the tab existed and is what a list of work should say.
	Orchestrator string
	// Conversation is what the conversation calls itself: the last `/rename`
	// (`customTitle`), otherwise the last `aiTitle`, out of the transcript.
	// Already passed through DisplayedConversationTitle.
	Conversation string
	// Thread is Codex's own thread name, or a fallback title kept for a Claude
	// conversation that has none yet.
	Thread string
	// Handle is `~/.claude/sessions/<pid>.json`'s `name` — never descriptive,
	// but durable and present before the transcript has a title.
	Handle string
	// Coordinate is where the session is. The last answer, never a job name.
	Coordinate string
}

// PreferredLabel is the first rung with something on it.
func PreferredLabel(r LabelRungs) string {
	for _, candidate := range []string{r.Manual, r.Orchestrator, r.Conversation, r.Thread, r.Handle} {
		if c := strings.TrimSpace(candidate); c != "" {
			return c
		}
	}
	return r.Coordinate
}

// DisplayedConversationTitle lets a weak conversation title step aside, but
// only for something underneath it.
//
// Claude Code writes `aiTitle` once and never revises it, so a conversation
// that opened with a pasted image is `Image #1` for life. That title still
// outranks the fallback rung below it, which would keep a correctly generated
// fallback off the screen forever — so a weak title gives way to a fallback.
// With no fallback it stays: `Image #1` at least says an image was.
func DisplayedConversationTitle(title string, weak bool, fallback string) string {
	if !weak || strings.TrimSpace(fallback) == "" {
		return title
	}
	return ""
}
