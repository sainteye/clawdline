package productcopy

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Agent instructions contain literal CLI syntax, including angle-bracket
// arguments. They share locale resolution with product copy, but are not HTML.
//
//go:embed board/*.json
var boardFiles embed.FS

var boardInstructions = loadBoardInstructions()

func loadBoardInstructions() map[string]map[string]string {
	out := make(map[string]map[string]string)
	for _, language := range []string{"en", "zh-Hant"} {
		raw, err := boardFiles.ReadFile("board/" + language + ".json")
		if err != nil {
			panic(err)
		}
		var entries map[string]string
		if err := json.Unmarshal(raw, &entries); err != nil {
			panic(err)
		}
		out[language] = entries
	}
	for key, source := range out["en"] {
		if source == "" || !sameHTTPRefusalForms(source, out["zh-Hant"][key]) {
			panic(fmt.Errorf("invalid Board instruction translation %q", key))
		}
	}
	if len(out["en"]) != len(out["zh-Hant"]) {
		panic("Board instruction catalogs have different keys")
	}
	return out
}

// BoardInstruction renders daemon-authored task prose. Authored item data is
// substituted once, so placeholders inside titles or descriptions stay literal.
// New secondary-language templates fall back to English until translated.
func BoardInstruction(language, key string, values map[string]string) string {
	template := boardInstructions[boardInstructionLanguage(language)][key]
	if template == "" {
		template = boardInstructions["en"][key]
	}
	return placeholder.ReplaceAllStringFunc(template, func(match string) string {
		return values[match[1:len(match)-1]]
	})
}

// BoardInstructionCoverage reports translated and missing task templates.
func BoardInstructionCoverage(language string) (translated, missing int) {
	selected := boardInstructions[boardInstructionLanguage(language)]
	for key := range boardInstructions["en"] {
		if selected[key] != "" {
			translated++
		} else {
			missing++
		}
	}
	return
}

// RootAssignmentOpeningID recognizes the first-message wrapper in every
// authored instruction language, including the historical English opening.
// Readers resolve its ID against daemon records, never a path in the prose.
func RootAssignmentOpeningID(text string) string {
	text = strings.TrimSpace(text)
	for _, entries := range boardInstructions {
		prefix, _, ok := strings.Cut(entries["root_line"], "{id}")
		if !ok || prefix == "" || !strings.HasPrefix(text, prefix) {
			continue
		}
		rest := strings.TrimPrefix(text, prefix)
		end := strings.IndexFunc(rest, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
		})
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	return ""
}

// The Agent preference may fall through to an unnormalized machine locale.
// Product interface locale resolution stays strict and unchanged.
func boardInstructionLanguage(language string) string {
	language = strings.TrimSpace(language)
	if i := strings.IndexAny(language, ".@"); i >= 0 {
		language = language[:i]
	}
	return Resolve(language)
}
