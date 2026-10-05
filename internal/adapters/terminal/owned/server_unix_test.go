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

// A batch tmux 3.7 cannot take in one command goes as several, and no UTF-8
// character is split between two of them.
func TestKeysSplitIntoCallsTmuxCanTakeWithoutSplittingACharacter(t *testing.T) {
	data := []byte(strings.Repeat("a", 511) + "中" + strings.Repeat("b", 600))
	parts := keyCalls(data, 512)
	if len(parts) != 3 || len(parts[0]) != 511 || string(parts[1][:3]) != "中" {
		t.Fatalf("parts of %v bytes", lens(parts))
	}
	if joined := strings.Join(strs(parts), ""); joined != string(data) {
		t.Fatal("the parts are not the batch")
	}
	if parts := keyCalls([]byte("x"), 512); len(parts) != 1 || string(parts[0]) != "x" {
		t.Fatalf("a small batch: %q", parts)
	}
	if parts := keyCalls(make([]byte, terminal.MaxInputBytes), keysPerCall); len(parts) != terminal.MaxInputBytes/keysPerCall {
		t.Fatalf("a batch at the limit: %d calls", len(parts))
	}
}

func lens(parts [][]byte) []int {
	var out []int
	for _, p := range parts {
		out = append(out, len(p))
	}
	return out
}

func strs(parts [][]byte) []string {
	var out []string
	for _, p := range parts {
		out = append(out, string(p))
	}
	return out
}
