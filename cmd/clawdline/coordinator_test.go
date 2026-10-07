package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCoordinatorBindDelegatesUnknownLivenessToFreshRebind(t *testing.T) {
	for _, tc := range []struct {
		name, inspection, path string
		posts                  int
	}{
		{"first", `{"coordinator":{"configured":false,"status":"unregistered"}}`, "/v1/orchestrator/coordinator/register", 1},
		{"offline", `{"coordinator":{"configured":true,"status":"offline","id":"role-1","generation":3,"session":{"session_id":"old"}}}`, "/v1/orchestrator/coordinator/rebind", 1},
		{"online", `{"coordinator":{"configured":true,"status":"online","id":"role-1","generation":3,"session":{"session_id":"old"}}}`, "", 0},
		{"unknown", `{"coordinator":{"configured":true,"status":"unknown","id":"role-1","generation":3,"session":{"session_id":"old"}}}`, "/v1/orchestrator/coordinator/rebind", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stand, broker := newStandIn(t, func(r *http.Request) (int, string) {
				if r.Method == http.MethodGet {
					return 200, tc.inspection
				}
				return 200, `{"ok":true}`
			})
			var out, errs bytes.Buffer
			code := bindCoordinatorWithWait(&out, &errs, broker, "", envOf(map[string]string{"CODEX_THREAD_ID": thinConversation}), func() {})
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
			if tc.path == "/v1/orchestrator/coordinator/rebind" &&
				(body["expected_coordinator_id"] != "role-1" || body["expected_generation"] != float64(3)) {
				t.Fatalf("rebind lost its guard: %#v", body)
			}
		})
	}
}

func TestCoordinatorBindReinspectsAfterFreshRebindCannotProveLiveness(t *testing.T) {
	reads, posts := 0, 0
	stand, broker := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			reads++
			if reads == 1 {
				return 200, `{"coordinator":{"configured":true,"status":"unknown","id":"role-1","generation":3,"session":{"session_id":"old"}}}`
			}
			return 200, `{"coordinator":{"configured":true,"status":"offline","id":"role-1","generation":3,"session":{"session_id":"old"}}}`
		}
		posts++
		if posts == 1 {
			return 409, `{"error":{"code":"coordinator_liveness_unknown","message":"scan in progress"}}`
		}
		return 200, `{"ok":true}`
	})
	var out, errs bytes.Buffer
	if code := bindCoordinatorWithWait(&out, &errs, broker, thinConversation, envOf(nil), func() {}); code != 0 {
		t.Fatalf("exit %d output=%q error=%q", code, out.String(), errs.String())
	}
	if reads != 2 || posts != 2 {
		t.Fatalf("reads=%d posts=%d requests=%+v", reads, posts, stand.requests())
	}
}

func TestCoordinatorBindCannotReplaceAnOnlineHolderAfterUnknownInspection(t *testing.T) {
	reads, posts := 0, 0
	stand, broker := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodPost {
			posts++
			return 409, `{"error":{"code":"coordinator_online","message":"holder is still online"}}`
		}
		reads++
		return 200, `{"coordinator":{"configured":true,"status":"unknown","id":"role-1","generation":3,"session":{"session_id":"old"}}}`
	})
	var out, errs bytes.Buffer
	if code := bindCoordinatorWithWait(&out, &errs, broker, thinConversation, envOf(nil), func() {}); code == 0 {
		t.Fatalf("online role replaced: %q", out.String())
	}
	if reads != 1 || posts != 1 || len(stand.requests()) != 2 {
		t.Fatalf("unexpected requests: %+v", stand.requests())
	}
}
