package projectassistantacp

import (
	"strings"
	"testing"
)

func TestAgentCompactionUpdateIsRecordedOnceWithUsage(t *testing.T) {
	c := &Client{fence: testFence(), sessionID: "own-session", turnID: "turn-1"}
	for _, u := range []string{
		`{"sessionUpdate":"usage_update","used":180000,"size":200000}`,
		`{"sessionUpdate":"compaction_update","compactionId":"c1","status":"in_progress"}`,
		`{"sessionUpdate":"compaction_update","compactionId":"c1","status":"completed"}`,
		`{"sessionUpdate":"usage_update","used":30000,"size":200000}`,
	} {
		if err := contextUpdate(t, c, "own-session", u); err != nil {
			t.Fatal(err)
		}
	}
	s := c.NativeContext()
	if s.CompactedTurnID != "turn-1" || s.Compactions != 1 || s.CompactedBefore != 180000 || s.CompactedAfter != 30000 {
		t.Fatalf("compaction not recorded once: %+v", s)
	}
}

func TestUsageDropIsACompactionOnlyForAgentsWithoutCompactionUpdates(t *testing.T) {
	c := &Client{fence: testFence(), sessionID: "own-session", turnID: "turn-1"}
	for _, u := range []string{
		`{"sessionUpdate":"usage_update","used":15000,"size":200000}`,
		`{"sessionUpdate":"usage_update","used":5000,"size":200000}`, // below the floor
		`{"sessionUpdate":"usage_update","used":120000,"size":200000}`,
		`{"sessionUpdate":"usage_update","used":70000,"size":200000}`, // not halved
		`{"sessionUpdate":"usage_update","used":20000,"size":200000}`, // halved
	} {
		if err := contextUpdate(t, c, "own-session", u); err != nil {
			t.Fatal(err)
		}
	}
	if s := c.NativeContext(); s.Compactions != 1 || s.CompactedBefore != 70000 || s.CompactedAfter != 20000 {
		t.Fatalf("usage drop misread: %+v", s)
	}
	r := &Client{fence: testFence(), sessionID: "own-session", turnID: "turn-1"}
	for _, u := range []string{
		`{"sessionUpdate":"compaction_update","compactionId":"c1","status":"in_progress"}`,
		`{"sessionUpdate":"usage_update","used":150000,"size":200000}`,
		`{"sessionUpdate":"usage_update","used":10000,"size":200000}`,
	} {
		if err := contextUpdate(t, r, "own-session", u); err != nil {
			t.Fatal(err)
		}
	}
	if s := r.NativeContext(); s.Compactions != 0 {
		t.Fatalf("a usage drop counted for an agent that reports compaction: %+v", s)
	}
}

func TestCompactionRedeliveryRepeatsMemoryThroughTheMemoryRenderer(t *testing.T) {
	text := compactionRedelivery("remember the blue door")
	if !strings.Contains(text, memoryUpdate("remember the blue door")) || !strings.HasPrefix(text, "<context_restored>") {
		t.Fatal(text)
	}
}
