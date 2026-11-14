package http

import (
	"fmt"
	"os"
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
	home, err := os.MkdirTemp("", "clawdline-http-test-home-")
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
