package projectassistantacp

import (
	"encoding/json"
	"strings"
	"testing"
)

// A Bot may ask the person but never answer: no tool accepts an offer.
func TestBotToolsCanAskThePersonButNeverAnswer(t *testing.T) {
	raw, err := json.Marshal(mcpTools())
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]any `json:"properties"`
		} `json:"inputSchema"`
	}
	if err = json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	asked := false
	for _, tool := range tools {
		if tool.Name == "ask_person" {
			asked = true
		}
		for name := range tool.InputSchema.Properties {
			// withdraw_question names a question only to close it unanswered.
			if name == "question_id" && tool.Name == "withdraw_question" {
				if len(tool.InputSchema.Properties) != 2 || tool.InputSchema.Properties["reason"] == nil {
					t.Errorf("withdraw_question accepts more than a question and a reason: %v", tool.InputSchema.Properties)
				}
				continue
			}
			if name == "offer_id" || name == "option_id" || name == "question_id" || name == "text" && strings.Contains(tool.Name, "question") {
				t.Errorf("%s accepts %s: a Bot could answer an offer", tool.Name, name)
			}
		}
		if strings.Contains(tool.Name, "answer") {
			t.Errorf("%s looks like an answer tool", tool.Name)
		}
	}
	if !asked {
		t.Fatal("ask_person is missing from the Bot tools")
	}
}
