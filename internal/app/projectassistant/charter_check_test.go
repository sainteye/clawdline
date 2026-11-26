package projectassistant

import (
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A resumed session that already holds the current charter text but was
// never asked to compare it, as every Bot is right after the check shipped,
// gets only the short check, not the text again.
func TestAResumedSessionHoldingTheCharterGetsOnlyTheCheck(t *testing.T) {
	s := &attemptStart{resume: true, charterVersion: 3, charterText: "Rule: the lead reviews.", charterCheck: true,
		input: &pa.Message{Text: "What next?"}, turn: pa.Turn{ID: "t1", Origin: pa.UserOrigin}}
	text, handoff, err := attemptPrompt(s, pa.ContextSnapshot{})
	if err != nil || handoff != nil {
		t.Fatalf("resumed prompt: %v %v", handoff, err)
	}
	if text != pa.CharterCheck(3)+"What next?" || strings.Contains(text, "Rule: the lead reviews.") {
		t.Fatalf("only the check precedes the request: %q", text)
	}
	s.charterCheck = false
	if text, _, _ = attemptPrompt(s, pa.ContextSnapshot{}); text != "What next?" {
		t.Fatalf("an already checked session gets nothing: %q", text)
	}
}
