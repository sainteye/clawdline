package skills

import (
	"strings"
	"testing"
)

// The capacity reference is read by Agents, whose credentials the live
// capacity route refuses (it needs a paired device). On 2026-11-10 a Codex
// Session setting up an hourly schedule read "read the target machine's live
// capacity state before an action; if a reading is missing ... stop", got the
// paired-device 403, and stopped without creating the schedule. The reference
// tells an Agent to act and handle the typed refusal instead.
func TestCapacityReferenceDoesNotMakeAnAgentReadWhatItCannot(t *testing.T) {
	for _, topic := range []string{"en", "zh-Hant"} {
		text, err := CapacityCatalog(topic)
		if err != nil {
			t.Fatal(err)
		}
		head, _, _ := strings.Cut(string(text), "| ")
		for _, banned := range []string{"before an action", "操作前請讀", "If a reading is missing", "缺少讀數"} {
			if strings.Contains(head, banned) {
				t.Errorf("%s: the preamble still says %q", topic, banned)
			}
		}
		if !strings.Contains(head, "retry_after") || !strings.Contains(head, "clawdline guide refusals") {
			t.Errorf("%s: the preamble does not say how to handle a capacity refusal:\n%s", topic, head)
		}
	}
}
