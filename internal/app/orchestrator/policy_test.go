package orchestrator

import (
	"strings"
	"testing"
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
