package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

// Each command group owns a separate directory, so independent command
// changes do not compete for one catalog file. English and Traditional Chinese
// stay complete. The other shipped languages may omit later additions.
//
//go:embed cli_catalogs/*/*.json
var cliCatalogFiles embed.FS

type cliCatalogGroup struct {
	english map[string]string
	locales map[string]map[string]string
}

var cliCatalogGroups = readCLICatalogs()

const cliLanguageOptionEnglish = "--lang <tag> before a command overrides CLAWDLINE_LANG, then product_language, for human-readable output"

func readCLICatalogs() map[string]cliCatalogGroup {
	groups := make(map[string]cliCatalogGroup)
	directories, err := fs.ReadDir(cliCatalogFiles, "cli_catalogs")
	if err != nil {
		return groups
	}
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		group := directory.Name()
		root := path.Join("cli_catalogs", group)
		english, err := readCLICatalogFile(path.Join(root, "en.json"))
		if err != nil || len(english) == 0 {
			continue
		}
		bundle := cliCatalogGroup{english: english, locales: make(map[string]map[string]string)}
		for _, language := range []string{"zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
			selected, err := readCLICatalogFile(path.Join(root, language+".json"))
			if err == nil && validCLICatalog(english, selected, language == "zh-Hant") {
				bundle.locales[language] = selected
			}
		}
		groups[group] = bundle
	}
	return groups
}

func readCLICatalogFile(filename string) (map[string]string, error) {
	data, err := cliCatalogFiles.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func validCLICatalog(english, selected map[string]string, complete bool) bool {
	if complete && len(selected) != len(english) {
		return false
	}
	for key, value := range selected {
		source, exists := english[key]
		if !exists || key == "" || strings.TrimSpace(value) == "" || !sameCLIVerbs(source, value) {
			return false
		}
	}
	return true
}

func sameCLIVerbs(source, selected string) bool {
	englishForms := strings.Split(source, "\x1f")
	selectedForms := strings.Split(selected, "\x1f")
	if len(englishForms) != len(selectedForms) {
		return false
	}
	for form := range englishForms {
		a, aValid := cliFormatVerbs(englishForms[form])
		b, bValid := cliFormatVerbs(selectedForms[form])
		if !aValid || !bValid || len(a) != len(b) || strings.TrimSpace(selectedForms[form]) == "" {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
	}
	return true
}

// Preserve each fmt directive, including its width and argument index. A
// literal percent must be escaped as %% on both sides.
func cliFormatVerbs(value string) ([]string, bool) {
	var verbs []string
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			continue
		}
		start := i
		i++
		if i == len(value) {
			return nil, false
		}
		if value[i] == '%' {
			continue
		}
		for i < len(value) && (value[i] == '[' || value[i] == ']' || value[i] == '#' || value[i] == '+' || value[i] == '-' || value[i] == ' ' || value[i] == '0' || value[i] == '.' || value[i] >= '1' && value[i] <= '9') {
			i++
		}
		if i == len(value) || !(value[i] >= 'a' && value[i] <= 'z' || value[i] >= 'A' && value[i] <= 'Z') {
			return nil, false
		}
		verbs = append(verbs, value[start:i+1])
	}
	return verbs, true
}

func cliCopy(group, key, english string) string {
	bundle, ok := cliCatalogGroups[group]
	if !ok || bundle.english[key] != english {
		return english
	}
	selected := bundle.locales[currentCLILanguage()]
	if translation := selected[key]; translation != "" {
		return translation
	}
	return english
}

func cliCatalogCoverage(group, language string) (translated, total int) {
	bundle := cliCatalogGroups[group]
	total = len(bundle.english)
	translated = len(bundle.locales[language])
	if language == "en" {
		translated = total
	}
	return translated, total
}
