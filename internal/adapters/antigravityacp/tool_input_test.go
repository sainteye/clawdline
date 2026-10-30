package antigravityacp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNativeToolInputsHandleLiveAndHistoricalShapes(t *testing.T) {
	input := `{"AbsolutePath":"/workspace/a.go","StartLine":3,"EndLine":7,"toolSummary":"Read a.go","Subagents":[{"Role":"Reviewer","Model":"inherit","Prompt":"private task"}],"secret":"not display data"}`
	quoted, _ := json.Marshal(input)
	var first *ToolInput
	for _, raw := range []string{input, string(quoted)} {
		out := updateTestDecode(t, `{"sessionUpdate":"tool_call","toolCallId":"22222222222222222222222222222222:0","title":"Running view_file","rawInput":`+raw+`}`)
		if out.Tool.Input == nil || out.Tool.Input.Path != "/workspace/a.go" || *out.Tool.Input.StartLine != 3 || len(out.Tool.Input.Delegates) != 1 || out.Tool.AgentID != "22222222222222222222222222222222" {
			t.Fatalf("input lost: %+v", out.Tool)
		}
		if first != nil && !reflect.DeepEqual(first, out.Tool.Input) {
			t.Fatal("replay changed input meaning")
		}
		first = out.Tool.Input
		encoded, _ := json.Marshal(out)
		if strings.Contains(string(encoded), "private task") || strings.Contains(string(encoded), "not display data") {
			t.Fatal("unrelated input fields retained")
		}
	}
	for _, id := range []string{updateTestSession + ":1", strings.ReplaceAll(updateTestSession, "-", "") + ":1", "call_123", "unverified:1"} {
		out := updateTestDecode(t, `{"sessionUpdate":"tool_call","toolCallId":"`+id+`","title":"Read"}`)
		if out.Tool.AgentID != "" {
			t.Fatalf("guessed an agent for %s", id)
		}
	}
}
