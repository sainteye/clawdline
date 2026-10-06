package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGermanSessionCapabilityFailuresPreserveTheSecurityBoundary(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	if _, err := readSquadCapability(""); err == nil ||
		!strings.Contains(err.Error(), squadCapabilityEnv) || !strings.Contains(err.Error(), "nicht gesetzt") {
		t.Fatalf("unset capability = %v", err)
	}
	dir := t.TempDir()
	if _, err := readSquadCapability(dir); err == nil || !strings.Contains(err.Error(), "keine reguläre Datei") {
		t.Fatalf("directory capability = %v", err)
	}
	path := filepath.Join(dir, "capability")
	if err := os.WriteFile(path, []byte("  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSquadCapability(path); err == nil || !strings.Contains(err.Error(), "ungültig") {
		t.Fatalf("invalid capability = %v", err)
	}
}
