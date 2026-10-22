package antigravity

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func privateRuntimeRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("native Unix contract")
	}
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProviderStateDirReadDoesNotCreateAndRejectsUnsafe(t *testing.T) {
	root := privateRuntimeRoot(t)
	path, err := ProviderStateDir(root)
	if err != nil || path != filepath.Join(root, "antigravity") {
		t.Fatalf("state path: %q %v", path, err)
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("read created state")
	}
	for _, bad := range []string{"relative", root + "/../bad"} {
		if _, err = ProviderStateDir(bad); err == nil {
			t.Fatal("unsafe root accepted")
		}
	}
	if err = os.Symlink(root, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err = ProviderStateDir(filepath.Join(root, "alias")); err == nil {
		t.Fatal("symlink accepted")
	}
	if err = os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = ProviderStateDir(root); err == nil {
		t.Fatal("public state accepted")
	}
}
