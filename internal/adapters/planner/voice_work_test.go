package planner

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestVoiceWorkPlansOneStructuredTurnWithoutExecutingATool(t *testing.T) {
	var args []string
	p := Planner{LookPath: func(name string) (string, error) { return "/bin/" + name, nil }, Run: func(_ context.Context, _ string, a []string, input, _ string, _ []string) ([]byte, error) {
		args = a
		if !strings.Contains(input, "pending-utterance") {
			t.Fatal("lost input")
		}
		return []byte(`{"structured_output":{"changes":[{"op":"add","id":"next","text":"Read progress","tool":"get_session_summary","args":"{\"session_id\":\"one\"}","depends_on":[],"priority":0}],"reply":"Recorded.","wait_for":["next"]}}`), nil
	}}
	plan, err := p.VoiceWork(context.Background(), `{"inputs":["pending-utterance"]}`, "claude")
	if err != nil || len(plan.Changes) != 1 || plan.Changes[0].Tool != "get_session_summary" || len(plan.WaitFor) != 1 || plan.WaitFor[0] != "next" {
		t.Fatalf("plan %+v %v", plan, err)
	}
	i := slices.Index(args, "--tools")
	if i < 0 || args[i+1] != "" || !slices.Contains(args, "--no-session-persistence") {
		t.Fatalf("not tool-less: %v", args)
	}
}
