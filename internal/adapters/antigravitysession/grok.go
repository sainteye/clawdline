package antigravitysession

import (
	"context"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func grokControlDraft(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "!") {
		return true
	}
	if !strings.HasPrefix(text, "/") {
		return false
	}
	if strings.HasPrefix(text, "/model ") {
		return false
	}
	token := text[1:]
	if i := strings.IndexAny(token, " \t\r\n"); i >= 0 {
		token = token[:i]
	}
	return !strings.ContainsAny(token, "/.")
}

// An explicit argv ceiling overrides the user's native defaults. Grok's Auto
// is not an edits-only permission and is not used to implement that ceiling.
func grokArgs(permission string) []string {
	if permission == "full" {
		return []string{"agent", "--no-leader", "--always-approve", "stdio"}
	}
	return []string{"--permission-mode", "default", "agent", "--no-leader", "stdio"}
}

func (h *Host) processArgs(launch LaunchContext) []string {
	if h.assistant() != session.AssistantGrok || len(h.opts.Args) != 0 {
		// Explicit arguments belong to an internal owner (including process test
		// peers), never a browser request or a Grok machine setting.
		return h.opts.Args
	}
	return grokArgs(launch.PermissionMode)
}

func (h *Host) configureGrok(ctx context.Context, row session.Session, model, effort, permission string) error {
	c, err := h.lookup(row)
	if err != nil {
		return err
	}
	c.mu.Lock()
	ceiling := c.launchContext.PermissionMode
	c.mu.Unlock()
	if ceiling == "" {
		ceiling = "ask"
	}
	if (permission != "ask" && permission != "full") || permission != ceiling {
		return refusal("unsupported_permission")
	}
	if effort != "" {
		return refusal("effort_not_offered")
	}
	if _, err := h.Resume(ctx, row.CWD, row.ConversationID); err != nil {
		return err
	}
	if model != "" {
		return h.selectModel(ctx, c, model, nil)
	}
	return nil
}
