//go:build darwin || linux

package process

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// One Codex terminal carries four processes, and only one of them is holding
// the conversation open. The npm package's `codex` is a script run by node, so
// the executable on its line is `node` and it is not an assistant at all —
// which is the only reason the pid asked about is the platform binary.
//
// It matters because the wrapper holds no rollout open: measured on this Mac
// on 2026-09-20, across four Codex sessions the wrapper held none and the
// binary held every one, so asking the wrapper would answer `no_record` for
// every session on the machine — the one wrong answer that looks exactly like
// the right one.
func TestOnlyTheProcessHoldingTheConversationIsTheSession(t *testing.T) {
	binary := "/Users/x/.nvm/versions/node/v24.1.0/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex"
	if got := classify(binary); got != session.AssistantCodex {
		t.Fatalf("the platform binary is the session, got %q", got)
	}
	if got := classify(binary + " resume c0de0001-0000-4000-8000-000000000001"); got != session.AssistantCodex {
		t.Fatalf("a resumed session is still one, got %q", got)
	}
	for _, command := range []string{
		"node /Users/x/.nvm/versions/node/v24.1.0/bin/codex",
		binary + "-code-mode-host",
		"/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node /Users/x/.codex/plugins/cache/openai-bundled/unified-computer-use/scripts/launch.mjs",
	} {
		if got := classify(command); got != "" {
			t.Fatalf("%q was taken for a %q session", command, got)
		}
	}
}
