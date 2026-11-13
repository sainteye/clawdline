package nextconfig

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestBrowserPolicyKeepsPriorityAndIndependentAutomaticSwitches(t *testing.T) {
	f := Open(t.TempDir())
	p := BrowserPolicy{Order: []string{"native", "isolated", "user_chrome"}, Automatic: map[string]bool{"native": true, "isolated": true, "user_chrome": false}}
	v, err := f.Set(map[string]any{"browser_policy": p})
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.BrowserPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if got.Order[0] != "native" || !got.Automatic["isolated"] || got.Automatic["user_chrome"] {
		t.Fatalf("policy: %+v", got)
	}
	if string(v.Raw["browser_mode"]) != `"native"` || string(v.Raw["browser_enabled"]) != "false" {
		t.Fatal("downgrade could enable another Chrome profile")
	}
	p.Order = []string{"isolated", "native", "user_chrome"}
	if _, err := f.Set(map[string]any{"browser_policy": p}); err != nil {
		t.Fatal(err)
	}
	v, err = f.Read()
	if err != nil {
		t.Fatal(err)
	}
	got, err = v.BrowserPolicy()
	if err != nil || got.Order[0] != "isolated" || !got.Automatic["native"] {
		t.Fatalf("stored: %+v %v", got, err)
	}
}

func TestBrowserPolicyRejectsMissingDuplicateUnknownOrNullSwitches(t *testing.T) {
	for _, raw := range []string{
		`{"order":["native","isolated","isolated"],"automatic":{"native":true,"isolated":true,"user_chrome":false}}`,
		`{"order":["native","isolated","user_chrome"],"automatic":{"native":true,"isolated":null,"user_chrome":false}}`,
		`{"order":["native","isolated","user_chrome"],"automatic":{"native":true,"isolated":false}}`,
		`{"order":["native","isolated","other"],"automatic":{"native":true,"isolated":true,"other":false}}`,
		`{"order":["native","isolated","user_chrome"],"automatic":{"native":true,"isolated":true,"user_chrome":false},"grant":true}`,
	} {
		if _, err := DecodeBrowserPolicy([]byte(raw)); !errors.Is(err, ErrBrowserPolicy) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
}

func TestBrowserPolicyMigrationDoesNotGrantAnUnselectedChrome(t *testing.T) {
	for _, mode := range BrowserKinds() {
		raw, _ := json.Marshal(mode)
		got, err := (Values{Raw: map[string]json.RawMessage{"browser_mode": raw}}).BrowserPolicy()
		if err != nil || got.Order[0] != mode {
			t.Fatalf("%s: %+v %v", mode, got, err)
		}
		for _, chrome := range []string{"isolated", "user_chrome"} {
			if got.Automatic[chrome] != (chrome == mode) {
				t.Fatalf("migration granted %s from %s", chrome, mode)
			}
		}
	}
}

func TestLegacyBrowserWritesCannotReplaceAnOrderedPolicy(t *testing.T) {
	f := Open(t.TempDir())
	policy := BrowserPolicy{Order: []string{"native", "isolated", "user_chrome"}, Automatic: map[string]bool{"native": true, "isolated": false, "user_chrome": false}}
	if _, err := f.Set(map[string]any{"browser_policy": policy}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []map[string]any{{"browser_enabled": true}, {"browser_mode": "user_chrome"}} {
		if _, err := f.Set(change); !errors.Is(err, ErrBrowserPolicy) {
			t.Fatalf("legacy write replaced independent policy: %v", err)
		}
	}
}
