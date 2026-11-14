package projectassistantacp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const modeTestSession = "c3000003-3333-4333-8333-333333333333"

func TestAClaudeSessionAsksBeforeEveryChangingAction(t *testing.T) {
	for _, resume := range []bool{false, true} {
		file := filepath.Join(t.TempDir(), "mode")
		q := persisted(testFence(), resume, modeTestSession)
		if !resume {
			q.Binding.NativeSessionID = ""
		}
		e, _ := initializePersisted(t, q, modeTestSession, modeTestSession, "CLAWDLINE_ACP_MODE_FILE="+file)
		if e != nil {
			t.Fatalf("resume=%v: %v", resume, e)
		}
		got, _ := os.ReadFile(file)
		if string(got) != askMode {
			t.Fatalf("resume=%v: session mode was %q, not set to ask before acting", resume, got)
		}
	}
}

func TestASessionThatWillNotAskIsNotOpened(t *testing.T) {
	q := persisted(testFence(), false, "")
	for _, substitute := range []string{"auto", "acceptEdits", "bypassPermissions"} {
		e, _ := initializePersisted(t, q, modeTestSession, "", "CLAWDLINE_ACP_MODE_SUBSTITUTE="+substitute)
		if pa.Code(e) != pa.Unsupported {
			t.Fatalf("a session in %s mode was opened: %v", substitute, e)
		}
	}
}

func TestLeavingAskModeEndsTheExecution(t *testing.T) {
	q := persisted(testFence(), false, "")
	e, _, c := initializeWith(t, q, modeTestSession, "", "CLAWDLINE_ACP_MODE_LATER=acceptEdits")
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("a switch to acceptEdits left the execution running")
	}
	c.mu.Lock()
	failure := c.failure
	c.mu.Unlock()
	if pa.Code(failure) != pa.ScopeDenied {
		t.Fatalf("wrong failure: %v", failure)
	}

	e, _, c = initializeWith(t, q, modeTestSession, "", "CLAWDLINE_ACP_MODE_LATER=plan")
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-c.done:
		t.Fatal("plan mode, which changes nothing, ended the execution")
	case <-time.After(500 * time.Millisecond):
	}
}
