package antigravity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookCapabilityPrivateExactLaunch(t *testing.T) {
	root := privateRuntimeRoot(t)
	id := "74000000-0000-4000-8000-000000000003"
	dir := filepath.Join(root, "launches", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "capability")
	if launch, err := LoadHookLaunch(root, ""); err != nil || launch != nil {
		t.Fatal("missing env is not unmanaged")
	}
	valid := strings.Repeat("ab", 32)
	for _, text := range []string{valid, valid + "\n", strings.ToUpper(valid), "short", strings.Repeat("z", 64)} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		launch, err := LoadHookLaunch(root, path)
		if text == valid {
			if err != nil || launch == nil || launch.ID != id || launch.Capability != valid {
				t.Fatal("valid private capability refused")
			}
		} else if err == nil || launch != nil {
			t.Fatal("invalid capability accepted")
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHookLaunch(root, path); err == nil {
		t.Fatal("shared inode accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHookLaunch(root, path); err == nil {
		t.Fatal("public file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "other"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHookLaunch(root, path); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := LoadHookLaunch(root, filepath.Join(root, "other", "capability")); err == nil {
		t.Fatal("escape accepted")
	}
}
