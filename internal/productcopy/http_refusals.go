package productcopy

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed http_refusals/*.json http_refusals/baseline.keys
var httpRefusalFiles embed.FS

var (
	httpRefusalOnce     sync.Once
	httpRefusalKeys     map[string]string
	httpRefusalEnglish  map[string]string
	httpRefusalLocales  map[string]map[string]string
	httpRefusalBaseline map[string]bool
)

func loadHTTPRefusals() {
	raw, err := httpRefusalFiles.ReadFile("http_refusals/en.json")
	if err != nil {
		panic(fmt.Errorf("embedded HTTP refusal catalog: %w", err))
	}
	if err := json.Unmarshal(raw, &httpRefusalEnglish); err != nil {
		panic(fmt.Errorf("embedded HTTP refusal catalog: %w", err))
	}
	httpRefusalKeys = make(map[string]string, len(httpRefusalEnglish))
	for key, english := range httpRefusalEnglish {
		digest := sha256.Sum256([]byte(english))
		if want := fmt.Sprintf("http.%x", digest[:8]); key != want || english == "" {
			panic(fmt.Errorf("embedded HTTP refusal catalog has invalid key %q", key))
		}
		if prior := httpRefusalKeys[english]; prior != "" {
			panic(fmt.Errorf("embedded HTTP refusal catalog repeats %q", prior))
		}
		httpRefusalKeys[english] = key
	}
	baseline, err := httpRefusalFiles.ReadFile("http_refusals/baseline.keys")
	if err != nil {
		panic(fmt.Errorf("embedded HTTP refusal baseline: %w", err))
	}
	httpRefusalBaseline = make(map[string]bool)
	for _, key := range strings.Fields(string(baseline)) {
		if httpRefusalEnglish[key] == "" || httpRefusalBaseline[key] {
			panic(fmt.Errorf("embedded HTTP refusal baseline has invalid key %q", key))
		}
		httpRefusalBaseline[key] = true
	}
	if len(httpRefusalBaseline) != 1192 {
		panic(fmt.Errorf("embedded HTTP refusal baseline has %d keys, want 1192", len(httpRefusalBaseline)))
	}
	httpRefusalLocales = make(map[string]map[string]string)
	for _, language := range Languages[1:] {
		raw, err := httpRefusalFiles.ReadFile("http_refusals/" + language + ".json")
		if err != nil {
			continue
		}
		var selected map[string]string
		if json.Unmarshal(raw, &selected) == nil && validHTTPRefusalCatalog(httpRefusalEnglish, selected, httpRefusalBaseline, language == "zh-Hant") {
			httpRefusalLocales[language] = selected
		}
	}
}

func validHTTPRefusalCatalog(english, selected map[string]string, baseline map[string]bool, complete bool) bool {
	if complete && len(selected) != len(english) {
		return false
	}
	for key := range baseline {
		if selected[key] == "" {
			return false
		}
	}
	for key, value := range selected {
		source, exists := english[key]
		if !exists || value == "" || !sameHTTPRefusalForms(source, value) {
			return false
		}
	}
	return true
}

func sameHTTPRefusalForms(source, selected string) bool {
	// A named interpolation value belongs to its own plural branch. Literal
	// JSON examples and angle-bracket command arguments are displayed raw.
	a, b := splitHTTPRefusalForms(source), splitHTTPRefusalForms(selected)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if b[i] == "" || !sameHTTPRefusalPlaceholders(a[i], b[i]) {
			return false
		}
	}
	return true
}

func splitHTTPRefusalForms(value string) []string {
	return strings.Split(value, "\x1f")
}

func sameHTTPRefusalPlaceholders(source, selected string) bool {
	want, got := make(map[string]int), make(map[string]int)
	for _, name := range placeholder.FindAllString(source, -1) {
		want[name]++
	}
	for _, name := range placeholder.FindAllString(selected, -1) {
		got[name]++
	}
	if len(want) != len(got) {
		return false
	}
	for name, count := range want {
		if got[name] != count {
			return false
		}
	}
	return true
}

// HTTPRefusalKey identifies an exact fixed English refusal sentence. The
// caller must first establish that the text came from authored fixed copy:
// matching bytes alone do not prove provenance for runtime or external text.
func HTTPRefusalKey(detail string) string {
	httpRefusalOnce.Do(loadHTTPRefusals)
	return httpRefusalKeys[detail]
}

// HTTPRefusalText returns a translated human-facing sentence only when the
// key names the exact English wire detail. Unknown keys, malformed catalogs
// and missing secondary-language entries keep the wire detail unchanged.
func HTTPRefusalText(language, key, detail string) string {
	httpRefusalOnce.Do(loadHTTPRefusals)
	if key == "" || httpRefusalEnglish[key] != detail {
		return detail
	}
	selected := httpRefusalLocales[Resolve(language)]
	if translation := selected[key]; translation != "" {
		return translation
	}
	return detail
}

// HTTPRefusalCoverage reports the count of present, validated translations.
func HTTPRefusalCoverage(language string) (translated, total int) {
	httpRefusalOnce.Do(loadHTTPRefusals)
	total = len(httpRefusalEnglish)
	if Resolve(language) == "en" {
		return total, total
	}
	return len(httpRefusalLocales[Resolve(language)]), total
}
