package antigravitysession

import (
	"encoding/json"
	"net/url"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
)

// LaunchContext is immutable daemon-prepared context, never browser input.
// URI names the existing published prompt; tools receive only the capability
// file path through their environment, never a capability value in the prompt.
type LaunchContext struct {
	URI                 string `json:"uri,omitempty"`
	Text                string `json:"text,omitempty"`
	Persona             string `json:"persona,omitempty"`
	SquadLaunchID       string `json:"squad_launch_id,omitempty"`
	SquadCapabilityPath string `json:"squad_capability_path,omitempty"`
}

type injectedTurn struct {
	OriginalText string `json:"original_text"`
}

func (l LaunchContext) validate() error {
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
	if !c.replaying || u.Kind != "user_message_chunk" || c.replaySawUser {
		return
	}
	c.replaySawUser = true
	if c.injected == nil || c.launchContext.Text == "" {
		return
	}
	want := "Embedded resource (" + c.launchContext.URI + "):\n" + c.launchContext.Text + " " + c.injected.OriginalText
	if u.Text == want {
		u.Text = c.injected.OriginalText
	}
}
