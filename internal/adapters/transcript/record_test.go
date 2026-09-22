package transcript

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every reader of a session's record, as its callers reach it.
var recordReaders = map[string]func(path string) error{
	"ReadClaude": func(p string) error { _, err := ReadClaude(p, 10); return err },
	"ReadCodex":  func(p string) error { _, err := ReadCodex(p, 10); return err },
	"RecordFacts.Read": func(p string) error {
		_, err := NewRecordFacts().Read(p, "claude")
		return err
	},
}

// A record that is not on disk yet is ErrNoRecord, from every reader: the
// state of a session that has just started, which its callers show as empty.
func TestARecordNotWrittenYetIsErrNoRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c6000004-0000-4000-8000-000000000004.jsonl")
	for name, read := range recordReaders {
		err := read(path)
		if !errors.Is(err, ErrNoRecord) {
			t.Errorf("%s: %v, want ErrNoRecord", name, err)
		}
		if err != nil && strings.Contains(err.Error(), filepath.Dir(path)) {
			t.Errorf("%s names the path: %v", name, err)
		}
	}
}

// A record that is there and cannot be read is an UnreadableError, which is
// neither ErrNoRecord nor a sentence with the file's path in it, and which
// still answers errors.Is for the operating system's reason.
func TestAnUnreadableRecordIsTypedAndNamesNoPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	path := filepath.Join(t.TempDir(), "c6000004-0000-4000-8000-000000000004.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	for name, read := range recordReaders {
		err := read(path)
		var unreadable *UnreadableError
		if !errors.As(err, &unreadable) || errors.Is(err, ErrNoRecord) {
			t.Errorf("%s: %v (%T), want an UnreadableError", name, err, err)
			continue
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("%s lost its reason: %v", name, err)
		}
		if strings.Contains(err.Error(), filepath.Dir(path)) || strings.Contains(err.Error(), filepath.Base(path)) {
			t.Errorf("%s names the path: %v", name, err)
		}
	}
}
