package projects

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Place is StartPoints.Place: one directory a session can be started in.
type Place struct {
	ID    string
	Path  string
	Label string
	At    time.Time
}

// Labeler is the project registry's name for a directory, or "" when it has
// none. internal/domain/icon.Registry.Label is the production one.
type Labeler func(path string) string

// Places answers StartPoints.places(limit:).
//
// The expensive half — proving what a Claude Code project folder stands for —
// is remembered against the folder's stamp, exactly as the Swift app does, so
// one instance is meant to live as long as the daemon.
type Places struct {
	Label Labeler
	// Registered reads the durable directories a person explicitly added.
	// It is separate from Fixture: production registration is a source beside
	// provider history, while a fixture replaces every production source.
	Registered func() ([]RegisteredPlace, error)
	// Managed is ManagedWorktreeRoots: where a Clawdline broker makes its
	// children's checkouts. Nothing at or below one of them is offered as a
	// place, however it became known — a transcript folder, or a session
	// running in it now.
	Managed []string
	// Fixture is StartPoints.fixtureForTesting's `places`: when set, it is the
	// whole list, and nothing on this machine is read to build it. It exists so
	// the start route can be driven in a directory made for the purpose — which
	// lives under a temporary root, and the durable-place rule would otherwise
	// keep off the list — without offering any of the person's real projects to
	// a test that presses rows. The paths still have to be real directories.
	Fixture []string

	mu       sync.Mutex
	resolved map[string]resolvedFolder
	heads    map[string]codexHead
}

type resolvedFolder struct {
	stamp string
	path  string
}

// FixtureEnv names a JSON file holding an array of absolute directories; when
// it is set, NewPlaces answers with exactly those. Test use only.
const FixtureEnv = "CLAWDLINE_NEXT_PLACES_FIXTURE"

func NewPlaces(label Labeler, managed []string) *Places {
	return &Places{Label: label, Managed: managed, Fixture: fixtureFromEnv(),
		resolved: map[string]resolvedFolder{}, heads: map[string]codexHead{}}
}

// fixtureFromEnv reads FixtureEnv. A named file that cannot be read is an
// empty list rather than the real one: somebody asked for a fixture, and
// falling back to their projects is the thing the fixture is there to stop.
func fixtureFromEnv() []string {
	name := os.Getenv(FixtureEnv)
	if name == "" {
		return nil
	}
	out := []string{}
	data, err := os.ReadFile(name)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// label is StartPoints.label(for:).
func (p *Places) label(path string) string {
	if p.Label != nil {
		if l := p.Label(path); l != "" {
			return l
		}
	}
	return filepath.Base(path)
}

// List is StartPoints.places(limit:): recorded Claude Code folders, Codex
// rollouts and the live sessions' directories, tidied. `live` is the working
// directory of every assistant session this machine can see now.
func (p *Places) List(live []string, limit int) []Place {
	now := time.Now()
	if p.Fixture != nil {
		var out []Place
		for _, path := range p.Fixture {
			if usable(path) && isDirectory(path) && len(out) < limit {
				out = append(out, Place{ID: PlaceID(path), Path: path, Label: p.label(path), At: now})
			}
		}
		return out
	}
	all := p.recorded(60, 240)
	all = append(all, p.codexRecorded(60, 40)...)
	if p.Registered != nil {
		if rows, err := p.Registered(); err == nil {
			for _, row := range rows {
				all = append(all, Place{ID: PlaceID(row.Path), Path: row.Path,
					Label: p.label(row.Path), At: time.Unix(row.AddedAt, 0)})
			}
		}
	}
	for _, cwd := range live {
		if cwd == "" {
			continue
		}
		all = append(all, Place{ID: PlaceID(cwd), Path: cwd, Label: p.label(cwd), At: now})
	}
	return tidy(all, limit, isDirectory, durableJudge(p.Managed))
}

func isDirectory(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// usable is StartPoints.usable.
func usable(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// tidy is StartPoints.tidy: dedupe by path, drop what is not a durable
// directory any more, newest first, and cap.
func tidy(places []Place, limit int, isDir, durable func(string) bool) []Place {
	best := map[string]Place{}
	for _, place := range places {
		if !usable(place.Path) || !durable(place.Path) || !isDir(place.Path) {
			continue
		}
		if had, ok := best[place.Path]; ok && !had.At.Before(place.At) {
			continue
		}
		best[place.Path] = place
	}
	out := make([]Place, 0, len(best))
	for _, place := range best {
		out = append(out, place)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].Path < out[j].Path
		}
		return out[i].At.After(out[j].At)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// resolved is URL.standardizedFileURL.resolvingSymlinksInPath().path.
func resolvedPath(path string) string { return canonicalFilesystemPath(path) }

// scratchRoots is StartPoints.scratchRoots(home:temporary:), and then
// `managed`: the Swift app knew one broker, its own, and its list names that
// broker's root; this daemon's is wherever its state directory is.
func scratchRoots(home, temporary string, managed []string) []string {
	roots := []string{home + "/.claude", home + "/.codex", home + "/Documents/Codex",
		home + "/Library/Application Support/Clawdline/worktrees",
		temporary, "/tmp", "/private/tmp"}
	return append(roots, managed...)
}

// durableJudge is StartPoints.isDurablePlace, with its roots resolved once per
// listing.
//
// A root is compared a whole component at a time, both sides with symlinks
// followed: `<root>/x` is under it, and `<root>-old/x`, or a directory that
// merely shares the root's last name, is not.
func durableJudge(managed []string) func(string) bool {
	h := resolvedPath(home())
	var roots []string
	for _, root := range scratchRoots(h, os.TempDir(), managed) {
		if root == "" {
			continue
		}
		roots = append(roots, resolvedPath(root))
	}
	return func(path string) bool {
		path = resolvedPath(path)
		if path == h {
			return false
		}
		for _, root := range roots {
			if path == root || strings.HasPrefix(path, root+"/") {
				return false
			}
		}
		return true
	}
}

// Slug is TranscriptPaths.slug(of:): every UTF-16 unit that is not an ASCII
// letter or digit becomes a dash.
func Slug(path string) string {
	var b strings.Builder
	for _, r := range path {
		alnum := (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		switch {
		case alnum:
			b.WriteRune(r)
		case r > 0xFFFF:
			b.WriteString("--") // a surrogate pair is two units
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// scratchMarks is StartPoints.scratchMarks, over scratchRoots with `managed`.
//
// Every child session leaves a transcript folder named for its checkout, so
// without the managed roots here those folders would be scanned as plausible
// projects and use up the folder budget before the person's own were reached.
func scratchMarks(managed []string) []string {
	h := home()
	marks := map[string]bool{}
	for _, root := range scratchRoots(h, os.TempDir(), managed) {
		if root == "" {
			continue
		}
		clean := standardized(root)
		for _, spelling := range []string{clean, resolvedPath(clean)} {
			marks[Slug(spelling)] = true
			if strings.HasPrefix(spelling, "/private/") {
				marks[Slug(strings.TrimPrefix(spelling, "/private"))] = true
			} else if strings.HasPrefix(spelling, "/var/") || strings.HasPrefix(spelling, "/tmp/") || spelling == "/tmp" {
				marks[Slug("/private"+spelling)] = true
			}
		}
	}
	out := make([]string, 0, len(marks))
	for m := range marks {
		out = append(out, m)
	}
	return out
}

// couldBeDurable is StartPoints.couldBeDurable.
func couldBeDurable(name string, marks []string) bool {
	for _, m := range marks {
		if name == m || strings.HasPrefix(name, m+"-") {
			return false
		}
	}
	return true
}

func modified(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// stamp is Paths.stamp(of:).
func stamp(path string) string {
	st, err := os.Stat(resolvedPath(path))
	if err != nil {
		return "0-0"
	}
	return fmt.Sprintf("%d-%d", st.Size(), st.ModTime().Unix())
}

// recorded is StartPoints.recorded(root:folders:scan:).
func (p *Places) recorded(folders, scan int) []Place {
	base := filepath.Join(home(), ".claude", "projects")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	marks := scratchMarks(p.Managed)
	type dir struct {
		name      string
		path      string
		at        time.Time
		plausible bool
	}
	var dirs []dir
	for _, e := range entries {
		full := filepath.Join(base, e.Name())
		if !isDirectory(full) {
			continue
		}
		dirs = append(dirs, dir{e.Name(), full, modified(full), couldBeDurable(e.Name(), marks)})
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].plausible == dirs[j].plausible {
			return dirs[i].at.After(dirs[j].at)
		}
		return dirs[i].plausible
	})
	if len(dirs) > scan {
		dirs = dirs[:scan]
	}
	var out []Place
	for _, d := range dirs {
		if len(out) >= folders {
			break
		}
		names, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		type transcript struct {
			path string
			at   time.Time
		}
		var ts []transcript
		for _, n := range names {
			if strings.HasSuffix(n.Name(), ".jsonl") {
				full := filepath.Join(d.path, n.Name())
				ts = append(ts, transcript{full, modified(full)})
			}
		}
		if len(ts) == 0 {
			continue
		}
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].at.After(ts[j].at) })
		paths := make([]string, len(ts))
		for i, t := range ts {
			paths[i] = t.path
		}
		path, ok := p.cachedDirectory(d.name, d.path, paths)
		if !ok {
			continue
		}
		out = append(out, Place{ID: PlaceID(path), Path: path, Label: p.label(path), At: ts[0].at})
	}
	return out
}

func (p *Places) cachedDirectory(name, folder string, transcripts []string) (string, bool) {
	s := stamp(folder)
	p.mu.Lock()
	hit, ok := p.resolved[name]
	p.mu.Unlock()
	if ok && hit.stamp == s {
		return hit.path, hit.path != ""
	}
	found := directoryNamed(name, transcripts)
	p.mu.Lock()
	p.resolved[name] = resolvedFolder{stamp: s, path: found}
	p.mu.Unlock()
	return found, found != ""
}

// directoryNamed is StartPoints.directory(named:transcripts:): head first, then
// tail, over the newest four transcripts, every candidate checked against the
// folder's own name before it is believed.
func directoryNamed(name string, transcripts []string) string {
	if len(transcripts) > 4 {
		transcripts = transcripts[:4]
	}
	for _, t := range transcripts {
		for _, text := range []string{readHead(t, 256<<10), readTail(t, 64<<10)} {
			for _, candidate := range cwds(text) {
				if Slug(candidate) == name {
					return candidate
				}
			}
		}
	}
	return ""
}

func readHead(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, n)
	got, _ := io.ReadFull(f, buf)
	return string(buf[:got])
}

func readTail(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() <= int64(n) {
		return "" // the head already covered it
	}
	buf := make([]byte, n)
	got, _ := f.ReadAt(buf, st.Size()-int64(n))
	return string(buf[:got])
}

// cwds is StartPoints.cwds(in:): every `"cwd":"…"` value, read textually.
func cwds(text string) []string {
	const needle = `"cwd":"`
	var found []string
	for {
		at := strings.Index(text, needle)
		if at < 0 {
			return found
		}
		text = text[at+len(needle):]
		var raw strings.Builder
		closed := false
		i := 0
		for i < len(text) {
			c := text[i]
			if c == '\\' {
				if i+1 >= len(text) {
					break
				}
				raw.WriteByte(c)
				raw.WriteByte(text[i+1])
				i += 2
				continue
			}
			if c == '"' {
				closed = true
				break
			}
			// A record is one line; half a path is not a path.
			if c == '\n' || c == '\r' {
				break
			}
			raw.WriteByte(c)
			i++
		}
		if !closed {
			continue
		}
		var value string
		if json.Unmarshal([]byte(`"`+raw.String()+`"`), &value) != nil || value == "" {
			continue
		}
		found = append(found, value)
	}
}

// codexHead is Codex.Head, only the parts StartPoints reads.
type codexHead struct {
	cwd              string
	isUser           bool
	isInteractiveCLI bool
	ok               bool
}

// stringAfter is Codex.string(after:in:).
func stringAfter(needle, text string) (string, bool) {
	at := strings.Index(text, needle)
	if at < 0 {
		return "", false
	}
	rest := strings.TrimLeft(text[at+len(needle):], " ")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]
	end := strings.IndexAny(rest, "\"\n")
	if end < 0 {
		return rest, true
	}
	return rest[:end], true
}

// codexHeadOf is Codex.head(of:), cached by path when it parsed.
func (p *Places) codexHeadOf(path string) codexHead {
	p.mu.Lock()
	hit, ok := p.heads[path]
	p.mu.Unlock()
	if ok {
		return hit
	}
	text := readHead(path, 16<<10)
	found := codexHead{}
	if list := cwds(text); len(list) > 0 && list[0] != "" {
		thread, hasThread := stringAfter(`"thread_source":`, text)
		source, hasSource := stringAfter(`"source":`, text)
		found = codexHead{cwd: list[0], ok: true,
			isUser:           !hasThread || thread == "user",
			isInteractiveCLI: !hasSource || source == "cli"}
		p.mu.Lock()
		p.heads[path] = found
		p.mu.Unlock()
	}
	return found
}

// childDirs is Codex.children(of:): subdirectory names, newest name first.
func childDirs(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(path, n)
	}
	return out
}

// dayFolders is Codex.dayFolders(under:limit:).
func dayFolders(base string, limit int) []string {
	var out []string
	years := childDirs(base)
	if len(years) > 2 {
		years = years[:2]
	}
	for _, year := range years {
		months := childDirs(year)
		if len(months) > 2 {
			months = months[:2]
		}
		for _, month := range months {
			out = append(out, childDirs(month)...)
			if len(out) >= limit {
				return out[:limit]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// codexRecorded is StartPoints.codexRecorded over Codex.workedIn(days:limit:).
func (p *Places) codexRecorded(days, limit int) []Place {
	root := filepath.Join(home(), ".codex")
	var files []string
	for _, day := range dayFolders(filepath.Join(root, "sessions"), days) {
		entries, _ := os.ReadDir(day)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "rollout-") && strings.HasSuffix(e.Name(), ".jsonl") {
				files = append(files, filepath.Join(day, e.Name()))
			}
		}
	}
	archive := filepath.Join(root, "archived_sessions")
	if entries, err := os.ReadDir(archive); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl") {
				files = append(files, filepath.Join(archive, e.Name()))
			}
		}
	}
	best := map[string]time.Time{}
	for _, f := range files {
		h := p.codexHeadOf(f)
		if !h.ok || !h.isUser || !h.isInteractiveCLI {
			continue
		}
		at := modified(f)
		if had, ok := best[h.cwd]; ok && !had.Before(at) {
			continue
		}
		best[h.cwd] = at
	}
	type worked struct {
		path string
		at   time.Time
	}
	var list []worked
	for path, at := range best {
		list = append(list, worked{path, at})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].at.Equal(list[j].at) {
			return list[i].path < list[j].path
		}
		return list[i].at.After(list[j].at)
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]Place, 0, len(list))
	for _, w := range list {
		out = append(out, Place{ID: PlaceID(w.path), Path: w.path, Label: p.label(w.path), At: w.at})
	}
	return out
}
