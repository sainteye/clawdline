package cloudops

import (
	"encoding/json"
	"testing"
)

func TestBrowserViewerModeAndMobileLayoutReachTheLocalRequest(t *testing.T) {
	tests := []struct{ word, raw, mode string }{
		{"chrome-tab-input", `{"type":"chrome-tab-input","session":"__clawdline_machine__","request":"input-1","tab":9,"op":"viewport","args":{"mode":"mobile"},"browser_mode":"user_chrome"}`, "user_chrome"},
		{"chrome-tab-control", `{"type":"chrome-tab-control","session":"__clawdline_machine__","request":"control-1","tab":9,"person":true,"browser_mode":"isolated"}`, "isolated"},
		{"chrome-settings-update", `{"type":"chrome-settings-update","session":"__clawdline_machine__","request":"settings-1","changes":{"browser_mode":"native"}}`, "native"},
	}
	for _, tc := range tests {
		b, err := goldenBody(json.RawMessage(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		_, request, ok := served(catalog[tc.word])(b)
		if !ok {
			t.Fatalf("%s refused mode/layout", tc.word)
		}
		var data map[string]any
		if err := json.Unmarshal(request.Body, &data); err != nil {
			t.Fatal(err)
		}
		key := "browser_mode"
		if data[key] != tc.mode {
			t.Fatalf("mode lost: %s", request.Body)
		}
		if tc.word == "chrome-tab-input" && data["args"].(map[string]any)["mode"] != "mobile" {
			t.Fatal("mobile layout lost")
		}
	}
	b, err := goldenBody(json.RawMessage(`{"type":"chrome-tab-screenshot","session":"__clawdline_machine__","request":"shot-1","tab":"9","browser_mode":"user_chrome"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, request, ok := served(catalog["chrome-tab-screenshot"])(b)
	if !ok || request.Query["browser_mode"] != "user_chrome" {
		t.Fatalf("picture mode lost: %+v", request)
	}
	b["browser_mode"] = "other"
	if _, _, ok := served(catalog["chrome-tab-screenshot"])(b); ok {
		t.Fatal("invalid profile mode admitted")
	}
}
