package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A catalog is accepted as a unit after validating every key it contains.
// Secondary languages may omit newly added keys; the Console fills those
// missing keys from English and counts them. Invalid existing keys reject the
// whole selected catalog.
var errEnglishCatalog = errors.New("English console catalog is unavailable")

var catalogPlaceholder = regexp.MustCompile(`\{[^{}]+\}`)
var catalogMarkup = regexp.MustCompile(`<[^>]*>`)

func validCatalogTag(tag string) bool {
	if len(tag) < 2 || len(tag) > 35 {
		return false
	}
	for i, part := range strings.Split(tag, "-") {
		if part == "" || len(part) > 8 {
			return false
		}
		for _, ch := range part {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
				continue
			}
			if i > 0 && ch >= '0' && ch <= '9' {
				continue
			}
			return false
		}
	}
	return true
}

func shippedCatalogTag(tag string) bool {
	switch tag {
	case "en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de":
		return true
	}
	return false
}

func readCatalogFile(root, lang string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(root, "catalogs", lang+".json"))
	if err != nil {
		return nil, err
	}
	var catalog map[string]string
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, err
	}
	if catalogContentCount(catalog) == 0 {
		return nil, errors.New("catalog is empty")
	}
	if declared := catalog["lang"]; declared != lang {
		return nil, errors.New("catalog language does not match its filename")
	}
	if direction := catalog["dir"]; direction != "ltr" {
		return nil, errors.New("catalog direction is unsupported")
	}
	for key, value := range catalog {
		if key == "lang" || key == "dir" {
			continue
		}
		if key == "" || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("catalog key %q is empty", key)
		}
	}
	return catalog, nil
}

func catalogShape(value string) (string, bool) {
	// All braces must belong to a placeholder. A broken interpolation is a
	// corrupt catalog even when it happens to leave the other tokens intact.
	withoutPlaceholders := catalogPlaceholder.ReplaceAllString(value, "")
	if strings.ContainsAny(withoutPlaceholders, "{}") {
		return "", false
	}
	forms := strings.Split(value, "\x1f")
	shapes := make([]string, len(forms))
	for i, form := range forms {
		placeholders := catalogPlaceholder.FindAllString(form, -1)
		sort.Strings(placeholders)
		shapes[i] = strings.Join(placeholders, ",")
	}
	return strings.Join(shapes, "\x1f"), true
}

func validCatalog(english, selected map[string]string, requireComplete bool) bool {
	if requireComplete && catalogContentCount(selected) != catalogContentCount(english) {
		return false
	}
	for key, value := range selected {
		if key == "lang" || key == "dir" {
			continue
		}
		source, ok := english[key]
		if !ok || strings.TrimSpace(value) == "" {
			return false
		}
		sourceShape, sourceOK := catalogShape(source)
		selectedShape, selectedOK := catalogShape(value)
		if !sourceOK || !selectedOK || sourceShape != selectedShape {
			return false
		}
		// Existing markup may be translated around, but its literal tags and
		// attributes must stay byte-identical. No new HTML enters through a
		// translated string.
		if !equalMarkup(source, value) {
			return false
		}
		for _, form := range strings.Split(value, "\x1f") {
			if strings.TrimSpace(form) == "" {
				return false
			}
		}
	}
	if requireComplete {
		for key := range english {
			if key == "lang" || key == "dir" {
				continue
			}
			if _, ok := selected[key]; !ok {
				return false
			}
		}
	}
	return true
}

func catalogContentCount(catalog map[string]string) int {
	count := len(catalog)
	for _, reserved := range []string{"lang", "dir"} {
		if _, ok := catalog[reserved]; ok {
			count--
		}
	}
	return count
}

func equalMarkup(source, selected string) bool {
	a := catalogMarkup.FindAllString(source, -1)
	b := catalogMarkup.FindAllString(selected, -1)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return !strings.ContainsAny(catalogMarkup.ReplaceAllString(selected, ""), "<>")
}

// loadCatalog returns the actual served language. If English is unreadable,
// callers report a typed refusal or omit the optional HTML embed; the console
// can then use its bundled English copy.
func loadCatalog(root, requested string) (map[string]string, error) {
	if root == "" {
		return nil, errEnglishCatalog
	}
	english, err := readCatalogFile(root, "en")
	if err != nil || !validCatalog(english, english, true) {
		return nil, errEnglishCatalog
	}
	actual := "en"
	catalog := english
	if requested != "en" && shippedCatalogTag(requested) {
		// English and Taiwan Traditional Chinese are synchronised on every
		// change. Secondary languages may defer a new key after the first
		// complete nine-language baseline; their existing keys still must
		// validate, and the client fills missing keys from English.
		strict := requested == "zh-Hant"
		if selected, err := readCatalogFile(root, requested); err == nil && validCatalog(english, selected, strict) {
			catalog, actual = selected, requested
		}
	}
	// Do not mutate a value shared with another caller or a future cache.
	out := make(map[string]string, len(catalog)+2)
	for key, value := range catalog {
		out[key] = value
	}
	out["lang"] = actual
	out["dir"] = "ltr"
	return out, nil
}
