package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// `clawdline task land` is the landing a root records by hand, with the
// orchestrator token; the completion notice names it, and every command a
// notice names has to be one this binary runs (2026-10-02).

// landingKeys is every key the landing route reads; any other is refused
// (internal/transport/http/orchestrator.go, brokerLanding).
var landingKeys = map[string]bool{"state": true, "target": true, "delivery": true, "commit": true,
	"carrier_task": true, "note": true}

// The flags come after the two words, as the notice writes them, and before
// them as well; only what was given is sent, with the orchestrator token.
func TestTaskLandSendsTheLandingWithTheOrchestratorToken(t *testing.T) {
	inv, err := landArgs([]string{taskTestID, "landed", "--target", "main", "--commit", "abc123", "--port", "7791"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.port != 7791 || inv.id != taskTestID {
		t.Fatalf("parsed %+v", inv)
	}
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"task":{"id":"` + taskTestID + `","landing":{"state":"landed","target":"main","commit":"abc123def"}}}`
	})
	var out, errs bytes.Buffer
	if code := landTask(&out, &errs, b, inv); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodPost || seen[0].Token != thinToken ||
		seen[0].EscapedPath != "/v1/orchestrator/tasks/"+taskTestID+"/landing" {
		t.Fatalf("asked %+v", seen)
	}
	var body map[string]string
	if err := json.Unmarshal(seen[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 3 || body["state"] != "landed" || body["target"] != "main" || body["commit"] != "abc123" {
		t.Fatalf("sent %s", seen[0].Body)
	}
	if out.String() != "recorded "+taskTestID+" landing landed on main at abc123def\n" {
		t.Fatalf("stdout: %q", out.String())
	}

	before, err := landArgs([]string{"--note", "picked by hand", taskTestID, "abandoned"})
	if err != nil || before.body["note"] != "picked by hand" || before.body["state"] != "abandoned" || len(before.body) != 2 {
		t.Fatalf("flags before the words: %+v %v", before, err)
	}
}

// A mistake is caught before anything is sent; the broker's refusal is
// printed code first and exits 1.
func TestTaskLandRefusesWhatTheRouteWouldAndSaysTheBrokersNo(t *testing.T) {
	for _, args := range [][]string{
		{taskTestID},
		{taskTestID, "merged"},
		{"not-a-task", "abandoned"},
		{taskTestID, "abandoned", "extra"},
		{taskTestID, "landed", "--branch", "main"},
	} {
		if _, err := landArgs(args); err == nil {
			t.Errorf("%q was accepted", args)
		}
	}
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 409, `{"error":{"code":"wrote_to_repository","message":"nothing_to_land says this task wrote nothing to land, and its checkout has uncommitted changes."}}`
	})
	inv, _ := landArgs([]string{taskTestID, "nothing_to_land"})
	var out, errs bytes.Buffer
	if code := landTask(&out, &errs, b, inv); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(errs.String(), "clawdline task land: refused, 409 wrote_to_repository: ") {
		t.Fatalf("stderr: %q", errs.String())
	}
}

// noticeCommand is a `clawdline task …` in one clause of a completion line.
var noticeCommand = regexp.MustCompile(`clawdline task (\w+)((?: [^\s,;()]+)*)$`)

// noticeCommands is every command a completion line names. A line is clauses
// joined by "; ", " — ", ", then ", " or " and parentheses, and a command runs
// to the end of its clause.
func noticeCommands(line string) [][]string {
	clauses := regexp.MustCompile(`; | — |, then |, or | or |, |\(|\)`).Split(line, -1)
	var out [][]string
	for _, c := range clauses {
		c = strings.TrimSpace(c)
		if i := strings.Index(c, "clawdline task "); i >= 0 {
			if m := noticeCommand.FindStringSubmatch(c[i:]); m != nil {
				out = append(out, m)
			} else {
				out = append(out, []string{c[i:], "?", ""})
			}
		}
	}
	return out
}

// Every command in every completion line the broker can write — for each thing
// a delivery branch can hold when the task ends — is one this binary parses
// and, for a landing, sends to the route as the route reads it. The lines are
// the broker's own (FinishedLine), not copies of them — and, for a committed
// branch, the cherry-pick line `task show` prints in the notice's place
// (CarriedByHand).
func TestEveryCommandACompletionNoticeNamesRuns(t *testing.T) {
	id := taskTestID
	worktree := &orchestrator.Worktree{Branch: "clawdline/task/" + id, Path: "/w/" + id}
	records := map[string]orchestrator.Record{}
	for _, s := range []orchestrator.LandingSettlement{orchestrator.SettlementEmpty, orchestrator.SettlementCarried,
		orchestrator.SettlementUnreadable} {
		records[string(s)] = orchestrator.Record{ID: id, Title: "t", State: orchestrator.StateSuccess,
			Worktree: worktree, Landing: &orchestrator.Landing{State: orchestrator.LandingPending, Settlement: s}}
	}
	records["shared checkout"] = orchestrator.Record{ID: id, Title: "t", State: orchestrator.StateSuccess,
		Claims: []string{"a.go"}, Landing: &orchestrator.Landing{State: orchestrator.LandingPending}}
	fill := map[string]string{"<branch>": "main", "<commit>": "abc123"}

	b := &orchestrator.Broker{}
	lands := 0
	for name, r := range records {
		line := b.FinishedLine(r, "n-1")
		if r.Landing.Settlement == orchestrator.SettlementCarried {
			line += "; " + orchestrator.CarriedByHand(id)
		}
		named := noticeCommands(line)
		if len(named) < 2 {
			t.Fatalf("%s: the line names %d commands:\n%s", name, len(named), line)
		}
		for _, m := range named {
			words := strings.Fields(m[2])
			for i, w := range words {
				if v, ok := fill[w]; ok {
					words[i] = v
				} else if strings.HasPrefix(w, "<") {
					t.Errorf("%s: %q leaves %s with no value to put there", name, m[0], w)
				}
			}
			switch m[1] {
			case "show":
				if len(words) != 3 || words[0] != id || words[1] != "--ack" || words[2] != "n-1" {
					t.Errorf("%s: %q is not `task show <task id> --ack <notice id>`", name, m[0])
				}
			case "ack":
				if len(words) != 2 || words[0] != id {
					t.Errorf("%s: %q is not `task ack <task id> <notice id>`", name, m[0])
				}
			case "land":
				lands++
				inv, err := landArgs(words)
				if err != nil {
					t.Errorf("%s: %q does not parse: %v", name, m[0], err)
					continue
				}
				s, client := newStandIn(t, func(*http.Request) (int, string) {
					return 200, `{"ok":true,"task":{"id":"` + id + `","landing":{"state":"` + inv.body["state"] + `"}}}`
				})
				var out, errs bytes.Buffer
				if code := landTask(&out, &errs, client, inv); code != 0 {
					t.Errorf("%s: %q exited %d: %s", name, m[0], code, errs.String())
					continue
				}
				sent := s.requests()[0]
				var body map[string]string
				_ = json.Unmarshal(sent.Body, &body)
				for key := range body {
					if !landingKeys[key] {
						t.Errorf("%s: %q sends %q, which the route refuses", name, m[0], key)
					}
				}
				if sent.Token == "" || sent.EscapedPath != "/v1/orchestrator/tasks/"+id+"/landing" {
					t.Errorf("%s: %q was sent as %+v", name, m[0], sent)
				}
			default:
				t.Errorf("%s: the line names `clawdline task %s`, which this binary does not have", name, m[1])
			}
		}
	}
	// Every pending landing a root still owes by hand is offered its command.
	if lands < len(records) {
		t.Errorf("%d task land commands across %d lines; each pending landing names one", lands, len(records))
	}
}
