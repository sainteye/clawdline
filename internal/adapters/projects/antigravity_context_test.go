package projects

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAntigravityImmutableContextAndNativeResponse(t *testing.T) {
	c, err := PrepareAntigravityContext("/private/launch/prompt.md", "Fixed role snapshot", "Project memory index", "zh-Hant-TW")
	if err != nil {
		t.Fatal(err)
	}
	r := antigravityRequest()
	r.SquadPromptPath, r.Persona, r.Language, r.Memory, r.AntigravityContext = c.PromptPath, "backend", c.Language, c.Memory, &c
	l, err := Admit(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(l.Arguments, " "), "prompt") {
		t.Fatal("provider base prompt replaced")
	}
	body, digest, err := c.Document()
	if err != nil || digest == "" {
		t.Fatal(err)
	}
	var restored AntigravityContext
	if json.Unmarshal(body, &restored) != nil || restored.Validate() != nil {
		t.Fatal("persisted resume context")
	}
	response, err := AntigravityContextResponse(restored, "PreInvocation", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		InjectSteps []struct {
			SystemMessage struct {
				SystemMessage string `json:"systemMessage"`
			} `json:"systemMessage"`
		} `json:"injectSteps"`
	}
	if json.Unmarshal(response, &native) != nil || len(native.InjectSteps) != 1 {
		t.Fatal(string(response))
	}
	for _, want := range []string{c.Prompt, c.Memory, "zh-Hant-TW", "Board item title", "clawdline guide note"} {
		if !strings.Contains(native.InjectSteps[0].SystemMessage.SystemMessage, want) {
			t.Fatal(want)
		}
	}
	for _, event := range []string{"SessionStart", "Stop", "PostInvocation"} {
		b, e := AntigravityContextResponse(c, event, json.RawMessage(`{}`))
		if e != nil || string(b) != "{}" {
			t.Fatalf("%s %s %v", event, b, e)
		}
	}
	c.Prompt = "mutated catalog"
	if c.Validate() == nil {
		t.Fatal("changed bytes retained original digest")
	}
	if _, err := Admit(r); !errors.Is(err, ErrSquadContextUnavailable) {
		t.Fatal(err)
	}
}

func TestAntigravityContextRejectsUnsafeOrUnmatchedSources(t *testing.T) {
	for _, args := range [][4]string{
		{"relative", "role", "", ""}, {"/private/p", "", "", ""}, {"", "", "", "bad tag"},
		{"", "", strings.Repeat("m", MemoryLaunchLimit+1), ""},
	} {
		if _, err := PrepareAntigravityContext(args[0], args[1], args[2], args[3]); !errors.Is(err, ErrSquadContextUnavailable) {
			t.Fatalf("%v", err)
		}
	}
	c, _ := PrepareAntigravityContext("", "", "memory", "")
	if _, err := AntigravityContextResponse(c, "PreInvocation", json.RawMessage(`{"injectSteps":[]}`)); err == nil {
		t.Fatal("foreign injection overwritten")
	}
	if _, err := AntigravityContextResponse(c, "PreInvocation", json.RawMessage(`null`)); err == nil {
		t.Fatal("null observer")
	}
}
