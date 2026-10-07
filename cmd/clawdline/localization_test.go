package main

import (
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func TestCLILanguagePrecedenceAndPrefix(t *testing.T) {
	values := nextconfig.Values{Raw: map[string]json.RawMessage{"product_language": json.RawMessage(`"de"`)}}
	for _, tc := range []struct{ flag, env, want string }{
		{"ja-JP", "es", "ja"}, {"", "es-MX", "es"}, {"", "", "de"},
		{"zh", "fr", "en"},
	} {
		if got := cliProductLanguage(tc.flag, tc.env, values); got != tc.want {
			t.Errorf("flag %q env %q = %q, want %q", tc.flag, tc.env, got, tc.want)
		}
	}
	args, language, err := commandLanguagePrefix([]string{"clawdline", "--lang", "zh-Hans-CN", "guide", "board"})
	if err != nil || language != "zh-Hans" || len(args) != 3 || args[1] != "guide" {
		t.Fatalf("parsed %#v, %q, %v", args, language, err)
	}
	if _, _, err := commandLanguagePrefix([]string{"clawdline", "--lang", "../", "guide"}); err == nil {
		t.Fatal("malformed language was accepted")
	}
	if _, _, err := commandLanguagePrefix([]string{"clawdline", "--lang", "zh--TW", "guide"}); err == nil {
		t.Fatal("empty language subtag was accepted")
	}
}

func TestGuideSingleArgumentUsesSectionNames(t *testing.T) {
	if !isGuideSection("board") || isGuideSection("ru") || isGuideSection("zh-TW") || isGuideSection("zh-Hans-CN") {
		t.Fatal("guide single-argument routing confused a section and a locale")
	}
}
