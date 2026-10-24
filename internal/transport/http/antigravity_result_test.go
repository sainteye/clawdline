package http

import (
	"os"
	"path/filepath"
	"testing"
)

// beforeWrite exposes the create/write interleaving without timing guesses.
// Only the synthetic test provider publishes these files.
func publishAntigravitySyntheticResult(path string, body []byte, beforeWrite func()) error {
	stage := path + ".writing"
	f, err := os.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(stage)
	defer f.Close()
	if beforeWrite != nil {
		beforeWrite()
	}
	if _, err := f.Write(body); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Presence is the reader's completion signal. It must never observe the
	// create/write gap, including an empty result that looks like bad JSON.
	return os.Rename(stage, path)
}

func TestAntigravitySyntheticResultAppearsOnlyWhenComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-result")
	body := []byte(`{"injectSteps":[{"systemMessage":{"systemMessage":"synthetic original context"}}]}`)
	err := publishAntigravitySyntheticResult(path, body, func() {
		partial, err := os.ReadFile(path)
		if !os.IsNotExist(err) {
			t.Errorf("unfinished synthetic result was visible: %d bytes, err=%v", len(partial), err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("completed result = %q, %v", got, err)
	}
	if _, err := os.Stat(path + ".writing"); !os.IsNotExist(err) {
		t.Fatal("synthetic publication left a staging file", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("result lost private mode", err)
	}
}
