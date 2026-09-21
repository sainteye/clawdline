package orchestrator

import (
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

func TestDisplayLanguageFollowsSettingMachineThenCatalog(t *testing.T) {
	machine := []whisper.Answer{{Tag: "zh-TW", Source: whisper.FromAppleLangs}}
	cases := []struct {
		name, setting, catalog, want string
		machine                      []whisper.Answer
	}{
		{"setting", "en", "zh-Hant", "en", machine},
		{"auto asks machine", "auto", "zh-Hant", "zh-TW", machine},
		{"missing asks machine", "", "zh-Hant", "zh-TW", machine},
		{"catalog fallback", "auto", "zh-Hant", "zh-Hant", nil},
		{"built-in fallback", "auto", "", "zh-Hant", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayLanguage(c.setting, c.machine, c.catalog); got != c.want {
				t.Fatalf("displayLanguage() = %q, want %q", got, c.want)
			}
		})
	}
}
