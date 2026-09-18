// Package schedule is a task template that runs at a local wall-clock time:
// the file a person writes, the grammar of its `when`, and the arithmetic that
// decides what the minute timer should do about one occurrence.
//
// The file is the Swift app's, field for field (`docs/schedules.md` in that
// repository, `Orchestrator.schedule(from:)` and `Sources/Schedules.swift`),
// so a schedule written for one app reads the same in the other and the
// migration is a copy rather than a translation. Every refusal carries the
// Swift parser's own sentence.
//
// One rule is this daemon's and not the Swift app's, and it is the one
// `docs/plan.md` §3.2 exists for: **an occurrence from before this daemon first
// saw the schedule is not one it may fire.** The Swift app treats a file with
// no `created_at` as "as far back as anyone knows", and inside the six-hour
// catch-up window that means a copied, restored or imported file fires the
// minute it appears — the second of the two incidents in that section, in a
// new spelling. So every occurrence is also measured against the daemon's
// first sighting of the row, which is stamped wherever the row is first read.
package schedule

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// CloseTab is what happens to a run's terminal when it finishes.
type CloseTab string

const (
	CloseOnSuccess CloseTab = "on_success"
	CloseAlways    CloseTab = "always"
	CloseNever     CloseTab = "never"
)

// Defaults a file that leaves a field out gets, as in the Swift parser.
const (
	DefaultCatchUpHours = 6
	MaxCatchUpHours     = 168
	TitleLimit          = 120
)

// DayNames is Sunday first, index = time.Weekday. One table read in both
// directions, for the reason the Swift app gives: two tables of the same seven
// facts are two chances to disagree about which day `sun` is.
var DayNames = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Day is one Gregorian calendar day, as `when.on` spells it. Deliberately not a
// time: `2026-09-06` becomes an instant only once an hour, a minute and the
// machine's time zone are known.
type Day struct{ Year, Month, Day int }

func (d Day) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// When is `when`: a time of day and exactly one of every day, some weekdays,
// or one named date.
type When struct {
	Hour, Minute int
	Daily        bool
	// Weekdays is indexed by time.Weekday. Meaningful only when Daily is false
	// and On is nil.
	Weekdays [7]bool
	// On is set for a schedule that runs once.
	On *Day
}

// Once reports whether this is a schedule that runs once.
func (w When) Once() bool { return w.On != nil }

// Same reports whether two whens name the same firing times — the question a
// save asks to decide whether it moved them. Compared as values, not as
// spellings: `09:00` and a re-typed `09:00` are the same time.
func (w When) Same(o When) bool {
	if w.Hour != o.Hour || w.Minute != o.Minute || w.Daily != o.Daily || w.Weekdays != o.Weekdays {
		return false
	}
	if (w.On == nil) != (o.On == nil) {
		return false
	}
	return w.On == nil || *w.On == *o.On
}

// At is `when.at` as the file spells it.
func (w When) At() string { return fmt.Sprintf("%02d:%02d", w.Hour, w.Minute) }

// Object is `when` back in the file's own spelling: `{at, days}` or `{at, on}`.
func (w When) Object() map[string]any {
	out := map[string]any{"at": w.At()}
	switch {
	case w.On != nil:
		out["on"] = w.On.String()
	case w.Daily:
		out["days"] = "daily"
	default:
		days := []string{}
		for i, on := range w.Weekdays {
			if on {
				days = append(days, DayNames[i])
			}
		}
		out["days"] = days
	}
	return out
}

// Schedule is one parsed file.
type Schedule struct {
	ID    string
	Title string
	When  When
	// Task is the template exactly as the file has it. A task is materialised
	// from it when an occurrence is dispatched, never before, so a save that
	// lands while a run is working changes the next run and not this one.
	Task            map[string]any
	Enabled         bool
	CloseTab        CloseTab
	CatchUpHours    int
	NotifyOnFailure bool
	// The three stamps. A zero time means the file does not carry the key.
	CreatedAt     time.Time
	WhenChangedAt time.Time
	FiredAt       time.Time
}

// TaskString reads one string field of the template.
func (s Schedule) TaskString(key string) string {
	v, _ := s.Task[key].(string)
	return v
}

// Refusal is the parser's sentence for a file or a body it will not accept.
type Refusal struct{ Why string }

func (r Refusal) Error() string { return r.Why }

func bad(format string, args ...any) error { return Refusal{Why: fmt.Sprintf(format, args...)} }

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	modelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// ValidID reports whether an id is one: a lower-case UUID, which is also the
// whole of the path handling for anything addressed by one.
func ValidID(id string) bool { return uuidPattern.MatchString(id) }

// Decode reads a JSON object with exact integers, which is what Parse expects:
// a stamp or a count that arrived as 1.5 is a bad file, not something to round.
func Decode(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, fmt.Errorf("not a JSON object")
	}
	return obj, nil
}

// Integer is a JSON number with no fraction and no exponent, or false.
func Integer(raw any) (int64, bool) {
	switch v := raw.(type) {
	case json.Number:
		if strings.ContainsAny(string(v), ".eE") {
			return 0, false
		}
		n, err := strconv.ParseInt(string(v), 10, 64)
		return n, err == nil
	case int:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

func boolean(raw any) (bool, bool) {
	b, ok := raw.(bool)
	return b, ok
}

var allowedFields = map[string]bool{
	"clawdline_schedule": true, "schedule_id": true, "title": true, "when": true, "task": true,
	"enabled": true, "close_tab": true, "catch_up_hours": true, "notify_on_failure": true,
	"created_at": true, "when_changed_at": true, "fired_at": true,
}

var allowedTaskFields = map[string]bool{
	"assistant": true, "model": true, "reasoning_effort": true, "project_dir": true, "title": true,
	"instructions": true, "claims": true, "serialize": true, "isolation": true, "isolation_base": true,
	"permission_mode": true, "timeout_minutes": true, "deliverables": true, "kind": true, "plan": true,
	"graph": true,
}

func unknownKeys(obj map[string]any, allowed map[string]bool) []string {
	out := []string{}
	for k := range obj {
		if !allowed[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Parse reads one schedule file. `id` is what the file is stored under — the
// Swift app's filename — and the file's own `schedule_id` must match it.
// `isDirectory` answers whether `task.project_dir` is a directory on this
// machine; it is a parameter so a test can describe a machine.
func Parse(obj map[string]any, id string, isDirectory func(string) bool) (Schedule, error) {
	if unknown := unknownKeys(obj, allowedFields); len(unknown) > 0 {
		return Schedule{}, bad("unknown field: %s", strings.Join(unknown, ", "))
	}
	if v, ok := Integer(obj["clawdline_schedule"]); !ok || v != 1 {
		return Schedule{}, bad("clawdline_schedule must be 1")
	}
	sid, _ := obj["schedule_id"].(string)
	if !ValidID(sid) || sid != id {
		return Schedule{}, bad("schedule_id must be a lowercase UUID matching the filename")
	}
	title, _ := obj["title"].(string)
	if title == "" || utf8.RuneCountInString(title) > TitleLimit {
		return Schedule{}, bad("title must be a non-empty string of at most 120 characters")
	}
	when, err := ParseWhen(obj["when"])
	if err != nil {
		return Schedule{}, err
	}
	task, ok := obj["task"].(map[string]any)
	if !ok {
		return Schedule{}, bad("task must be an object")
	}
	if err := validateTask(task, isDirectory); err != nil {
		return Schedule{}, err
	}
	enabled, ok := boolean(obj["enabled"])
	if !ok {
		return Schedule{}, bad("enabled must be a boolean")
	}
	out := Schedule{ID: sid, Title: title, When: when, Task: task, Enabled: enabled,
		CloseTab: CloseOnSuccess, CatchUpHours: DefaultCatchUpHours, NotifyOnFailure: true}
	if raw, present := obj["close_tab"]; present {
		name, _ := raw.(string)
		switch CloseTab(name) {
		case CloseOnSuccess, CloseAlways, CloseNever:
			out.CloseTab = CloseTab(name)
		default:
			return Schedule{}, bad("close_tab must be on_success, always or never")
		}
	}
	if raw, present := obj["catch_up_hours"]; present {
		n, ok := Integer(raw)
		if !ok || n < 0 || n > MaxCatchUpHours {
			return Schedule{}, bad("catch_up_hours must be an integer from 0 through 168")
		}
		out.CatchUpHours = int(n)
	}
	if raw, present := obj["notify_on_failure"]; present {
		b, ok := boolean(raw)
		if !ok {
			return Schedule{}, bad("notify_on_failure must be a boolean")
		}
		out.NotifyOnFailure = b
	}
	// Unix seconds, optional on purpose: a file without them means "as far back
	// as anyone knows", which this daemon then narrows to its own first sighting.
	for _, stamp := range []struct {
		key string
		to  *time.Time
	}{{"created_at", &out.CreatedAt}, {"when_changed_at", &out.WhenChangedAt}, {"fired_at", &out.FiredAt}} {
		raw, present := obj[stamp.key]
		if !present {
			continue
		}
		n, ok := Integer(raw)
		if !ok || n < 0 {
			return Schedule{}, bad("%s must be a whole number of seconds since 1970", stamp.key)
		}
		*stamp.to = time.Unix(n, 0)
	}
	if !out.FiredAt.IsZero() && !when.Once() {
		return Schedule{}, bad("fired_at belongs to a schedule that runs once; this one has when.days")
	}
	return out, nil
}

// ParseWhen reads `when`, with the parser's own sentence for every way it can
// be wrong.
func ParseWhen(raw any) (When, error) {
	obj, ok := raw.(map[string]any)
	_, hasAt := obj["at"]
	if !ok || len(obj) != 2 || !hasAt || len(unknownKeys(obj, map[string]bool{"at": true, "days": true, "on": true})) > 0 {
		return When{}, bad("when must contain at and exactly one of days or on")
	}
	var w When
	if on, present := obj["on"]; present {
		text, _ := on.(string)
		day, ok := ParseDay(text)
		if !ok {
			return When{}, bad("when.on must be a real calendar date spelled YYYY-MM-DD")
		}
		w.On = &day
	} else if s, ok := obj["days"].(string); ok && s == "daily" {
		w.Daily = true
	} else {
		days, ok := obj["days"].([]any)
		if !ok || len(days) == 0 {
			return When{}, bad("when.days must be daily or a non-empty weekday array")
		}
		for i, d := range days {
			name, _ := d.(string)
			n := -1
			for j, known := range DayNames {
				if known == name {
					n = j
				}
			}
			if n < 0 {
				return When{}, bad("when.days[%d] must be sun, mon, tue, wed, thu, fri or sat", i)
			}
			if w.Weekdays[n] {
				return When{}, bad("when.days must not contain duplicates")
			}
			w.Weekdays[n] = true
		}
	}
	at, _ := obj["at"].(string)
	parts := strings.Split(at, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return When{}, bad("when.at must be HH:MM in local time")
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 || !digits(parts[0]+parts[1]) {
		return When{}, bad("when.at must be HH:MM in local time")
	}
	w.Hour, w.Minute = h, m
	return w, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseDay reads `YYYY-MM-DD`, and only a real Gregorian day: `2026-02-30` is
// refused rather than rolled forward.
func ParseDay(text string) (Day, bool) {
	parts := strings.Split(text, "-")
	if len(parts) != 3 || len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 ||
		!digits(parts[0]+parts[1]+parts[2]) {
		return Day{}, false
	}
	y, _ := strconv.Atoi(parts[0])
	mo, _ := strconv.Atoi(parts[1])
	d, _ := strconv.Atoi(parts[2])
	if y < 1970 {
		return Day{}, false
	}
	t := time.Date(y, time.Month(mo), d, 12, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return Day{}, false
	}
	return Day{y, mo, d}, true
}

// validateTask is the part of the Swift app's task.json validation a schedule
// template goes through, each sentence prefixed `task.` as it is there.
func validateTask(task map[string]any, isDirectory func(string) bool) error {
	if unknown := unknownKeys(task, allowedTaskFields); len(unknown) > 0 {
		return bad("unknown task field: %s", strings.Join(unknown, ", "))
	}
	if raw, present := task["deliverables"]; present {
		values, ok := raw.([]any)
		if !ok || len(values) > 32 {
			return bad("task.deliverables must be an array of at most 32 non-empty strings")
		}
		for _, v := range values {
			s, ok := v.(string)
			if !ok || s == "" || utf8.RuneCountInString(s) > 300 {
				return bad("task.deliverables must be an array of at most 32 non-empty strings")
			}
		}
	}
	for _, key := range []string{"model", "reasoning_effort", "title", "permission_mode", "kind", "plan"} {
		if raw, present := task[key]; present {
			if _, ok := raw.(string); !ok {
				return bad("task.%s must be a string", key)
			}
		}
	}
	if raw, present := task["timeout_minutes"]; present {
		if _, ok := Integer(raw); !ok {
			return bad("task.timeout_minutes must be an integer")
		}
	}
	assistant, _ := task["assistant"].(string)
	if assistant != "claude" && assistant != "codex" {
		return bad("task.assistant must be claude or codex")
	}
	dir, _ := task["project_dir"].(string)
	if !usablePath(dir) || isDirectory == nil || !isDirectory(dir) {
		return bad("task.project_dir must be an absolute path to a directory")
	}
	instructions, _ := task["instructions"].(string)
	if instructions == "" || len(instructions) > 16384 {
		return bad("task.instructions must be non-empty and at most 16 KiB")
	}
	if model, _ := task["model"].(string); model != "" && !modelPattern.MatchString(model) {
		return bad("task.model must be a model name: lower-case letters, digits, . _ -, at most 64 characters")
	}
	if raw, present := task["reasoning_effort"]; present {
		if assistant != "codex" {
			return bad("task.reasoning_effort is only valid when assistant is codex")
		}
		if name, _ := raw.(string); name != "high" && name != "xhigh" {
			return bad("task.reasoning_effort must be one of: high, xhigh")
		}
	}
	if plan, _ := task["plan"].(string); len(plan) > 64*1024 {
		return bad("task.plan must be at most 64 KiB")
	}
	isolation := "none"
	if raw, present := task["isolation"]; present {
		isolation, _ = raw.(string)
		if isolation != "none" && isolation != "worktree" {
			return bad("task.isolation must be one of: none, worktree")
		}
	}
	if _, present := task["isolation_base"]; present && isolation != "worktree" {
		return bad("task.isolation_base is only valid when isolation is worktree")
	}
	if raw, present := task["serialize"]; present {
		values, ok := raw.([]any)
		if !ok || len(values) > 4 {
			return bad("task.serialize must be an array of at most 4 tokens")
		}
		for i, v := range values {
			if s, ok := v.(string); !ok || !modelPattern.MatchString(s) {
				return bad("task.serialize[%d] must be 1–64 lower-case letters, digits, . _ -, and not begin with -", i)
			}
		}
	}
	if raw, present := task["claims"]; present {
		if _, err := Claims(raw); err != nil {
			return bad("task.%s", err.Error())
		}
	}
	if raw, present := task["permission_mode"]; present {
		if name, _ := raw.(string); name != "" && name != "ask" && name != "edits" && name != "full" {
			return bad("task.permission_mode must be one of: ask, edits, full")
		}
	}
	if raw, present := task["timeout_minutes"]; present {
		if n, _ := Integer(raw); n < 1 || n > 240 {
			return bad("task.timeout_minutes must be 1…240")
		}
	}
	return nil
}

// Claims reads a template's `claims`: 0–32 relative POSIX paths.
func Claims(raw any) ([]string, error) {
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("claims must be an array of 0–32 relative POSIX paths")
	}
	errs := []string{}
	if len(values) > 32 {
		errs = append(errs, "claims must contain 0–32 paths")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for i, v := range values {
		p, ok := v.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("claims[%d] must be a string", i))
			continue
		}
		if p == "" || utf8.RuneCountInString(p) > 1024 {
			errs = append(errs, fmt.Sprintf("claims[%d] must be 1–1024 characters", i))
		}
		if strings.HasPrefix(p, "/") {
			errs = append(errs, fmt.Sprintf("claims[%d] must be relative to project_dir", i))
		}
		for _, part := range strings.Split(p, "/") {
			if part == ".." {
				errs = append(errs, fmt.Sprintf("claims[%d] must not contain a .. component", i))
				break
			}
		}
		if strings.ContainsRune(p, 0) {
			errs = append(errs, fmt.Sprintf("claims[%d] must be a POSIX path without NUL", i))
		}
		if seen[p] {
			errs = append(errs, fmt.Sprintf("claims[%d] duplicates %s", i, p))
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

// usablePath is `StartPoints.usable`: absolute, and no control characters.
func usablePath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// fireOn is the instant a schedule's time of day names on one local day.
func (w When) fireOn(y int, m time.Month, d int, loc *time.Location) time.Time {
	return time.Date(y, m, d, w.Hour, w.Minute, 0, 0, loc)
}

func (w When) runsOn(t time.Time) bool {
	return w.Daily || w.Weekdays[t.Weekday()]
}

// LatestFire is the most recent occurrence at or before now, or zero for none —
// a one-shot whose date is still ahead, or a pattern with no day in the last
// week, which the grammar cannot express but the arithmetic still answers.
func (w When) LatestFire(now time.Time, loc *time.Location) time.Time {
	now = now.In(loc)
	if w.On != nil {
		fire := w.fireOn(w.On.Year, time.Month(w.On.Month), w.On.Day, loc)
		if !fire.After(now) {
			return fire
		}
		return time.Time{}
	}
	y, m, d := now.Date()
	for ago := 0; ago <= 7; ago++ {
		c := w.fireOn(y, m, d-ago, loc)
		if c.After(now) || !w.runsOn(c) {
			continue
		}
		return c
	}
	return time.Time{}
}

// NextFire is the next occurrence after now, or zero for a schedule that will
// never fire again — a one-shot whose instant has passed.
func (w When) NextFire(now time.Time, loc *time.Location) time.Time {
	now = now.In(loc)
	if w.On != nil {
		fire := w.fireOn(w.On.Year, time.Month(w.On.Month), w.On.Day, loc)
		if fire.After(now) {
			return fire
		}
		return time.Time{}
	}
	y, m, d := now.Date()
	for ahead := 0; ahead <= 7; ahead++ {
		c := w.fireOn(y, m, d+ahead, loc)
		if !c.After(now) || !w.runsOn(c) {
			continue
		}
		return c
	}
	return time.Time{}
}

// Action is what the minute timer does about one occurrence.
type Action string

const (
	Run            Action = "run"
	AlreadyHandled Action = "already_handled"
	Active         Action = "active"
	Missed         Action = "missed"
	// The four that do nothing and record nothing. They are separate words
	// because they are separate reasons, and a reader of an audit or a test is
	// owed the right one.
	BeforeCreation Action = "before_creation"
	BeforeRetiming Action = "before_retiming"
	BeforeSighting Action = "before_sighting"
	Spent          Action = "spent"
)

// Occurrence is everything the decision reads besides the schedule itself.
type Occurrence struct {
	Now  time.Time
	Fire time.Time
	// FirstSeen is when this daemon first had the row in front of it. Zero
	// means nothing has claimed to have seen it, and nothing may fire it.
	FirstSeen time.Time
	// Handled is the latest occurrence this daemon already decided — ran,
	// skipped as active, or missed. It is durable, so a restart inside a
	// catch-up window does not run the same occurrence a second time.
	Handled time.Time
	// LastRun is when the newest task this schedule made was created, manual
	// runs included: a manual run at or after an occurrence stands for it.
	LastRun time.Time
	// Active is whether a task from this schedule has not finished.
	Active bool
}

// Window is the catch-up window: at least a minute, so a zero-hour schedule
// still runs on the tick after its minute.
func (s Schedule) Window() time.Duration {
	w := time.Duration(s.CatchUpHours) * time.Hour
	if w < time.Minute {
		w = time.Minute
	}
	return w
}

// Decide is `Orchestrator.scheduleAction`, with the first-sighting gate added
// after the two gates the file carries.
func (s Schedule) Decide(o Occurrence) Action {
	if !s.FiredAt.IsZero() {
		return Spent
	}
	if !s.CreatedAt.IsZero() && o.Fire.Before(s.CreatedAt) {
		return BeforeCreation
	}
	if !s.WhenChangedAt.IsZero() && o.Fire.Before(s.WhenChangedAt) {
		return BeforeRetiming
	}
	// The rule from docs/plan.md §3.2. A row this daemon has not seen yet, and
	// an occurrence from before it saw it, are not runs anybody missed here:
	// whatever was due then belonged to whoever held the schedule then.
	if o.FirstSeen.IsZero() || o.Fire.Before(o.FirstSeen) {
		return BeforeSighting
	}
	if !o.Handled.IsZero() && !o.Handled.Before(o.Fire) {
		return AlreadyHandled
	}
	if !o.LastRun.IsZero() && !o.LastRun.Before(o.Fire) {
		return AlreadyHandled
	}
	if o.Active {
		return Active
	}
	if o.Now.Sub(o.Fire) <= s.Window() {
		return Run
	}
	return Missed
}

// Stale is `scheduleFireIsStale`: whether an occurrence decided a moment ago is
// still the file's to run, asked again at the moment of dispatch.
func (s Schedule) Stale(fire time.Time) bool {
	if !s.FiredAt.IsZero() {
		return true
	}
	return !s.WhenChangedAt.IsZero() && fire.Before(s.WhenChangedAt)
}
