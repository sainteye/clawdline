package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func TestPreparingUserChromeNeverInstallsItsNativeHost(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", dir)
	var out, errOut strings.Builder
	if code := runBrowser([]string{"prepare", "--mode", "user_chrome"}, os.Getenv, &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, errOut.String())
	}
	var receipt struct {
		Directory string `json:"directory"`
		ID        string `json:"extension_id"`
		Command   string `json:"connect_command"`
	}
	if err := json.Unmarshal([]byte(out.String()), &receipt); err != nil {
		t.Fatal(err)
	}
	if !browserlaunch.ValidExtensionID(receipt.ID) || !strings.HasPrefix(receipt.Directory, dir+string(filepath.Separator)) {
		t.Fatalf("invalid payload receipt: %+v", receipt)
	}
	if _, err := os.Stat(filepath.Join(receipt.Directory, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(receipt.Command, "--mode user_chrome --extension-id "+receipt.ID) {
		t.Fatal("no separate connection command")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "chrome-extension-") {
		t.Fatalf("prepare wrote outside its payload: %v", entries)
	}
}
func TestPreparingChromeRequiresAnExplicitKnownMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", dir)
	for _, args := range [][]string{nil, {"--mode", "native"}, {"--mode", "user_chrome", "extra"}} {
		if code := runBrowser(append([]string{"prepare"}, args...), os.Getenv, &strings.Builder{}, &strings.Builder{}); code != 2 {
			t.Fatal(code)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid request wrote files", err)
	}
}

func TestTheUserChromeChoiceNeverLaunchesAnIsolatedBrowser(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", dir)
	if _, err := nextconfig.Open(dir).Set(map[string]any{"browser_mode": "user_chrome"}); err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if code := runBrowser([]string{"launch", "--browser", "/not/a/browser"}, os.Getenv, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "browser_user_chrome_setup_required") {
		t.Fatalf("wrong profile launch: %d %s", code, errOut.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != nextconfig.FileName {
		t.Fatal("wrong profile launch wrote data", err)
	}
}
