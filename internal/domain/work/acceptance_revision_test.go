package work

import "testing"

func TestAcceptanceRevisionInstructionUsesOnlyUnambiguousConversationContext(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name, text string
		sole, want bool
	}{
		{"contextual single item", "請把這個 Epic 的第 1 點驗收改成跨 Session 可定位", true, true},
		{"ambiguous several items", "請把這個 Epic 的第 1 點驗收改成跨 Session 可定位", false, false},
		{"named among several", "Revise acceptance for " + id, false, true},
		{"unrelated message", "請提醒我明天開會", true, false},
		{"foreign id", "Revise acceptance for 10000000-0000-4000-8000-000000000002", true, false},
		{"prohibition", "不要修改驗收", true, false},
		{"polite prohibition", "請不要修改驗收", true, false},
		{"question", "為何不能修改驗收？", true, false},
		{"discussion", "我們應該討論如何修改驗收", true, false},
		{"direct polite question", "可以請你修改這個項目的驗收嗎？", true, true},
		{"policy discussion is not a revision request", "我不認為 Agent 不應該在要求下不能修改驗收", true, false},
		{"explicit prohibition survives two negatives", "我不認為不能討論，但請不要修改驗收", true, false},
		{"request for another action", "請開議題。不要修改驗收", true, false},
		{"unrelated denial before request", "這件事不應由我處理。請修改驗收", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AcceptanceRevisionInstruction(tc.text, id, "Parent Epic", tc.sole); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}
