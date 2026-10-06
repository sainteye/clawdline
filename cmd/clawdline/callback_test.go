package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestCallbackArgumentsNameACommandAndATitle(t *testing.T) {
	inv, err := callbackArgs([]string{"--title", "The deploy is live", "--timeout", "15m", "--", "sh", "-c", "deploy && wait"}, "/w")
	if err != nil {
		t.Fatal(err)
	}
	if inv.dir != "/w" || inv.timeout != 15*time.Minute || strings.Join(inv.argv, "|") != "sh|-c|deploy && wait" {
		t.Fatalf("%+v", inv)
	}
	for _, args := range [][]string{
		{"--title", "t"},
		{"--", "true"},
		{"--title", "t", "--timeout", "30s", "--", "true"},
		{"--title", "t", "--timeout", "5h", "--", "true"},
		{"--title", "t", "--task-id", "NOT-A-UUID", "--", "true"},
		{"--title", "t", "--env", "TOKEN=x", "--", "true"},
	} {
		if _, err := callbackArgs(args, "/w"); err == nil {
			t.Errorf("%q was accepted", args)
		}
	}
}

func TestANewCallbackIDIsALowercaseUUID(t *testing.T) {
	id := newCallbackID()
	if len(id) != 36 || id[14] != '4' || strings.ToLower(id) != id {
		t.Fatalf("%q", id)
	}
}

// A finished callback is shown as a command, not as a child that forgot its
// result.json.
func TestAFinishedCallbackIsShownAsACommand(t *testing.T) {
	var out bytes.Buffer
	writeTaskView(&out, contract.BrokerTask{ID: "c", Title: "t", Kind: orchestrator.TaskKindCallback,
		State: contract.TaskStateFailure, Verdict: "exit 1 after 3s; last lines:\nboom", Summary: "exit 1 after 3s; last lines:\nboom"})
	got := out.String()
	if !strings.Contains(got, "state:        failure — exit 1 after 3s") || strings.Contains(got, "result.json") ||
		strings.Contains(got, "landing") || strings.Count(got, "boom") != 1 {
		t.Fatalf("%s", got)
	}
}
