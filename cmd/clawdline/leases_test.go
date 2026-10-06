package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const leaseAnswer = `{"at":1700000000,"leases":[{"resource":"landing","key":"/repo","queue_depth":1,
 "holder":{"holder":"task-a","lease_id":"l1","liveness":"alive","liveness_reason":"session_live","held_seconds":42,
  "reason":"landing  item-1\non main","request_id":"r1","acquired_at":1,"renewed_at":2,"renewal_age_seconds":3},
 "queue":[{"holder":"task-b","position":1,"proving":false,"reason":"landing item-2","request_id":"r2",
  "requested_at":5,"waited_seconds":7}]}]}`

// `leases` reads the daemon's lease list and prints a row per lease with its
// holder, then one line per waiter.
func TestLeasesPrintsTheHolderAndTheWaiters(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, leaseAnswer })
	var out, errs bytes.Buffer
	if code := leasesRun(&out, &errs, b, false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 1 || seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/orchestrator/leases" ||
		seen[0].Token != thinToken {
		t.Fatalf("requests = %+v", seen)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "RESOURCE KEY HOLDER LIVENESS HELD REASON WAITING" ||
		strings.Join(strings.Fields(lines[1]), " ") != "landing /repo task-a alive 42s landing item-1 on main 1" ||
		strings.TrimSpace(lines[2]) != "waiting for landing /repo: #1 task-b, 7s: landing item-2" {
		t.Fatalf("stdout:\n%s", out.String())
	}
}

func TestLeasesGermanHeadingKeepsLeaseIdentity(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	_, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, leaseAnswer })
	var out, errs bytes.Buffer
	if code := leasesRun(&out, &errs, b, false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	got := out.String()
	if !strings.Contains(got, "RESSOURCE") || !strings.Contains(got, "WARTENDE") ||
		!strings.Contains(got, "landing") || !strings.Contains(got, "/repo") ||
		!strings.Contains(got, "task-a") || !strings.Contains(got, "landing item-1 on main") {
		t.Fatalf("German lease heading or raw values: %q", got)
	}
}

// --json prints the daemon's answer itself.
func TestLeasesJSONPrintsTheAnswer(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, leaseAnswer })
	var out, errs bytes.Buffer
	if code := leasesRun(&out, &errs, b, true); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	var got, want any
	if json.Unmarshal(out.Bytes(), &got) != nil || json.Unmarshal([]byte(leaseAnswer), &want) != nil {
		t.Fatalf("stdout is not JSON: %s", out.String())
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Fatalf("stdout %s, want %s", g, w)
	}
}

// An answer that is no lease list is an error, never an empty table.
func TestLeasesRefusesAnAnswerThatIsNoList(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"500", `{"error":"internal","detail":"boom"}`, 500},
		{"no list", `{"ok":true}`, 200},
		{"not JSON", `<html>`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, b := newStandIn(t, func(r *http.Request) (int, string) { return tc.status, tc.body })
			for _, asJSON := range []bool{false, true} {
				var out, errs bytes.Buffer
				if code := leasesRun(&out, &errs, b, asJSON); code == 0 || out.Len() != 0 || errs.Len() == 0 {
					t.Fatalf("json=%t: exit %d, stdout %q, stderr %q", asJSON, code, out.String(), errs.String())
				}
			}
		})
	}
}
