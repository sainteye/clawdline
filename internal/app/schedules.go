package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/schedule"
)

// SchedulePlace is one row of `GET /v1/places`: a schedule names a project by
// this id and never by a path, so a device can only name a project this machine
// has already shown it.
type SchedulePlace struct{ ID, Label, Path string }

// ScheduleAuthority says which of the schedule route's two doors a write
// used. Machine is the daemon-wide orchestrator credential. A non-empty Run
// is a person's recent message to Session, validated by the HTTP owner before
// it is handed here; it lets that session carry the person's standing
// instruction without granting the machine credential blanket cron authority.
// Run and Session bind the act to an auditable message and conversation; they
// do not identify the HTTP caller, because the orchestrator credential remains
// machine-wide. They are audit evidence, never schedule-file fields.
type ScheduleAuthority struct {
	Machine bool
	Run     string
	Session string
}

func (a ScheduleAuthority) audit(fields map[string]string) map[string]string {
	if a.Run != "" {
		fields["actor"] = "user_via_session:" + a.Run
		fields["session"] = a.Session
	}
	return fields
}

// ScheduleReply is one answer, in the envelope every route here uses: a body on
// success, a status, a code and a sentence otherwise.
type ScheduleReply struct {
	Status  int
	Code    string
	Message string
	Body    map[string]any
	// Extra is what a broker refusal carries inside its error object — the
	// blocking task of a `workspace_busy`, the `retry_after` of an
	// `over_capacity` — passed through as the broker said it, so a manual
	// run is refused in the same words a dispatch is.
	Extra map[string]any
}

// OK reports whether the reply is a success.
func (r ScheduleReply) OK() bool { return r.Code == "" }

func answered(body map[string]any) ScheduleReply { return ScheduleReply{Status: 200, Body: body} }

func refusedSchedule(status int, code, message string) ScheduleReply {
	return ScheduleReply{Status: status, Code: code, Message: message}
}

// ScheduleBook is every schedule this daemon holds and every way one changes:
// the Swift app's `Orchestrator` schedule half and `ScheduleService`, over this
// daemon's own store rather than `~/.config/clawdline/schedules`.
type ScheduleBook struct {
	Store *store.Store
	// Broker is the one way a run is started: the broker's own dispatch
	// path, so a scheduled run is arbitrated against, briefed, timed and
	// collected exactly as a dispatched task is (docs/design-decisions.md D07).
	Broker ScheduleBroker
	// Places is the list a `place_id` is resolved against.
	Places func(ctx context.Context) []SchedulePlace
	// IsDirectory answers `task.project_dir must be … a directory`.
	IsDirectory func(string) bool
	// Location is the machine's wall clock; nil means time.Local.
	Location *time.Location
	Now      func() time.Time
	// DispatchEnabled is `orchestrator_enabled`. A schedule made while it is
	// off is listed and never fires.
	DispatchEnabled func() bool
	// Audit writes one line to this daemon's audit log.
	Audit func(event string, fields map[string]string)
	// Notify pushes one sentence to the paired devices. Nil sends nothing.
	Notify func(ctx context.Context, title, body, tag string)
	// Activate turns a bound hook on at the Cloud. Nil is a machine with no
	// Cloud credential, which is what the Swift app answers
	// `401 no_machine_credential` for.
	Activate func(ctx context.Context, hookID, requestID string) (int64, error)
	// ImportsEnabled is `schedule_imports_enabled`; nil or false keeps the
	// import route shut.
	ImportsEnabled func() bool

	// mu is the one lane every dispatch goes through, timer and manual alike,
	// so the "is a run from this schedule still working" question and the
	// dispatch that answers it cannot interleave.
	mu sync.Mutex

	rateMu sync.Mutex
	writes []time.Time

	invalidMu sync.Mutex
	// invalidSeen is `invalidScheduleFingerprints`: which content of which
	// unreadable row has already been audited and pushed, so a polled list
	// does not turn one bad row into a flood.
	invalidSeen map[string]string
}

func (b *ScheduleBook) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *ScheduleBook) loc() *time.Location {
	if b.Location != nil {
		return b.Location
	}
	return time.Local
}

func (b *ScheduleBook) dispatchEnabled() bool {
	return b.DispatchEnabled == nil || b.DispatchEnabled()
}

func (b *ScheduleBook) audit(event string, fields map[string]string) {
	if b.Audit != nil {
		b.Audit(event, fields)
	}
	log.Printf("%s %v", event, fields)
}

func (b *ScheduleBook) isDirectory(p string) bool {
	if b.IsDirectory != nil {
		return b.IsDirectory(p)
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// held is one valid stored schedule with the object it came from.
type held struct {
	s   schedule.Schedule
	f   store.ScheduleFile
	obj map[string]any
}

type inventory struct {
	valid   []held
	invalid []map[string]any
}

// load reads every row through the parser. A malformed row cannot hide a
// valid neighbour; it becomes an `invalid` row, audited and pushed once per
// content revision.
func (b *ScheduleBook) load(ctx context.Context) (inventory, error) {
	files, err := b.Store.ScheduleFiles(ctx)
	if err != nil {
		return inventory{}, err
	}
	inv := inventory{}
	fresh := map[string]string{}
	type bad struct{ file, why, kind, title, fingerprint string }
	newly := []bad{}
	b.invalidMu.Lock()
	for _, f := range files {
		name := f.ID + ".json"
		sum := sha256.Sum256(f.Body)
		fingerprint := hex.EncodeToString(sum[:])
		obj, derr := schedule.Decode(f.Body)
		if derr != nil {
			inv.invalid = append(inv.invalid, invalidRow(name, "The file does not contain a JSON object.", "unreadable_json"))
			fresh[name] = fingerprint
			if b.invalidSeen[name] != fingerprint {
				newly = append(newly, bad{name, "The file does not contain a JSON object.", "unreadable_json", f.ID, fingerprint})
			}
			continue
		}
		s, perr := schedule.Parse(obj, f.ID, b.isDirectory)
		if perr != nil {
			why := perr.Error()
			if len(why) > 500 {
				why = why[:500]
			}
			kind := "schema"
			if strings.Contains(why, "project_dir must be an absolute path to a directory") {
				kind = "project_unavailable"
			}
			inv.invalid = append(inv.invalid, invalidRow(name, why, kind))
			fresh[name] = fingerprint
			if b.invalidSeen[name] != fingerprint {
				title, _ := obj["title"].(string)
				if title == "" {
					title = f.ID
				}
				newly = append(newly, bad{name, why, kind, title, fingerprint})
			}
			continue
		}
		inv.valid = append(inv.valid, held{s: s, f: f, obj: obj})
	}
	b.invalidSeen = fresh
	b.invalidMu.Unlock()
	for _, n := range newly {
		b.audit("orchestrator.schedule.invalid", map[string]string{"file": n.file, "why": n.why, "kind": n.kind})
		if b.Notify != nil {
			body := scheduleNotice(b.notificationLanguage(), invalidScheduleNoticeKind(n.kind), n.file)
			b.Notify(ctx, n.title, body, "schedule-invalid-"+n.file)
		}
	}
	sort.Slice(inv.valid, func(i, j int) bool { return inv.valid[i].s.ID < inv.valid[j].s.ID })
	sort.Slice(inv.invalid, func(i, j int) bool {
		return inv.invalid[i]["file"].(string) < inv.invalid[j]["file"].(string)
	})
	return inv, nil
}

func invalidRow(file, why, kind string) map[string]any {
	return map[string]any{"file": file, "state": "invalid", "error": why, "error_kind": kind}
}

func (b *ScheduleBook) named(ctx context.Context, id string) (held, bool, error) {
	if !schedule.ValidID(id) {
		return held{}, false, nil
	}
	inv, err := b.load(ctx)
	if err != nil {
		return held{}, false, err
	}
	for _, h := range inv.valid {
		if h.s.ID == id {
			return h, true, nil
		}
	}
	return held{}, false, nil
}

// runsBySchedule groups the retained runs, newest first within each.
func (b *ScheduleBook) runsBySchedule(ctx context.Context) (map[string][]store.ScheduleRun, error) {
	runs, err := b.Store.ScheduleRuns(ctx, "")
	if err != nil {
		return nil, err
	}
	out := map[string][]store.ScheduleRun{}
	for _, r := range runs {
		out[r.ScheduleID] = append(out[r.ScheduleID], r)
	}
	return out, nil
}

// ScheduleBroker is what a schedule needs of the broker: to hand it one
// occurrence to run. It is an interface only so that the book can be read
// apart from a whole broker; the daemon's is *orchestrator.Broker.
type ScheduleBroker interface {
	DispatchScheduled(ctx context.Context, run orchestrator.ScheduledRun) (orchestrator.ScheduledDispatch, error)
}

// finished is whether a run's task can still be working. The state is the
// broker's; empty is a run whose task no broker row holds, which is not
// working either.
func finished(state string) bool {
	return orchestrator.State(state).Terminal()
}

// List is `scheduleRecords`: what exists, when it next fires and how the last
// run went. No task-template field but `project_dir`.
func (b *ScheduleBook) List(ctx context.Context) ([]map[string]any, error) {
	inv, err := b.load(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := b.runsBySchedule(ctx)
	if err != nil {
		return nil, err
	}
	now := b.now()
	out := make([]map[string]any, 0, len(inv.valid)+len(inv.invalid))
	for _, h := range inv.valid {
		row := b.summary(h.s, now)
		if h.s.When.Once() {
			row["once"] = true
		}
		if !h.s.FiredAt.IsZero() {
			row["fired_at"] = h.s.FiredAt.Unix()
		}
		if dir := h.s.TaskString("project_dir"); dir != "" {
			row["project_dir"] = dir
		}
		if list := runs[h.s.ID]; len(list) > 0 {
			row["last_run"] = lastRun(list[0])
		}
		if !h.f.LastMissed.IsZero() {
			row["last_missed_at"] = h.f.LastMissed.Unix()
		}
		out = append(out, row)
	}
	return append(out, inv.invalid...), nil
}

// summary is the three fields every answer about one schedule starts with.
func (b *ScheduleBook) summary(s schedule.Schedule, now time.Time) map[string]any {
	row := map[string]any{"id": s.ID, "title": s.Title, "enabled": s.Enabled}
	if next := s.When.NextFire(now, b.loc()); !next.IsZero() {
		row["next_fire"] = next.Unix()
	}
	return row
}

func lastRun(r store.ScheduleRun) map[string]any {
	return map[string]any{"task_id": r.TaskID, "state": r.State, "at": r.Created.Unix()}
}

// RunsRetained is how many runs one schedule's detail lists before it says the
// list may be missing its oldest — the Swift registry's 200.
const RunsRetained = 200

// Detail is `scheduleRecord`: one schedule in full, with the template and the
// retained runs the list leaves out.
func (b *ScheduleBook) Detail(ctx context.Context, id string) (map[string]any, bool, error) {
	h, ok, err := b.named(ctx, id)
	if err != nil || !ok {
		return nil, false, err
	}
	s := h.s
	out := b.summary(s, b.now())
	out["file"] = s.ID + ".json"
	out["task"] = s.Task
	out["close_tab"] = string(s.CloseTab)
	out["catch_up_hours"] = s.CatchUpHours
	out["notify_on_failure"] = s.NotifyOnFailure
	out["when"] = s.When.Object()
	if s.When.Once() {
		out["once"] = true
	}
	if !s.FiredAt.IsZero() {
		out["fired_at"] = s.FiredAt.Unix()
	}
	runs, err := b.Store.ScheduleRuns(ctx, s.ID)
	if err != nil {
		return nil, false, err
	}
	if len(runs) > 0 {
		out["last_run"] = lastRun(runs[0])
	}
	if len(runs) > RunsRetained {
		runs = runs[:RunsRetained]
		out["runs_may_be_truncated"] = true
	}
	records := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		rec := map[string]any{"task_id": r.TaskID, "state": r.State, "assistant": r.Assistant,
			"project_dir": r.ProjectDir, "created": r.Created.Unix()}
		if r.Summary != "" {
			rec["summary"] = r.Summary
		}
		records = append(records, rec)
	}
	out["runs"] = records
	if !h.f.LastMissed.IsZero() {
		out["last_missed_at"] = h.f.LastMissed.Unix()
	}
	// `ScheduleWebhookProjection.apply`.
	if hook, bound, err := b.Store.ScheduleWebhook(ctx, s.ID); err != nil {
		out["webhook_binding_availability"] = "binding_store_unavailable"
	} else if bound {
		out["webhook_hook_id"] = hook
		out["webhook_binding_availability"] = "active"
	} else {
		out["webhook_binding_availability"] = "unbound"
	}
	return out, true, nil
}

// MachineRefusal is `machineScheduleRefusal`: this machine's orchestrator
// token makes, changes and removes a schedule that runs once, and a repeating
// schedule stays a person's to arrange.
func (b *ScheduleBook) MachineRefusal(ctx context.Context, method, id string, body map[string]any) *ScheduleReply {
	var once bool
	if method == "POST" {
		_, on := body["on"]
		_, days := body["days"]
		once = on && !days
	} else {
		h, ok, err := b.named(ctx, id)
		if err != nil || !ok {
			return nil
		}
		once = h.s.When.Once()
	}
	if once {
		return nil
	}
	r := refusedSchedule(403, "forbidden",
		"This machine's orchestrator token may make, change and remove a schedule that runs once — "+
			"a when with an on date. A repeating schedule is arranged by a person: in Settings, or "+
			"from a paired device that may send. GET /v1/orchestrator/schedules/:id reads back what "+
			"you wrote, through the same parser this route would have used.")
	return &r
}

var formFields = map[string]bool{
	"title": true, "at": true, "days": true, "on": true, "place_id": true, "assistant": true,
	"instructions": true, "enabled": true, "close_tab": true, "catch_up_hours": true,
	"notify_on_failure": true, "timeout_minutes": true, "model": true, "permission_mode": true,
}

// templateFields are the task-template keys a create may carry in `template`:
// the ones a save already carries from the stored file (`build`), so a
// schedule moved from another machine arrives with them rather than without.
// `permission_mode` is not one of them: it is the form's own field, which
// `permissionRefusal` keeps from an agent, and `template` must not be a
// second door around that check.
var templateFields = map[string]bool{
	"claims": true, "serialize": true, "isolation": true, "isolation_base": true,
	"deliverables": true, "kind": true, "plan": true, "graph": true, "reasoning_effort": true,
}

// createTemplate takes `template` out of a create's body. What it holds is
// handed to `build` as if a stored file carried it, so the parser reads each
// value exactly as it reads a stored one; this only says which keys may come.
func createTemplate(body map[string]any) (map[string]any, map[string]any, *ScheduleReply) {
	raw, present := body["template"]
	if !present {
		return body, nil, nil
	}
	form := make(map[string]any, len(body))
	for k, v := range body {
		if k != "template" {
			form[k] = v
		}
	}
	tmpl, ok := raw.(map[string]any)
	if !ok {
		r := refusedSchedule(400, "bad_request", "template must be an object of task-template fields.")
		return nil, nil, &r
	}
	if _, named := tmpl["permission_mode"]; named {
		r := refusedSchedule(400, "template_permission_mode",
			"template may not set permission_mode: send it as the permission_mode field, as the form does.")
		return nil, nil, &r
	}
	unknown := []string{}
	for k := range tmpl {
		if !templateFields[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		r := refusedSchedule(400, "bad_request", "unknown template field: "+strings.Join(unknown, ", "))
		return nil, nil, &r
	}
	return form, tmpl, nil
}

func orEmpty(body map[string]any, key string) any {
	if v, ok := body[key]; ok {
		return v
	}
	return ""
}

// build is `scheduleObject`: the flat body a form sends, arranged into the
// file and handed straight to the parser, which stays the authority.
func (b *ScheduleBook) build(ctx context.Context, body map[string]any, id string, createdAt time.Time,
	carried map[string]any) (map[string]any, schedule.Schedule, *ScheduleReply) {
	unknown := []string{}
	for k := range body {
		if !formFields[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		r := refusedSchedule(400, "bad_request", "unknown field: "+strings.Join(unknown, ", "))
		return nil, schedule.Schedule{}, &r
	}
	placeID, _ := body["place_id"].(string)
	var place *SchedulePlace
	if b.Places != nil && placeID != "" {
		for _, p := range b.Places(ctx) {
			if p.ID == placeID {
				p := p
				place = &p
				break
			}
		}
	}
	if place == nil {
		r := refusedSchedule(400, "bad_request", "place_id must be one of the ids GET /v1/places lists.")
		return nil, schedule.Schedule{}, &r
	}
	tmpl := map[string]any{
		"assistant":    orEmpty(body, "assistant"),
		"project_dir":  place.Path,
		"title":        orEmpty(body, "title"),
		"instructions": orEmpty(body, "instructions"),
	}
	// No `model` key leaves the model alone; `"model": ""` takes it off.
	if model, ok := body["model"]; ok {
		if s, isString := model.(string); !isString || s != "" {
			tmpl["model"] = model
		}
	} else if kept, ok := carried["model"]; ok {
		tmpl["model"] = kept
	}
	// `permission_mode` as `model`: no key keeps what the file says, `""`
	// takes it off (the machine's default), a name is read by the parser.
	if mode, ok := body["permission_mode"]; ok {
		name, isString := mode.(string)
		if !isString {
			r := refusedSchedule(400, "bad_request",
				`permission_mode must be one of: ask, edits, full, or "" for this machine's default`)
			return nil, schedule.Schedule{}, &r
		}
		if name != "" {
			tmpl["permission_mode"] = name
		}
	} else if kept, ok := carried["permission_mode"]; ok {
		tmpl["permission_mode"] = kept
	}
	if timeout, ok := body["timeout_minutes"]; ok {
		tmpl["timeout_minutes"] = timeout
	}
	// Fields no form has a control for are carried, not dropped.
	for _, key := range []string{"claims", "serialize", "isolation",
		"isolation_base", "deliverables", "kind", "plan", "graph"} {
		if kept, ok := carried[key]; ok {
			tmpl[key] = kept
		}
	}
	if tmpl["assistant"] == "codex" {
		if kept, ok := carried["reasoning_effort"]; ok {
			tmpl["reasoning_effort"] = kept
		}
	}
	when := map[string]any{"at": orEmpty(body, "at"), "days": orEmpty(body, "days")}
	if on, ok := body["on"]; ok {
		when["on"] = on
		if _, days := body["days"]; !days {
			delete(when, "days")
		}
	}
	enabled := any(true)
	if v, ok := body["enabled"]; ok {
		enabled = v
	}
	obj := map[string]any{
		"clawdline_schedule": 1,
		"schedule_id":        id,
		"title":              orEmpty(body, "title"),
		"when":               when,
		"task":               tmpl,
		"enabled":            enabled,
	}
	if !createdAt.IsZero() {
		obj["created_at"] = createdAt.Unix()
	}
	for _, key := range []string{"close_tab", "catch_up_hours", "notify_on_failure"} {
		if v, ok := body[key]; ok {
			obj[key] = v
		}
	}
	made, err := b.parseObject(obj, id)
	if err != nil {
		r := refusedSchedule(400, "bad_request", err.Error())
		return nil, schedule.Schedule{}, &r
	}
	return obj, made, nil
}

// parseObject round-trips an assembled object through JSON first, so the
// parser sees exactly what a stored file will say — numbers included.
func (b *ScheduleBook) parseObject(obj map[string]any, id string) (schedule.Schedule, error) {
	raw, err := encodeFile(obj)
	if err != nil {
		return schedule.Schedule{}, err
	}
	decoded, err := schedule.Decode(raw)
	if err != nil {
		return schedule.Schedule{}, err
	}
	return schedule.Parse(decoded, id, b.isDirectory)
}

// permissionOf is a template's `permission_mode`, "" when it names none —
// which is also what an empty value means to the parser.
func permissionOf(task map[string]any) string {
	name, _ := task["permission_mode"].(string)
	return name
}

// permissionRefusal is the one thing an agent may not do to a schedule: set
// or change the permission its runs get. The orchestrator token is
// machine-wide, and a session relaying a person's message is still an agent
// choosing the words, so either could otherwise grant itself a recurring
// full-permission wake-up. They may keep what the file already says.
func permissionRefusal(authority ScheduleAuthority, obj, stored map[string]any) *ScheduleReply {
	if !authority.Machine && authority.Run == "" {
		return nil
	}
	task, _ := obj["task"].(map[string]any)
	if permissionOf(task) == permissionOf(stored) {
		return nil
	}
	r := refusedSchedule(403, "permission_needs_person",
		"A schedule's permission is set by a person, from the schedule form in the console. "+
			"This machine's orchestrator token, and a session carrying a person's message, may keep the "+
			"permission_mode a schedule already has, not set or change it: leave permission_mode out.")
	return &r
}

// permissionAudit adds the permission to a write's audit line when it changed.
func permissionAudit(fields map[string]string, obj, stored map[string]any) map[string]string {
	task, _ := obj["task"].(map[string]any)
	now, was := permissionOf(task), permissionOf(stored)
	if now == was {
		return fields
	}
	word := func(name string) string {
		if name == "" {
			return "default"
		}
		return name
	}
	fields["permission"] = word(now)
	fields["permission_was"] = word(was)
	return fields
}

// encodeFile is how this daemon writes a schedule: sorted keys, indented, no
// HTML escaping — the Swift app's `.prettyPrinted, .sortedKeys,
// .withoutEscapingSlashes`, near enough that a person diffing the two reads
// the same file.
func encodeFile(obj map[string]any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(b.String(), "\n")), nil
}

// takeWriteRate is the ten-in-ten-minutes brake on making and saving. Delete
// is not braked: it leaves nothing behind, and it is what somebody reaches for
// when they want work to stop.
func (b *ScheduleBook) takeWriteRate() bool {
	b.rateMu.Lock()
	defer b.rateMu.Unlock()
	now := b.now()
	kept := b.writes[:0]
	for _, t := range b.writes {
		if now.Sub(t) < 10*time.Minute {
			kept = append(kept, t)
		}
	}
	b.writes = kept
	if len(b.writes) >= 10 {
		return false
	}
	b.writes = append(b.writes, now)
	return true
}

func rateLimited() ScheduleReply {
	return refusedSchedule(429, "rate_limited",
		"This machine has been asked for several schedules in the last few minutes. Try again shortly.")
}

// Create is `createSchedule`.
func (b *ScheduleBook) Create(ctx context.Context, body map[string]any, authority ScheduleAuthority) ScheduleReply {
	if authority.Machine {
		if r := b.MachineRefusal(ctx, "POST", "", body); r != nil {
			return *r
		}
	}
	body, carried, refusal := createTemplate(body)
	if refusal != nil {
		return *refusal
	}
	now := b.now()
	id := newUUID()
	obj, _, refusal := b.build(ctx, body, id, now, carried)
	if refusal != nil {
		return *refusal
	}
	if refusal := permissionRefusal(authority, obj, nil); refusal != nil {
		return *refusal
	}
	if !b.takeWriteRate() {
		return rateLimited()
	}
	raw, err := encodeFile(obj)
	if err != nil {
		return refusedSchedule(500, "write_failed", "The schedule file could not be written.")
	}
	// A schedule made now is seen now: its first sighting is its creation, and
	// `created_at` answers every question the timer asks about its first day.
	if err := b.Store.CreateScheduleFile(ctx, id, raw, now, time.Time{}); err != nil {
		b.audit("orchestrator.schedule.created", map[string]string{"schedule": id, "ok": "0", "why": "write_failed"})
		return refusedSchedule(500, "write_failed", "The schedule file could not be written.")
	}
	made, ok := b.readBack(ctx, id)
	if !ok {
		_, _ = b.Store.DeleteScheduleFile(ctx, id)
		b.audit("orchestrator.schedule.created", map[string]string{"schedule": id, "ok": "0", "why": "unreadable"})
		return refusedSchedule(500, "write_failed", "The schedule was written and could not be read back, so it has been removed.")
	}
	fields := map[string]string{"schedule": id, "ok": "1", "cwd": made.TaskString("project_dir")}
	if placeID, _ := body["place_id"].(string); placeID != "" {
		fields["place"] = placeID
	}
	b.audit("orchestrator.schedule.created", authority.audit(permissionAudit(fields, obj, nil)))
	return answered(map[string]any{"ok": true, "schedule": b.summary(made, now),
		"dispatch_enabled": b.dispatchEnabled()})
}

// readBack reads a just-written row through the parser, the way the Swift app
// re-reads the file off disk before telling anybody it worked.
func (b *ScheduleBook) readBack(ctx context.Context, id string) (schedule.Schedule, bool) {
	f, ok, err := b.Store.ScheduleFileByID(ctx, id)
	if err != nil || !ok {
		return schedule.Schedule{}, false
	}
	obj, err := schedule.Decode(f.Body)
	if err != nil {
		return schedule.Schedule{}, false
	}
	s, err := schedule.Parse(obj, id, b.isDirectory)
	return s, err == nil
}

// Update is `updateSchedule`: a save rewrites the whole file from the body a
// create takes, carrying `created_at`, the fields no form can show, and — only
// when the firing times did not move — `when_changed_at` and `fired_at`.
func (b *ScheduleBook) Update(ctx context.Context, id string, body map[string]any, authority ScheduleAuthority) ScheduleReply {
	if authority.Machine {
		if r := b.MachineRefusal(ctx, "PATCH", id, body); r != nil {
			return *r
		}
	}
	if _, named := body["template"]; named {
		return refusedSchedule(400, "bad_request",
			"template is taken when a schedule is made; a save keeps the stored template fields itself.")
	}
	// On the dispatch lane: a save that lands while an occurrence is being
	// dispatched would otherwise race the one-shot's `fired_at` stamp.
	b.mu.Lock()
	defer b.mu.Unlock()
	existing, ok, err := b.named(ctx, id)
	if err != nil {
		return refusedSchedule(500, "store_unreadable", "The schedule could not be read.")
	}
	if !ok {
		return refusedSchedule(404, "not_found", "No schedule named that")
	}
	carried, _ := existing.obj["task"].(map[string]any)
	obj, made, refusal := b.build(ctx, body, id, existing.s.CreatedAt, carried)
	if refusal != nil {
		return *refusal
	}
	if refusal := permissionRefusal(authority, obj, carried); refusal != nil {
		return *refusal
	}
	moves := !made.When.Same(existing.s.When)
	if made.When.Once() != existing.s.When.Once() {
		if existing.s.When.On != nil {
			return refusedSchedule(400, "bad_request", "This schedule runs once, on "+existing.s.When.On.String()+
				". A save may change when it runs, not whether it repeats: send when.on, or remove it and make a new one.")
		}
		return refusedSchedule(400, "bad_request", "This schedule repeats. A save may change when it runs, "+
			"not whether it repeats: send when.days, or remove it and make a new one.")
	}
	if !existing.s.FiredAt.IsZero() && moves {
		return refusedSchedule(409, "schedule_spent", fmt.Sprintf("This schedule was made to run once and already "+
			"ran at %d. A save may still change its title or what its session is told to do; moving when it runs "+
			"would be asking a schedule that has run for a second run. Make a new one.", existing.s.FiredAt.Unix()))
	}
	now := b.now()
	if moves {
		obj["when_changed_at"] = now.Unix()
	} else {
		if !existing.s.WhenChangedAt.IsZero() {
			obj["when_changed_at"] = existing.s.WhenChangedAt.Unix()
		}
		if !existing.s.FiredAt.IsZero() {
			obj["fired_at"] = existing.s.FiredAt.Unix()
		}
	}
	if !b.takeWriteRate() {
		return rateLimited()
	}
	raw, err := encodeFile(obj)
	if err != nil {
		return refusedSchedule(500, "write_failed", "The schedule file could not be written.")
	}
	if ok, err := b.Store.ReplaceScheduleFile(ctx, id, raw); err != nil || !ok {
		b.audit("orchestrator.schedule.updated", map[string]string{"schedule": id, "ok": "0", "why": "write_failed"})
		return refusedSchedule(500, "write_failed", "The schedule file could not be written.")
	}
	saved, ok := b.readBack(ctx, id)
	if !ok {
		_, _ = b.Store.ReplaceScheduleFile(ctx, id, existing.f.Body)
		b.audit("orchestrator.schedule.updated", map[string]string{"schedule": id, "ok": "0", "why": "unreadable"})
		return refusedSchedule(500, "write_failed",
			"The change was written and could not be read back, so the schedule you already had has been put back.")
	}
	b.audit("orchestrator.schedule.updated",
		authority.audit(permissionAudit(map[string]string{"schedule": id, "ok": "1"}, obj, carried)))
	return answered(map[string]any{"ok": true, "schedule": b.summary(saved, now)})
}

// Delete is `deleteSchedule`. Content is not read: a row nobody can parse is
// the one somebody most wants gone. Removing a schedule is not cancelling its
// task.
func (b *ScheduleBook) Delete(ctx context.Context, id string, authority ScheduleAuthority) ScheduleReply {
	if authority.Machine {
		if r := b.MachineRefusal(ctx, "DELETE", id, nil); r != nil {
			return *r
		}
	}
	if !schedule.ValidID(id) {
		return refusedSchedule(404, "not_found", "No schedule named that")
	}
	// On the dispatch lane, so a removal cannot land between the timer's last
	// look at the row and the session it opens.
	b.mu.Lock()
	defer b.mu.Unlock()
	gone, err := b.Store.DeleteScheduleFile(ctx, id)
	if err != nil {
		b.audit("orchestrator.schedule.deleted", map[string]string{"schedule": id, "ok": "0", "why": "remove_failed"})
		return refusedSchedule(500, "delete_failed", "The schedule file could not be removed.")
	}
	if !gone {
		return refusedSchedule(404, "not_found", "No schedule named that")
	}
	b.invalidMu.Lock()
	delete(b.invalidSeen, id+".json")
	b.invalidMu.Unlock()
	b.audit("orchestrator.schedule.deleted", authority.audit(map[string]string{"schedule": id, "ok": "1"}))
	return answered(map[string]any{"ok": true, "deleted": id})
}

// Run is `runSchedule`: now, ignoring `enabled` and the clock, and refusing
// while a run from this schedule is still working or a one-shot has run.
func (b *ScheduleBook) Run(ctx context.Context, id string) ScheduleReply {
	if !b.dispatchEnabled() {
		return refusedSchedule(403, "orchestrator_disabled", "Task dispatch is switched off in Settings.")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok, err := b.named(ctx, id)
	if err != nil {
		// Not knowing is not "there is none": a 404 here would be filed against
		// the caller's key for ten minutes.
		return refusedSchedule(500, "store_unreadable", "The schedule could not be read.")
	}
	if !ok {
		return refusedSchedule(404, "not_found", "No schedule named that")
	}
	if !h.s.FiredAt.IsZero() {
		return refusedSchedule(409, "schedule_spent", fmt.Sprintf(
			"This schedule was made to run once and already ran at %d. Make a new one.", h.s.FiredAt.Unix()))
	}
	runs, err := b.Store.ScheduleRuns(ctx, id)
	if err != nil {
		return refusedSchedule(500, "store_unreadable", "The schedule's runs could not be read.")
	}
	// No settling here: whether a run is finished is the broker's answer,
	// kept current by its own beat — the result collected, the timeout run —
	// and read straight off its row.
	for _, r := range runs {
		// A run whose task record is gone cannot be working; one still queued,
		// spawning or briefed is.
		if r.State != "" && !finished(r.State) {
			return refusedSchedule(409, "schedule_active", "The previous task from this schedule is still active.")
		}
	}
	b.audit("orchestrator.schedule.run", map[string]string{"schedule": id, "how": "manual"})
	// A run at or after an occurrence stands for it, so that occurrence is
	// claimed before the dispatch — the timer must not open a second session for
	// it if this process dies half way. A validation press before the scheduled
	// time consumes nothing, and so does not spend a one-shot. A refused run
	// hands the occurrence back, as the Swift app records it only on success.
	consumed := h.s.When.LatestFire(b.now(), b.loc())
	claimed := false
	if !consumed.IsZero() {
		claimed, _ = b.Store.ClaimScheduleFire(ctx, id, consumed)
	}
	taskID, dir, replayed, warning, err := b.dispatch(ctx, h.s, consumed, "manual")
	if err != nil {
		if claimed {
			_ = b.Store.RestoreScheduleFire(ctx, id, consumed, h.f.LastFire)
		}
		var refusal orchestrator.Refusal
		if errors.As(err, &refusal) {
			status := refusal.Status
			if status == 0 {
				status = 500
			}
			r := refusedSchedule(status, refusal.Code, refusal.Message)
			r.Extra = refusal.Extra
			return r
		}
		return refusedSchedule(500, "dispatch_failed", err.Error())
	}
	if !consumed.IsZero() && h.s.When.Once() {
		b.markFired(ctx, id, consumed)
	}
	body := map[string]any{"ok": true, "task_id": taskID, "task_dir": dir, "replayed": replayed}
	if warning != "" {
		body["warnings"] = []string{warning}
	}
	return answered(body)
}

// RunWebhook admits a Cloud delivery with the task id already durably chosen
// by the delivery journal. Unlike a manual validation press, a webhook honors
// enabled: it is unattended execution and must not revive a paused schedule.
func (b *ScheduleBook) RunWebhook(ctx context.Context, id, taskID string) ScheduleReply {
	if !b.dispatchEnabled() {
		return refusedSchedule(403, "orchestrator_disabled", "Task dispatch is switched off in Settings.")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok, err := b.named(ctx, id)
	if err != nil {
		return refusedSchedule(500, "binding_store_unavailable", "The schedule could not be read.")
	}
	if !ok {
		return refusedSchedule(404, "schedule_not_found", "No schedule named that")
	}
	runs, err := b.Store.ScheduleRuns(ctx, id)
	if err != nil {
		return refusedSchedule(500, "binding_store_unavailable", "The schedule's runs could not be read.")
	}
	replay := false
	for _, run := range runs {
		if run.TaskID == taskID {
			// The broker is idempotent by task id. Let it replay the exact
			// dispatch so a crash after admission advances the receipt journal.
			replay = true
			continue
		}
		if run.State != "" && !finished(run.State) {
			return refusedSchedule(409, "schedule_active", "The previous task from this schedule is still active.")
		}
	}
	if !replay && !h.s.Enabled {
		return refusedSchedule(409, "schedule_disabled", "This schedule is disabled.")
	}
	if !replay && !h.s.FiredAt.IsZero() {
		return refusedSchedule(409, "schedule_spent", "This one-time schedule already ran.")
	}
	b.audit("orchestrator.schedule.run", map[string]string{"schedule": id, "how": "webhook"})
	id, dir, replayed, warning, err := b.dispatchID(ctx, h.s, time.Time{}, "webhook", taskID)
	if err != nil {
		var refusal orchestrator.Refusal
		if errors.As(err, &refusal) {
			status := refusal.Status
			if status == 0 {
				status = 500
			}
			r := refusedSchedule(status, refusal.Code, refusal.Message)
			r.Extra = refusal.Extra
			return r
		}
		return refusedSchedule(500, "dispatch_failed", err.Error())
	}
	if h.s.When.Once() {
		b.markFired(ctx, h.s.ID, b.now())
	}
	body := map[string]any{"ok": true, "task_id": id, "task_dir": dir, "replayed": replayed}
	if warning != "" {
		body["warnings"] = []string{warning}
	}
	return answered(body)
}

// WebhookTaskState reads the broker-backed state of one webhook run.
func (b *ScheduleBook) WebhookTaskState(ctx context.Context, scheduleID, taskID string) (string, error) {
	runs, err := b.Store.ScheduleRuns(ctx, scheduleID)
	if err != nil {
		return "", err
	}
	for _, run := range runs {
		if run.TaskID == taskID {
			return run.State, nil
		}
	}
	return "", nil
}

// dispatch hands one occurrence to the broker, which runs it as a task like
// any other: the same claims arbitration, record, briefing, secret, timeout
// and result collection (D07). No rule is repeated here.
//
// The run is linked to the schedule before the broker records the task, so a
// process that dies between the two still leaves the schedule knowing it made
// that task. The link is taken back only on the broker's answer that it
// recorded nothing; a broker that could not say leaves the link, which names a
// task that either exists or reads as "record gone" — never as working.
//
// A tab that never opened is the broker's `spawn_failed`, already terminal
// and so not holding back the next occurrence; it is a run that could not
// start, and is answered as a refusal. A tab that opened and could not be
// given its first message is a run, answered with a warning: the broker's
// beat decides what became of it, and its timeout ends it either way.
func (b *ScheduleBook) dispatch(ctx context.Context, s schedule.Schedule, fire time.Time, how string) (string, string, bool, string, error) {
	return b.dispatchID(ctx, s, fire, how, newUUID())
}

func (b *ScheduleBook) dispatchID(ctx context.Context, s schedule.Schedule, fire time.Time, how, id string) (string, string, bool, string, error) {
	if b.Broker == nil {
		return id, "", false, "", errors.New("this daemon has no broker to run a schedule through")
	}
	if err := b.Store.RecordScheduleRun(ctx, id, s.ID, b.now(), fire, how); err != nil {
		return "", "", false, "", fmt.Errorf("the run could not be recorded: %w", err)
	}
	// The broker waits for the child's composer before it types, and a tab
	// that opens slowly is not this pass's fault; three minutes, as a
	// dispatch route gives it, and never the caller's own deadline.
	opening, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	out, err := b.Broker.DispatchScheduled(opening, orchestrator.ScheduledRun{
		TaskID: id, ScheduleID: s.ID, Title: s.Title, Template: s.Task, CloseTab: string(s.CloseTab),
	})
	if out.Absent {
		_ = b.Store.ForgetScheduleRun(ctx, id)
	}
	if err != nil {
		return id, "", false, "", err
	}
	record := out.Record
	if record.State == orchestrator.StateSpawnFailed {
		why := record.SpawnError
		if why == "" {
			why = record.Verdict
		}
		return id, record.Dir, out.Replayed, "", orchestrator.Refusal{
			Status: 500, Code: "spawn_failed", Message: "The session did not open: " + why,
		}
	}
	warning := ""
	if record.SpawnError != "" {
		warning = "The session opened, and its first message could not be typed into it: " + record.SpawnError
		log.Printf("schedule %s: task %s: %s", s.ID, id, warning)
	}
	payload, _ := json.Marshal(map[string]any{"schedule": s.ID, "how": how, "fire": unixOrZero(fire), "warning": warning})
	_ = b.Store.Append(ctx, store.Event{Kind: "schedule.fired", Subject: id, Payload: payload})
	return id, record.Dir, out.Replayed, warning, nil
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// markFired is `markScheduleFired`: `fired_at` onto a one-shot's own file, so
// its retirement outlives this process. Everything else is carried untouched.
func (b *ScheduleBook) markFired(ctx context.Context, id string, fired time.Time) {
	f, ok, err := b.Store.ScheduleFileByID(ctx, id)
	if err != nil || !ok {
		return
	}
	obj, err := schedule.Decode(f.Body)
	if err != nil {
		return
	}
	if _, stamped := obj["fired_at"]; stamped {
		return
	}
	// Only onto the file that still names this occurrence: a save that moved
	// the date while the session was opening has made a new occurrence, and
	// stamping it would spend a run that has not happened.
	current, err := schedule.Parse(obj, id, b.isDirectory)
	if err != nil || !current.When.Once() || current.Stale(fired) ||
		!current.When.LatestFire(fired, b.loc()).Equal(fired) {
		b.audit("orchestrator.schedule.spent", map[string]string{"schedule": id, "ok": "0", "why": "retimed"})
		return
	}
	obj["fired_at"] = fired.Unix()
	raw, err := encodeFile(obj)
	if err == nil {
		_, err = b.Store.ReplaceScheduleFile(ctx, id, raw)
	}
	ok = err == nil
	b.audit("orchestrator.schedule.spent", map[string]string{"schedule": id, "ok": boolText(ok)})
}

func boolText(ok bool) string {
	if ok {
		return "1"
	}
	return "0"
}

// ImportFile is one schedule file handed over whole, as the Swift app wrote it.
type ImportFile struct {
	Name string
	Body []byte
}

// Import takes schedule files from another holder — the Swift app's
// `~/.config/clawdline/schedules/*.json` — and stores each byte for byte.
//
// Everything the previous holder was responsible for stays its own: the row's
// first sighting is now, and its latest occurrence at or before now is recorded
// as already decided, so importing at 09:05 does not run a 09:00 the Swift app
// already ran, and does not announce it as missed either. The next occurrence
// is this daemon's.
//
// An import stores repeating schedules and any project directory, which makes
// it what a hand-written file was for the Swift app — the door the orchestrator
// token's once-only rule does not cover. So it is shut unless this daemon's
// config.json turns it on (`schedule_imports_enabled`), for the migration.
//
// A file that does not parse is still imported, and is listed as the
// `invalid` row it is — hiding it would be the one way a migration loses a
// schedule without saying so. A name already held is left alone; the same
// bytes again are reported as unchanged.
func (b *ScheduleBook) Import(ctx context.Context, files []ImportFile) ([]map[string]any, *ScheduleReply) {
	if b.ImportsEnabled == nil || !b.ImportsEnabled() {
		r := refusedSchedule(403, "schedule_imports_disabled",
			"Importing schedule files is switched off. Set \"schedule_imports_enabled\": true in this daemon's "+
				"config.json for the migration, and take it out again afterwards.")
		return nil, &r
	}
	now := b.now()
	out := []map[string]any{}
	for _, file := range files {
		row := map[string]any{"file": file.Name}
		id := strings.TrimSuffix(file.Name, ".json")
		if !strings.HasSuffix(file.Name, ".json") || !schedule.ValidID(id) {
			row["state"] = "refused"
			row["error"] = "the file name must be <schedule-id>.json with a lower-case UUID"
			out = append(out, row)
			continue
		}
		row["id"] = id
		var parsed *schedule.Schedule
		if obj, err := schedule.Decode(file.Body); err != nil {
			row["error"] = "The file does not contain a JSON object."
		} else if s, err := schedule.Parse(obj, id, b.isDirectory); err != nil {
			row["error"] = err.Error()
			if title, _ := obj["title"].(string); title != "" {
				row["title"] = title
			}
		} else {
			parsed = &s
			row["title"] = s.Title
			row["enabled"] = s.Enabled
		}
		handled := time.Time{}
		if parsed != nil {
			handled = parsed.When.LatestFire(now, b.loc())
		}
		err := b.Store.CreateScheduleFile(ctx, id, file.Body, now, handled)
		switch {
		case errors.Is(err, store.ErrScheduleExists):
			if f, ok, _ := b.Store.ScheduleFileByID(ctx, id); ok && string(f.Body) == string(file.Body) {
				row["state"] = "unchanged"
			} else {
				row["state"] = "exists"
			}
		case err != nil:
			row["state"] = "refused"
			row["error"] = "The schedule could not be stored."
		case parsed == nil:
			row["state"] = "invalid"
		default:
			row["state"] = "imported"
		}
		if parsed != nil {
			if next := parsed.When.NextFire(now, b.loc()); !next.IsZero() {
				row["next_fire"] = next.Unix()
			}
		}
		sum := sha256.Sum256(file.Body)
		row["sha256"] = hex.EncodeToString(sum[:])
		b.audit("orchestrator.schedule.imported", map[string]string{"schedule": id, "state": fmt.Sprint(row["state"])})
		out = append(out, row)
	}
	return out, nil
}

// Export is every stored schedule file, byte for byte, by the name the Swift
// app would give it.
func (b *ScheduleBook) Export(ctx context.Context) (map[string]string, error) {
	files, err := b.Store.ScheduleFiles(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range files {
		out[f.ID+".json"] = string(f.Body)
	}
	return out, nil
}

var hookPattern = regexp.MustCompile(`^swh_[0-9abcdefghjkmnpqrstvwxyz]{26}$`)

// BindWebhook is `ScheduleWebhookBindingCoordinator.bind`: the local mapping
// from an opaque Cloud hook to a schedule, made durable before the Cloud is
// asked to activate the hook, so a lost answer is repaired by replaying the
// same request id. It never accepts a trigger token.
func (b *ScheduleBook) BindWebhook(ctx context.Context, requestID, hookID, scheduleID string,
	replaceHookID *string, sender string) (int, []byte) {
	replace := any(nil)
	if replaceHookID != nil {
		replace = *replaceHookID
	}
	canonical, _ := json.Marshal(map[string]any{"hook_id": hookID, "replace_hook_id": replace, "schedule_id": scheduleID})
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	finish := func(status int, code string, body []byte) (int, []byte) {
		b.audit("schedule_webhook.bind", map[string]string{"request": trim(requestID, 64),
			"sender": trim(sender, 64), "status": fmt.Sprint(status), "result": orText(code, "accepted")})
		return status, body
	}
	failure := func(status int, code string) (int, string, []byte) {
		body, _ := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": code}})
		return status, code, body
	}
	if !schedule.ValidID(requestID) {
		return finish(failure(503, "binding_store_unavailable"))
	}
	begun, err := b.Store.BeginWebhookBind(ctx, requestID, digest, b.now())
	if err != nil {
		return finish(failure(503, "binding_store_unavailable"))
	}
	switch {
	case begun.Conflict:
		return finish(failure(409, "idempotency_conflict"))
	case !begun.Fresh && !begun.Pending:
		return finish(begun.Status, begun.Code, begun.Body)
	}
	complete := func(status int, code string, body []byte) (int, []byte) {
		if status < 500 {
			if err := b.Store.CompleteWebhookBind(ctx, requestID, digest, status, code, body, b.now()); err != nil {
				return finish(failure(503, "binding_store_unavailable"))
			}
		}
		return finish(status, code, body)
	}
	if _, ok, err := b.named(ctx, scheduleID); err != nil {
		return complete(failure(503, "binding_store_unavailable"))
	} else if !ok {
		return complete(failure(404, "schedule_not_found"))
	}
	if !hookPattern.MatchString(hookID) || (replaceHookID != nil && !hookPattern.MatchString(*replaceHookID)) {
		return complete(failure(503, "binding_store_unavailable"))
	}
	replacing := ""
	if replaceHookID != nil {
		replacing = *replaceHookID
	}
	if err := b.Store.BindScheduleWebhook(ctx, hookID, scheduleID, replacing, b.now()); err != nil {
		if errors.Is(err, store.ErrBindingConflict) {
			return complete(failure(409, "binding_conflict"))
		}
		return complete(failure(503, "binding_store_unavailable"))
	}
	if b.Activate == nil {
		return complete(failure(401, "no_machine_credential"))
	}
	revision, err := b.Activate(ctx, hookID, requestID)
	if err != nil {
		return complete(failure(503, "temporarily_unavailable"))
	}
	body, _ := json.Marshal(map[string]any{"accepted": true, "hook_id": hookID, "schedule_id": scheduleID,
		"binding_state": "active", "hook_revision": revision})
	return complete(200, "", body)
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func orText(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// newUUID is a random lower-case UUID: a schedule id, and a scheduled task's
// id, as the Swift app mints both.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("no randomness: " + err.Error())
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
