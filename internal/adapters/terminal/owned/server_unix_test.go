//go:build !windows

package owned

import (
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

func TestAbsentIsOnlyWhatTmuxSaysIsAbsent(t *testing.T) {
	for said, want := range map[string]bool{
		"can't find session: clt-0123":                                 true,
		"no server running on /x/term.sock":                            true,
		"error connecting to /x/term.sock (No such file or directory)": true,
		"error connecting to /x/term.sock (Connection refused)":        true,
		"error connecting to /x/term.sock (Permission denied)":         false,
		"directory /x/tmux has unsafe permissions":                     false,
		"":                                     false,
		"server exited unexpectedly":           false,
		"lost server":                          false,
		"open terminal failed: not a terminal": false,
		"can't find pane: %9":                  true,
		"create window failed: fork failed: Resource temporarily unavailable": false,
	} {
		if absent(said) != want {
			t.Errorf("%q: absent=%v", said, !want)
		}
	}
}

func TestASocketPathTooLongIsTyped(t *testing.T) {
	deep := "/" + strings.Repeat("d", 120)
	_, err := New(deep)
	if code, ok := terminal.CodeOf(err); !ok || code != terminal.CodeSocketPathTooLong {
		t.Fatalf("New(%d-byte dir) = %v", len(deep), err)
	}
	// The longest directory that fits, and one byte more.
	fits := "/" + strings.Repeat("d", sunPathBytes-1-len("/tmux/term.sock")-1)
	if _, err := New(fits); err != nil {
		t.Fatalf("a %d-byte socket path was refused: %v", len(fits)+len("/tmux/term.sock"), err)
	}
	if _, err := New(fits + "d"); err == nil {
		t.Fatal("a 104-byte socket path was accepted")
	}
}
