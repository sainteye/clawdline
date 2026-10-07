package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestNewReleaseCopyUsesChineseAndKeepsMachineValues(t *testing.T) {
	entryLanguage(t, "zh-Hant")
	if len(cliCatalogGroups["setup"].locales["zh-Hant"]) != len(cliCatalogGroups["setup"].english) {
		t.Fatal("Traditional Chinese setup catalog is incomplete")
	}
	var out bytes.Buffer
	printNext(setupHost{out: &out}, setupOptions{port: 7727}, nextStep{
		version: "vX.Y.Z", command: "clawdline", opened: "Clawdline Next.app", found: map[string]string{"codex": "/usr/bin/codex"},
	})
	for _, want := range []string{"接下來", "登入", "tmux", "clawdline setup --uninstall", "vX.Y.Z", "http://127.0.0.1:7727"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("setup guidance lacks %q: %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Sign in:") || strings.Contains(out.String(), "Next:") {
		t.Errorf("setup guidance fell back to English: %q", out.String())
	}
	out.Reset()
	printUpdate(&out, contract.UpdateStatus{
		InstallKind: contract.UpdateInstallKindRelease,
		State:       contract.UpdateStateCurrent,
		Latest:      contract.BuildStamp{Version: "vX.Y.Z"},
		SourceURL:   "https://example.invalid/release",
	})
	for _, want := range []string{"安裝類型", "狀態", "vX.Y.Z", "https://example.invalid/release"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("update status lacks %q: %q", want, out.String())
		}
	}
	out.Reset()
	if code := setupRefuse(setupHost{errOut: &out}, &install.Refusal{
		Code: install.CodePortHeld, Detail: "port 7727 belongs to another process",
	}); code != 1 {
		t.Fatalf("setup refusal exit = %d", code)
	}
	for _, want := range []string{"安裝未變更", "--port", install.CodePortHeld, "port 7727"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("setup safety refusal lacks %q: %q", want, out.String())
		}
	}
}

func TestLaterReleaseKeysFallBackToEnglishInSecondaryLanguage(t *testing.T) {
	entryLanguage(t, "ja")
	var out bytes.Buffer
	printNext(setupHost{out: &out}, setupOptions{port: 7727}, nextStep{version: "vX.Y.Z", command: "clawdline", found: map[string]string{}})
	if !strings.Contains(out.String(), "Next:") || !strings.Contains(out.String(), "Sign in:") {
		t.Fatalf("missing later setup keys did not fall back to English: %q", out.String())
	}
	bundle := cliCatalogGroups["entry"]
	copyOfJapanese := make(map[string]string, len(bundle.locales["ja"]))
	for key, value := range bundle.locales["ja"] {
		copyOfJapanese[key] = value
	}
	delete(copyOfJapanese, bundle.baseline[0])
	if hasCLIBaseline(copyOfJapanese, bundle.baseline) {
		t.Fatal("a missing first-release key was accepted as later copy")
	}
}
