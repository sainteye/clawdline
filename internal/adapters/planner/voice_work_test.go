package planner

import (
	"context"
	"errors"
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

func TestVoiceWorkRefusesMissingOrEmptyPlanFieldsWithSafeDiagnostics(t *testing.T) {
	for _, raw := range []string{`{}`, `{"changes":[],"reply":"Recorded."}`, `{"changes":null,"reply":"Recorded.","wait_for":[]}`, `{"changes":[],"reply":"","wait_for":[]}`, `{"changes":[],"reply":"Recorded.","wait_for":[],"extra":true}`} {
		p := Planner{LookPath: func(name string) (string, error) { return "/bin/" + name, nil }, Run: func(context.Context, string, []string, string, string, []string) ([]byte, error) {
			return []byte(`{"structured_output":` + raw + `}`), nil
		}}
		_, err := p.VoiceWork(context.Background(), "synthetic input", "claude")
		var failure *VoiceWorkFailure
		if !errors.As(err, &failure) || failure.Code != "voice_work_invalid_plan" || failure.Stage != "decode" {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}
