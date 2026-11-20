package projectassistant

import (
	"strings"
	"testing"
)

// A group's description is what Codync calls "What this group works on". An
// edit that omits it keeps it, so a console that predates it cannot erase one.
func TestGroupDescriptionIsKeptUnlessAnEditSaysOtherwise(t *testing.T) {
	scope := groupScope()
	cat := groupCatalog(groupBot("a", "Alice", scope))
	members := []BotKey{key("a")}
	g, err := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Description: "  Reviews pull requests  ", Members: members}, cat, testGroupLimits, groupAt)
	if err != nil || g.Description != "Reviews pull requests" {
		t.Fatalf("created description %q: %v", g.Description, err)
	}
	kept, err := EditGroup(g, GroupEdit{ExpectedVersion: 1, Name: "Renamed", Members: members}, cat, testGroupLimits, groupAt)
	if err != nil || kept.Description != "Reviews pull requests" {
		t.Fatalf("omitted description changed to %q: %v", kept.Description, err)
	}
	next := "寫測試並修 bug"
	changed, err := EditGroup(kept, GroupEdit{ExpectedVersion: 2, Name: "Renamed", Description: &next, Members: members}, cat, testGroupLimits, groupAt)
	if err != nil || changed.Description != next {
		t.Fatalf("replaced description %q: %v", changed.Description, err)
	}
	empty := ""
	cleared, err := EditGroup(changed, GroupEdit{ExpectedVersion: 3, Name: "Renamed", Description: &empty, Members: members}, cat, testGroupLimits, groupAt)
	if err != nil || cleared.Description != "" {
		t.Fatalf("cleared description %q: %v", cleared.Description, err)
	}
}

func TestGroupDescriptionHasACharacterLimit(t *testing.T) {
	scope := groupScope()
	cat := groupCatalog(groupBot("a", "Alice", scope))
	members := []BotKey{key("a")}
	// Counted in characters, so the limit is the same in every language.
	fits := strings.Repeat("說", MaxGroupDescriptionRunes)
	if _, err := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Description: fits, Members: members}, cat, testGroupLimits, groupAt); err != nil {
		t.Fatalf("a description at the limit was refused: %v", err)
	}
	long := fits + "說"
	if _, err := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Description: long, Members: members}, cat, testGroupLimits, groupAt); Code(err) != Invalid {
		t.Fatalf("an over-long description was accepted: %v", err)
	}
	g, _ := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Members: members}, cat, testGroupLimits, groupAt)
	if _, err := EditGroup(g, GroupEdit{ExpectedVersion: 1, Name: "Room", Description: &long, Members: members}, cat, testGroupLimits, groupAt); Code(err) != Invalid {
		t.Fatalf("an over-long edit was accepted: %v", err)
	}
	bad := "ok\xff"
	if _, err := EditGroup(g, GroupEdit{ExpectedVersion: 1, Name: "Room", Description: &bad, Members: members}, cat, testGroupLimits, groupAt); Code(err) != Invalid {
		t.Fatalf("invalid UTF-8 was accepted: %v", err)
	}
}
