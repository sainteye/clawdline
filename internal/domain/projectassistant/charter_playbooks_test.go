package projectassistant

import (
	"errors"
	"strings"
	"testing"
)

func playbookVersion(version int64, text string, playbooks ...CharterPlaybook) CharterVersion {
	return CharterVersion{Version: version, Author: CharterByPerson, Text: text, Playbooks: playbooks}
}

// A Bot hears the charter text and only its own playbooks, after the rule
// that puts the charter above them and them above its instructions.
func TestDeliveredTextCarriesOnlyTheBotsPlaybooks(t *testing.T) {
	v := playbookVersion(3, "Ask before deploying.",
		CharterPlaybook{ID: "pb-a", Title: `Release "steps"`, Text: "Tag, then ship.", BotIDs: []string{"maker"}},
		CharterPlaybook{ID: "pb-b", Title: "Review", Text: "Two approvals.", BotIDs: []string{"lead"}})
	got := v.DeliveredText("maker")
	if !strings.HasPrefix(got, "Ask before deploying.\n\n"+playbookRule+"\n") ||
		!strings.Contains(got, `<playbook id="pb-a" title="Release &quot;steps&quot;">`+"\nTag, then ship.\n</playbook>") ||
		strings.Contains(got, "Two approvals.") || strings.HasSuffix(got, "\n") {
		t.Fatalf("maker hears %q", got)
	}
	if !strings.Contains(playbookRule, "the charter, then these playbooks, then your own instructions, then your memory") || !strings.Contains(playbookRule, "優先順序") {
		t.Fatalf("the precedence is stated in both languages: %q", playbookRule)
	}
	if got := v.DeliveredText("other"); got != "Ask before deploying." {
		t.Fatalf("a Bot without playbooks hears only the charter: %q", got)
	}
	opening := CharterOpening(v.DeliveryVersion("maker"), v.DeliveredText("maker"))
	if !strings.HasSuffix(opening, "</playbook>\n</team_charter>\n") {
		t.Fatalf("the playbooks are children of the charter block: %q", opening)
	}
}

// Each Bot's delivery version moves only when what it hears changed.
func TestDeliveryVersionMovesOnlyForTheBotsAPlaybookConcerns(t *testing.T) {
	v1 := playbookVersion(1, "Charter.")
	NextCharterDeliveries(CharterVersion{}, &v1)
	if v1.TextVersion != 1 || v1.DeliveryVersion("maker") != 1 {
		t.Fatalf("a first charter is everybody's version 1: %+v", v1)
	}
	v2 := playbookVersion(2, "Charter.", CharterPlaybook{ID: "pb-a", Title: "A", Text: "One.", BotIDs: []string{"maker"}})
	NextCharterDeliveries(v1, &v2)
	if v2.DeliveryVersion("maker") != 2 || v2.DeliveryVersion("lead") != 1 {
		t.Fatalf("assigning re-delivers only to the assigned Bot: maker=%d lead=%d", v2.DeliveryVersion("maker"), v2.DeliveryVersion("lead"))
	}
	v3 := playbookVersion(3, "Charter.", CharterPlaybook{ID: "pb-a", Title: "A", Text: "One.", BotIDs: []string{"maker"}},
		CharterPlaybook{ID: "pb-b", Title: "B", Text: "Two.", BotIDs: []string{"lead"}})
	NextCharterDeliveries(v2, &v3)
	if v3.DeliveryVersion("maker") != 2 || v3.DeliveryVersion("lead") != 3 || v3.DeliveryVersion("idle") != 1 {
		t.Fatalf("another Bot's playbook keeps maker's delivery: maker=%d lead=%d idle=%d", v3.DeliveryVersion("maker"), v3.DeliveryVersion("lead"), v3.DeliveryVersion("idle"))
	}
	v4 := playbookVersion(4, "Charter.", CharterPlaybook{ID: "pb-a", Title: "A", Text: "One.", BotIDs: nil},
		CharterPlaybook{ID: "pb-b", Title: "B", Text: "Two.", BotIDs: []string{"lead"}})
	NextCharterDeliveries(v3, &v4)
	if v4.DeliveryVersion("maker") != 4 || v4.DeliveryVersion("lead") != 3 {
		t.Fatalf("an unassigned Bot hears the update: maker=%d lead=%d", v4.DeliveryVersion("maker"), v4.DeliveryVersion("lead"))
	}
	v5 := playbookVersion(5, "Charter, changed.", v4.Playbooks...)
	NextCharterDeliveries(v4, &v5)
	if len(v5.Deliveries) != 0 || v5.DeliveryVersion("maker") != 5 || v5.DeliveryVersion("lead") != 5 {
		t.Fatalf("a text change reaches everybody: %+v", v5)
	}
	old := playbookVersion(7, "Old row.")
	if old.DeliveryVersion("maker") != 7 {
		t.Fatalf("a row saved before playbooks reads as its own text version: %d", old.DeliveryVersion("maker"))
	}
	removed := CharterVersion{Version: 8, Author: CharterByRemoval}
	NextCharterDeliveries(v5, &removed)
	if removed.DeliveryVersion("maker") != 0 {
		t.Fatalf("a removal is version 0 for everybody")
	}
	after := playbookVersion(9, "")
	NextCharterDeliveries(removed, &after)
	if after.DeliveryVersion("maker") != 9 {
		t.Fatalf("a charter after a removal is new for everybody: %d", after.DeliveryVersion("maker"))
	}
}

func TestCharterDocumentBounds(t *testing.T) {
	ok := CharterPlaybook{ID: "pb-1", Title: "T", Text: "x", BotIDs: []string{"b"}}
	cases := map[string]struct {
		text      string
		playbooks []CharterPlaybook
		field     string
	}{
		"title":     {"", []CharterPlaybook{{ID: "pb-1", Title: strings.Repeat("t", MaxCharterPlaybookTitleBytes+1)}}, "charter_playbook_title"},
		"multiline": {"", []CharterPlaybook{{ID: "pb-1", Title: "a\nb"}}, "playbook_title"},
		"text":      {"", []CharterPlaybook{{ID: "pb-1", Title: "T", Text: strings.Repeat("x", MaxCharterPlaybookTextBytes+1)}}, "charter_playbook_text"},
		"bots":      {"", []CharterPlaybook{{ID: "pb-1", Title: "T", BotIDs: make([]string, MaxCharterPlaybookBots+1)}}, "charter_playbook_bots"},
		"repeat":    {"", []CharterPlaybook{{ID: "pb-1", Title: "T", BotIDs: []string{"b", "b"}}}, "bot_ids"},
		"duplicate": {"", []CharterPlaybook{ok, ok}, "playbook_id"},
		"count":     {"", make([]CharterPlaybook, MaxCharterPlaybooks+1), "charter_playbooks"},
		"document": {strings.Repeat("c", MaxCharterTextBytes), func() []CharterPlaybook {
			out := []CharterPlaybook{}
			for i := range 7 {
				out = append(out, CharterPlaybook{ID: "pb-" + string(rune('a'+i)), Title: "T", Text: strings.Repeat("p", MaxCharterPlaybookTextBytes)})
			}
			return out
		}(), "charter_document"},
	}
	for name, c := range cases {
		err := ValidateCharterDocument(c.text, c.playbooks)
		var f *Failure
		if !errors.As(err, &f) || f.Field != c.field {
			t.Errorf("%s: %v, want field %s", name, err, c.field)
		}
	}
	if err := ValidateCharterDocument("Charter.", []CharterPlaybook{ok}); err != nil {
		t.Fatal(err)
	}
}
