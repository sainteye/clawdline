package nextconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A control changes one key. Everything else in the file — keys this daemon has
// never heard of, a hand edit made a second ago — comes through as it was, and
// the file is the person's alone.
func TestASetKeepsEveryOtherKeyAndIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	f := Open(dir)

	first, err := f.Set(map[string]any{"hotkey": "cmd+shift+k"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := first.String("hotkey"); got != "cmd+shift+k" {
		t.Fatalf("hotkey = %q", got)
	}

	// A hand edit between two writes.
	hand := `{"hotkey":"cmd+shift+k","voice_vocabulary":["Clawdline",{"x":1}],"width":880.5}`
	if err := os.WriteFile(f.Path(), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Set(map[string]any{"scope_app": ""}); err != nil {
		t.Fatal(err)
	}

	back, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	var vocab []any
	if err := json.Unmarshal(back.Raw["voice_vocabulary"], &vocab); err != nil || len(vocab) != 2 {
		t.Fatalf("unknown key lost: %s (%v)", back.Raw["voice_vocabulary"], err)
	}
	if string(back.Raw["width"]) != "880.5" {
		t.Fatalf("width = %s", back.Raw["width"])
	}
	if s, ok := back.String("scope_app"); !ok || s != "" {
		t.Fatalf("scope_app = %q %v", s, ok)
	}
	if s, _ := back.String("hotkey"); s != "cmd+shift+k" {
		t.Fatalf("hotkey = %q", s)
	}

	info, err := os.Stat(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v", info.Mode().Perm())
	}
	dinfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dinfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", dinfo.Mode().Perm())
	}
	// Nothing temporary is left beside it.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v", names)
	}
}

// A file with a comma in the wrong place is somebody's; a write that replaced
// it with one key would throw the rest away.
func TestABrokenFileIsRefusedAndLeftAlone(t *testing.T) {
	dir := t.TempDir()
	f := Open(dir)
	for _, body := range []string{`{"hotkey": "cmd+k",}`, `null`, `["hotkey"]`} {
		if err := os.WriteFile(f.Path(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Read(); !errors.Is(err, ErrNotObject) {
			t.Fatalf("%s: read err = %v", body, err)
		}
		if _, err := f.Set(map[string]any{"hotkey": "cmd+j"}); !errors.Is(err, ErrNotObject) {
			t.Fatalf("%s: set err = %v", body, err)
		}
		after, _ := os.ReadFile(f.Path())
		if string(after) != body {
			t.Fatalf("%s: file became %s", body, after)
		}
	}
}

// No file is the first-run answer, not a failure.
func TestNoFileReadsAsDefaults(t *testing.T) {
	v, err := Open(t.TempDir()).Read()
	if err != nil || v.Exists || len(v.Raw) != 0 {
		t.Fatalf("exists=%v raw=%v err=%v", v.Exists, v.Raw, err)
	}
}

// The Swift app's directory is never written, by its own spelling, from
// inside it, or through a link that leads there.
func TestTheSwiftAppsDirectoryIsNeverWritten(t *testing.T) {
	root := t.TempDir()
	swift := filepath.Join(root, "clawdline")
	if err := os.MkdirAll(swift, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "clawdline-next")
	if err := os.Symlink(swift, link); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		swift,
		swift + "/",
		filepath.Join(swift, "nested"),
		filepath.Join(root, "x", "..", "clawdline"),
		link,
		filepath.Join(link, "deeper"),
	} {
		_, err := Open(dir, swift).Set(map[string]any{"hotkey": "cmd+j"})
		if !errors.Is(err, ErrForeignDir) {
			t.Fatalf("%s: err = %v", dir, err)
		}
	}
	entries, _ := os.ReadDir(swift)
	if len(entries) != 0 {
		t.Fatalf("the Swift directory now holds %d entries", len(entries))
	}
	// A sibling that only shares a prefix is not the same directory.
	if _, err := Open(filepath.Join(root, "clawdline-nextdoor"), swift).Set(map[string]any{"hotkey": ""}); err != nil {
		t.Fatalf("sibling refused: %v", err)
	}
}

func TestHotkeysAreWhatTheShellCanRegister(t *testing.T) {
	for _, ok := range []string{"", "cmd+shift+k", "option+space", "⌃⌥space", "control+f5", "f5", "F12", "cmd+`"} {
		if !ValidHotkey(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"k", "f", "space", "cmd+", "cmd+shift+kk", "cmd+f13", "cmd+\x00", strings.Repeat("cmd+", 20) + "k"} {
		if ValidHotkey(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"", "com.googlecode.iterm2", "com.googlecode.iterm2, com.apple.Terminal"} {
		if !ValidScope(ok) {
			t.Errorf("scope %q refused", ok)
		}
	}
	for _, bad := range []string{"com.x;rm", "a b", "com.x\n"} {
		if ValidScope(bad) {
			t.Errorf("scope %q accepted", bad)
		}
	}
}
