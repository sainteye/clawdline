package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestBrowserKeyboardQueuesAreRegistered(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/session/browser-keyboard.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for symbol, name := range map[string]string{"BROWSER_INPUT_EDITS": "browser.keyboard_pending_edits", "BROWSER_INPUT_BYTES": "browser.keyboard_pending_bytes"} {
		match := regexp.MustCompile(`export const ` + symbol + ` = ([0-9]+)`).FindSubmatch(source)
		if match == nil {
			t.Fatalf("%s not declared", symbol)
		}
		limit, _ := strconv.ParseInt(string(match[1]), 10, 64)
		found := false
		for _, row := range Register() {
			if row.Name == name {
				found = true
				if row.Limit != limit || row.AtLimit != Refuse {
					t.Fatalf("%s: %+v", symbol, row)
				}
			}
		}
		if !found {
			t.Fatalf("%s not registered", name)
		}
	}
}
