package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
)

// paragraphs builds a base of n characters in paragraphs of 100.
func paragraphs(n int, fill string) string {
	var b strings.Builder
	for b.Len() == 0 || len([]rune(b.String())) < n {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Repeat(fill, 98))
	}
	return string([]rune(b.String())[:n])
}

// ⑧ Over the limit, the base is cut and the local file is kept whole — the
// Swift app's I1. The first Go version counted bytes and cut from the end,
// which is the local file (D23 ①).
func TestHouseRulesCutTheBaseAndKeepTheLocal(t *testing.T) {
	// Today's sizes (15,091 + 283, measured 2026-09-18), then the base grown
	// by the ~600 that would have made the first version drop the local tail.
	local := strings.Repeat("l", 270) + "\nLOCAL-TAIL"
	for _, baseChars := range []int{15091, 15850, 16550} {
		base := paragraphs(baseChars, "b")
		out, reading := ComposePolicy(base, local)
		if !strings.HasSuffix(out, "LOCAL-TAIL") || !strings.Contains(out, local) {
			t.Fatalf("base %d: the local file did not survive whole", baseChars)
		}
		if n := len([]rune(out)); n > PolicyLimit {
			t.Fatalf("base %d: %d characters, over the limit", baseChars, n)
		}
		if reading.LocalChars != len([]rune(local)) || reading.BaseChars != baseChars {
			t.Fatalf("base %d: reading %+v", baseChars, reading)
		}
		if !reading.NearLimit {
			t.Errorf("base %d: %d characters is past 90%% and was not called near the limit", baseChars, baseChars)
		}
		wantCut := baseChars+2+len([]rune(local)) > PolicyLimit
		if reading.Cut != wantCut {
			t.Errorf("base %d: cut=%v, want %v", baseChars, reading.Cut, wantCut)
		}
		if reading.Cut {
			kept := strings.TrimSuffix(out, "\n\n"+local)
			if !strings.HasPrefix(base, kept) || !strings.HasPrefix(base[len(kept):], "\n\n") {
				t.Errorf("base %d: the cut is not at a paragraph break", baseChars)
			}
		}
	}
}

// Characters, not bytes: 6,050 CJK characters are 18,150 bytes and fit.
func TestHouseRulesCountCharacters(t *testing.T) {
	base := paragraphs(6050, "規")
	out, reading := ComposePolicy(base, "local")
	if reading.Cut || reading.NearLimit || !strings.HasSuffix(out, "local") || !strings.Contains(out, base) {
		t.Fatalf("a %d-byte, 6,050-character policy was cut: %+v", len(base), reading)
	}
}

// A local file that alone is over the limit is still carried whole: a long
// briefing is visible, a rule that went missing is not.
func TestAnOversizedLocalFileIsNeverCut(t *testing.T) {
	local := strings.Repeat("x", PolicyLimit+10)
	out, reading := ComposePolicy("base rules", local)
	if out != local || !reading.Cut {
		t.Fatalf("out %d chars, reading %+v", len([]rune(out)), reading)
	}
}

// A child's briefing carries the person's own rules whole and not the shipped
// base, which is about handing work out — something a child cannot do. It
// names where the base is instead. Before, the base was pasted whole: 4,977 of
// a briefing's 8,761 tokens, re-read on every call of every child, and cited
// by none of the twelve child transcripts read on 2026-09-25.
func TestAChildBriefingCarriesThePersonsRulesAndPointsAtTheBase(t *testing.T) {
	dir := t.TempDir()
	b := &Broker{Tasks: taskdir.New(t.TempDir()), Dir: dir,
		Policy: func() (string, string) { return string(ShippedPolicy()), "Never touch the billing module." }}
	r := Record{ID: "a7000000-0000-4000-8000-000000000009", Title: "slim", Kind: "custom",
		TimeoutMinutes: 30, ProjectDir: "/p", Assistant: "claude",
		Root: &RootRef{SessionID: "conv", Assistant: "claude"}}
	brief := b.ChildBrief(r, "/p")
	if !strings.Contains(brief, "Never touch the billing module.") {
		t.Error("the person's own rule is not in the briefing")
	}
	if !strings.Contains(brief, filepath.Join(dir, PolicyBaseFile)) {
		t.Error("the briefing does not say where the dispatch policy is")
	}
	for _, heading := range []string{"# How work is handed out on this machine", "## Should this be dispatched at all?",
		"## Pick a shape", "## Check in proportion to risk"} {
		if strings.Contains(brief, heading) {
			t.Errorf("the base is still pasted into a child's briefing: %q", heading)
		}
	}
	if strings.Contains(brief, "/inflight") {
		t.Error("the briefing still sends every child to /inflight before it starts")
	}

	// With no local file the pointer is still there; with no policy at all
	// the section is absent.
	b.Policy = func() (string, string) { return string(ShippedPolicy()), "" }
	if brief := b.ChildBrief(r, "/p"); !strings.Contains(brief, PolicyBaseFile) || strings.Contains(brief, "House rules the person wrote") {
		t.Error("a machine with only the base does not point at it, or claims rules the person did not write")
	}
	b.Policy = nil
	if strings.Contains(b.ChildBrief(r, "/p"), "## What this machine says") {
		t.Error("a broker with no policy still writes the section")
	}
}
