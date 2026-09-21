package whisper

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// What WantsTraditional said before `auto` learned to ask anybody, it still
// says — the negatives as much as the positives, because a rule that answers
// "Traditional" to everything passes every positive on a machine set to zh-TW.
func TestWantsTraditionalKeepsItsAnswers(t *testing.T) {
	for _, tag := range []string{"zh-TW", "zh-HK", "zh-Hant", "zh-MO", "zh-Hant-TW", "zh_TW.UTF-8", "ZH-hk"} {
		if !WantsTraditional(tag) {
			t.Errorf("%s: want Traditional", tag)
		}
	}
	for _, tag := range []string{"zh-CN", "zh-Hans", "zh", "zh-SG", "zh_CN.UTF-8", "zh-Hans-CN",
		// A script subtag outranks a region: Simplified written in Hong Kong.
		"zh-Hans-HK",
		"en", "en-TW", "ja", "auto", ""} {
		if WantsTraditional(tag) {
			t.Errorf("%s: want not Traditional", tag)
		}
	}
}

func TestMachineLanguagesAskInTheirOwnOrder(t *testing.T) {
	ctx := context.Background()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	apple := func(langs, locale string) func(context.Context, string) (string, error) {
		return func(_ context.Context, key string) (string, error) {
			if key == "AppleLanguages" && langs != "" {
				return langs, nil
			}
			if key == "AppleLocale" && locale != "" {
				return locale + "\n", nil
			}
			return "", errors.New("does not exist")
		}
	}
	cases := []struct {
		name string
		m    Machine
		want []Answer
	}{
		// A terminal's LANG is often only an encoding on a Mac; the person's
		// choice is Language & Region, so it comes first.
		{"mac", Machine{GOOS: "darwin", Getenv: env(map[string]string{"LANG": "en_US.UTF-8"}),
			Defaults: apple("(\n    \"zh-Hant-TW\",\n    en\n)\n", "zh_TW")},
			[]Answer{{"zh-Hant-TW", FromAppleLangs}, {"en", FromAppleLangs}, {"zh_TW", FromAppleLocale}, {"en_US.UTF-8", "LANG"}}},
		{"mac with defaults unreadable", Machine{GOOS: "darwin", Getenv: env(nil), Defaults: apple("", "")},
			[]Answer{}},
		{"linux", Machine{GOOS: "linux", Getenv: env(map[string]string{
			"LANGUAGE": "zh_TW:en", "LC_ALL": "", "LC_MESSAGES": "zh_CN.UTF-8", "LANG": "C.UTF-8"})},
			[]Answer{{"zh_TW", "LANGUAGE"}, {"en", "LANGUAGE"}, {"zh_CN.UTF-8", "LC_MESSAGES"}}},
		// gettext ignores LANGUAGE under the C locale, and so does this.
		{"linux C locale", Machine{GOOS: "linux", Getenv: env(map[string]string{"LANGUAGE": "zh_TW", "LANG": "C"})},
			[]Answer{}},
		{"windows", Machine{GOOS: "windows", Getenv: env(nil), UserLocale: func() string { return "zh-TW" }},
			[]Answer{{"zh-TW", FromWindows}}},
	}
	for _, c := range cases {
		got := c.m.Languages(ctx)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	hant := []Answer{{"zh-Hant-TW", FromAppleLangs}, {"zh-Hant", FromCatalog}}
	cases := []struct {
		name     string
		language string
		follow   []Answer
		code     string
		script   string
		from     string
	}{
		{"explicit wins", "zh-CN", hant, "zh", "Hans", FromVoiceLanguage},
		{"explicit English", "en", hant, "en", "", FromVoiceLanguage},
		{"auto asks", "auto", hant, "zh", "Hant", FromAppleLangs},
		{"empty is auto", "", []Answer{{"zh_CN.UTF-8", "LANG"}}, "zh", "Hans", "LANG"},
		{"unusable answers are skipped", "auto", []Answer{{"C.UTF-8", "LANG"}, {"auto", FromLanguage}, {"zh-Hant", FromCatalog}},
			"zh", "Hant", FromCatalog},
		{"non-Chinese first leaves detection", "auto", []Answer{{"en-US", FromAppleLangs}, {"zh-Hans-CN", FromAppleLangs}, {"zh-Hant", FromCatalog}},
			"auto", "Hans", FromAppleLangs},
		{"nobody at all", "auto", nil, "auto", "", ""},
	}
	for _, c := range cases {
		p := Decide(c.language, c.follow)
		if p.Code != c.code || p.Script != c.script || p.From.Source != c.from {
			t.Errorf("%s: %+v", c.name, p)
		}
		if p.Code == "zh" && p.Seed != Seeds["zh-"+p.Script] {
			t.Errorf("%s: seed %q for script %s", c.name, p.Seed, p.Script)
		}
		if p.Code == "auto" && p.Seed != "" {
			t.Errorf("%s: a seed under detection: %q", c.name, p.Seed)
		}
	}
}

func TestLooksChinese(t *testing.T) {
	for text, want := range map[string]bool{
		"我们现在开始说话。":          true,
		"那個 webhook 的 retry": true,
		"学校に行きます。":           false,
		"コード":                false,
		"Push the branch.":   false,
		"안녕하세요":              false,
		"":                   false,
	} {
		if LooksChinese(text) != want {
			t.Errorf("%q: want %v", text, want)
		}
	}
}
