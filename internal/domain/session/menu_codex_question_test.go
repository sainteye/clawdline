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
	if m.Question != "Allow this request?" {
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
func TestCodexCommandApprovalSaysTheCommand(t *testing.T) {
	m, ok := ReadMenu(fixture(t, "menu-codex-command-approval.txt"), AssistantCodex, false)
	if !ok {
		t.Fatal("no menu on a Codex command approval")
	}
	if !strings.HasPrefix(m.Question, "Would you like to run the following command?\n\nEnvironment: local\n\n$ if [ -d") ||
		!strings.HasSuffix(m.Question, "npm run build'") || strings.Contains(m.Question, "Show details") {
		t.Fatalf("question %q", m.Question)
	}
	if m.Options[0].Detail != "" {
		t.Fatalf("a single-column row grew a detail: %+v", m.Options[0])
	}
}
