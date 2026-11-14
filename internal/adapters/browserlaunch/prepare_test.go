package browserlaunch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestPrepareKeepsAnExistingPayloadAndReturnsItsRealDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "it's $(printf surprise) state")
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := PrepareUserChrome(dir); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	got, err := PrepareUserChrome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got.Directory) || got.ExtensionID != UnpackedExtensionID(got.Directory) || !strings.Contains(got.ConnectCommand, got.ExtensionID) {
		t.Fatalf("receipt: %+v", got)
	}
	if runtime.GOOS == "darwin" {
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nprintf '%s' \"$1\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		command := exec.Command("/bin/sh", "-c", got.OpenCommand)
		command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
		output, err := command.Output()
		if err != nil || string(output) != got.Directory {
			t.Fatalf("open command changed the folder argument: %q %v", output, err)
		}
	}
	file := filepath.Join(got.Directory, "worker.js")
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareUserChrome(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(file)
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("preparing again replaced a loaded file")
	}
	if err = os.WriteFile(file, []byte("preserve unexpected bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The stable folder is Clawdline's own: a changed copy is replaced by
	// this build's files, and the changed one is kept once beside it.
	again, err := PrepareUserChrome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Directory != got.Directory || again.ExtensionID != got.ExtensionID {
		t.Fatalf("the stable folder moved: %+v then %+v", got, again)
	}
	data, _ := os.ReadFile(file)
	if string(data) == "preserve unexpected bytes" {
		t.Fatal("a changed stable folder was reported ready")
	}
	kept, _ := os.ReadFile(filepath.Join(previousDir(dir), "worker.js"))
	if string(kept) != "preserve unexpected bytes" {
		t.Fatal("the replaced copy was not kept")
	}
}

func TestTheStableFolderCarriesItsMarkerAndVersion(t *testing.T) {
	dir := t.TempDir()
	changed, err := WriteStableExtension(dir)
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	marker, err := os.ReadFile(filepath.Join(StableExtensionDir(dir), FolderMarker))
	if err != nil || !strings.Contains(string(marker), `"stable":true`) || !strings.Contains(string(marker), EmbeddedVersion()) {
		t.Fatalf("marker %s %v", marker, err)
	}
	if EmbeddedVersion() == "" {
		t.Fatal("the build's extension version is unknown")
	}
	if changed, err := WriteStableExtension(dir); err != nil || changed {
		t.Fatalf("a current folder was rewritten: changed=%v err=%v", changed, err)
	}
}
