package main

import (
	"bytes"
	"testing"
)

// TestReportPrintsRefusalExtras: a refusal's scalar extras follow its code
// line, one `key: value` each and sorted, and its remediation comes last;
// nested values and the envelope's own keys are not repeated.
func TestReportPrintsRefusalExtras(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"broker envelope", `{"error":{"code":"stale_inventory","message":"Read it again.","request_id":"r1",` +
			`"retry_after":3,"blocking_task":"t-9","nested":{"a":1},` +
			`"remediation":{"available":true,"command":"curl --fail-with-body -sS 'http://127.0.0.1:<port>/v1/x'"}}}`,
			"clawdline x: refused, 409 stale_inventory: Read it again.\n" +
				"blocking_task: t-9\nretry_after: 3\n" +
				"remediation: curl --fail-with-body -sS 'http://127.0.0.1:<port>/v1/x'\n"},
		{"remedy without a route", `{"error":{"code":"succession_required","message":"No.",` +
			`"remediation":{"available":false,"because":"Not implemented\non this daemon."}}}`,
			"clawdline x: refused, 409 succession_required: No.\nremediation: Not implemented on this daemon.\n"},
		{"flat envelope", `{"error":"invalid_transition","detail":"Cannot.","route":"/v1/work"}`,
			"clawdline x: refused, 409 invalid_transition: Cannot.\nroute: /v1/work\n"},
		{"nothing extra", `{"error":{"code":"unauthorized","message":"No token.","request_id":"r2"}}`,
			"clawdline x: refused, 409 unauthorized: No token.\n"},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if code := report(&stdout, &stderr, "x", answer{Status: 409, Body: []byte(c.body)}); code != 1 {
			t.Errorf("%s: exit %d, want 1", c.name, code)
		}
		if got := stderr.String(); got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}
