package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const verifyRecordAnswer = `{"at":1790000000,"verification":{"id":"v-1","title":"a check","why":"because",
"started_at":1789400000,"due_at":1790090000,"criteria":[
 {"index":0,"text":"cheaper","state":"passed","updated_at":1789900000},
 {"index":1,"text":"no worse","state":"unset","updated_at":0}],
"source":{"kind":"compaction_compare","since":"14d"},"schedule_id":"sched-1",
"notes":[{"id":1,"at":1789950000,"author_kind":"session","author":"task:t1","text":"readout"}],
"status":"open","close_reason":"","closed_at":0,"seed":"","created_at":1789400000,"updated_at":1789950000}}`

// add sends the record the flags spell, with the due time read in the
// machine's zone and the data source only when it was named.
func TestVerifyAddSendsWhatTheFlagsSay(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, verifyRecordAnswer })
	var out, errs bytes.Buffer
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, loc)
	code := runVerify(&out, &errs, b, "add", []string{"--title", "a check", "--due", "2026-10-03 09:00",
		"--criterion", "cheaper", "--criterion", "no worse", "--compaction-since", "14d", "--schedule", "sched-1"},
		envOf(nil), now)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodPost || seen[0].EscapedPath != "/v1/verifications" ||
		!strings.HasPrefix(seen[0].Key, "verify-add-") {
		t.Fatalf("%+v", seen)
	}
	var sent map[string]any
	if err := json.Unmarshal(seen[0].Body, &sent); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 3, 9, 0, 0, 0, loc).Unix()
	if sent["title"] != "a check" || sent["due_at"] != float64(due) || sent["schedule_id"] != "sched-1" ||
		len(sent["criteria"].([]any)) != 2 {
		t.Fatalf("sent %s", seen[0].Body)
	}
	if src, _ := sent["source"].(map[string]any); src["kind"] != "compaction_compare" || src["since"] != "14d" {
		t.Fatalf("source %v", sent["source"])
	}
	if !strings.Contains(out.String(), "[✓] 0. cheaper") || !strings.Contains(out.String(), "note ") {
		t.Fatalf("printed:\n%s", out.String())
	}

	// Without --compaction-since there is no source at all.
	s2, b2 := newStandIn(t, func(r *http.Request) (int, string) { return 201, verifyRecordAnswer })
	runVerify(&out, &errs, b2, "add", []string{"--title", "t", "--due", "7d", "--criterion", "c"}, envOf(nil), now)
	if body := string(s2.requests()[0].Body); strings.Contains(body, "source") ||
		!strings.Contains(body, `"due_at":`+jsonNumber(now.Add(7*24*time.Hour).Unix())) {
		t.Fatalf("sent %s", body)
	}
}

// Each subcommand asks its own route; a note is signed with the calling
// session; delete takes exactly one id and asks force only when told.
func TestVerifySubcommandsAskTheirRoutes(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	for _, tc := range []struct {
		sub          string
		args         []string
		method, path string
		query, body  string
	}{
		{"list", nil, http.MethodGet, "/v1/verifications", "", ""},
		{"show", []string{"v-1"}, http.MethodGet, "/v1/verifications/v-1", "", ""},
		{"note", []string{"v-1", "looked twice"}, http.MethodPost, "/v1/verifications/v-1/notes", "",
			`{"session":"conv-9","text":"looked twice"}`},
		{"done", []string{"v-1", "--rejected", "it cost more"}, http.MethodPost, "/v1/verifications/v-1/close", "",
			`{"reason":"it cost more","status":"rejected"}`},
		{"delete", []string{"v-1"}, http.MethodDelete, "/v1/verifications/v-1", "", ""},
		{"delete", []string{"v-1", "--force"}, http.MethodDelete, "/v1/verifications/v-1", "force=1", ""},
	} {
		s, b := newStandIn(t, func(r *http.Request) (int, string) {
			if r.URL.Path == "/v1/verifications" && r.Method == http.MethodGet {
				return 200, `{"at":1790000000,"verifications":[` + strings.TrimSuffix(strings.SplitN(verifyRecordAnswer, `"verification":`, 2)[1], "}") + `]}`
			}
			if r.Method == http.MethodDelete {
				return 204, ""
			}
			return 200, verifyRecordAnswer
		})
		var out, errs bytes.Buffer
		if code := runVerify(&out, &errs, b, tc.sub, tc.args, envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": "conv-9"}), now); code != 0 {
			t.Fatalf("%s: exit %d: %s", tc.sub, code, errs.String())
		}
		seen := s.requests()
		if len(seen) != 1 || seen[0].Method != tc.method || seen[0].EscapedPath != tc.path || seen[0].Query != tc.query {
			t.Fatalf("%s: %+v", tc.sub, seen)
		}
		if tc.body != "" && string(seen[0].Body) != tc.body {
			t.Fatalf("%s sent %s", tc.sub, seen[0].Body)
		}
		if tc.sub == "list" && !strings.Contains(out.String(), "v-1  open      due in 1d 1h") {
			t.Fatalf("list printed:\n%s", out.String())
		}
	}
}

// A misused command asks nothing: no id, two ids, a bulk flag, a verdict
// that is neither.
func TestVerifyRefusesAMisuseBeforeAsking(t *testing.T) {
	for _, tc := range []struct {
		sub  string
		args []string
	}{
		{"delete", nil},
		{"delete", []string{"v-1", "v-2"}},
		{"delete", []string{"--all"}},
		{"delete", []string{"v-1", "--all"}},
		{"done", []string{"v-1", "--maybe", "x"}},
		{"done", []string{"v-1", "--accepted"}},
		{"note", []string{"v-1"}},
		{"show", nil},
		{"add", []string{"--title", "t"}},
		{"add", []string{"--title", "t", "--due", "next week", "--criterion", "c"}},
		{"purge", nil},
	} {
		s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, "{}" })
		var out, errs bytes.Buffer
		if code := runVerify(&out, &errs, b, tc.sub, tc.args, envOf(nil), time.Now()); code != 2 || len(s.requests()) != 0 {
			t.Fatalf("%s %v: exit %d, asked %d", tc.sub, tc.args, code, len(s.requests()))
		}
	}
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
