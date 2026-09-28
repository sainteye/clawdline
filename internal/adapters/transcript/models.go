package transcript

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

var claudeModelPattern = regexp.MustCompile(`claude-(fable|opus|sonnet|haiku)-([0-9]+)(?:-([0-9]+))?`)

type claudeVersion struct {
	family       string
	major, minor int
}

func (v claudeVersion) id() string {
	id := fmt.Sprintf("claude-%s-%d", v.family, v.major)
	if v.minor > 0 {
		id += fmt.Sprintf("-%d", v.minor)
	}
	return id
}

func (v claudeVersion) label() string {
	name := map[string]string{"fable": "Fable", "opus": "Opus", "sonnet": "Sonnet", "haiku": "Haiku"}[v.family]
	if v.minor == 0 {
		return fmt.Sprintf("%s %d", name, v.major)
	}
	return fmt.Sprintf("%s %d.%d", name, v.major, v.minor)
}

func compareClaudeVersion(a, b claudeVersion) int {
	if a.major != b.major {
		return a.major - b.major
	}
	return a.minor - b.minor
}

// claudeModels reconstructs the versioned rows Claude Code's /model picker
// can select without starting a second assistant or making a provider call.
// The account cache says which newest family versions are actually exposed;
// the installed executable supplies the legacy rows that cache omits.
func claudeModels(home, executable string) []Model {
	state := filepath.Join(home, ".claude.json")
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		state = filepath.Join(dir, ".claude.json")
	}
	account := readClaudeAccountVersions(state)
	installed := readClaudeVersions(executable)
	if len(account) == 0 && len(installed) == 0 {
		return []Model{
			{Name: "Opus", Command: "opus"},
			{Name: "Fable", Command: "fable"},
			{Name: "Sonnet", Command: "sonnet"},
			{Name: "Haiku", Command: "haiku"},
		}
	}

	all := make(map[claudeVersion]struct{}, len(account)+len(installed))
	for version := range account {
		all[version] = struct{}{}
	}
	for version := range installed {
		all[version] = struct{}{}
	}

	latest := make(map[string]claudeVersion, 4)
	for _, family := range []string{"opus", "fable", "sonnet", "haiku"} {
		source := account
		if !hasClaudeFamily(source, family) {
			source = installed
		}
		for version := range source {
			if version.family != family {
				continue
			}
			if current, ok := latest[family]; !ok || compareClaudeVersion(version, current) > 0 {
				latest[family] = version
			}
		}
	}

	out := make([]Model, 0, len(all))
	for _, family := range []string{"opus", "fable", "sonnet", "haiku"} {
		if version, ok := latest[family]; ok {
			out = append(out, Model{ID: version.id(), Name: version.label(), Command: family})
		}
	}
	for _, family := range []string{"fable", "opus"} {
		current, ok := latest[family]
		if !ok {
			continue
		}
		legacy := make([]claudeVersion, 0)
		for version := range all {
			if version.family != family || compareClaudeVersion(version, current) >= 0 || !shownClaudeLegacy(version) {
				continue
			}
			legacy = append(legacy, version)
		}
		sort.Slice(legacy, func(i, j int) bool { return compareClaudeVersion(legacy[i], legacy[j]) > 0 })
		for _, version := range legacy {
			command := version.id()
			if family == "opus" {
				command = "opus" + strconv.Itoa(version.major)
				if version.minor > 0 {
					command += strconv.Itoa(version.minor)
				}
			}
			out = append(out, Model{ID: version.id(), Name: version.label(), Command: command})
		}
	}
	return out
}

// readClaudeAccountVersions only accepts model-bearing fields in Claude
// Code's state. Scanning every string would let an unrelated project name or
// announcement that merely mentions a future model become a picker choice.
func readClaudeAccountVersions(path string) map[claudeVersion]struct{} {
	out := map[claudeVersion]struct{}{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	var walk func([]string) error
	walk = func(path []string) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						return err
					}
					if err := walk(append(path, key.(string))); err != nil {
						return err
					}
				}
				_, err = decoder.Token()
				return err
			case '[':
				for decoder.More() {
					if err := walk(path); err != nil {
						return err
					}
				}
				_, err = decoder.Token()
				return err
			}
		}
		value, ok := token.(string)
		if !ok || !claudeAccountModelField(path) {
			return nil
		}
		addClaudeVersions(out, []byte(value))
		return nil
	}
	if err := walk(nil); err != nil {
		return map[claudeVersion]struct{}{}
	}
	return out
}

func claudeAccountModelField(path []string) bool {
	if len(path) == 0 {
		return false
	}
	last := path[len(path)-1]
	if last == "model" && (len(path) == 1 || path[0] == "clientDataCacheSlots") {
		return true
	}
	if last == "requiresModel" && path[0] == "cachedGrowthBookFeatures" {
		return true
	}
	return (path[0] == "additionalModelOptionsCache" && last == "value") ||
		path[0] == "modelAccessCache" || path[0] == "orgModelDefaultCache"
}

func hasClaudeFamily(versions map[claudeVersion]struct{}, family string) bool {
	for version := range versions {
		if version.family == family {
			return true
		}
	}
	return false
}

func shownClaudeLegacy(version claudeVersion) bool {
	switch version.family {
	case "fable":
		return version.major >= 5
	case "opus":
		return version.major > 4 || version.major == 4 && version.minor >= 5
	default:
		return false
	}
}

// readClaudeVersions scans in fixed memory because the native Claude Code
// executable is large. The overlap keeps an identifier split across reads.
func readClaudeVersions(path string) map[claudeVersion]struct{} {
	out := map[claudeVersion]struct{}{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	buf := make([]byte, 256<<10)
	var overlap []byte
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			window := make([]byte, len(overlap)+n)
			copy(window, overlap)
			copy(window[len(overlap):], buf[:n])
			addClaudeVersions(out, window)
			keep := 96
			if keep > len(window) {
				keep = len(window)
			}
			overlap = append(overlap[:0], window[len(window)-keep:]...)
		}
		if readErr != nil {
			if readErr != io.EOF {
				return map[claudeVersion]struct{}{}
			}
			break
		}
	}
	return out
}

func addClaudeVersions(out map[claudeVersion]struct{}, data []byte) {
	for _, match := range claudeModelPattern.FindAllSubmatch(data, -1) {
		major, errMajor := strconv.Atoi(string(match[2]))
		minor := 0
		var errMinor error
		if len(match[3]) > 0 {
			minor, errMinor = strconv.Atoi(string(match[3]))
		}
		// A date-stamped major model such as claude-opus-4-20250514 is
		// Opus 4, not a fictional version 4.20250514.
		if minor > 99 {
			minor = 0
		}
		if errMajor == nil && errMinor == nil {
			out[claudeVersion{family: string(match[1]), major: major, minor: minor}] = struct{}{}
		}
	}
}

func claudeExecutable(home string) string {
	var candidates []string
	if found, err := exec.LookPath("claude"); err == nil {
		candidates = append(candidates, found)
	}
	candidates = append(candidates,
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".local", "bin", "claude.exe"),
		filepath.Join(home, "AppData", "Roaming", "npm", "claude.cmd"),
	)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
