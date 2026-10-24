package orchestrator

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"testing"
)

func TestDispatchUsesMachinePermissionUnlessTaskExplicitlyOverrides(t *testing.T) {
	b := &Broker{Dir: t.TempDir()}
	p := Protocol
	claims := []string{}
	root := json.RawMessage(`{"session_id":"10000000-0000-4000-8000-000000000001","assistant":"claude"}`)
	d := draft{Protocol: &p, TaskID: "11111111-1111-4111-8111-111111111111", Assistant: "claude", ProjectDir: t.TempDir(), Title: "Run a focused task", Instructions: "Inspect the parser", Claims: &claims, Root: &root}
	for _, mode := range []string{"full", "ask", "edits"} {
		if _, err := nextconfig.Open(b.Dir).Set(map[string]any{"orchestrator_permission": mode}); err != nil {
			t.Fatal(err)
		}
		got, err := b.admit(d.TaskID, d, false, false)
		if err != nil || got.PermissionMode != mode {
			t.Fatalf("default %s: %s %v", mode, got.PermissionMode, err)
		}
		explicit := d
		explicit.PermissionMode = "ask"
		got, err = b.admit(d.TaskID, explicit, false, false)
		if err != nil || got.PermissionMode != "ask" {
			t.Fatalf("explicit: %s %v", got.PermissionMode, err)
		}
	}
}

// Turning the full-permission default off restores native permission behavior instead
// of introducing a second permission policy over the person's provider settings.
func TestAskDispatchLeavesNativePermissionSettingsUntouched(t *testing.T) {
	for _, assistant := range []string{"claude", "codex"} {
		args := permissionArgs(Record{Assistant: assistant, PermissionMode: "ask"}, "/tmp/fixture", "")
		if len(args) != 0 {
			t.Fatalf("%s overrides native settings: %v", assistant, args)
		}
	}
}
