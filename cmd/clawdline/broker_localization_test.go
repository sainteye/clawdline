package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokerGermanRefusalFramingKeepsStatusCodeAndRawDetail(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	var out, errs bytes.Buffer
	if code := report(&out, &errs, "task", answer{Status: 403,
		Body: []byte(`{"error":"forbidden","detail":"raw upstream detail"}`)}); code != 1 ||
		out.Len() != 0 || !strings.Contains(errs.String(), "abgelehnt") ||
		!strings.Contains(errs.String(), "403 forbidden") ||
		!strings.Contains(errs.String(), "raw upstream detail") {
		t.Fatalf("German refusal framing = %d stdout %q stderr %q", code, out.String(), errs.String())
	}
	errs.Reset()
	if code := report(&out, &errs, "task", answer{Status: 503, Body: []byte("plain upstream")}); code != 1 ||
		!strings.Contains(errs.String(), "Daemon antwortete mit 503") ||
		!strings.Contains(errs.String(), "plain upstream") {
		t.Fatalf("German unmodeled refusal framing = %d %q", code, errs.String())
	}
}

func TestGermanMachineTokenFailuresPreservePathAndDoNotReadSymlink(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator-token")
	if _, err := machineToken(dir); err == nil || !strings.Contains(err.Error(), "kein Orchestrator-Token") ||
		!strings.Contains(err.Error(), path) {
		t.Fatalf("missing token error = %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := machineToken(dir); err == nil || !strings.Contains(err.Error(), "keine reguläre Datei") ||
		!strings.Contains(err.Error(), path) {
		t.Fatalf("symlink token error = %v", err)
	}
}
