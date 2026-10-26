//go:build darwin || linux

package process

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/session"
	"os"
	"testing"
)

// Opt-in, read-only acceptance against an existing native TUI. It launches no
// provider and sends no message; expected identity must be supplied explicitly.
func TestManagedCodexNativeWriterObservation(t *testing.T) {
	tty, expected := os.Getenv("CLAWDLINE_TEST_CODEX_TTY"), os.Getenv("CLAWDLINE_TEST_CODEX_CONVERSATION")
	if tty == "" || expected == "" {
		t.Skip("native observation not requested")
	}
	inv, err := New().ScanTTYs(context.Background(), map[string]bool{tty: true})
	if err != nil || !inv.Complete || len(inv.Sessions) != 1 {
		t.Fatalf("native inventory: %+v %v", inv, err)
	}
	row := inv.Sessions[0]
	if row.ConversationID != expected || row.Binding != session.BindingOpenFile {
		t.Fatalf("native writer did not bind: %+v", row)
	}
}
