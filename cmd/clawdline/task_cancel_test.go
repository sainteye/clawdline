package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// `clawdline task cancel`: the reason it requires, the Session it names, and
// the key that makes running it twice one cancel.

func TestTaskCancelSendsTheReasonTheCallerAndADerivedKey(t *testing.T) {
	inv, err := cancelArgs([]string{taskTestID, "--reason", " wrong\n brief ", "--port", "7791"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.port != 7791 || inv.id != taskTestID || inv.reason != "wrong brief" {
		t.Fatalf("parsed %+v", inv)
	}
	answer := `{"ok":true,"task":{"id":"` + taskTestID + `","title":"t","state":"cancelled",` +
		`"verdict":"Cancelled by its root: wrong brief","landing":{"state":"pending",` +
		`"settlement":"branch_carries_commits","note":"cancelled; branch clawdline/task/x was kept and has 2 commit(s) to look at"}}}`
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, answer })
	env := envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})
	var out, errs bytes.Buffer
	if code := cancelTask(&out, &errs, b, inv, env); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if code := cancelTask(&out, &errs, b, inv, env); code != 0 {
		t.Fatalf("second run exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[0].Method != http.MethodPost || seen[0].Token != thinToken ||
		seen[0].EscapedPath != "/v1/orchestrator/tasks/"+taskTestID+"/cancel" {
		t.Fatalf("asked %+v", seen)
	}
	var body map[string]string
	if err := json.Unmarshal(seen[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["reason"] != "wrong brief" || body["session_id"] != thinConversation {
		t.Fatalf("sent %s", seen[0].Body)
	}
	if seen[0].Key == "" || seen[0].Key != seen[1].Key || seen[0].Key != cancelKey(taskTestID, thinConversation) {
		t.Fatalf("keys %q and %q: the same cancel run twice must carry one key", seen[0].Key, seen[1].Key)
	}
	if cancelKey(taskTestID, "another") == seen[0].Key {
		t.Fatal("two callers share a key")
	}
	for _, want := range []string{"cancelled " + taskTestID, "Cancelled by its root: wrong brief",
		"was kept and has 2 commit(s) to look at"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout does not say %q:\n%s", want, out.String())
		}
	}
}

func TestTaskCancelRefusesWithoutAReasonAndPrintsTheDaemonsRefusal(t *testing.T) {
	for _, args := range [][]string{
		{taskTestID},
		{taskTestID, "--reason", "  "},
		{taskTestID, "--reason", strings.Repeat("x", orchestrator.CancelReasonLimit+1)},
		{"not-an-id", "--reason", "r"},
		{taskTestID, taskTestID, "--reason", "r"},
	} {
		if _, err := cancelArgs(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 403, `{"error":{"code":"not_task_root","message":"Only the root Session that dispatched this task (conversation r) or the person, from the console, may cancel it."}}`
	})
	var out, errs bytes.Buffer
	inv := cancelInvocation{id: taskTestID, reason: "r", conversation: "other"}
	if code := cancelTask(&out, &errs, b, inv, envOf(nil)); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs.String(), "refused, 403 not_task_root") {
		t.Fatalf("stderr: %q", errs.String())
	}
}

// The capability goes with the routes that hold the caller to an identity,
// and only those.
func TestTheSquadCapabilityGoesWithTheCancel(t *testing.T) {
	for path, want := range map[string]bool{
		"/v1/orchestrator/tasks":                            true,
		"/v1/orchestrator/tasks/" + taskTestID + "/cancel":  true,
		"/v1/work/v2/agent/items/x":                         true,
		"/v1/orchestrator/tasks/" + taskTestID + "/landing": false,
		"/v1/orchestrator/tasks/" + taskTestID + "/respawn": false,
	} {
		if got := sendsCapability(http.MethodPost, path); got != want {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
	if sendsCapability(http.MethodGet, "/v1/orchestrator/tasks/"+taskTestID+"/cancel") {
		t.Error("a GET carries the capability")
	}
}
