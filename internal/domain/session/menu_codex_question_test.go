package session

import (
	"strings"
	"testing"
)

// Codex's MCP tool approval, as its TUI snapshots draw it
// (codex-rs/tui/src/bottom_pane/snapshots, mcp_server_elicitation approval
// forms). On 2026-11-10 a phone showed this card with four buttons and no
// question at all — "Allow Run the tool and continue", "Cancel Cancel this
// tool call" — so nobody could tell what was being approved.
const codexToolApprovalSessionPersist = `
  Field 1/1
  Allow this request?
  › 1. Allow                   Run the tool and continue
    2. Allow for this session  Run the tool and remember this choice for this session
    3. Always allow            Run the tool and remember this choice for future tool calls
    4. Cancel                  Cancel this tool call








  enter to submit | esc to cancel`

const codexToolApprovalParams = `
  Field 1/1
  Allow Calendar to create an event

  Calendar: primary
  Title: Roadmap review
  Notes: This is a deliberately long note that should truncate bef...

  › 1. Allow   Run the tool and continue
    2. Cancel  Cancel this tool call





  enter to submit | esc to cancel`

func TestCodexToolApprovalSaysWhatItAsksAndSplitsItsColumns(t *testing.T) {
	m, ok := ReadMenu(codexToolApprovalSessionPersist, AssistantCodex, false)
	if !ok {
		t.Fatal("no menu on Codex's tool approval")
	}
	if m.CommandApproval || m.Question != "Allow this request?" {
		t.Fatalf("question %q", m.Question)
	}
	if got := labels(m); got != "Allow|Allow for this session|Always allow|Cancel" {
		t.Fatalf("labels %q", got)
	}
	if m.Options[3].Detail != "Cancel this tool call" || m.Options[0].Detail != "Run the tool and continue" {
		t.Fatalf("details %+v", m.Options)
	}
	if m.Selected == nil || *m.Selected != 1 || !m.Numbered {
		t.Fatalf("menu %+v", m)
	}
}

func TestCodexToolApprovalCarriesItsParameters(t *testing.T) {
	m, ok := ReadMenu(codexToolApprovalParams, AssistantCodex, false)
	if !ok {
		t.Fatal("no menu on Codex's tool approval with parameters")
	}
	want := "Allow Calendar to create an event\n\nCalendar: primary\nTitle: Roadmap review\nNotes: This is a deliberately long note that should truncate bef..."
	if m.Question != want {
		t.Fatalf("question %q", m.Question)
	}
}

// The command approval's question is the question and the command it asks
// about, with the command's own line breaks; the two blank rows over it close
// it off from the turn that ran into it.
func TestCodexCommandApprovalIsIdentifiedWhenCommandHeaderClips(t *testing.T) {
	screen := "\n  $ printf fixture\n\n\n" +
		"› 1. Yes, proceed (y)\n" +
		"  2. Yes, and don't ask again for commands that start with `printf` (p)\n" +
		"  3. No, and tell Codex what to do differently (esc)\n\n" +
		"  Press enter to confirm or esc to cancel"
	m, ok := ReadMenu(screen, AssistantCodex, false)
	if !ok || !m.CommandApproval {
		t.Fatalf("clipped command approval was not identified: %+v", m)
	}
}

func TestCodexCommandApprovalSaysTheCommand(t *testing.T) {
	m, ok := ReadMenu(fixture(t, "menu-codex-command-approval.txt"), AssistantCodex, false)
	if !ok {
		t.Fatal("no menu on a Codex command approval")
	}
	if !strings.HasPrefix(m.Question, "Would you like to run the following command?\n\nEnvironment: local\n\n$ if [ -d") ||
		!strings.HasSuffix(m.Question, "npm run build'") || strings.Contains(m.Question, "Show details") {
		t.Fatalf("question %q", m.Question)
	}
	if !m.CommandApproval {
		t.Fatal("native command approval was not identified")
	}
	if m.Options[0].Detail != "" {
		t.Fatalf("a single-column row grew a detail: %+v", m.Options[0])
	}
}

// The Codex 0.166.0 rate-limit prompt has two blank rows before its
// options (chatwidget/tests/status_and_layout.rs).
func TestCodexRateLimitModelSwitch(t *testing.T) {
	screen := fixture(t, "menu-codex-rate-limit.txt")
	for _, padding := range []string{"\n\n\n", "\n\n"} {
		m, ok := ReadMenu(strings.Replace(screen, "\n\n\n", padding, 1), AssistantCodex, false)
		if !ok {
			t.Fatal("no menu on Codex's rate-limit prompt")
		}
		if m.Question != "Approaching rate limits\nSwitch to fixture-mini for lower credit usage?" {
			t.Fatalf("question %q", m.Question)
		}
		if got := labels(m); got != "Switch to fixture-mini|Keep current model|Keep current model (never show again)" {
			t.Fatalf("labels %q", got)
		}
		if m.Options[0].Detail != "Use fewer credits on simpler tasks." || m.Options[2].Detail != "Hide future reminders" {
			t.Fatalf("details %+v", m.Options)
		}
		if m.Selected == nil || *m.Selected != 1 || !m.Numbered || m.CommandApproval {
			t.Fatalf("menu %+v", m)
		}
	}
	draft := strings.ReplaceAll(screen, "2. Keep current model", "2. Write an example")
	if _, ok := ReadMenu(draft, AssistantCodex, false); ok {
		t.Fatal("a numbered draft below the reminder was read as a menu")
	}
}
