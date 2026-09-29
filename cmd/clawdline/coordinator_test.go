package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCoordinatorBindRegistersOrRebindsOnlyAnOfflineRole(t *testing.T) {
	for _, tc := range []struct {
		name, inspection, path string
		posts                  int
	}{
		{"first", `{"coordinator":{"configured":false,"status":"unregistered"}}`, "/v1/orchestrator/coordinator/register", 1},
		{"offline", `{"coordinator":{"configured":true,"status":"offline","id":"role-1","generation":3,"session":{"session_id":"old"}}}`, "/v1/orchestrator/coordinator/rebind", 1},
		{"online", `{"coordinator":{"configured":true,"status":"online","id":"role-1","generation":3,"session":{"session_id":"old"}}}`, "", 0},
		{"unknown", `{"coordinator":{"configured":true,"status":"unknown","id":"role-1","generation":3,"session":{"session_id":"old"}}}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stand, broker := newStandIn(t, func(r *http.Request) (int, string) {
				if r.Method == http.MethodGet {
					return 200, tc.inspection
				}
				return 200, `{"ok":true}`
			})
			var out, errs bytes.Buffer
			code := bindCoordinator(&out, &errs, broker, "", envOf(map[string]string{"CODEX_THREAD_ID": thinConversation}))
			if (code == 0) != (tc.posts == 1) {
				t.Fatalf("exit %d output=%q error=%q", code, out.String(), errs.String())
			}
			seen := stand.requests()
			if len(seen) != 1+tc.posts || seen[0].EscapedPath != "/v1/orchestrator/coordinator" {
				t.Fatalf("requests: %+v", seen)
			}
			if tc.posts == 0 {
				return
			}
			if seen[1].EscapedPath != tc.path || seen[1].Token != thinToken {
				t.Fatalf("bind request: %+v", seen[1])
			}
			var body map[string]any
			if err := json.Unmarshal(seen[1].Body, &body); err != nil || body["session_id"] != thinConversation {
				t.Fatalf("bind body: %s", seen[1].Body)
			}
			if tc.name == "offline" && (body["expected_coordinator_id"] != "role-1" || body["expected_generation"] != float64(3)) {
				t.Fatalf("rebind lost its guard: %#v", body)
			}
		})
	}
}
