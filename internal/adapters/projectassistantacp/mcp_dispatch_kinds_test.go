package projectassistantacp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

type mcpKindTools struct {
	mcpTestTools
	kinds []string
}

func (t mcpKindTools) DispatchKinds(context.Context) []string { return t.kinds }

// The dispatch tool's kind argument names the person's rule kinds and their
// hints for this Project, so the model does not have to guess a kind name.
func TestDispatchKindDescriptionNamesThePersonsRules(t *testing.T) {
	f := newMCPFixture(t, mcpKindTools{kinds: []string{"coding (Write or change code)", "image"}})
	f.initialize(t)
	code, list := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if code != 200 || !bytes.Contains(list, []byte(`The person's kinds for this Project: coding (Write or change code); image.`)) {
		t.Fatalf("tools/list: %d %s", code, list)
	}
	plain := newMCPFixture(t, mcpKindTools{})
	plain.initialize(t)
	if _, list = plain.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil); bytes.Contains(list, []byte(`The person's kinds`)) {
		t.Fatalf("a Project without rules lists kinds: %s", list)
	}
	// The shared definitions were not modified by the first listing.
	if again := mcpTools(); bytes.Contains(func() []byte {
		raw, err := json.Marshal(again)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}(), []byte(`The person's kinds`)) {
		t.Fatal("withDispatchKinds modified the shared tool definitions")
	}
}
