package http

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
)

// Constructors can read legacy project marks before a fixture replaces their
// registry. Keep that first read in an empty home too, so tests never depend on
// the invoking account's marks or its directory permissions.
func TestMain(m *testing.M) {
	// No test finds, probes or installs from the developer's real Claude Code and Node.
	projectassistantacp.SearchDirectories = func(string) []string { return nil }
	// Helper copies inherit this home; only its owning process removes it.
	if os.Getenv("CLAWDLINE_HTTP_TEST_ISOLATED") == "1" {
		os.Exit(m.Run())
	}
	home, err := isolatedTestHome(os.TempDir(), os.Getpid())
	if err != nil {
		fmt.Fprintln(os.Stderr, "create isolated test home:", err)
		os.Exit(2)
	}
	if err = os.Setenv("HOME", home); err != nil {
		_ = os.RemoveAll(home)
		fmt.Fprintln(os.Stderr, "select isolated test home:", err)
		os.Exit(2)
	}
	if err = os.Setenv("CLAWDLINE_HTTP_TEST_ISOLATED", "1"); err != nil {
		_ = os.RemoveAll(home)
		fmt.Fprintln(os.Stderr, "mark isolated test environment:", err)
		os.Exit(2)
	}
	code := m.Run()
	if err = os.RemoveAll(home); err != nil {
		fmt.Fprintln(os.Stderr, "remove isolated test home:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// testHomesDir holds one isolated home per running test binary, named
// "<pid>-<random>". A binary killed by -timeout or a signal never reaches its
// RemoveAll, so the next run removes every home whose named process is
// provably gone. A live, reused or unreadable PID keeps its home.
const testHomesDir = "clawdline-http-test-homes"

func isolatedTestHome(tmp string, pid int) (string, error) {
	parent := filepath.Join(tmp, testHomesDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	sweepAbandonedTestHomes(parent, pid)
	return os.MkdirTemp(parent, strconv.Itoa(pid)+"-")
}

// sweepAbandonedTestHomes removes the homes of test binaries that are no
// longer running and reports how many it removed. It never looks outside
// parent, and a name it cannot parse is not its to remove.
func sweepAbandonedTestHomes(parent string, self int) int {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		owner, _, ok := strings.Cut(e.Name(), "-")
		pid, err := strconv.Atoi(owner)
		if !ok || err != nil || pid <= 0 || pid == self || !e.IsDir() || !testHomeOwnerGone(pid) {
			continue
		}
		if os.RemoveAll(filepath.Join(parent, e.Name())) == nil {
			removed++
		}
	}
	return removed
}
