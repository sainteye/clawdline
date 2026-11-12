package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Chromium names an unpacked extension by the SHA-256 of its path, each hex
// digit spelled a-p; TestBrowserBridgeEndToEnd shows Chromium accepting the
// host manifest written with this id. The expected values are that rule
// worked by hand for two short paths.
func TestUnpackedExtensionIDSpellsTheHashInAToP(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/ext":              "lcfjooiecahccmjaipimfaidcnaihadb",
		"/opt/clawdline/chrome": "",
	} {
		got := unpackedExtensionID(path)
		if want != "" && got != want {
			t.Errorf("%s: id = %s, want %s", path, got, want)
		}
		if len(got) != 32 || strings.Trim(got, "abcdefghijklmnop") != "" {
			t.Errorf("%s: id %q is not 32 letters a-p", path, got)
		}
	}
}

func TestTheEmbeddedExtensionIsWrittenWhole(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chrome-extension")
	if err := writeExtension(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "worker.js", "page.js", "popup.html", "popup.js", "_locales/en/messages.json", "_locales/zh_TW/messages.json", "icons/icon-128.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestTypeFromStdinIsMarkedSecretAndLosesOneNewline(t *testing.T) {
	var errOut strings.Builder
	args, _, code := browserArgs("type", []string{"--stdin"}, strings.NewReader("hunter2\n"), &errOut)
	if code != 0 || args["text"] != "hunter2" || args["secret"] != true {
		t.Fatalf("args %v code %d %s", args, code, errOut.String())
	}
	if _, _, code := browserArgs("type", []string{"--stdin"}, strings.NewReader(""), &errOut); code != 2 {
		t.Fatal("empty standard input was accepted")
	}
}
