package antigravitysession

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
)

// LaunchContext is immutable daemon-prepared context, never browser input.
// URI names the existing published prompt; tools receive only the capability
// file path through their environment, never a capability value in the prompt.
type LaunchContext struct {
	// PermissionMode is frozen by the daemon before a Grok process starts.
	PermissionMode      string `json:"permission_mode,omitempty"`
	URI                 string `json:"uri,omitempty"`
	Text                string `json:"text,omitempty"`
	Persona             string `json:"persona,omitempty"`
	SquadLaunchID       string `json:"squad_launch_id,omitempty"`
	SquadCapabilityPath string `json:"squad_capability_path,omitempty"`
	WorkflowLaunchID    string `json:"workflow_launch_id,omitempty"`
	// Reminder is a short sentence appended to every later prompt. Empty means none.
	Reminder string `json:"reminder,omitempty"`
}

// AGY 1.5 replays an embedded resource as a bare resource_link, and its model
// was measured not to follow instructions sent that way. Gemini receives the
// launch context inside the first prompt's own text instead.
func inlineContext(context, text string) string {
	return "<clawdline_instructions>\n" + context + "\n</clawdline_instructions>\n\n" + text
}

func withReminder(text, reminder string) string {
	if reminder == "" {
		return text
	}
	return text + "\n\n<clawdline_reminder>" + reminder + "</clawdline_reminder>"
}

type injectedTurn struct {
	OriginalText string `json:"original_text"`
}

func (l LaunchContext) validate() error {
	if l.PermissionMode != "" && l.PermissionMode != "ask" && l.PermissionMode != "full" {
		return refusal("unsupported_permission")
	}
	if l.SquadCapabilityPath != "" && !filepath.IsAbs(l.SquadCapabilityPath) {
		return refusal("invalid_launch_context")
	}
	if l.Text != "" {
		u, err := url.Parse(l.URI)
		if err != nil || u.Scheme != "file" || u.Host != "" || !filepath.IsAbs(u.Path) || u.RawQuery != "" || u.Fragment != "" {
			return refusal("invalid_launch_context")
		}
	}
	// Reuse the existing frame budget for the persisted launch payload too.
	raw, err := json.Marshal(l)
	if err != nil || len(raw) > MaxFrameBytes {
		return refusal("invalid_launch_context")
	}
	return nil
}

// Only the first replayed user chunk can represent the first injected turn.
// Match the measured single flattened chunk exactly, including original text.
// Split chunks, changed formatting or other unknown shapes remain untouched;
// matching a later similar message would erase unrelated history.
func (c *conversation) cleanReplayContext(u *antigravityacp.SessionUpdate) {
	if !c.replaying || u.Kind != "user_message_chunk" {
		return
	}
	first := !c.replaySawUser
	c.replaySawUser = true
	if suffix := withReminder("", c.launchContext.Reminder); suffix != "" && strings.HasSuffix(u.Text, suffix) {
		u.Text = strings.TrimSuffix(u.Text, suffix)
		return
	}
	if !first || c.injected == nil || c.launchContext.Text == "" {
		return
	}
	original := c.injected.OriginalText
	legacy := "Embedded resource (" + c.launchContext.URI + "):\n" + c.launchContext.Text + " " + original
	if u.Text == legacy || u.Text == inlineContext(c.launchContext.Text, original) {
		u.Text = original
	}
}
