package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestGermanTaskArgumentFailuresKeepTheTaskIDAndWaitBound(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })

	if _, err := cancelArgs([]string{"bad-id"}); err == nil ||
		!strings.Contains(err.Error(), `"bad-id"`) || !strings.Contains(err.Error(), "keine Aufgaben-ID") {
		t.Fatalf("invalid cancellation ID = %v", err)
	}
	ids := make([]string, waitIDLimit+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("task-%d", i)
	}
	if _, err := waitArgs(ids); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprintf("%d Aufgaben-IDs", len(ids))) ||
		!strings.Contains(err.Error(), fmt.Sprintf("höchstens %d", waitIDLimit)) {
		t.Fatalf("too many wait IDs = %v", err)
	}
}
