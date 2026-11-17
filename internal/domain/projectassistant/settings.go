package projectassistant

import (
	"reflect"
	"unicode/utf8"
)

const (
	MaxConfigurationProbes     = 1
	MaxConfigurationOptions    = 100
	MaxConfigurationChoices    = 100
	MaxConfigurationValueBytes = 16 << 10
	MaxMemoryPreviewRunes      = 128
)

// RuntimeSettings contains only ACP selections, never launch commands or flags.
// A nil edit preserves these settings for older clients. Empty values inherit.
type RuntimeSettings struct {
	Model   string            `json:"model"`
	Options map[string]string `json:"options"`
}

type ConfigChoice struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type ConfigOption struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Category string         `json:"category"`
	Current  string         `json:"current"`
	Choices  []ConfigChoice `json:"choices"`
}

type ConfigurationCatalog struct {
	Agent   Agent          `json:"agent"`
	Model   string         `json:"model"`
	Options []ConfigOption `json:"options"`
}

func ValidateRuntimeSettings(s RuntimeSettings) error {
	if len(s.Options) > MaxConfigurationOptions || len(s.Model) > MaxConfigurationValueBytes || !utf8.ValidString(s.Model) {
		return fail(Invalid, "runtime_settings")
	}
	for k, v := range s.Options {
		if k == "" || len(k) > MaxConfigurationValueBytes || len(v) > MaxConfigurationValueBytes || !utf8.ValidString(k) || !utf8.ValidString(v) {
			return fail(Invalid, "runtime_settings")
		}
	}
	return nil
}

func SameRuntimeSettings(a, b RuntimeSettings) bool {
	return a.Model == b.Model && (len(a.Options) == 0 && len(b.Options) == 0 || reflect.DeepEqual(a.Options, b.Options))
}
