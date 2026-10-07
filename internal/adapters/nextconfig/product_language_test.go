package nextconfig

import (
	"encoding/json"
	"testing"
)

func TestProductLanguageAliasesAndSeparatedPreference(t *testing.T) {
	for input, want := range map[string]string{
		"": "en", "en-US": "en", "zh-Hant-TW": "zh-Hant", "zh-TW": "zh-Hant",
		"zh-HK": "zh-Hant", "zh-Hans-CN": "zh-Hans", "zh-SG": "zh-Hans",
		"zh-Latn-TW": "en", "zh-Hans-TW": "zh-Hans", "zh-Hant-CN": "zh-Hant",
		"zh-MO": "zh-Hant", "zh_MO": "zh-Hant",
		"zh": "en", "ja-JP": "ja", "ja_JP": "ja", "pt-PT": "pt-BR", "es-MX": "es",
		"fr-CA": "fr", "de-AT": "de", "ko-KR": "ko", "ru": "en",
	} {
		if got := ResolveProductLanguage(input); got != want {
			t.Errorf("ResolveProductLanguage(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"zh--TW", " zh-TW", "../ja", "zh-"} {
		if ValidProductLanguageTag(input) || ResolveProductLanguage(input) != "en" {
			t.Errorf("malformed product tag %q was accepted", input)
		}
	}
	if !ValidProductLanguageTag("zh_MO") {
		t.Fatal("legacy underscore locale was rejected")
	}
	values := Values{Raw: map[string]json.RawMessage{
		"language":         json.RawMessage(`"zh-Hant"`),
		"voice_language":   json.RawMessage(`"ja"`),
		"product_language": json.RawMessage(`"fr"`),
	}}
	if got := ProductLanguage(values); got != "fr" {
		t.Fatalf("product_language = %q", got)
	}
	if got, _ := values.String("language"); got != "zh-Hant" {
		t.Fatalf("agent language changed: %q", got)
	}
	if got, _ := values.String("voice_language"); got != "ja" {
		t.Fatalf("voice language changed: %q", got)
	}
	values.Raw["product_language"] = json.RawMessage(`"ru"`)
	if got := ProductLanguage(values); got != "en" {
		t.Fatalf("unsupported product language = %q", got)
	}
	values.Raw["product_language"] = json.RawMessage(`"zh_MO"`)
	if got := ProductLanguage(values); got != "zh-Hant" {
		t.Fatalf("legacy underscore product language = %q", got)
	}
}
