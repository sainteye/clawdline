package http

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Both briefs an owner receives — typed into an existing Session, and the
// acceptance of a new Session's Root Assignment — tell it to break
// multi-stage work into steps with `clawdline item step-add` first, and that
// a single straightforward change takes none.
func TestEveryOwnerBriefSaysWhenToBreakTheItemIntoSteps(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	if _, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
		t.Fatal(err)
	}
	sent := p.done()
	if len(sent) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	for name, brief := range map[string]string{
		"existing Session":  sent[0],
		"Root Assignment":   workV2RootAssignmentAcceptance(v.Item.ID),
		"brief as composed": workV2AssignmentBrief(v.Item.ID, v.Item.Title),
	} {
		for _, want := range []string{"clawdline item step-add " + v.Item.ID, "clawdline item step-done",
			"multi-stage", "takes no steps", "clawdline item phase " + v.Item.ID + " implementing"} {
			if !strings.Contains(brief, want) {
				t.Errorf("%s brief lacks %q:\n%s", name, want, brief)
			}
		}
	}
}
