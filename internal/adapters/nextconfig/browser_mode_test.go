package nextconfig

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBrowserModeMigrationNeverGrantsUserChrome(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{}`, BrowserModeNative}, {`{"browser_enabled":false}`, BrowserModeNative}, {`{"browser_enabled":true}`, BrowserModeIsolated},
		{`{"browser_enabled":true,"browser_mode":"invalid"}`, BrowserModeNative},
		{`{"browser_enabled":true,"browser_mode":"native"}`, BrowserModeNative},
		{`{"browser_mode":"user_chrome"}`, BrowserModeUserChrome},
	} {
		v := Values{}
		if err := json.Unmarshal([]byte(tc.raw), &v.Raw); err != nil {
			t.Fatal(err)
		}
		if got := v.BrowserMode(); got != tc.want {
			t.Errorf("%s: %s", tc.raw, got)
		}
	}
}
func TestBrowserModeAndLegacyConsentRemainOneChoice(t *testing.T) {
	f := Open(t.TempDir())
	set := func(changes map[string]any, want string) {
		t.Helper()
		v, err := f.Set(changes)
		if err != nil {
			t.Fatal(err)
		}
		on, _ := v.Bool("browser_enabled")
		if v.BrowserMode() != want || on != (want != BrowserModeNative) {
			t.Fatalf("%v: %s %v", changes, v.BrowserMode(), on)
		}
	}
	set(map[string]any{"browser_enabled": true}, BrowserModeIsolated)
	set(map[string]any{"browser_mode": BrowserModeUserChrome}, BrowserModeUserChrome)
	set(map[string]any{"browser_enabled": true}, BrowserModeUserChrome)
	before, _ := os.ReadFile(f.Path())
	for _, changes := range []map[string]any{{"browser_mode": "invalid"}, {"browser_mode": BrowserModeNative, "browser_enabled": true}} {
		if _, err := f.Set(changes); err == nil {
			t.Fatalf("accepted %v", changes)
		}
		after, _ := os.ReadFile(f.Path())
		if string(before) != string(after) {
			t.Fatal("invalid mode changed consent")
		}
	}
	set(map[string]any{"browser_enabled": false}, BrowserModeNative)
	set(map[string]any{"browser_enabled": true}, BrowserModeIsolated)
}

func TestUserChromeFailsClosedForAnOlderBooleanOnlyReader(t *testing.T) {
	f := Open(t.TempDir())
	for _, change := range []map[string]any{{"browser_mode": BrowserModeUserChrome}, {"browser_enabled": true}} {
		values, err := f.Set(change)
		if err != nil {
			t.Fatal(err)
		}
		enabled, _ := values.Bool("browser_enabled")
		if !enabled || values.BrowserMode() != BrowserModeUserChrome {
			t.Fatal("new reader lost user Chrome choice")
		}
		var oldReader bool
		if err := json.Unmarshal(values.Raw["browser_enabled"], &oldReader); err != nil {
			t.Fatal(err)
		}
		if oldReader {
			t.Fatal("a downgraded daemon could reinterpret user Chrome as permission for its old isolated channel")
		}
	}
}
