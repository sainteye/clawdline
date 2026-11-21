package http

import (
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestWaitingMenuProjectsTheNativeCommandApproval(t *testing.T) {
	item := session.Session{
		Assistant: session.AssistantCodex,
		State:     session.StateWaiting,
		Menu: &session.Menu{
			Question: "$ printf fixture",
			Options:  []session.MenuOption{{Number: 1, Label: "Yes, proceed (y)"}},
		},
	}
	for _, commandApproval := range []bool{true, false} {
		item.Menu.CommandApproval = commandApproval
		body, err := json.Marshal(wireMenu(item))
		if err != nil {
			t.Fatal(err)
		}
		var projected map[string]any
		if err := json.Unmarshal(body, &projected); err != nil {
			t.Fatal(err)
		}
		flag, present := projected["command_approval"]
		if present != commandApproval || (present && flag != true) {
			t.Fatalf("command approval %t projected as %s", commandApproval, body)
		}
	}
	item.State = session.StateIdle
	item.Menu.CommandApproval = true
	if got := wireMenu(item); got != nil {
		t.Fatalf("idle menu was projected: %+v", got)
	}
}
