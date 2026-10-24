package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/release"
)

func TestDMGDeliveryDoesNotChangeUpdaterManifest(t *testing.T) {
	dir := t.TempDir()
	version := "v2.1.2"
	archive := "Clawdline-" + version + "-macos-arm64.tar.gz"
	for _, name := range []string{archive, "Clawdline-" + version + "-macos-arm64.dmg", "Clawdline-" + version + "-macos-arm64.dmg.delivery.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := manifest([]string{"-version", version, "-commit", "239adc8ad4020f31509bff017bd4e5a3ff913197", "-committed-at", "2026-10-22T00:00:00Z", "-base-url", "https://example.invalid/" + version, dir}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m release.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0].Name != archive || m.Artifacts[0].Kind != release.KindApp {
		t.Fatalf("delivery container entered updater manifest: %+v", m.Artifacts)
	}
}
