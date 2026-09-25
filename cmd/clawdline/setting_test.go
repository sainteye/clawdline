package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// `clawdline setting` against a settings route that keeps what it was sent,
// the way /v1/settings does.
func TestSettingGetsAndSetsTheCompactionWindow(t *testing.T) {
	stored := map[string]any{}
	var posted []map[string]any
	call := func(method string, body any) (int, []byte, error) {
		if method == http.MethodPost {
			change := body.(map[string]any)
			posted = append(posted, change)
			if n, _ := change["claude_auto_compact_window"].(int64); n != 0 && n < 50000 {
				return 400, []byte(`{"error":"invalid_auto_compact_window","detail":"claude_auto_compact_window: 0 for none, or a whole number of tokens from 50000 to 1000000, not 5"}`), nil
			}
			for k, v := range change {
				stored[k] = v
			}
		}
		data, _ := json.Marshal(stored)
		return 200, data, nil
	}
	run := func(args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		code := runSetting(&out, &errs, args, call)
		return code, out.String(), errs.String()
	}

	if code, out, _ := run("get", "claude_auto_compact_window"); code != 0 || !strings.Contains(out, "off") {
		t.Fatalf("get with nothing set: %d %q", code, out)
	}
	if code, out, _ := run("set", "claude_auto_compact_window", "120000"); code != 0 || out != "claude_auto_compact_window: 120000 tokens\n" {
		t.Fatalf("set: %d %q", code, out)
	}
	if got := posted[len(posted)-1]["claude_auto_compact_window"]; got != int64(120000) {
		t.Fatalf("set posted %#v", got)
	}
	if code, out, _ := run("set", "claude_auto_compact_window", "off"); code != 0 || !strings.Contains(out, "off") {
		t.Fatalf("off: %d %q", code, out)
	}
	if got := posted[len(posted)-1]["claude_auto_compact_window"]; got != int64(0) {
		t.Fatalf("off posted %#v, want 0", got)
	}
	// The daemon's refusal is said with its code; a value that is not a
	// number never reaches it; nor does a key this command does not set.
	sent := len(posted)
	if code, _, errs := run("set", "claude_auto_compact_window", "5"); code != 1 || !strings.Contains(errs, "invalid_auto_compact_window") {
		t.Fatalf("refused: %d %q", code, errs)
	}
	if code, _, _ := run("set", "claude_auto_compact_window", "lots"); code != 2 {
		t.Fatalf("a word: %d", code)
	}
	if code, _, _ := run("set", "hotkey", "cmd+k"); code != 2 {
		t.Fatalf("another key: %d", code)
	}
	if len(posted) != sent+1 {
		t.Fatalf("%d writes reached the daemon, want only the one it refused", len(posted)-sent)
	}
}

// The usage words for a session's window: none said as none, and nothing
// said for a session Clawdline did not launch.
func TestUsageSaysTheWindowInWords(t *testing.T) {
	zero, n := int64(0), int64(60000)
	for _, c := range []struct {
		w    *int64
		want string
	}{{nil, ""}, {&zero, "launched with no compaction window"}, {&n, "launched to compact at 60.0k"}} {
		if got := usageWindow(c.w); got != c.want {
			t.Errorf("%v: %q, want %q", c.w, got, c.want)
		}
	}
}
