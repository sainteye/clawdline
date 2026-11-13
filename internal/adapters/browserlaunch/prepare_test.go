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
	if _, err = PrepareUserChrome(dir); err == nil {
		t.Fatal("modified payload reported ready")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "preserve unexpected bytes" {
		t.Fatal("modified files overwritten")
	}
}
