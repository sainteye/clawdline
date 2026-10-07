package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestPairingPromptsUseProductLanguageAndKeepCodeAndDeviceName(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		commandLanguage = language
		if translated, total := cliCatalogCoverage("core", language); translated < 8 || total < 8 {
			t.Fatalf("%s core coverage = %d/%d", language, translated, total)
		}
		var out bytes.Buffer
		printPairingTo(&out, contract.PairingNotice{Name: "Device from a person", Code: "123-456", Expires: 1700000000})
		got := out.String()
		if !strings.Contains(got, "Device from a person") || !strings.Contains(got, "123-456") ||
			!strings.Contains(got, time.Unix(1700000000, 0).Format("15:04:05")) {
			t.Errorf("%s pairing lost authored or machine data: %q", language, got)
		}
		if language != "en" && strings.Contains(got, "wants to pair with this machine") {
			t.Errorf("%s pairing remained English: %q", language, got)
		}
	}
}

func TestGermanLocalAuthFailuresPreservePortAndTokenPath(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	t.Setenv("CLAWDLINE_NEXT_PORT", "not-a-port")
	if _, err := daemonPort(); err == nil || !strings.Contains(err.Error(), "keine gültige Portnummer") ||
		!strings.Contains(err.Error(), `"not-a-port"`) {
		t.Fatalf("invalid port error = %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "local-token")
	if _, err := localToken(config.Config{Dir: dir}); err == nil ||
		!strings.Contains(err.Error(), "kein lokales Token") || !strings.Contains(err.Error(), path) {
		t.Fatalf("missing token error = %v", err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := localToken(config.Config{Dir: dir}); err == nil ||
		!strings.Contains(err.Error(), "ist leer") || !strings.Contains(err.Error(), path) {
		t.Fatalf("empty token error = %v", err)
	}
}
