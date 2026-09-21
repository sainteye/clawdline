package board

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
)

// With the legacy switch off the Swift board and its card logs are not
// opened, and the answer says `disabled` — known, and not `absent`. The
// control is the same file read with the switch on.
func TestTheSwiftBoardIsNotReadWhenSwitchedOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project-board.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"revision":7,"items":[],"projects":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(HistoryDir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv(swiftstore.EnvLegacyStore, "on")
	if state, _, err := OpenLegacy(path).Read(); err != nil || state == nil || state.Revision != 7 {
		t.Fatalf("control: the board was not read with the switch on: %v %v", state, err)
	}
	if _, src := OpenHistory(HistoryDir(path)).Read(nil); src.Status != "ok" {
		t.Fatalf("control: card logs %+v", src)
	}

	t.Setenv(swiftstore.EnvLegacyStore, "off")
	state, _, err := OpenLegacy(path).Read()
	if state != nil || !errors.Is(err, ErrLegacyDisabled) {
		t.Fatalf("switched off, the board read %v, %v", state, err)
	}
	if _, src := OpenHistory(HistoryDir(path)).Read([]string{"x"}); src.Status != "disabled" {
		t.Fatalf("switched off, card logs %+v", src)
	}
}
