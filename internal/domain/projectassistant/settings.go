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
	// A native configuration catalog is reused for this long, then read again.
	MaxConfigurationCacheSeconds = 600
	// Distinct Agent account, model and Project directory catalogs kept at once.
	MaxConfigurationCacheEntries = 32
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
	// Description is the Agent's note for the choice, such as the model a
	// "Default" entry currently resolves to. Empty when the Agent gives none.
	Description string `json:"description,omitempty"`
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
