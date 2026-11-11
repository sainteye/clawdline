package nextconfig

import (
	"strings"
	"testing"
)

func TestVoiceVocabularyTakesOnlyALineEachAndAtMostTheBound(t *testing.T) {
	key, ok := SettableByName("voice_vocabulary")
	if !ok {
		t.Fatal("voice_vocabulary is not settable")
	}
	tooMany := make([]string, MaxVoiceVocabularyTerms+1)
	for i := range tooMany {
		tooMany[i] = "term" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('A'+i/26))
	}
	for _, c := range []struct {
		list []string
		ok   bool
	}{
		{[]string{}, true},
		{[]string{"Tailscale", "PostgreSQL", "台北"}, true},
		{[]string{strings.Repeat("字", MaxVoiceVocabularyRunes)}, true},
		{[]string{strings.Repeat("字", MaxVoiceVocabularyRunes+1)}, false},
		{[]string{"Tailscale", "tailscale"}, false},
		{[]string{""}, false},
		{[]string{" padded"}, false},
		{[]string{"two\nlines"}, false},
		{tooMany[:MaxVoiceVocabularyTerms], true},
		{tooMany, false},
	} {
		if got := key.Allows(c.list); got != c.ok {
			t.Errorf("%d terms starting %q: allowed %v, want %v", len(c.list), first(c.list), got, c.ok)
		}
	}
	if key.Allows("Tailscale") {
		t.Error("a bare string is not a list")
	}
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}
