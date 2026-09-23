package app

import (
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestLinuxWithoutTmuxIsNeverToldToOpenITermOnAMac(t *testing.T) {
	for _, choice := range []projects.TerminalChoice{projects.TerminalAuto, projects.TerminalITerm} {
		got := unavailableTerminal(choice, "linux")
		if got.Code != "terminal_unsupported" || strings.Contains(got.Message, "not running") ||
			strings.Contains(got.Message, "Mac") {
			t.Errorf("%s: %+v", choice, got)
		}
	}

	mac := unavailableTerminal(projects.TerminalAuto, "darwin")
	if mac.Code != "terminal_closed" || mac.App != "iTerm2" || !strings.Contains(mac.Message, "not running") {
		t.Fatalf("darwin: %+v", mac)
	}
}
