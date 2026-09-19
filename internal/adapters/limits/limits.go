// Package limits reads what the providers last said about the plan windows of
// the accounts on this machine: Claude's five-hour and weekly windows from the
// status line's cache, Codex's from its rollouts.
//
// It is the Swift app's `AssistantQuota` (Sources/AssistantQuota.swift) and
// the window readers it shares with `SessionInfo` (Sources/SessionInfo.swift),
// ported rule for rule, because the status line under an open session draws
// `AssistantQuota.machineLimits` and two apps showing the same account must
// show the same numbers.
//
// A window is an account-level fact. Every session of one assistant on this
// machine shares it, so a reading is taken per assistant, not per session, and
// held for five seconds.
//
// Everything here is read-only. The files read are the status line's
// `rate-limits.json` and the tails of Codex rollouts; neither holds a token,
// and only the rate-limit keys of either are decoded.
package limits

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Window is one plan window.
type Window struct {
	// Name is `5h`, `7d`, or the window's length.
	Name string
	// UsedPercent is nil when the provider did not say.
	UsedPercent *float64
	// ResetsAt is Unix seconds, nil when the provider did not say.
	ResetsAt *int64
	// Hit means the window is spent.
	Hit bool
}

// Depleted is a Codex record that named no window at all (`SessionInfo.Limits.Depleted`).
// Both facts are optional because an absent field is "not established", never
// the more alarming of the values it could have had.
type Depleted struct {
	LimitID    *string
	HasCredits *bool
	At         *int64
}

// Limits is what is known about a plan, and when it was known.
// Empty Windows is "nobody said", never 0%.
type Limits struct {
	Windows []Window
	// At is Unix seconds of the record the windows came from.
	At       *int64
	Depleted *Depleted
}

// Settings are the three Swift app settings the reading depends on. The zero
// value is the Swift app's defaults.
type Settings struct {
	// StatusDir is `status_dir`: where the status line keeps its cache.
	// "" is ~/.claude/statusline-cache.
	StatusDir string
	// CodexHome is `codex_home`. "" falls back to $CODEX_HOME, then ~/.codex.
	CodexHome string
	// LowThreshold is `assistant_quota_low_threshold`; 0 is the default, 85.
	LowThreshold float64
}

// Availability is `AssistantQuota`'s four-valued answer.
type Availability string

const (
	OK        Availability = "ok"
	Low       Availability = "low"
	Exhausted Availability = "exhausted"
	Unknown   Availability = "unknown"
)

const (
	defaultLowThreshold = 85
	// cacheFor is `AssistantQuota.current`'s cache.
	cacheFor = 5 * time.Second
	// rolloutDays, rolloutScan, rolloutKeep and rolloutTail are
	// `recentRolloutResults`' bounds.
	rolloutDays = 2
	rolloutScan = 40
	rolloutKeep = 5
	rolloutTail = 256 * 1024
)

// ---------- reading one record ----------

// WindowName is `SessionInfo.windowName(minutes:)`.
func WindowName(minutes int64) string {
	switch {
	case minutes == 300:
		return "5h"
	case minutes == 10080:
		return "7d"
	case minutes%1440 == 0:
		return strconv.FormatInt(minutes/1440, 10) + "d"
	case minutes%60 == 0:
		return strconv.FormatInt(minutes/60, 10) + "h"
	}
	return strconv.FormatInt(minutes, 10) + "m"
}

func claudeWindowName(kind string) string {
	switch kind {
	case "five_hour":
		return "5h"
	case "seven_day":
		return "7d"
	}
	return kind
}

// ClaudeCache is `SessionInfo.claudeLimits(cache:now:)`: the windows the
// status line last wrote down, less every window whose reset has passed.
func ClaudeCache(data []byte, now time.Time) Limits {
	obj, ok := decodeObject(data)
	if !ok {
		return Limits{}
	}
	rates, ok := obj["rate_limits"].(map[string]any)
	if !ok {
		return Limits{}
	}
	out := fromRates(rates, intOf(obj["at"]))
	live := out.Windows[:0]
	for _, w := range out.Windows {
		if w.ResetsAt != nil && float64(*w.ResetsAt) <= epoch(now) {
			continue
		}
		live = append(live, w)
	}
	out.Windows = live
	return out
}

// fromRates is `SessionInfo.fromRates`: the status line's shape.
func fromRates(rates map[string]any, at *int64) Limits {
	out := Limits{At: at, Windows: []Window{}}
	for _, key := range []string{"five_hour", "seven_day"} {
		window, ok := rates[key].(map[string]any)
		if !ok {
			continue
		}
		used := doubleOf(window["used_percentage"])
		if used == nil {
			continue
		}
		out.Windows = append(out.Windows, Window{Name: claudeWindowName(key), UsedPercent: used,
			ResetsAt: intOf(window["resets_at"]), Hit: *used >= 100})
	}
	return out
}

// CodexRollout is `SessionInfo.codexLimits(rollout:)`: the newest
// `token_count` event that names a window, with the newest one that named
// none remembered as Depleted rather than returned.
func CodexRollout(data []byte) Limits {
	var depleted *Depleted
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(line) == 0 || !bytes.Contains(line, []byte("token_count")) ||
			!bytes.Contains(line, []byte("rate_limits")) {
			continue
		}
		obj, ok := decodeObject(line)
		if !ok {
			continue
		}
		payload, ok := obj["payload"].(map[string]any)
		if !ok || payload["type"] != "token_count" {
			continue
		}
		rates, ok := payload["rate_limits"].(map[string]any)
		if !ok {
			continue
		}
		out := Limits{At: stampOf(obj["timestamp"]), Windows: []Window{}}
		for _, key := range []string{"primary", "secondary"} {
			window, ok := rates[key].(map[string]any)
			if !ok {
				continue
			}
			used := doubleOf(window["used_percent"])
			if used == nil {
				continue
			}
			var minutes int64
			if m := intOf(window["window_minutes"]); m != nil {
				minutes = *m
			}
			out.Windows = append(out.Windows, Window{Name: WindowName(minutes), UsedPercent: used,
				ResetsAt: intOf(window["resets_at"]), Hit: *used >= 100})
		}
		if len(out.Windows) == 0 {
			if depleted == nil {
				d := &Depleted{At: out.At}
				if id, ok := rates["limit_id"].(string); ok {
					d.LimitID = &id
				}
				if credits, ok := rates["credits"].(map[string]any); ok {
					if has, ok := credits["has_credits"].(bool); ok {
						d.HasCredits = &has
					}
				}
				depleted = d
			}
			continue
		}
		out.Depleted = depleted
		return out
	}
	return Limits{Windows: []Window{}, Depleted: depleted}
}

// ---------- the four-valued answer ----------

// availabilityOf is `AssistantQuota.availability(from:now:lowThreshold:)`.
func availabilityOf(windows []Window, now time.Time, low float64) Availability {
	live := []Window{}
	for _, w := range windows {
		if w.ResetsAt == nil || float64(*w.ResetsAt) > epoch(now) {
			live = append(live, w)
		}
	}
	if len(live) == 0 {
		return Unknown
	}
	tightest := 0.0
	seen := false
	for _, w := range live {
		if w.Hit {
			return Exhausted
		}
		if w.UsedPercent != nil && (!seen || *w.UsedPercent > tightest) {
			tightest, seen = *w.UsedPercent, true
		}
	}
	switch {
	case tightest >= 100:
		return Exhausted
	case tightest >= low:
		return Low
	}
	return OK
}

// tightest is the first window with the highest used percentage (Swift's
// `max(by:)` keeps the first of equals), a missing percentage counting as 0.
func tightest(windows []Window) *Window {
	var best *Window
	for i := range windows {
		if best == nil || used(windows[i]) > used(*best) {
			best = &windows[i]
		}
	}
	return best
}

func used(w Window) float64 {
	if w.UsedPercent == nil {
		return 0
	}
	return *w.UsedPercent
}

// tightestResetsAt is `AssistantQuota.tightestWindowResetsAt`.
func tightestResetsAt(windows []Window) *int64 {
	if w := tightest(windows); w != nil {
		return w.ResetsAt
	}
	return nil
}

// codexCreditsDepleted is `AssistantQuota.codexCreditsDepleted`.
func codexCreditsDepleted(d *Depleted, lastNamedUsed *float64) bool {
	return d != nil && d.LimitID != nil && d.HasCredits != nil &&
		*d.LimitID != "codex" && !*d.HasCredits &&
		lastNamedUsed != nil && *lastNamedUsed >= 95
}

// staleAfter is `AssistantQuota.staleAfter(windowMinutes:)`: 5% of the
// window, clamped to fifteen minutes and six hours.
func staleAfter(windowMinutes int64) float64 {
	return math.Min(math.Max(float64(windowMinutes)*60*0.05, 15*60), 6*3600)
}

// minutesOf is `AssistantQuota.minutes(forWindowNamed:)`.
func minutesOf(name string) int64 {
	switch name {
	case "5h":
		return 300
	case "7d":
		return 10080
	}
	if len(name) > 1 {
		n, err := strconv.ParseInt(name[:len(name)-1], 10, 64)
		if err == nil {
			switch name[len(name)-1] {
			case 'd':
				return n * 1440
			case 'h':
				return n * 60
			case 'm':
				return n
			}
		}
	}
	return 10080
}

// quota is `AssistantQuota` less the two fields no reading here fills
// (`loggedIn`, `plan`: no identity probe runs, as in the Swift app).
type quota struct {
	availability Availability
	observedAt   *int64
	resetsAt     *int64
	windows      []Window
	// stale, lastKnown and detail are what decay says about the reading:
	// `low` past its line is stale, an `ok` past it is unknown with lastKnown
	// set, and detail is the sentence a client prints.
	stale     bool
	lastKnown Availability
	detail    string
}

// decayed is `AssistantQuota.decayed(_:now:)`. Where it writes a sentence of
// its own, that sentence is final; otherwise detail is left for the reader to
// fill from the windows.
func decayed(q quota, now time.Time) quota {
	if q.observedAt == nil {
		return q
	}
	age := epoch(now) - float64(*q.observedAt)
	window := int64(10080)
	if w := tightest(q.windows); w != nil {
		window = minutesOf(w.Name)
	}
	switch q.availability {
	case Exhausted:
		if q.resetsAt != nil && float64(*q.resetsAt) <= epoch(now) {
			q.availability = Unknown
			q.observedAt, q.resetsAt, q.windows = nil, nil, []Window{}
			q.stale, q.lastKnown = false, ""
			q.detail = "unknown; the window that was exhausted has since reset"
		}
	case Low:
		q.stale = age > staleAfter(window)
	case OK:
		if age > staleAfter(window) {
			q.lastKnown = OK
			q.availability = Unknown
			live := []Window{}
			for _, w := range q.windows {
				if w.ResetsAt != nil && float64(*w.ResetsAt) > epoch(now) {
					live = append(live, w)
				}
			}
			if len(live) == 0 {
				q.observedAt, q.resetsAt, q.windows = nil, nil, []Window{}
				q.stale = false
				q.detail = "unknown; last known ok"
			} else {
				// A provider window still open stays visible as a stale lower
				// bound.
				q.windows = live
				q.resetsAt = tightestResetsAt(live)
				q.stale = true
				q.detail = detailOf(live, Unknown, false, OK, now) + "; stale lower bound"
			}
		}
	}
	// `unknown` is already the floor.
	return q
}

// formatDuration is `AssistantQuota.formatDuration(seconds:)`.
func formatDuration(seconds float64) string {
	total := int64(math.Max(0, seconds))
	days, hours, minutes := total/86_400, (total%86_400)/3_600, (total%3_600)/60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	}
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%dm", minutes)
}

// detailOf is `AssistantQuota.detail(windows:availability:creditsExhausted:lastKnown:now:)`:
// one sentence a person, or a client with no UI of its own, prints as it
// stands.
func detailOf(windows []Window, availability Availability, credits bool, lastKnown Availability, now time.Time) string {
	if len(windows) == 0 {
		if lastKnown != "" {
			return "no fresh signal; last known " + string(lastKnown)
		}
		if credits {
			return "premium credits exhausted; no windows reported"
		}
		return "no signal yet"
	}
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		pct := "?"
		if w.UsedPercent != nil {
			pct = fmt.Sprintf("%.0f%%", *w.UsedPercent)
		}
		parts = append(parts, w.Name+" "+pct)
	}
	text := strings.Join(parts, ", ")
	if availability == Exhausted {
		if w := tightest(windows); w != nil && w.ResetsAt != nil && float64(*w.ResetsAt) > epoch(now) {
			text += "; resets in " + formatDuration(float64(*w.ResetsAt)-epoch(now))
		}
	}
	if credits {
		text += "; premium credits exhausted"
	}
	return text
}

// finished is a reading with its sentence: decay's own when it wrote one,
// otherwise the windows'. The Swift app's `claude()` and `codex()` both end
// this way.
func finished(q quota, credits bool, now time.Time) quota {
	q = decayed(q, now)
	if q.detail == "" {
		q.detail = detailOf(q.windows, q.availability, credits, q.lastKnown, now)
	}
	return q
}

// ---------- the machine reading ----------

// Reader takes the machine-level readings, held for five seconds each.
type Reader struct {
	home     string
	settings func() Settings

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	until time.Time
	quota Quota
}

// Quota is `AssistantQuota` as GET /v1/orchestrator/assistants sends it: what
// this Mac can say about one assistant's account. LoggedIn and Plan have no
// field because nothing fills them — no identity probe runs, as in the Swift
// app — and the route says null for both.
type Quota struct {
	Assistant    string
	Installed    bool
	Availability Availability
	// ObservedAt is the provider record's own time; nil exactly when nothing
	// usable has been seen.
	ObservedAt *int64
	ResetsAt   *int64
	Windows    []Window
	Stale      bool
	// LastKnown is what an Unknown was before it aged out of OK; "" on an
	// Unknown that is plain silence.
	LastKnown Availability
	Detail    string
}

// NewReader reads under home. settings is asked on every reading that is not
// cached; nil means the defaults.
func NewReader(home string, settings func() Settings) *Reader {
	if settings == nil {
		settings = func() Settings { return Settings{} }
	}
	return &Reader{home: home, settings: settings, cache: map[string]cached{}}
}

// Machine is `AssistantQuota.machineLimits(for:now:)`: the windows every
// session of this assistant shows, and when the provider wrote them. An
// assistant this package does not know has no windows.
func (r *Reader) Machine(assistant string, now time.Time) Limits {
	if assistant != "claude" && assistant != "codex" {
		return Limits{Windows: []Window{}}
	}
	q := r.Quota(assistant, now)
	return Limits{Windows: q.Windows, At: q.ObservedAt}
}

// Assistants is the order `AssistantQuota.all` answers in.
var Assistants = []string{"claude", "codex"}

// Quota is `AssistantQuota.current(for:now:)`: the whole machine-level
// reading of one assistant, file-only and held for five seconds. The status
// line's windows (Machine) are this reading's windows, so a session's `/info`
// and the assistants route can never show one account two ways.
func (r *Reader) Quota(assistant string, now time.Time) Quota {
	r.mu.Lock()
	if hit, ok := r.cache[assistant]; ok && hit.until.After(now) {
		r.mu.Unlock()
		return hit.quota
	}
	r.mu.Unlock()

	s := r.settings()
	if s.LowThreshold <= 0 || s.LowThreshold >= 100 {
		s.LowThreshold = defaultLowThreshold
	}
	var q quota
	var home string
	switch assistant {
	case "claude":
		q = r.claude(s, now)
		home = filepath.Join(r.home, ".claude")
	case "codex":
		q = r.codex(s, now)
		home = r.codexHome(s)
	default:
		return Quota{Assistant: assistant, Availability: Unknown, Windows: []Window{}, Detail: "no signal yet"}
	}
	out := Quota{
		Assistant:    assistant,
		Installed:    isDir(home),
		Availability: q.availability,
		ObservedAt:   q.observedAt,
		ResetsAt:     q.resetsAt,
		Windows:      q.windows,
		Stale:        q.stale,
		LastKnown:    q.lastKnown,
		Detail:       q.detail,
	}
	if out.Windows == nil {
		out.Windows = []Window{}
	}

	r.mu.Lock()
	r.cache[assistant] = cached{until: now.Add(cacheFor), quota: out}
	r.mu.Unlock()
	return out
}

// isDir is `Assistant.isInstalled`: the assistant's home is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// statusDir is `ProjectStatus.cacheDirectory`.
func (r *Reader) statusDir(s Settings) string {
	if dir := resolve(s.StatusDir, r.home); dir != "" {
		return dir
	}
	return filepath.Join(r.home, ".claude", "statusline-cache")
}

// codexHome is `Codex.home`.
func (r *Reader) codexHome(s Settings) string {
	if dir := resolve(s.CodexHome, r.home); dir != "" {
		return dir
	}
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return expand(env, r.home)
	}
	return filepath.Join(r.home, ".codex")
}

// claude is `AssistantQuota.claude(cacheDirectory:now:)`.
func (r *Reader) claude(s Settings, now time.Time) quota {
	var limits Limits
	if data, err := readSmall(filepath.Join(r.statusDir(s), "rate-limits.json")); err == nil {
		limits = ClaudeCache(data, now)
	}
	q := quota{
		availability: availabilityOf(limits.Windows, now, s.LowThreshold),
		observedAt:   limits.At,
		resetsAt:     tightestResetsAt(limits.Windows),
		windows:      limits.Windows,
	}
	return finished(q, false, now)
}

// codex is `AssistantQuota.codex(sessionsRoot:now:)`.
func (r *Reader) codex(s Settings, now time.Time) quota {
	results := recentRollouts(filepath.Join(r.codexHome(s), "sessions"))
	var named *Limits
	var depleted *Depleted
	for i := range results {
		l := &results[i]
		if len(l.Windows) > 0 && (named == nil || at(l.At) > at(named.At)) {
			named = l
		}
		if d := l.Depleted; d != nil && (depleted == nil || at(d.At) > at(depleted.At)) {
			depleted = d
		}
	}
	windows := []Window{}
	var namedAt *int64
	if named != nil {
		namedAt = named.At
		for _, w := range named.Windows {
			if w.ResetsAt == nil || float64(*w.ResetsAt) > epoch(now) {
				windows = append(windows, w)
			}
		}
	}
	var top *float64
	for _, w := range windows {
		if w.UsedPercent != nil && (top == nil || *w.UsedPercent > *top) {
			v := *w.UsedPercent
			top = &v
		}
	}
	var depletedAt *int64
	if depleted != nil {
		depletedAt = depleted.At
	}
	current := at(depletedAt) >= at(namedAt)
	credits := current && codexCreditsDepleted(depleted, top)
	availability := availabilityOf(windows, now, s.LowThreshold)
	observed := namedAt
	if credits {
		availability = Exhausted
		v := at(depletedAt)
		if n := at(namedAt); n > v {
			v = n
		}
		observed = &v
	}
	q := quota{availability: availability, observedAt: observed,
		resetsAt: tightestResetsAt(windows), windows: windows}
	return finished(q, credits, now)
}

func at(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// recentRollouts is `AssistantQuota.recentRolloutResults`: up to five of the
// most recently modified rollouts of the newest two days that said anything,
// looking at no more than forty.
func recentRollouts(root string) []Limits {
	type candidate struct {
		path  string
		mtime time.Time
	}
	var files []candidate
	for _, day := range dayFolders(root, rolloutDays) {
		entries, err := os.ReadDir(day)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			path := filepath.Join(day, name)
			var mtime time.Time
			if info, err := os.Stat(path); err == nil {
				mtime = info.ModTime()
			}
			files = append(files, candidate{path, mtime})
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })
	if len(files) > rolloutScan {
		files = files[:rolloutScan]
	}
	out := []Limits{}
	for _, f := range files {
		tail, err := tailOf(f.path, rolloutTail)
		if err != nil {
			continue
		}
		l := CodexRollout(tail)
		if len(l.Windows) == 0 && l.Depleted == nil {
			continue
		}
		out = append(out, l)
		if len(out) == rolloutKeep {
			break
		}
	}
	return out
}

// dayFolders is `Codex.dayFolders(under:limit:)`: the newest day folders of a
// YYYY/MM/DD tree, by name, looking in the newest two years and two months.
func dayFolders(base string, limit int) []string {
	out := []string{}
	for _, year := range children(base, 2) {
		for _, month := range children(year, 2) {
			out = append(out, children(month, -1)...)
			if len(out) >= limit {
				return out[:limit]
			}
		}
		if len(out) >= limit {
			return out[:limit]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// children is a directory's entries, hidden ones left out, newest name
// first; limit < 0 keeps them all.
func children(dir string, limit int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := []string{}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if limit >= 0 && len(names) > limit {
		names = names[:limit]
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(dir, n)
	}
	return out
}

// tailOf is `Transcript.tailData`: the last n bytes, less the line the cut
// went through.
func tailOf(path string, n int64) ([]byte, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if info.Size() > n {
		start = info.Size() - n
	}
	if _, err := fh.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(bufio.NewReader(io.LimitReader(fh, n)))
	if err != nil {
		return nil, err
	}
	if start > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return nil, nil
		}
		data = data[i+1:]
	}
	return data, nil
}

// readSmall reads a file of at most a megabyte; the cache is a few hundred
// bytes, and anything larger is not the file this reads.
func readSmall(path string) ([]byte, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return io.ReadAll(io.LimitReader(fh, 1<<20))
}

// ---------- values as JSONSerialization hands them over ----------

func decodeObject(data []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

// intOf is `SessionInfo.int`: an integer, a number truncated toward zero, or
// a string that spells an integer.
func intOf(v any) *int64 {
	switch x := v.(type) {
	case json.Number:
		if n, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return &n
		}
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < math.MinInt64 || f >= math.MaxInt64 {
			return nil
		}
		n := int64(f)
		return &n
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return &n
		}
	}
	return nil
}

// doubleOf is `SessionInfo.double`: a number, or a string that spells one.
func doubleOf(v any) *float64 {
	var s string
	switch x := v.(type) {
	case json.Number:
		s = string(x)
	case string:
		s = x
	default:
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

// stampOf is `SessionInfo.stamp`: an ISO 8601 instant, with or without a
// fraction, as whole Unix seconds.
func stampOf(v any) *int64 {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	n := t.Unix()
	return &n
}

func epoch(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}

// resolve is `Paths.resolve`: "" for an empty setting, `~` expanded.
func resolve(setting, home string) string {
	trimmed := strings.TrimSpace(setting)
	if trimmed == "" {
		return ""
	}
	return expand(trimmed, home)
}

func expand(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return home + path[1:]
	}
	return path
}
