package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	"github.com/sainteye/clawdline/internal/config"
)

// TestMain runs the package in a machine of its own. Without it the CLI read
// the developer's saved product_language (so a machine set to Traditional
// Chinese failed every test comparing English copy), their token, and the
// daemon answering on 7727. A test that wants any of these sets it with
// t.Setenv, which restores this baseline afterwards.
func TestMain(m *testing.M) {
	// No test finds, probes or installs from the developer's real Claude Code and Node.
	projectassistantacp.SearchDirectories = func(string) []string { return nil }
	// A test that re-runs this binary as a helper hands it an environment
	// already isolated here, plus the CLAWDLINE_* switches the helper reads.
	if os.Getenv(cliTestIsolated) == "1" {
		os.Exit(m.Run())
	}
	cleanup, err := isolateCLITestEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cmd/clawdline tests:", err)
		os.Exit(2)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

const cliTestIsolated = "CLAWDLINE_CLI_TEST_ISOLATED"

func isolateCLITestEnvironment() (func(), error) {
	root, err := os.MkdirTemp("", "clawdline-cli-test-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { os.RemoveAll(root) }
	// Every CLAWDLINE_* the invoking shell carries — a language, a port, a
	// task secret, a Session's markers — belongs to that shell, not to a test.
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "CLAWDLINE_") {
			os.Unsetenv(name)
		}
	}
	// config.DefaultDir, the installed daemon's directory, resolves under
	// these too; some commands fall back to it when the selected one has no
	// token.
	home := filepath.Join(root, "home")
	for name, dir := range map[string]string{
		"CLAWDLINE_NEXT_DIR": filepath.Join(root, "state"),
		"HOME":               home,
		"XDG_CONFIG_HOME":    filepath.Join(home, ".config"),
		"APPDATA":            filepath.Join(home, "AppData"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			cleanup()
			return nil, err
		}
		os.Setenv(name, dir)
	}
	port, err := closedLoopbackPort()
	if err != nil {
		cleanup()
		return nil, err
	}
	os.Setenv("CLAWDLINE_NEXT_PORT", strconv.Itoa(port))
	os.Setenv(cliTestIsolated, "1")
	return cleanup, nil
}

// closedLoopbackPort is a port nothing listens on, so a command that reaches
// for a daemon the test did not start is refused instead of finding one.
func closedLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

// The baseline TestMain sets is what every other test here relies on; this
// fails on any machine, an English CI runner included, when it is missing.
func TestTheCLITestsRunOnAMachineOfTheirOwn(t *testing.T) {
	if got := currentCLILanguage(); got != "en" {
		t.Errorf("CLI language = %q, want en", got)
	}
	port, err := daemonPort()
	if err != nil || port == config.DefaultPort {
		t.Errorf("daemonPort() = %d, %v; want a port other than the installed daemon's", port, err)
	}
	if dir := config.Dir(); dir == config.DefaultDir() || !strings.HasPrefix(dir, os.TempDir()) {
		t.Errorf("config.Dir() = %q, want a temporary directory apart from %q", dir, config.DefaultDir())
	}
	if os.Getenv(cliTestIsolated) != "1" {
		t.Errorf("%s is not set", cliTestIsolated)
	}
}
