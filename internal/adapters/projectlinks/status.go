// Package projectlinks answers "where can I go from here" for one working
// directory: the addresses a project already has, as the Swift app's
// `ProjectArtifact.linksPayload` gathers them.
//
// **Nothing here is invented.** Every row is a URL some other program already
// wrote into a file on this machine. The status files come from the terminal
// status line's cache (`~/.claude/statusline-cache/`, the Swift app's
// `ProjectStatus`), the server rows from a project's own `.devstack.json`, and
// the workflow file is named after the repository's GitHub remote, which is the
// one thing here that costs a subprocess. Clawdline's contribution is that they
// are one list on a phone rather than separate facts on a machine in another
// room.
//
// **Nothing here reaches the network.** A deploy row is a file the CI poller
// left behind, not an API call: what this daemon cannot learn from the local
// directory it does not claim to know.
package projectlinks

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// maxStatusBytes bounds one read of one status file (read). These files are a
// handful of keys written by another program; a larger one is not one of them,
// and it is refused rather than parsed.
const maxStatusBytes = 256 << 10

// maxHealthComponents bounds the component rows kept from one health file
// (page). A multi-surface receipt lists a site's parts; past this the rest are
// not drawn and the answer says it was cut.
const maxHealthComponents = 32

// maxLabelRunes bounds a label, a status word or a reason as a row draws them
// (page). These strings come from another program's file and are drawn in a
// single line of a sheet.
const maxLabelRunes = 120

// maxURLBytes bounds a URL taken from one of those files (page). Anything
// longer is not an address somebody follows from a list.
const maxURLBytes = 2048

// The states each reader knows. A state outside its set draws **nothing**,
// which is the whole reason these sets exist: `{"state":"none"}` is what a
// producer with nothing to say writes, and of the fifteen workflow files on the
// Swift app's machine twelve were exactly that (`ProjectStatus.swift`), so a
// reader that believed every state drew twelve red crosses for twelve projects
// with no CI at all. A red mark that is always wrong is worse than no mark.
var (
	deployStates = map[string]bool{"running": true, "ok": true, "fail": true}
	runStates    = map[string]bool{"running": true, "ok": true, "fail": true}
	// Two vocabularies, because two producers write these files: the status
	// line's `ok | sick | offline` and the multi-surface receipt's
	// `online | not_deployed | unhealthy | unreachable`. `none` and `unknown`
	// are out of both: they mean *nothing to say*, and a dot for "not checked
	// yet" is the always-wrong red mark wearing another word.
	healthStates = map[string]bool{
		"ok": true, "sick": true, "offline": true,
		"online": true, "not_deployed": true, "unhealthy": true, "unreachable": true,
	}
)

// defaultStaleAfter is what a `running` row that does not say means: long
// enough for a slow full suite, short enough that a killed run is gone before
// anybody trusts its bar again. The ceiling lives in the reader on purpose —
// this daemon has no poller to tidy up after a producer, so a `kill -9`'d run
// would otherwise spin in the bar for ever, and every reader gets the ceiling
// including the ones nobody has written yet.
const defaultStaleAfter = 900.0

// Deploy is a workflow run, from `ghrun-<owner>-<repo>.json`.
type Deploy struct {
	Label          string
	State          string // running | ok | fail
	StartedAt      float64
	TypicalSeconds float64
	URL            string
}

// Run is a test or a build on this machine, from `run-<directory key>.json`.
//
// Keyed by working directory rather than by remote, as the Swift app keys it:
// one run belongs to one tree, and a machine routinely has several worktrees of
// one repository running at once, which a remote cannot tell apart.
type Run struct {
	Label          string
	State          string // running | ok | fail
	Phase          string // producer text, drawn in place of the percentage
	StartedAt      float64
	TypicalSeconds float64
	UpdatedAt      float64
	StaleAfter     float64
}

// Fresh is the one rule this file has that the workflow file does not. A
// finished row is never stale: `ok` and `fail` are a verdict, not a claim about
// something still moving.
func (r Run) Fresh(now float64) bool {
	if r.State != "running" {
		return true
	}
	return now-r.UpdatedAt <= r.StaleAfter
}

// Health is one check, from `health-<directory key>.json`.
type Health struct {
	Label  string
	State  string
	URL    string
	Kind   string
	Reason string
}

// VisualState is the dot's colour in the one vocabulary both surfaces read.
// The receipt's own word stays in `status`; this is only the colour.
func (h Health) VisualState() string {
	switch h.State {
	case "online":
		return "ok"
	case "not_deployed":
		return "down"
	case "unhealthy", "unreachable":
		return "fail"
	}
	return h.State
}

// Status is what one directory's status files say.
type Status struct {
	Deploy *Deploy
	Run    *Run
	// Components are the lossless rows of a multi-surface receipt. Older
	// receipts have none, and then Health is the one overall check.
	Health     *Health
	Components []Health
	// Truncated says a health file listed more components than one answer
	// carries.
	Truncated bool
}

// DirectoryKey is `ProjectStatus.key(forPath:)`: the status line names its
// per-project files by path with the separators turned into dashes, the same
// shape Claude Code uses for its own project folders.
func DirectoryKey(path string) string {
	return strings.ReplaceAll(path, "/", "-")
}

// GitHubRepo is `Project.githubRepo`: `owner-repo` out of a remote URL, in
// either of the two forms git hands back, and that is the name the workflow
// file carries. Empty when the remote is not GitHub, which is a fourth kind of
// nothing and is reported as itself rather than as an absent remote.
func GitHubRepo(url string) string {
	url = strings.TrimSpace(url)
	if !strings.Contains(url, "github.com") {
		return ""
	}
	tail := url
	if i := strings.Index(tail, "github.com"); i >= 0 {
		tail = tail[i+len("github.com"):]
	}
	tail = strings.Trim(tail, ":/")
	tail = strings.TrimSuffix(tail, ".git")
	parts := strings.Split(tail, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "-" + parts[1]
}

// ReadStatus reads one directory's status files out of dir.
//
// Absent is the normal case, and every field is optional on the way in: these
// files belong to another program and their shape can change, so a reader that
// threw a whole row away because one key moved would be worse than one that
// shows the parts it still recognises.
func ReadStatus(dir, cwd, repo string, now float64) Status {
	var out Status
	key := DirectoryKey(cwd)
	if repo != "" {
		out.Deploy = parseDeploy(readJSON(filepath.Join(dir, "ghrun-"+repo+".json")))
	}
	out.Run = parseRun(readJSON(filepath.Join(dir, "run-"+key+".json")), now)
	health := readJSON(filepath.Join(dir, "health-"+key+".json"))
	out.Health = parseHealth(health, health)
	out.Components, out.Truncated = parseComponents(health)
	return out
}

// readJSON is one bounded read of one object. A file larger than
// maxStatusBytes, or one that is not an object, is nothing rather than half of
// something.
func readJSON(path string) map[string]any {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxStatusBytes {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStatusBytes+1))
	if err != nil || len(data) > maxStatusBytes {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal(data, &obj) != nil {
		return nil
	}
	return obj
}

func str(row map[string]any, key string) string {
	v, _ := row[key].(string)
	return v
}

func num(row map[string]any, key string) (float64, bool) {
	v, ok := row[key].(float64)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func parseDeploy(row map[string]any) *Deploy {
	if row == nil {
		return nil
	}
	state := str(row, "state")
	if !deployStates[state] {
		return nil
	}
	started, _ := num(row, "started_at")
	typical, _ := num(row, "typical_seconds")
	return &Deploy{
		Label:          label(str(row, "label"), "deploy"),
		State:          state,
		StartedAt:      started,
		TypicalSeconds: typical,
		URL:            address(str(row, "url")),
	}
}

func parseRun(row map[string]any, now float64) *Run {
	if row == nil {
		return nil
	}
	state := str(row, "state")
	if !runStates[state] {
		return nil
	}
	// `updated_at` is required of a `running` row and a malformed value is an
	// absent one: it is what the liveness ceiling is measured against, and a
	// row that says it is running and will not say when is malformed rather
	// than merely thin. Malformed is drawn as nothing.
	updated, hasUpdated := num(row, "updated_at")
	if state == "running" && !hasUpdated {
		return nil
	}
	started, _ := num(row, "started_at")
	typical, _ := num(row, "typical_seconds")
	stale, ok := num(row, "stale_after")
	if !ok || stale <= 0 {
		stale = defaultStaleAfter
	}
	parsed := &Run{
		Label:          label(str(row, "label"), "run"),
		State:          state,
		Phase:          oneLine(strings.TrimSpace(str(row, "phase"))),
		StartedAt:      started,
		TypicalSeconds: typical,
		UpdatedAt:      updated,
		StaleAfter:     stale,
	}
	if !parsed.Fresh(now) {
		return nil
	}
	return parsed
}

// parseHealth takes its label and url from the file, or from the registry row
// beside it: the endpoint is a fact about the project, the result is a fact
// about this minute, and they live in different files.
func parseHealth(row, registry map[string]any) *Health {
	if row == nil {
		return nil
	}
	state := str(row, "state")
	if !healthStates[state] {
		return nil
	}
	url := str(row, "url")
	if url == "" && registry != nil {
		url = str(registry, "url")
	}
	name := str(row, "label")
	if name == "" && registry != nil {
		name = str(registry, "label")
	}
	return &Health{
		Label:  label(name, "health"),
		State:  state,
		URL:    address(url),
		Kind:   oneLine(str(row, "kind")),
		Reason: oneLine(strings.ReplaceAll(str(row, "reason"), "_", " ")),
	}
}

func parseComponents(row map[string]any) ([]Health, bool) {
	if row == nil {
		return nil, false
	}
	rows, _ := row["components"].([]any)
	var out []Health
	for _, raw := range rows {
		component, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Its own row is its own registry: a component carries the endpoint
		// and the result together.
		if h := parseHealth(component, component); h != nil {
			out = append(out, *h)
			if len(out) == maxHealthComponents {
				return out, len(rows) > maxHealthComponents
			}
		}
	}
	return out, false
}

// label is a drawn name: a producer's word, or the reader's own when the file
// has none.
func label(given, fallback string) string {
	given = oneLine(strings.TrimSpace(given))
	if given == "" {
		return fallback
	}
	return given
}

// address is a URL as a row carries it: nothing longer than maxURLBytes, and
// no control characters, which is what keeps a file's string from ending up in
// an attribute it was not meant to leave.
func address(url string) string {
	url = strings.TrimSpace(url)
	if len(url) > maxURLBytes {
		return ""
	}
	if strings.ContainsFunc(url, unicode.IsControl) {
		return ""
	}
	return url
}

// oneLine is a producer's string as a row draws it: no control characters, cut
// to maxLabelRunes.
func oneLine(s string) string {
	mapped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	runes := []rune(strings.Join(strings.Fields(mapped), " "))
	if len(runes) > maxLabelRunes {
		runes = runes[:maxLabelRunes]
	}
	return string(runes)
}
