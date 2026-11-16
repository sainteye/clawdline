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

func TestBrowserAddressNavigationReachesTheSamePersonRoute(t *testing.T) {
	b, err := goldenBody(json.RawMessage(`{"type":"chrome-tab-input","session":"__clawdline_machine__","request":"navigate-1","tab":9,"op":"navigate","args":{"url":"https://example.com/next"},"browser_mode":"isolated"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, request, ok := served(catalog["chrome-tab-input"])(b)
	if !ok {
		t.Fatal("person address navigation was refused by the Cloud decoder")
	}
	var body struct {
		Op   string `json:"op"`
		Tab  int    `json:"tab"`
		Args struct {
			URL string `json:"url"`
		} `json:"args"`
	}
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Op != "navigate" || body.Tab != 9 || body.Args.URL != "https://example.com/next" {
		t.Fatalf("navigation changed across the encrypted command: %s", request.Body)
	}
}

// A Bot target rides the same chrome words: bot_id with its conversation
// reaches the local request intact, a Bot without its conversation or with an
// unknown field is refused, and the older Session shapes are unchanged.
func TestBrowserBotTargetsRideTheSameChromeWords(t *testing.T) {
	const target = `"bot_id":"機器人 α","conversation":"10000000-0000-4000-8000-000000000051"`
	accepted := []struct{ word, raw string }{
		{"chrome-tab-status", `{"type":"chrome-tab-status","session":"__clawdline_machine__","request":"s",` + target + `}`},
		{"chrome-tab-activity", `{"type":"chrome-tab-activity","session":"__clawdline_machine__","request":"a",` + target + `}`},
		{"chrome-audit", `{"type":"chrome-audit","session":"__clawdline_machine__","request":"u","limit":50,"site":"example.com",` + target + `}`},
		{"chrome-tab-screenshot", `{"type":"chrome-tab-screenshot","session":"__clawdline_machine__","request":"p","tab":"9","quality":"45","browser_mode":"isolated",` + target + `}`},
		{"chrome-tab-control", `{"type":"chrome-tab-control","session":"__clawdline_machine__","request":"c","tab":9,"person":true,"browser_mode":"isolated",` + target + `}`},
		{"chrome-tab-input", `{"type":"chrome-tab-input","session":"__clawdline_machine__","request":"i","tab":9,"op":"click","args":{"x":1,"y":2},` + target + `}`},
		{"chrome-tab-open", `{"type":"chrome-tab-open","session":"__clawdline_machine__","request":"o","url":"https://example.com/","layout":"mobile",` + target + `}`},
		{"chrome-tab-mute", `{"type":"chrome-tab-mute","session":"__clawdline_machine__","request":"m","muted":true,` + target + `}`},
	}
	for _, tc := range accepted {
		b, err := goldenBody(json.RawMessage(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		_, request, ok := served(catalog[tc.word])(b)
		if !ok {
			t.Fatalf("%s refused a Bot target", tc.word)
		}
		got := map[string]any{}
		if request.Method == "GET" {
			for k, v := range request.Query {
				got[k] = v
			}
		} else if err := json.Unmarshal(request.Body, &got); err != nil {
			t.Fatal(err)
		}
		if got["bot_id"] != "機器人 α" || got["conversation"] != "10000000-0000-4000-8000-000000000051" {
			t.Fatalf("%s lost the Bot target: %+v %s", tc.word, request.Query, request.Body)
		}
		partial := cloneBody(b)
		delete(partial, "conversation")
		if _, _, ok := served(catalog[tc.word])(partial); ok {
			t.Fatalf("%s accepted a Bot without its conversation", tc.word)
		}
		unknown := cloneBody(b)
		unknown["owner"] = "bot-0000"
		if _, _, ok := served(catalog[tc.word])(unknown); ok {
			t.Fatalf("%s accepted a client-supplied owner", tc.word)
		}
		dot := cloneBody(b)
		dot["bot_id"] = ".."
		if _, _, ok := served(catalog[tc.word])(dot); ok {
			t.Fatalf("%s accepted a dot-segment Bot id", tc.word)
		}
	}
	// A tab word still refuses a bare conversation: only a Bot names one there.
	b, err := goldenBody(json.RawMessage(`{"type":"chrome-tab-control","session":"__clawdline_machine__","request":"c","tab":9,"person":true,"conversation":"10000000-0000-4000-8000-000000000051"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := served(catalog["chrome-tab-control"])(b); ok {
		t.Fatal("chrome-tab-control accepted a conversation without a Bot")
	}
	b, err = goldenBody(json.RawMessage(`{"type":"chrome-tab-control","session":"__clawdline_machine__","request":"c","tab":9,"person":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := served(catalog["chrome-tab-control"])(b); !ok {
		t.Fatal("the Session shape of chrome-tab-control is refused")
	}
}
