package cloudops

import (
	"context"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
	"strings"
	"testing"
)

func TestHumanRoutineCloudWordsPreserveScopeCommandsAndCeilings(t *testing.T) {
	// The vocabulary examples are executable requests, not hand-maintained
	// copies of their expected decoder source.
	for _, word := range []string{"routines", "routine-runs", "routine-save", "routine-enabled", "routine-delete", "routine-test", "routine-hook"} {
		t.Run(word, func(t *testing.T) {
			var b body
			operations, err := cloudvocab.Operations()
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range operations {
				if operation.Op == "project-assistants."+word {
					raw, _ := json.Marshal(operation.Example)
					if err := json.Unmarshal(raw, &b); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if b == nil {
				t.Fatal("missing executable example")
			}
			b["type"] = "project-assistants." + word
			calls := 0
			bridge := open(projectAssistantRouter(func(_ context.Context, r LocalRequest) (LocalResponse, error) {
				calls++
				if !strings.Contains(r.Path, "/routines") {
					t.Fatal(r)
				}
				if strings.Contains(word, "delete") && r.Method != "DELETE" {
					t.Fatal(r)
				}
				return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1"}`)}, nil
			}))
			answer := bridge.Handle(context.Background(), request(t, ClassCtl, b))
			if !answer.OK() || calls != 1 {
				t.Fatal(answer, calls)
			}
			if word == "routines" || word == "routine-runs" {
				bridge.AllowCommands = func() bool { return false }
				answer = bridge.Handle(context.Background(), request(t, ClassCtl, b))
				if !answer.OK() {
					t.Fatal("read required write ceiling", answer)
				}
			} else {
				bridge.AllowCommands = func() bool { return false }
				answer = bridge.Handle(context.Background(), request(t, ClassCtl, b))
				if answer.OK() {
					t.Fatal("write bypassed source ceiling")
				}
			}
		})
	}
}
