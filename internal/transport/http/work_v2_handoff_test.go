package http

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// An item being implemented is moved to a new Session after its owner stopped
// (its quota ran out) and its terminal is gone. Nothing waits on the old
// owner: the Feature Root's ASSIGNMENT.md points to a handoff pack first, and
// the pack says the old owner's last message could not be read and why.
func TestATakeoverOfAnInFlightItemNeedsNothingFromTheOldOwner(t *testing.T) {
	s, p, v := reassignServer(t)
	ctx := context.Background()
	first, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	working, err := s.workV2().Advance(ctx, v.Item.ID, app.AdvanceWorkV2{ExpectedVersion: first.Item.Version,
		SessionID: p.s.ConversationID, Next: work.PhaseImplementing, Actor: p.s.ConversationID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gone := p.s.ID
	p.s.ID = "%99" // the old owner's terminal is no longer there

	// This broker has no terminal to open a Session in, so the assignment
	// fails after its ASSIGNMENT.md is written.
	_, assignErr := s.assignWorkV2(ctx, v.Item.ID, "local", working.Item.Version, "new_session", "", "claude", "", nil)
	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	ra := ""
	for _, a := range after.Assignments {
		if a.RootAssignment != "" {
			ra = a.RootAssignment
		}
	}
	if ra == "" {
		t.Fatalf("no Root Assignment was opened (%v): %+v", assignErr, after.Assignments)
	}
	opened, err := s.broker.RootAssignmentByID(ctx, ra)
	if err != nil {
		t.Fatalf("root assignment %q: %v", ra, err)
	}
	brief, _ := os.ReadFile(opened.BriefPath)
	pack := filepath.Join(filepath.Dir(opened.BriefPath), "handoff", "HANDOFF.md")
	if !strings.Contains(string(brief), "HANDOFF\nThis item was taken over") || !strings.Contains(string(brief), pack) {
		t.Fatalf("ASSIGNMENT.md does not point to the handoff pack:\n%s", brief)
	}
	md, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Phase: implementing", "Session: " + first.Item.OwnerSession,
		"Last message: unknown / could not read: its terminal " + gone + " is gone"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("HANDOFF.md lacks %q:\n%s", want, md)
		}
	}
}

// Only a takeover of an in-flight item gets a pack: an item's first
// assignment does not.
func TestAFirstAssignmentHasNoHandoffPack(t *testing.T) {
	s, _, v := reassignServer(t)
	ctx := context.Background()
	_, _ = s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "new_session", "", "claude", "", nil)
	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := s.broker.RootAssignmentByID(ctx, after.Assignments[0].RootAssignment)
	if err != nil {
		t.Fatal(err)
	}
	brief, _ := os.ReadFile(opened.BriefPath)
	if strings.Contains(string(brief), "HANDOFF") {
		t.Fatalf("a first assignment carries a handoff section:\n%s", brief)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(opened.BriefPath), "handoff")); !os.IsNotExist(err) {
		t.Fatalf("a first assignment wrote a pack: %v", err)
	}
}
