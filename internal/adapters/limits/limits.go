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
// Nothing here runs an assistant, asks a provider anything, or keeps a beat of
// its own: a reading is taken when somebody asks for one, out of files the
// providers were going to write anyway. Spending quota to find out how much
// quota is left would be the one cost this package must never have.
//
// Everything here is read-only. The files read are the status line's
// `rate-limits.json` and the tails of Codex rollouts; neither holds a token,
// and only the rate-limit keys of either are decoded.
//
// # What `unknown` means
//
// `unknown` on its own has been four different facts wearing one word, and a
// person choosing whom to dispatch to needs to tell them apart: a provider
// that has written nothing here (Codex before its first run on this machine),
// a record that names no window, a record that exists and could not be read,
// and a reading that has aged out. Every unknown carries a Reason saying
// which, and a sentence naming the file it rests on.
//
// # How old is too old
//
// A reading is good for five percent of its shortest live window, never under
// fifteen minutes and never over six hours — the Swift app's line, measured
// against the shortest window rather than the tightest one. The window that
// moves fastest is the one whose percentage goes wrong first: a reading that
// is still honest about a week is not therefore honest about five hours. Past
// that line the reading is marked stale whatever it says, and an `ok` becomes
// `unknown` with what it last was. Every reading carries its own age, so a
// percentage from three hours ago is never drawn as a number from now.
package limits

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
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
	// Stale, FreshFor, Reason and Detail are what the machine reading knows
	// about this reading's age and its silence. The record decoders above
	// leave them zero: a decoded record has no age until somebody says when
	// it was read against. Reader.Machine fills them.
	Stale    bool
	FreshFor int64
	Reason   Reason
	Detail   string
}

// Reason is which kind of nothing an Unknown is. One word for four facts is
// what made a quota reading unusable: a root deciding whom to dispatch to
// cannot tell "Codex has never run here" from "the file is there and I could
// not read it", and the two want opposite actions.
type Reason string

const (
	// NoRecord: nothing the reader looks at exists. The provider has not
	// written here — Codex before its first run on this machine writes no
	// rollout at all, and dispatching to it once is what produces a signal.
	NoRecord Reason = "no_record"
	// NoReading: a record is there and names no window with a usable
	// percentage, or every window it names has since reset.
	NoReading Reason = "no_reading"
	// Unreadable: a record is there and this reader could not turn it into
	// one — permission, a broken file, a half-written line.
	Unreadable Reason = "unreadable"
	// TooOld: there was a reading and it is past the age it was good for.
	TooOld Reason = "too_old"
)

// found is what the files said besides the windows: which kind of silence a
// reading with no window was, and one clause naming where the answer rests,
// with the home directory written `~`.
type found struct {
	reason Reason
	note   string
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
	// cacheFor is `AssistantQuota.current`'s cache: the window in which two
	// callers asking at once — the assistants route and every open session's
	// `/info` — are one reading of the files rather than one each. It is not
	// a refresh rhythm; nothing wakes up to fill it.
	cacheFor = 5 * time.Second
	// rolloutDays, rolloutScan, rolloutKeep and rolloutTail are
	// `recentRolloutResults`' bounds: the newest two day folders, at most
	// forty files looked at, the first five that said anything kept, and the
	// last 256 KiB of each. They bound how much of a directory this daemon
	// does not own one reading will read.
	rolloutDays      = 2
	rolloutScanLimit = 40
	rolloutKeep      = 5
	rolloutTailLimit = 256 * 1024
	// rateLimitsReadLimit is the most of the status line's cache one reading
	// reads. The file is a few hundred bytes; anything larger is not it.
	rateLimitsReadLimit = 1 << 20
)

// quotaCacheLimit is how many readings are held at once: the capacity
// register's `cache.assistant_quota` (docs/limits.md N18). One per assistant,
// with room for more; past it the reading handed out longest ago is let go,
// and a miss reads the files again.
var quotaCacheLimit = capacity.Default(capacity.CacheAssistantQuota)

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
	out, _ := claudeRecord(data, now, "the status line's cache")
	return out
}

// claudeRecord is ClaudeCache saying, when it answers no window, which kind of
// nothing that was. where names the file for the sentence a person reads.
func claudeRecord(data []byte, now time.Time, where string) (Limits, found) {
	obj, ok := decodeObject(data)
	if !ok {
		return Limits{}, found{Unreadable, where + " is not a JSON object"}
	}
	rates, ok := obj["rate_limits"].(map[string]any)
	if !ok {
		return Limits{}, found{NoReading, where + " holds no rate_limits"}
	}
	out := fromRates(rates, intOf(obj["at"]))
	written := len(out.Windows)
	live := out.Windows[:0]
	for _, w := range out.Windows {
		if w.ResetsAt != nil && float64(*w.ResetsAt) <= epoch(now) {
			continue
		}
		live = append(live, w)
	}
	out.Windows = live
	switch {
	case len(live) > 0:
		return out, found{}
	case written > 0:
		// A record, and every window in it belongs to a period that has
		// ended. That is not the same silence as a record that never named
		// one, and saying so is the difference between "wait" and "look".
		return out, found{NoReading, "every window in " + where + " has reset since it was written"}
	}
	return out, found{NoReading, where + " names no window with a used percentage"}
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
//
// Five percent because a used percentage can only move as fast as its window
// is spent, so five percent of the window is about the most a reading can
// drift before the number on screen is wrong by more than it measures. The
// floor keeps a five-hour window from flapping on a reading a minute old; the
// ceiling stops a weekly window being trusted for a quarter of a day.
func staleAfter(windowMinutes int64) float64 {
	return math.Min(math.Max(float64(windowMinutes)*60*0.05, 15*60), 6*3600)
}

// shortest is the window with the fewest minutes, the first of equals.
func shortest(windows []Window) *Window {
	var best *Window
	for i := range windows {
		if best == nil || minutesOf(windows[i].Name) < minutesOf(best.Name) {
			best = &windows[i]
		}
	}
	return best
}

// freshFor is how many seconds a reading of these windows is good for: the
// staleness line of its *shortest* window, not its tightest.
//
// The shortest window sets it because that is the one that moves fastest: an
// account at 19% of five hours and 62% of a week is read against the five-hour
// line, since a reading that is still honest about a week is not therefore
// honest about five hours. With no window at all there is nothing to age and
// the weekly line is the floor, as it is in the Swift app.
func freshFor(windows []Window) int64 {
	minutes := int64(10080)
	if w := shortest(windows); w != nil {
		minutes = minutesOf(w.Name)
	}
	return int64(staleAfter(minutes))
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
	// past its line a reading is stale whatever it says, an `ok` past it is
	// unknown with lastKnown set, and detail is the sentence a client prints.
	stale     bool
	lastKnown Availability
	detail    string
	// freshFor is the line stale was decided against, in seconds.
	freshFor int64
	// reason and note are why there is no reading: which of the four kinds of
	// nothing, and the clause naming the file it was looked for in.
	reason Reason
	note   string
}

// decayed is `AssistantQuota.decayed(_:now:)`. Where it writes a sentence of
// its own, that sentence is final; otherwise detail is left for the reader to
// fill from the windows.
//
// Past its line a reading is marked stale whatever it says — an `exhausted`
// from eight hours ago is still the best answer there is, and it is still an
// answer from eight hours ago. Only an `ok` is taken away, because "fine" is
// the one verdict that an old reading turns into a lie.
func decayed(q quota, now time.Time) quota {
	if q.observedAt == nil {
		// Nothing was read, so nothing can age: the reason already says which
		// kind of nothing this is, and there is no line to quote.
		q.freshFor = 0
		return q
	}
	// The line is taken from the windows as read, before this function drops
	// any: it is the line this reading was measured against.
	q.freshFor = freshFor(q.windows)
	age := epoch(now) - float64(*q.observedAt)
	old := age > float64(q.freshFor)
	// Past the line the reading is marked, whatever it says. Only the
	// branches below that leave no reading on screen unmark it, because there
	// is then nothing for `stale` to qualify.
	q.stale = old
	switch q.availability {
	case Exhausted:
		if q.resetsAt != nil && float64(*q.resetsAt) <= epoch(now) {
			q.availability, q.reason = Unknown, TooOld
			q.observedAt, q.resetsAt, q.windows = nil, nil, []Window{}
			q.stale, q.lastKnown, q.freshFor = false, "", 0
			q.detail = "unknown: the window that was exhausted has since reset"
			return q
		}
	case OK:
		if old {
			q.lastKnown = OK
			q.availability, q.reason = Unknown, TooOld
			live := []Window{}
			for _, w := range q.windows {
				if w.ResetsAt != nil && float64(*w.ResetsAt) > epoch(now) {
					live = append(live, w)
				}
			}
			if len(live) == 0 {
				q.observedAt, q.resetsAt, q.windows = nil, nil, []Window{}
				q.stale = false
				q.detail = "unknown: the last reading was taken " + ageText(age) +
					" ago, past the " + formatDuration(float64(q.freshFor)) +
					" it was good for; last known ok"
				q.freshFor = 0
			} else {
				// A provider window still open stays visible as a stale lower
				// bound.
				q.windows = live
				q.resetsAt = tightestResetsAt(live)
				q.stale = true
				q.detail = detailOf(q, false, now) + "; unknown now, a stale lower bound"
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

// ageText is how old a reading is in the shortest form that is not a lie.
// formatDuration is the Swift app's and rounds anything under a minute up to
// one; a reading taken nine seconds ago is not a minute old.
func ageText(seconds float64) string {
	if seconds < 60 {
		return strconv.FormatInt(int64(math.Max(0, seconds)), 10) + "s"
	}
	return formatDuration(seconds)
}

// read is the clause that gives a reading its age, and names the line when it
// is past it. Every sentence that prints a percentage ends with one: a number
// on screen with no age beside it reads as a number from now.
func read(q quota, now time.Time) string {
	if q.observedAt == nil {
		return "never read"
	}
	out := "read " + ageText(epoch(now)-float64(*q.observedAt)) + " ago"
	if q.stale {
		out += ", past the " + formatDuration(float64(q.freshFor)) + " it was good for"
	}
	return out
}

// silence is the sentence for a reading with no window: which of the four
// kinds of nothing it is, and where a person looks next. Each reads
// differently on purpose — one word for four facts is what this replaces.
func silence(q quota, credits bool, now time.Time) string {
	var text string
	switch {
	case credits && q.availability != Unknown:
		text = "premium credits exhausted; no windows reported"
	case q.reason == NoRecord:
		text = "unknown: nothing has been written here to read"
	case q.reason == NoReading:
		text = "unknown: a record is there and names no window"
	case q.reason == Unreadable:
		text = "unknown: a record is there and could not be read"
	case q.reason == TooOld:
		text = "unknown: the last reading is past the age it was good for"
	default:
		text = "unknown: no signal yet"
	}
	if q.note != "" {
		text += "; " + q.note
	}
	if q.lastKnown != "" {
		text += "; last known " + string(q.lastKnown)
	}
	if credits && q.availability == Unknown {
		text += "; premium credits exhausted"
	}
	if q.observedAt != nil {
		text += "; " + read(q, now)
	}
	return text
}

// detailOf is `AssistantQuota.detail(windows:availability:creditsExhausted:lastKnown:now:)`:
// one sentence a person, or a client with no UI of its own, prints as it
// stands. Every sentence ends with how old the reading behind it is.
func detailOf(q quota, credits bool, now time.Time) string {
	if len(q.windows) == 0 {
		return silence(q, credits, now)
	}
	parts := make([]string, 0, len(q.windows))
	for _, w := range q.windows {
		pct := "?"
		if w.UsedPercent != nil {
			pct = fmt.Sprintf("%.0f%%", *w.UsedPercent)
		}
		parts = append(parts, w.Name+" "+pct)
	}
	text := strings.Join(parts, ", ")
	if q.availability == Exhausted {
		if w := tightest(q.windows); w != nil && w.ResetsAt != nil && float64(*w.ResetsAt) > epoch(now) {
			text += "; resets in " + formatDuration(float64(*w.ResetsAt)-epoch(now))
		}
	}
	if credits {
		text += "; premium credits exhausted"
	}
	return text + "; " + read(q, now)
}

// finished is a reading with its age and its sentence: decay's own when it
// wrote one, otherwise the windows'. The Swift app's `claude()` and `codex()`
// both end this way.
func finished(q quota, credits bool, now time.Time) quota {
	q = decayed(q, now)
	if q.detail == "" {
		q.detail = detailOf(q, credits, now)
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
	// evicted and lastEvicted are the capacity register's counters for
	// cache.assistant_quota.
	evicted     int64
	lastEvicted time.Time
}

type cached struct {
	until time.Time
	// used is when this reading was last handed out, so the one let go at the
	// limit is the one nobody has asked for in longest.
	used  time.Time
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
	// Stale is the reading being past the age it was good for. It is set
	// whatever the availability: an `exhausted` from eight hours ago is
	// still the best answer there is, and still an answer from eight hours
	// ago.
	Stale bool
	// FreshFor is how long this reading was good for, in seconds: the line
	// Stale was decided against. Zero when there is no reading to age.
	FreshFor int64
	// LastKnown is what an Unknown was before it aged out of OK; "" on an
	// Unknown that is plain silence.
	LastKnown Availability
	// Reason is which of the four kinds of nothing an Unknown is, and "" on
	// every other availability.
	Reason Reason
	Detail string
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
// session of this assistant shows, when the provider wrote them, and what is
// known about that reading's age. An assistant this package does not know has
// no windows.
//
// It is the same reading the assistants route answers with, so a session's
// `/info` and that route can never show one account two ways — including how
// old the numbers are.
func (r *Reader) Machine(assistant string, now time.Time) Limits {
	if assistant != "claude" && assistant != "codex" {
		return Limits{Windows: []Window{}}
	}
	q := r.Quota(assistant, now)
	return Limits{Windows: q.Windows, At: q.ObservedAt, Stale: q.Stale,
		FreshFor: q.FreshFor, Reason: q.Reason, Detail: q.Detail}
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
		hit.used = now
		r.cache[assistant] = hit
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
		return Quota{Assistant: assistant, Availability: Unknown, Windows: []Window{},
			Reason: NoRecord, Detail: "unknown: nothing has been written here to read; " +
				assistant + " is not an assistant this machine reads windows for"}
	}
	out := Quota{
		Assistant:    assistant,
		Installed:    isDir(home),
		Availability: q.availability,
		ObservedAt:   q.observedAt,
		ResetsAt:     q.resetsAt,
		Windows:      q.windows,
		Stale:        q.stale,
		FreshFor:     q.freshFor,
		LastKnown:    q.lastKnown,
		Reason:       q.reason,
		Detail:       q.detail,
	}
	if out.Windows == nil {
		out.Windows = []Window{}
	}
	if out.Availability != Unknown {
		// A reason is an answer to "why is there no reading", and there is
		// one.
		out.Reason = ""
	}

	r.mu.Lock()
	r.hold(assistant, cached{until: now.Add(cacheFor), used: now, quota: out})
	r.mu.Unlock()
	return out
}

// hold puts a reading in the cache, making room first. The caller holds the
// lock. Past the limit the reading handed out longest ago is let go; a miss
// reads the files again, which is why this row is a cache and not evidence.
func (r *Reader) hold(assistant string, entry cached) {
	if _, replacing := r.cache[assistant]; !replacing {
		for int64(len(r.cache)) >= quotaCacheLimit && quotaCacheLimit > 0 {
			oldest := ""
			for name, held := range r.cache {
				if oldest == "" || held.used.Before(r.cache[oldest].used) {
					oldest = name
				}
			}
			delete(r.cache, oldest)
			r.evicted++
			r.lastEvicted = time.Now()
		}
	}
	r.cache[assistant] = entry
}

// Reading is the capacity register's `cache.assistant_quota` row: the readings
// held now, and how many have been let go since this process started. It
// measures the map and reads no file.
func (r *Reader) Reading() capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	return capacity.Reading{Known: true, Used: int64(len(r.cache)),
		WindowSeconds: int64(cacheFor / time.Second),
		Counters:      capacity.Counters{Evicted: r.evicted, LastActionAt: r.lastEvicted}}
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

// claude is `AssistantQuota.claude(cacheDirectory:now:)`, saying which kind of
// nothing it found when it found none.
func (r *Reader) claude(s Settings, now time.Time) quota {
	path := filepath.Join(r.statusDir(s), "rate-limits.json")
	where := r.tilde(path)
	var limits Limits
	var f found
	data, err := readSmall(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f = found{NoRecord, where + " is not there; Claude Code's status line writes it while a session runs"}
	case err != nil:
		f = found{Unreadable, where + " could not be read: " + why(err)}
	default:
		limits, f = claudeRecord(data, now, where)
	}
	q := quota{
		availability: availabilityOf(limits.Windows, now, s.LowThreshold),
		observedAt:   limits.At,
		resetsAt:     tightestResetsAt(limits.Windows),
		windows:      limits.Windows,
		reason:       f.reason,
		note:         f.note,
	}
	return finished(q, false, now)
}

// why is an error without the path it already said: `os.Open` answers a
// PathError whose text repeats the file the sentence around it has just named.
func why(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Err != nil {
		return pe.Err.Error()
	}
	return err.Error()
}

// tilde writes a path the way a person would say it, with the home directory
// as `~`. Every sentence a reading produces names a file, and a home
// directory spelled out is both noise and somebody's name.
func (r *Reader) tilde(path string) string {
	if r.home != "" && strings.HasPrefix(path, r.home+string(filepath.Separator)) {
		return "~" + path[len(r.home):]
	}
	return path
}

// codex is `AssistantQuota.codex(sessionsRoot:now:)`, saying which kind of
// nothing it found when it found none.
func (r *Reader) codex(s Settings, now time.Time) quota {
	root := filepath.Join(r.codexHome(s), "sessions")
	results, f := recentRollouts(root, r.tilde(root))
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
	if availability == Unknown && f.reason == "" {
		// Rollouts were read and every window they name belongs to a period
		// that has ended.
		f = found{NoReading, "every window the newest Codex rollout names has reset since it was written"}
	}
	q := quota{availability: availability, observedAt: observed,
		resetsAt: tightestResetsAt(windows), windows: windows,
		reason: f.reason, note: f.note}
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
// looking at no more than forty. It also answers which kind of nothing it
// found — the difference between "Codex has never run here", "it ran and the
// turn was never answered", and "the files are there and I could not read
// them", which a caller cannot recover from an empty slice.
func recentRollouts(root, where string) ([]Limits, found) {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, found{NoRecord, where + " is not there; Codex writes a rollout there the first time it runs"}
		}
		return nil, found{Unreadable, where + " could not be read: " + why(err)}
	}
	type candidate struct {
		path  string
		mtime time.Time
	}
	var files []candidate
	var dirErr error
	for _, day := range dayFolders(root, rolloutDays) {
		entries, err := os.ReadDir(day)
		if err != nil {
			if dirErr == nil {
				dirErr = err
			}
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
	if len(files) == 0 {
		if dirErr != nil {
			return nil, found{Unreadable, "the day folders under " + where + " could not be read: " + why(dirErr)}
		}
		return nil, found{NoRecord, "no Codex rollout in the newest " +
			strconv.Itoa(rolloutDays) + " day folders under " + where}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })
	if len(files) > rolloutScanLimit {
		files = files[:rolloutScanLimit]
	}
	out := []Limits{}
	var readErr error
	for _, f := range files {
		tail, err := tailOf(f.path, rolloutTailLimit)
		if err != nil {
			if readErr == nil {
				readErr = err
			}
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
	looked := plural(len(files), "rollout")
	switch {
	case len(out) > 0:
		return out, found{}
	case readErr != nil:
		return out, found{Unreadable, "the newest " + looked + " under " + where +
			" could not be read: " + why(readErr)}
	}
	return out, found{NoReading, "no rate_limits in the newest " + looked + " under " + where +
		"; Codex writes them once a turn has been answered"}
}

// plural counts a thing the way a sentence a person reads counts it.
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
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
	return io.ReadAll(io.LimitReader(fh, rateLimitsReadLimit))
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
