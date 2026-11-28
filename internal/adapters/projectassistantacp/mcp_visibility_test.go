package projectassistantacp

import (
	"encoding/json"
	"slices"
	"testing"
)

type visibilityTestTools struct {
	mcpTestTools
	hidden map[string]bool
}

func (t visibilityTestTools) ToolVisible(name string) bool {
	return !t.hidden[name]
}

func mcpToolNames(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// ToolVisibility omits unavailable or turn-forbidden tools from tools/list
// without affecting callers whose NativeTools does not implement it.
func TestOwnedMCPToolsListRespectsToolVisibility(t *testing.T) {
	all := newMCPFixture(t, mcpTestTools{})
	all.initialize(t)
	code, allBody := all.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if code != 200 {
		t.Fatalf("tools/list: %d %s", code, allBody)
	}
	allNames := mcpToolNames(t, allBody)
	if !slices.Contains(allNames, "browser_command") || !slices.Contains(allNames, "network_bot") || !slices.Contains(allNames, "react") {
		t.Fatalf("default fixture missing expected tools: %v", allNames)
	}

	hidden := map[string]bool{
		"browser_status":   true,
		"browser_command":  true,
		"network_bot":      true,
		"react":            true,
		"send_file":        true,
		"publish_artifact": true,
	}
	filtered := newMCPFixture(t, visibilityTestTools{hidden: hidden})
	filtered.initialize(t)
	code, filteredBody := filtered.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if code != 200 {
		t.Fatalf("filtered tools/list: %d %s", code, filteredBody)
	}
	names := mcpToolNames(t, filteredBody)
	for h := range hidden {
		if slices.Contains(names, h) {
			t.Errorf("hidden tool %q still listed", h)
		}
	}
	for _, kept := range []string{"send_message", "dispatch_work", "ask_person", "read_memory"} {
		if !slices.Contains(names, kept) {
			t.Errorf("allowed tool %q was unexpectedly hidden", kept)
		}
	}
	if len(filteredBody) >= len(allBody) {
		t.Fatalf("expected filtered tools/list (%d bytes) to be smaller than unfiltered (%d bytes)", len(filteredBody), len(allBody))
	}
}
