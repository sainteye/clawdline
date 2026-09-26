// Package nextconfig reads and writes this app's own settings file,
// ~/.config/clawdline-next/config.json, the one the macOS shell reads its
// hotkey from (shell/darwin/NextConfig.swift).
//
// The keys are the Swift app's spellings, so a line copied from one file means
// the same thing in the other. The file is the Swift app's `Config` in shape and
// in manners, and the manners are the point of this package:
//
//   - **A write is a merge onto what is on disk now.** A person may have edited
//     the file by hand since it was last read, and `Config.save()` keeps that
//     edit; so does this. Keys this daemon does not know are carried over as
//     their exact JSON.
//   - **A write is atomic.** The new contents go to a temporary file in the same
//     directory, are synced, and are renamed over the old one. A reader — the
//     shell reloading on the page's word — sees the old file or the new one,
//     never half of either.
//   - **The file is the person's alone**: 0600, in a 0700 directory.
//   - **A file that is not a JSON object is not overwritten.** It is somebody's
//     hand edit with a comma in the wrong place, and replacing it with the one
//     key a control changed would throw the rest of it away. The write is
//     refused and the file left for its author.
//   - **Never the Swift app's directory.** ~/.config/clawdline belongs to the
//     app that is running beside this one. A directory that is, or resolves
//     into, that one is refused for writing whatever the environment says.
package nextconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileName is the settings file inside this app's directory.
const FileName = "config.json"

// ErrNotObject is the answer for a file that exists and is not one JSON object.
var ErrNotObject = errors.New("the settings file is not a JSON object")

// ErrForeignDir is the answer for a directory that is the Swift app's.
var ErrForeignDir = errors.New("refusing to write the Swift app's settings directory")

// Values is one reading of the file.
type Values struct {
	// Exists is false when there is no file. That is the ordinary first-run
	// answer: every key has its default.
	Exists bool
	// Raw holds every top-level key as the exact JSON the file carries.
	Raw map[string]json.RawMessage
}

// String returns a key's value when it is a JSON string, and whether it was.
func (v Values) String(key string) (string, bool) {
	raw, ok := v.Raw[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// Bool returns a key's value when it is a JSON boolean, and whether it was.
func (v Values) Bool(key string) (bool, bool) {
	raw, ok := v.Raw[key]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, false
	}
	return b, true
}

// Number returns a key's value when it is a JSON number, and whether it was.
func (v Values) Number(key string) (float64, bool) {
	raw, ok := v.Raw[key]
	if !ok {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false
	}
	return f, true
}

// File is the settings file of one directory. One per process is enough; its
// lock orders this process's writers, and a write re-reads the disk under it.
type File struct {
	dir     string
	foreign []string
	mu      sync.Mutex
}

// Open names the file in dir. foreign are directories this will never write —
// the Swift app's, by every name it may be known by.
func Open(dir string, foreign ...string) *File {
	return &File{dir: dir, foreign: foreign}
}

// Path is where the file is, whether or not it exists.
func (f *File) Path() string { return filepath.Join(f.dir, FileName) }

// Read returns what the file says now.
func (f *File) Read() (Values, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read()
}

func (f *File) read() (Values, error) {
	data, err := os.ReadFile(f.Path())
	if errors.Is(err, os.ErrNotExist) {
		return Values{Exists: false, Raw: map[string]json.RawMessage{}}, nil
	}
	if err != nil {
		return Values{}, err
	}
	var raw map[string]json.RawMessage
	// `null` decodes into a nil map without complaint, and it is not an object.
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return Values{Exists: true}, ErrNotObject
	}
	return Values{Exists: true, Raw: raw}, nil
}

// Set writes the given keys over what is on disk and returns the file as
// written. Each value is marshalled as JSON; every other key is kept as it was.
func (f *File) Set(changes map[string]any) (Values, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.checkDir(); err != nil {
		return Values{}, err
	}
	current, err := f.read()
	if err != nil {
		return Values{}, err
	}
	return f.set(current, changes)
}

// Change reads the file and chooses changes while holding the same lock that
// writes them. It is for a key whose value is itself a collection: two session
// titles saved together must each be added to the newest array, rather than
// both reading an older array and the second write replacing the first.
func (f *File) Change(change func(Values) (map[string]any, error)) (Values, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.checkDir(); err != nil {
		return Values{}, err
	}
	current, err := f.read()
	if err != nil {
		return Values{}, err
	}
	changes, err := change(current)
	if err != nil {
		return Values{}, err
	}
	return f.set(current, changes)
}

// set merges changes onto a reading taken under f.mu and writes the result.
func (f *File) set(current Values, changes map[string]any) (Values, error) {
	next := make(map[string]json.RawMessage, len(current.Raw)+len(changes))
	for k, v := range current.Raw {
		next[k] = v
	}
	for k, v := range changes {
		encoded, err := json.Marshal(v)
		if err != nil {
			return Values{}, fmt.Errorf("%s: %w", k, err)
		}
		next[k] = encoded
	}
	// Sorted keys and two-space indentation, as `Config.save()` writes it, so a
	// person diffing the file sees only what changed.
	body, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return Values{}, err
	}
	body = append(body, '\n')
	if err := f.writeAtomically(body); err != nil {
		return Values{}, err
	}
	return Values{Exists: true, Raw: next}, nil
}

// checkDir refuses the Swift app's directory, as spelled and as resolved, and
// makes this app's own directory when it is not there yet.
func (f *File) checkDir() error {
	if f.dir == "" {
		return errors.New("no settings directory")
	}
	for _, foreign := range f.foreign {
		if foreign != "" && (within(f.dir, foreign) || within(resolved(f.dir), resolved(foreign))) {
			return fmt.Errorf("%w: %s", ErrForeignDir, f.dir)
		}
	}
	return os.MkdirAll(f.dir, 0o700)
}

func (f *File) writeAtomically(body []byte) (err error) {
	tmp, err := os.CreateTemp(f.dir, "."+FileName+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, f.Path()); err != nil {
		return err
	}
	// The rename is durable once the directory is; a failure here is not a
	// failed write, the new contents are already the file.
	if d, derr := os.Open(f.dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// within reports whether path is dir or inside it, comparing cleaned absolute
// spellings.
func within(path, dir string) bool {
	p, err1 := filepath.Abs(path)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		// A path that cannot be made absolute cannot be shown to be elsewhere.
		return true
	}
	if p == d {
		return true
	}
	return strings.HasPrefix(p, d+string(filepath.Separator))
}

// resolved follows symlinks as far as the path exists, so a directory that does
// not exist yet still resolves through the parents that do.
func resolved(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rest := ""
	for cur := abs; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// ValidHotkey reports whether spec is a combination the shell can register —
// shell/darwin/HotKey.swift `parse`, key table and all — or empty, which means
// none. A bare key is refused as the Swift app's recorder refuses it: a letter
// registered globally is a letter taken from every other app. Function keys
// are the exception, being nobody's letter.
func ValidHotkey(spec string) bool {
	if spec == "" {
		return true
	}
	if len(spec) > 64 {
		return false
	}
	n := strings.ToLower(spec)
	for from, to := range map[string]string{"⌘": "cmd+", "⌥": "option+", "⌃": "control+", "⇧": "shift+"} {
		n = strings.ReplaceAll(n, from, to)
	}
	mods := 0
	key := ""
	for _, part := range strings.Split(n, "+") {
		switch part = strings.TrimSpace(part); part {
		case "cmd", "command", "opt", "option", "alt", "ctrl", "control", "shift":
			mods++
		case "":
			continue
		default:
			key = part
		}
	}
	if !hotkeyKeys[key] {
		return false
	}
	return mods > 0 || functionKey(key)
}

// functionKey is f1 to f12. The Swift app's recorder asks `hasPrefix("f") &&
// count <= 3`, which also lets the bare letter f through; this does not.
func functionKey(key string) bool {
	return len(key) >= 2 && len(key) <= 3 && key[0] == 'f' && strings.Trim(key[1:], "0123456789") == ""
}

var hotkeyKeys = func() map[string]bool {
	out := map[string]bool{}
	for _, k := range strings.Fields(`a s d f h g z x c v b q w e r y t 1 2 3 4 6 5 = 9 7 - 8 0 ] o u [ i p
		return enter l j ' k ; \ , / n m . tab space ` + "`" + ` escape esc left right down up
		f1 f2 f3 f4 f5 f6 f7 f8 f9 f10 f11 f12`) {
		out[k] = true
	}
	return out
}()

// ValidScope reports whether s reads as the shell reads `scope_app`: empty for
// every app, otherwise bundle identifiers separated by commas.
func ValidScope(s string) bool {
	if len(s) > 2048 || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return false
	}
	for _, id := range strings.Split(s, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			if strings.TrimSpace(s) == "" {
				return true
			}
			continue
		}
		for _, r := range id {
			ok := r == '.' || r == '-' || r == '_' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !ok {
				return false
			}
		}
	}
	return true
}

// Settable is one key the settings window may change, and what the file will
// take for it.
//
// A table rather than thirty-six `if` statements in the route, for the reason
// the route is not the right place to know any of this: what `output_size`
// accepts is a fact about the file, and the shell, the daemon and a later
// platform's shell all have to agree on it. The bounds are the Swift app's
// `Config.load` bounds, not its sliders' — a hand-edited file that app would
// keep is a file this one keeps too.
type Settable struct {
	// Name is the key as it is spelled in the file.
	Name string
	// Kind is one of "string", "bool", "number", "int".
	Kind string
	// Choices is the closed set a string may be, when it is closed.
	Choices []string
	// Check is the test for a string that is not a closed set.
	Check func(string) bool
	// Min and Max bound a number or an integer, inclusive.
	Min, Max float64
	// Zero lets an integer also be 0, outside Min and Max: the key's "off".
	Zero bool
	// Refusal is the code a rejected value answers with.
	Refusal string
	// Because is what the refusal says after the code.
	Because string
}

// Allows reports whether v is a value this key will take. v is a string, a
// bool, a float64 or an int64, matching Kind.
func (k Settable) Allows(v any) bool {
	switch k.Kind {
	case "string":
		s, ok := v.(string)
		if !ok {
			return false
		}
		if k.Check != nil {
			return k.Check(s)
		}
		for _, choice := range k.Choices {
			if s == choice {
				return true
			}
		}
		return false
	case "bool":
		_, ok := v.(bool)
		return ok
	case "number":
		f, ok := v.(float64)
		return ok && f >= k.Min && f <= k.Max
	case "int":
		i, ok := v.(int64)
		if ok && i == 0 && k.Zero {
			return true
		}
		return ok && float64(i) >= k.Min && float64(i) <= k.Max
	}
	return false
}

// Settables is every key the settings window writes, in the order the window's
// tabs reach them. A key absent from here is a key the route will not take, so
// adding a row to that window is adding a line here first.
var Settables = []Settable{
	{Name: "hotkey", Kind: "string", Check: ValidHotkey, Refusal: "invalid_hotkey",
		Because: "not a combination the shell can register"},
	{Name: "scope_app", Kind: "string", Check: ValidScope, Refusal: "invalid_scope",
		Because: "scope_app is bundle identifiers separated by commas"},
	{Name: "language", Kind: "string", Choices: Languages, Refusal: "invalid_language",
		Because: "auto, or a tag the catalog resolves"},
	{Name: "mascot", Kind: "string", Check: ValidName, Refusal: "invalid_mascot",
		Because: "a pack name: letters, digits, dot, dash or underscore"},
	{Name: "terminal", Kind: "string", Choices: []string{"auto", "iterm", "tmux"},
		Refusal: "invalid_terminal", Because: "auto, iterm or tmux"},
	{Name: "reopen_on_return", Kind: "bool"},
	{Name: "follow_target", Kind: "bool"},
	{Name: "codex_auto_name", Kind: "bool"},
	{Name: "auto_name_assistant", Kind: "string", Choices: []string{"claude", "codex", "auto"},
		Refusal: "invalid_assistant", Because: "claude, codex or auto"},
	{Name: "notch", Kind: "bool"},
	// The Swift app takes y_fraction strictly between 0.02 and 0.9; the bound
	// here is inclusive, so it is stated a step inside on both sides.
	{Name: "y_fraction", Kind: "number", Min: 0.021, Max: 0.899},
	{Name: "width", Kind: "number", Min: 360, Max: 1400},
	{Name: "card_opacity", Kind: "number", Min: 0, Max: 1},
	{Name: "output_mode", Kind: "string", Choices: []string{"auto", "transcript", "terminal"},
		Refusal: "invalid_output_mode", Because: "auto, transcript or terminal"},
	{Name: "output_height", Kind: "number", Min: 80, Max: 900},
	{Name: "output_size", Kind: "number", Min: 8, Max: 28},
	{Name: "output_font", Kind: "string", Check: ValidFont, Refusal: "invalid_font",
		Because: "a font family name"},
	{Name: "backdrop", Kind: "number", Min: 0, Max: 1},
	{Name: "output_newest_first", Kind: "bool"},
	{Name: "voice_engine", Kind: "string", Choices: []string{"auto", "apple", "whisper"},
		Refusal: "invalid_voice_engine", Because: "auto, apple or whisper"},
	// Open rather than a closed list: whisper reads more languages than the
	// window offers, and a hand-written `zh_TW` is a tag this file keeps.
	{Name: "voice_language", Kind: "string", Check: ValidLanguageTag, Refusal: "invalid_voice_language",
		Because: "auto, or a language tag such as zh-Hant, zh-Hans or en"},
	{Name: "voice_settle_seconds", Kind: "number", Min: 0, Max: 30},
	{Name: "voice_stop_seconds", Kind: "number", Min: 0, Max: 300},
	{Name: "remote", Kind: "bool"},
	{Name: "remote_write", Kind: "bool"},
	{Name: "remote_tunnel", Kind: "string", Choices: []string{"off", "quick", "named"},
		Refusal: "invalid_tunnel", Because: "off, quick or named"},
	{Name: "remote_hostname", Kind: "string", Check: ValidHostname, Refusal: "invalid_hostname",
		Because: "a hostname, or empty"},
	{Name: "push_on_delivery", Kind: "bool"},
	{Name: "push_on_fanout", Kind: "bool"},
	{Name: "smart_notifications", Kind: "bool"},
	{Name: "push_on_deploy", Kind: "bool"},
	{Name: "orchestrator_agent_notify", Kind: "bool"},
	{Name: "orchestrator_enabled", Kind: "bool"},
	{Name: "orchestrator_max_children", Kind: "int", Min: 1, Max: 10},
	{Name: "orchestrator_permission", Kind: "string", Choices: []string{"ask", "edits", "full"},
		Refusal: "invalid_permission", Because: "ask, edits or full"},
	{Name: "orchestrator_notify_root", Kind: "bool"},
	{Name: "orchestrator_child_linger", Kind: "int", Min: -1, Max: 3600},
	// The window Claude sessions this daemon opens compact at, in tokens; 0
	// is none, the default. The range is the broker's (orchestrator
	// compact.go), and a test holds the two to one another.
	{Name: "claude_auto_compact_window", Kind: "int", Min: 50_000, Max: 1_000_000, Zero: true,
		Refusal: "invalid_auto_compact_window",
		Because: "0 for none, or a whole number of tokens from 50000 to 1000000"},
}

// SettableByName finds one key, or false for a name this file does not set.
func SettableByName(name string) (Settable, bool) {
	for _, k := range Settables {
		if k.Name == name {
			return k, true
		}
	}
	return Settable{}, false
}

// Languages is `auto` plus the tags the Swift app's language popup offers, in
// its order (`Settings.swift` languagePopUp). This build ships one catalog;
// the key is still written so that a build with more reads it.
var Languages = []string{"auto", "en", "zh-Hant", "zh-Hans", "ja", "ko", "es", "pt", "fr", "de",
	"ru", "it", "hi", "id", "tr"}

// ValidLanguageTag reports whether s is `auto` or could be a language tag:
// letters first, then letters, digits, dashes and underscores, as BCP 47 and
// POSIX locales both spell them. What language it names is whisper's to know.
func ValidLanguageTag(s string) bool {
	if s == "" || len(s) > 35 {
		return false
	}
	for i, r := range s {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if i == 0 && !letter {
			return false
		}
		if !letter && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// ValidName reports whether s is a plain identifier — a mascot pack's name.
// Anything that could be a path, a control character or a surprise is refused:
// this value reaches a file name in the shell.
func ValidName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		ok := r == '.' || r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return s != "." && s != ".."
}

// ValidFont reports whether s could be a font family name. The list of faces on
// a machine is the shell's to know; this only refuses what no name is.
func ValidFont(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// ValidHostname reports whether s is a hostname, or empty for none.
func ValidHostname(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			ok := r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !ok {
				return false
			}
		}
	}
	return true
}
