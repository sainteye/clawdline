package productcopy

import (
	"strings"
	"testing"
)

func TestNotificationCatalogShipsNineCompleteLanguages(t *testing.T) {
	if len(Languages) != 9 || len(notices) == 0 {
		t.Fatalf("languages=%v keys=%d", Languages, len(notices))
	}
	for _, language := range Languages {
		if err := Validate(language); err != nil {
			t.Errorf("%s catalog: %v", language, err)
		}
		translated, missing := Coverage(language)
		if translated != len(notices) || missing != 0 {
			t.Errorf("%s: translated=%d missing=%d total=%d", language, translated, missing, len(notices))
		}
		for key := range notices {
			out := Format(language, key, map[string]string{
				"label": "L", "minutes": "7", "name": "N", "amount": "A", "percent": "75%",
				"used": "U", "limit": "C", "consequence": "K", "at": "10:30", "count": "2",
				"drops": "D", "title": "T", "state": "S", "attempts": "3", "reason": "R",
			})
			if out == "" || strings.ContainsAny(out, "{}<>") {
				t.Errorf("%s %s rendered %q", language, key, out)
			}
		}
	}
}

func TestProductLanguageResolution(t *testing.T) {
	cases := map[string]string{
		"": "en", "en-US": "en", "zh-Hant": "zh-Hant", "zh-Hant-TW": "zh-Hant",
		"zh-TW": "zh-Hant", "zh-HK": "zh-Hant", "zh-Hans": "zh-Hans", "zh-Hans-CN": "zh-Hans",
		"zh-CN": "zh-Hans", "zh-SG": "zh-Hans", "zh": "en", "ja-JP": "ja",
		"pt": "pt-BR", "pt-PT": "pt-BR", "pt-BR": "pt-BR", "es-MX": "es",
		"fr-CA": "fr", "de-AT": "de", "ko-KR": "ko", "xx": "en", "zh--TW": "en",
		"zh-Latn-TW": "en", "zh-Hans-TW": "zh-Hans", "zh-Hant-CN": "zh-Hant",
		"zh-MO": "zh-Hant", "zh_MO": "zh-Hant", "ja_JP": "ja", " ja-JP ": "en",
	}
	for input, want := range cases {
		if got := Resolve(input); got != want {
			t.Errorf("Resolve(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestMalformedTranslationFallsBackAtomically(t *testing.T) {
	for _, malformed := range []string{"", "Bonjour {wrong}", "Bonjour {title", "<b>{title}</b>"} {
		if valid("Hello {title}", malformed) {
			t.Errorf("accepted malformed translation %q", malformed)
		}
	}
	key := "board.body"
	forms := notices[key]
	defer func() { notices[key] = forms }()
	forms[7] = "Fini {wrong}"
	notices[key] = forms
	if err := Validate("fr"); err == nil {
		t.Fatal("invalid French catalog passed validation")
	}
	if got := Format("fr", key, map[string]string{"title": "Task"}); got != "Task is complete." {
		t.Fatalf("mixed a malformed translation into %q", got)
	}
	if got := Format("fr", "board.title", nil); got != "Board item completed" {
		t.Fatalf("invalid catalog was only rejected one key at a time: %q", got)
	}
	translated, missing := Coverage("fr")
	if translated != len(notices)-1 || missing != 1 {
		t.Fatalf("coverage: %d translated, %d missing", translated, missing)
	}
}

func TestMissingSecondaryKeyFallsBackAlone(t *testing.T) {
	key := "board.body"
	forms := notices[key]
	defer func() { notices[key] = forms }()
	forms[7] = ""
	notices[key] = forms
	if err := Validate("fr"); err != nil {
		t.Fatal(err)
	}
	if got := Format("fr", key, map[string]string{"title": "Task"}); got != "Task is complete." {
		t.Fatalf("missing key did not use English: %q", got)
	}
	if got := Format("fr", "board.title", nil); got != "Élément du tableau terminé" {
		t.Fatalf("valid French key was lost: %q", got)
	}
}

func TestEnglishAndTraditionalChineseMustStayComplete(t *testing.T) {
	key := "board.title"
	original := notices[key]
	defer func() { notices[key] = original }()
	forms := original
	forms[1] = ""
	notices[key] = forms
	if err := Validate("zh-Hant"); err == nil {
		t.Fatal("missing Traditional Chinese key passed validation")
	}
	forms = notices[key]
	forms[0] = ""
	notices[key] = forms
	if err := Validate("en"); err == nil {
		t.Fatal("missing English key passed validation")
	}
	if got := Format("fr", "board.body", map[string]string{"title": "Task"}); got != "" {
		t.Fatalf("rendered text against an invalid English catalog: %q", got)
	}
}

func TestDataThatLooksLikeAPlaceholderStaysVerbatim(t *testing.T) {
	got := Format("en", "dead.body", map[string]string{"title": "{state}", "state": "finished", "attempts": "1"})
	if !strings.Contains(got, "“{state}”") {
		t.Fatalf("user title was interpreted as a template: %q", got)
	}
}
