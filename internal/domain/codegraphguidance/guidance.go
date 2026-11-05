// Package codegraphguidance owns the versioned, optional conversation preference.
package codegraphguidance

import (
	_ "embed"
	"encoding/json"
)

//go:embed instructions.json
var document []byte
var instructions struct {
	Version string            `json:"version"`
	English map[string]string `json:"en"`
	Chinese map[string]string `json:"zh-Hant"`
}

func init() {
	if err := json.Unmarshal(document, &instructions); err != nil {
		panic(err)
	}
}
func Version() string { return instructions.Version }
func Text(language string, enabled bool) string {
	words := instructions.English
	if language == "zh-Hant" {
		words = instructions.Chinese
	}
	if enabled {
		return words["enabled"]
	}
	return words["disabled"]
}
