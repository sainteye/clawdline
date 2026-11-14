package projectassistantacp

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// No test finds, probes or installs from the developer's real Claude Code
	// and Node; tests that need tools supply their own directories.
	SearchDirectories = func(string) []string { return nil }
	os.Exit(m.Run())
}
