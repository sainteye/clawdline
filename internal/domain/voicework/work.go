// Package voicework owns the voice assistant's durable inbox and work ledger.
// It neither runs tools nor grants authority to run them.
package voicework

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxPlanRounds     = 4
	MaxProgress       = 8
	MaxRecords        = 256
	MaxTextRunes      = 4000
	MaxStateBytes     = 2 << 20
	MaxPlanInputs     = 8
	MaxParallelReads  = 2
	LeaseSecondsLimit = 90
	MaxRequestBytes   = 64 << 10
)

var (
	ErrConflict = errors.New("voice_work_conflict")
	ErrFull     = errors.New("voice_work_full")
	ErrInvalid  = errors.New("voice_work_invalid")
	ErrUnknown  = errors.New("voice_work_outcome_unknown")
)

// Focus is captured with the utterance, never read from a later call's screen.
type Focus struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Machine      string `json:"machine,omitempty"`
	Conversation string `json:"conversation,omitempty"`
}

type Input struct {
	Cancelled     bool     `json:"cancelled,omitempty"`
	Context       string   `json:"context,omitempty"`
	Parked        bool     `json:"parked,omitempty"`
	Deferred      bool     `json:"deferred,omitempty"`
	Focus         *Focus   `json:"focus,omitempty"`
	PlanningError string   `json:"planning_error,omitempty"`
	PlanningStage string   `json:"planning_stage,omitempty"`
	ID            string   `json:"id"`
	Call          string   `json:"call,omitempty"`
	Text          string   `json:"text"`
	At            int64    `json:"at"`
	Planned       bool     `json:"planned"`
	Ready         bool     `json:"ready"`
	WaitFor       []string `json:"wait_for,omitempty"`
	PlanRounds    int      `json:"plan_rounds,omitempty"`
}

type Prepared struct {
	Sequence int64  `json:"sequence"`
	Value    string `json:"value"`
}

type Task struct {
	Call            string         `json:"call,omitempty"`
	StopAction      string         `json:"stop_action,omitempty"`
	EffectStarted   bool           `json:"effect_started,omitempty"`
	Attempt         int64          `json:"attempt,omitempty"`
	Progress        []Prepared     `json:"progress,omitempty"`
	ResultRevision  int64          `json:"result_revision,omitempty"`
	ID              string         `json:"id"`
	Text            string         `json:"text"`
	Tool            string         `json:"tool"`
	Args            map[string]any `json:"args"`
	Sources         []string       `json:"sources"`
	DependsOn       []string       `json:"depends_on"`
	Priority        int            `json:"priority"`
	ApprovedVersion int64          `json:"approved_version,omitempty"`
	Version         int64          `json:"version"`
	Status          string         `json:"status"`
	Result          string         `json:"result,omitempty"`
	Error           string         `json:"error,omitempty"`
	At              int64          `json:"at"`
	UpdatedAt       int64          `json:"updated_at"`
	Runner          string         `json:"runner,omitempty"`
	ActionKey       string         `json:"action_key,omitempty"`
	LeaseUntil      int64          `json:"lease_until,omitempty"`
}

type State struct {
	ID           string  `json:"id"`
	Revision     int64   `json:"revision"`
	PlanVersion  int64   `json:"plan_version"`
	Inputs       []Input `json:"inputs"`
	Tasks        []Task  `json:"tasks"`
	Reply        string  `json:"reply,omitempty"`
	PlannerError string  `json:"planner_error,omitempty"`
}

type Change struct {
	Op        string         `json:"op"`
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	DependsOn []string       `json:"depends_on"`
	Priority  int            `json:"priority"`
}

func New(id string) State { return State{ID: id, Revision: 1, Inputs: []Input{}, Tasks: []Task{}} }

func ValidText(s string) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= MaxTextRunes
}

// IsRead is deliberately closed: a model cannot turn an unknown tool into a read.
func IsRead(tool string) bool {
	return slices.Contains([]string{"list_sessions", "get_sessions_summary", "get_session_summary", "get_session_recap", "get_session_activity", "explain_session_reply", "get_session_notes", "get_session_updates", "get_session_screen", "list_machines", "list_projects", "get_devstack_links", "clawdline_help", "list_snippets"}, tool)
}
func ReadTask(t Task) bool {
	return IsRead(t.Tool) && !(t.Tool == "get_devstack_links" && t.Args["open"] == true)
}
func CallScoped(tool string, args map[string]any) bool {
	return slices.Contains([]string{"set_speaking_speed", "set_answer_length", "open_session", "open_snippets", "run_snippet"}, tool) || tool == "get_devstack_links" && args["open"] == true
}
func KnownTool(tool string) bool {
	return IsRead(tool) || slices.Contains([]string{"send_message_to_session", "answer_session_question", "close_session", "start_session", "run_snippet", "open_session", "open_snippets", "set_speaking_speed", "set_answer_length", "smart_rename_sessions"}, tool)
}

func (s *State) AddInput(in Input) (bool, error) {
	if in.Focus != nil {
		if !ValidText(in.Focus.ID) {
			return false, ErrInvalid
		}
		for _, v := range []string{in.Focus.Label, in.Focus.Machine, in.Focus.Conversation} {
			if v != "" && !ValidText(v) {
				return false, ErrInvalid
			}
		}
		held := *in.Focus
		in.Focus = &held
	}
	s.isolateLegacyFailure()
	if in.Context != "" && !ValidText(in.Context) {
		return false, ErrInvalid
	}
	if !ValidText(in.ID) || in.Call != "" && !ValidText(in.Call) || !ValidText(in.Text) {
		return false, ErrInvalid
	}
	for _, old := range s.Inputs {
		if old.ID == in.ID {
			if old.Text != in.Text || old.Call != in.Call {
				return false, ErrConflict
			}
			return false, nil
		}
	}
	if len(s.Inputs) >= MaxRecords {
		return false, ErrFull
	}
	in.Planned = false
	s.Inputs = append(s.Inputs, in)
	s.Revision++
	return true, nil
}

func (s State) Pending() []Input {
	out := []Input{}
	for _, in := range s.Inputs {
		if !in.Planned {
			out = append(out, in)
			if len(out) == MaxPlanInputs {
				break
			}
		}
	}
	return out
}

// Ready binds an explicitly supplied directory to exactly the saved inputs it
// was fetched for. A newer utterance cannot run against an older catalogue.
func (s *State) Ready(ids []string) error {
	s.isolateLegacyFailure()
	for _, id := range ids {
		found := false
		for _, in := range s.Inputs {
			if in.ID == id && !in.Planned {
				found = true
				break
			}
		}
		if !found {
			return ErrConflict
		}
	}
	for i := range s.Inputs {
		if slices.Contains(ids, s.Inputs[i].ID) && (!s.Inputs[i].Ready || s.Inputs[i].Deferred) {
			s.Inputs[i].Deferred = false
			s.Inputs[i].Parked = false
			s.Inputs[i].Ready = true
			s.Revision++
		}
	}
	return nil
}
func (s State) ReadyPending() []Input {
	out := []Input{}
	for _, in := range s.Inputs {
		if in.Planned || in.Deferred || in.PlanningError != "" {
			continue
		}
		if !in.Ready {
			break
		}
		out = append(out, in)
		if len(out) == MaxPlanInputs {
			break
		}
	}
	return out
}

// Old stores have only one workspace error. Isolate those old pending inputs
// before adding anything new; a fresh request is not a retry of earlier work.
func (s *State) isolateLegacyFailure() {
	if s.PlannerError == "" {
		return
	}
	for _, in := range s.Inputs {
		if !in.Planned && in.PlanningError != "" {
			return
		}
	}
	for i := range s.Inputs {
		in := &s.Inputs[i]
		if !in.Planned && !in.Deferred {
			in.PlanningError = s.PlannerError
			in.PlanningStage = "legacy"
		}
	}
}

func (s *State) FailPlanning(ids []string, code, stage string) {
	changed := false
	for i := range s.Inputs {
		if slices.Contains(ids, s.Inputs[i].ID) && !s.Inputs[i].Planned {
			s.Inputs[i].PlanningError = code
			changed = true
			s.Inputs[i].PlanningStage = stage
		}
	}
	if changed {
		s.PlannerError = code
		s.Revision++
	}
}

func (s *State) RetryPlanning(ids []string) error {
	s.isolateLegacyFailure()
	for _, id := range ids {
		found := false
		for _, in := range s.Inputs {
			if in.ID == id && !in.Planned {
				found = true
			}
		}
		if !found {
			return ErrConflict
		}
	}
	for i := range s.Inputs {
		if slices.Contains(ids, s.Inputs[i].ID) {
			s.Inputs[i].PlanningError = ""
			s.Inputs[i].PlanningStage = ""
			s.Inputs[i].PlanRounds = 0
		}
	}
	s.PlannerError = ""
	for _, in := range s.Inputs {
		if !in.Planned && in.PlanningError != "" {
			s.PlannerError = in.PlanningError
			break
		}
	}
	s.Revision++
	return nil
}

// Answer only resolves a conversational input; it cannot complete a work task.
func (s *State) Answer(id, call string) error {
	for i := range s.Inputs {
		in := &s.Inputs[i]
		if in.ID == id {
			if in.Call != call || !in.Deferred {
				return ErrConflict
			}
			if !in.Planned {
				in.Planned = true
				s.Revision++
			}
			return nil
		}
	}
	return ErrInvalid
}

// Dismiss is an explicit inbox action. It retains the words and never changes
// a task's approval or completion state.
func (s *State) Dismiss(id string) error {
	for i := range s.Inputs {
		if s.Inputs[i].ID == id {
			in := &s.Inputs[i]
			if in.Planned {
				return ErrConflict
			}
			in.Cancelled = true
			in.Planned = true
			in.WaitFor = nil
			s.PlannerError = ""
			for _, other := range s.Inputs {
				if !other.Planned && other.PlanningError != "" {
					s.PlannerError = other.PlanningError
					break
				}
			}
			s.Revision++
			return nil
		}
	}
	return ErrInvalid
}

// Park retains an interrupted conversation for explicit later submission. It
// does not cancel queued work or claim the person's request was completed.
func (s *State) Park(id, call string) error {
	for i := range s.Inputs {
		in := &s.Inputs[i]
		if in.ID == id {
			if in.Call != call || !in.Deferred {
				return ErrConflict
			}
			if !in.Parked && !in.Planned {
				in.Parked = true
				s.Revision++
			}
			return nil
		}
	}
	return ErrInvalid
}
func (s *State) ParkCall(call string) error {
	if !ValidText(call) {
		return ErrInvalid
	}
	for i := range s.Inputs {
		if s.Inputs[i].Call == call && s.Inputs[i].Deferred && !s.Inputs[i].Planned && !s.Inputs[i].Parked {
			s.Inputs[i].Parked = true
			s.Revision++
		}
	}
	return nil
}

func (s *State) task(id string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			return &s.Tasks[i]
		}
	}
	return nil
}

// Apply changes a copy first. A malformed dependency or stale plan changes nothing.
// New inbox entries do not invalidate an older plan for an already received prefix.
func (s *State) Apply(expected int64, sources []string, changes []Change, reply string, now int64) error {
	return s.ApplyPlan(expected, sources, changes, reply, nil, now)
}

func (s *State) ApplyPlan(expected int64, sources []string, changes []Change, reply string, waitFor []string, now int64) error {
	if s.PlanVersion != expected {
		return ErrConflict
	}
	if len(sources) == 0 {
		return ErrInvalid
	}
	raw, _ := json.Marshal(s)
	var next State
	_ = json.Unmarshal(raw, &next)
	for _, id := range sources {
		found := false
		for _, in := range next.Inputs {
			if in.ID == id && !in.Planned && in.Ready {
				found = true
			}
		}
		if !found {
			return ErrConflict
		}
	}
	call := ""
	for _, in := range next.Inputs {
		if slices.Contains(sources, in.ID) && in.Call != "" {
			if call != "" && call != in.Call {
				call = "ambiguous"
				break
			}
			call = in.Call
		}
	}
	for _, c := range changes {
		t := next.task(c.ID)
		switch c.Op {
		case "add":
			if t != nil || c.ID == "" || !ValidText(c.Text) || !KnownTool(c.Tool) || c.Args == nil {
				return ErrInvalid
			}
			if len(next.Tasks) >= MaxRecords {
				return ErrFull
			}
			status := "queued"
			if !ReadTask(Task{Tool: c.Tool, Args: c.Args}) {
				status = "waiting_confirmation"
			}
			taskCall := ""
			if CallScoped(c.Tool, c.Args) {
				if call == "ambiguous" {
					return ErrInvalid
				}
				taskCall = call
			}
			next.Tasks = append(next.Tasks, Task{Call: taskCall, ID: c.ID, Text: c.Text, Tool: c.Tool, Args: c.Args, Sources: slices.Clone(sources), DependsOn: c.DependsOn, Priority: c.Priority, Version: 1, Status: status, At: now, UpdatedAt: now})
		case "amend":
			if t == nil || t.Status == "running" || t.Status == "cancel_requested" || t.Status == "done" || t.Status == "outcome_unknown" {
				return ErrConflict
			}
			if !ValidText(c.Text) || !KnownTool(c.Tool) || c.Args == nil {
				return ErrInvalid
			}
			t.Text, t.Tool, t.Args, t.DependsOn, t.Priority = c.Text, c.Tool, c.Args, c.DependsOn, c.Priority
			t.Sources = append(t.Sources, sources...)
			if CallScoped(c.Tool, c.Args) {
				if call == "ambiguous" {
					return ErrInvalid
				}
				t.Call = call
			} else {
				t.Call = ""
			}
			t.Version++
			t.Result = ""
			t.Error = ""
			t.Progress = nil
			t.ResultRevision = 0
			t.EffectStarted = false
			t.Runner = ""
			t.ActionKey = ""
			t.LeaseUntil = 0
			t.StopAction = ""
			t.UpdatedAt = now
			t.Status = "queued"
			if !ReadTask(*t) {
				t.Status = "waiting_confirmation"
			}
		case "pause", "cancel":
			if t == nil || t.Status == "done" || t.Status == "outcome_unknown" {
				return ErrConflict
			}
			if t.Status == "running" || t.Status == "cancel_requested" {
				t.Status = "cancel_requested"
				t.StopAction = c.Op
			} else if c.Op == "pause" {
				t.Status = "paused"
			} else {
				t.Status = "cancelled"
			}
			t.UpdatedAt = now
		case "priority":
			if t == nil {
				return ErrInvalid
			}
			t.Priority = c.Priority
			t.UpdatedAt = now
		default:
			return ErrInvalid
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return false
		}
		if visited[id] {
			return true
		}
		t := next.task(id)
		if t == nil {
			return false
		}
		visiting[id] = true
		for _, d := range t.DependsOn {
			if !visit(d) {
				return false
			}
		}
		delete(visiting, id)
		visited[id] = true
		return true
	}
	for _, t := range next.Tasks {
		if !visit(t.ID) {
			return ErrInvalid
		}
	}
	for _, id := range waitFor {
		t := next.task(id)
		if t == nil || !ReadTask(*t) || t.Status != "queued" && t.Status != "running" {
			return ErrInvalid
		}
	}
	for i := range next.Inputs {
		if slices.Contains(sources, next.Inputs[i].ID) {
			if next.Inputs[i].PlanRounds >= MaxPlanRounds {
				return ErrFull
			}
			next.Inputs[i].PlanRounds++
			next.Inputs[i].PlanningError = ""
			next.Inputs[i].PlanningStage = ""
			next.Inputs[i].WaitFor = slices.Clone(waitFor)
			next.Inputs[i].Planned = true
		}
	}
	if utf8.RuneCountInString(reply) > MaxTextRunes {
		return ErrInvalid
	}
	next.Reply, next.PlannerError = reply, ""
	for _, in := range next.Inputs {
		if !in.Planned && in.PlanningError != "" {
			next.PlannerError = in.PlanningError
			break
		}
	}
	next.PlanVersion++
	next.releaseContinuations()
	next.Revision++
	*s = next
	return nil
}

// Edit is a person's explicit current-version action, not a model completion.
func (s *State) Edit(id string, version int64, action string, now int64) error {
	t := s.task(id)
	if t == nil {
		return ErrInvalid
	}
	if t.Version != version {
		return ErrConflict
	}
	switch action {
	case "confirm":
		if t.Status != "waiting_confirmation" {
			return ErrConflict
		}
		t.Status = "queued"
		t.ApprovedVersion = t.Version
	case "resume":
		if t.Status != "paused" && t.Status != "failed" && !(t.Status == "outcome_unknown" && ReadTask(*t)) {
			return ErrConflict
		}
		t.Status = "queued"
		t.Error = ""
	case "pause", "cancel":
		if t.Status == "done" || t.Status == "outcome_unknown" {
			return ErrConflict
		}
		if t.Status == "running" || t.Status == "cancel_requested" {
			t.Status = "cancel_requested"
			t.StopAction = action
		} else if action == "pause" {
			t.Status = "paused"
		} else {
			t.Status = "cancelled"
		}
	case "complete":
		if t.Status != "waiting_result" && t.Status != "outcome_unknown" {
			return ErrConflict
		}
		t.Status = "done"
		t.Result = "Completed by the person."
		t.ResultRevision = s.Revision + 1
	default:
		return ErrInvalid
	}
	s.releaseContinuations()
	t.UpdatedAt = now
	s.PlanVersion++
	s.Revision++
	return nil
}

// Claim captures the exact task version. A lost write is never made safe by a lease.
func (s *State) Claim(runner string, now int64) *Task { return s.ClaimFor(runner, "", nil, now) }

// ClaimFor skips work this browser cannot execute, including another live call's settings.
func (s *State) ClaimFor(runner, call string, tools []string, now int64) *Task {
	if runner == "" {
		return nil
	}
	running := 0
	writing := false
	resources := map[string]bool{}
	resource := func(t Task) string { return fmt.Sprint(t.Args["machine_id"], "/", t.Args["session_id"]) }
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if (t.Status == "running" || t.Status == "cancel_requested") && t.LeaseUntil < now {
			t.Status = "outcome_unknown"
			t.Error = "The executor stopped reporting; check the result before retrying."
			s.Revision++
		}
		if t.Status == "running" || t.Status == "cancel_requested" {
			running++
			writing = writing || !ReadTask(*t)
			resources[resource(*t)] = true
		}
	}
	if writing || running >= MaxParallelReads {
		return nil
	}
	var picked *Task
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.Status != "queued" || resources[resource(*t)] || tools != nil && !slices.Contains(tools, t.Tool) || t.Call != "" && t.Call != call {
			continue
		}
		if !ReadTask(*t) {
			if t.ApprovedVersion != t.Version {
				t.Status = "waiting_confirmation"
				s.Revision++
				continue
			}
			if running > 0 || len(s.Pending()) > 0 {
				continue
			}
		}
		ready := true
		for _, id := range t.DependsOn {
			d := s.task(id)
			if d == nil || d.Status != "done" {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		if picked == nil || t.Priority > picked.Priority {
			picked = t
		}
	}
	if picked == nil {
		return nil
	}
	picked.Attempt++
	picked.Progress = nil
	picked.Result = ""
	picked.Error = ""
	picked.ResultRevision = 0
	picked.EffectStarted = false
	picked.StopAction = ""
	picked.Status = "running"
	picked.Runner = runner
	picked.ActionKey = fmt.Sprintf("voice:%s:%d:%d", picked.ID, picked.Version, picked.Attempt)
	picked.LeaseUntil = now + LeaseSecondsLimit
	picked.UpdatedAt = now
	s.Revision++
	copy := *picked
	return &copy
}

func (s *State) Heartbeat(runner string, now int64) {
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.Runner == runner && (t.Status == "running" || t.Status == "cancel_requested") {
			t.LeaseUntil = now + LeaseSecondsLimit
		}
	}
}

// Progress is evidence from the current executor, not completion.
func (s *State) Progress(id, runner, key, result string, now int64) error {
	t := s.task(id)
	if t == nil || t.Runner != runner || t.ActionKey != key || (t.Status != "running" && t.Status != "cancel_requested") {
		return ErrConflict
	}
	if utf8.RuneCountInString(result) > MaxTextRunes {
		return ErrInvalid
	}
	if len(t.Progress) >= MaxProgress {
		return ErrFull
	}
	t.Progress = append(t.Progress, Prepared{Sequence: int64(len(t.Progress) + 1), Value: result})
	t.Result = result
	t.UpdatedAt = now
	s.Revision++
	return nil
}

func (s *State) Finish(id, runner, key, result, problem string, unknown bool, now int64) error {
	s.isolateLegacyFailure()
	t := s.task(id)
	if t == nil || t.Runner != runner || t.ActionKey != key || (t.Status != "running" && t.Status != "cancel_requested") {
		return ErrConflict
	}
	if utf8.RuneCountInString(result) > MaxTextRunes || utf8.RuneCountInString(problem) > MaxTextRunes {
		return ErrInvalid
	}
	t.Result, t.Error, t.UpdatedAt = result, problem, now
	t.ResultRevision = s.Revision + 1
	switch {
	case t.Status == "cancel_requested" && problem == "voice_work_stopped" && !unknown && !t.EffectStarted:
		t.Status = "cancelled"
		if t.StopAction == "pause" {
			t.Status = "paused"
		}
		t.Error = ""
	case unknown || t.EffectStarted && problem == "voice_work_stopped":
		t.Status = "outcome_unknown"
	case problem != "":
		t.Status = "failed"
	case t.Tool == "send_message_to_session":
		t.Status = "waiting_result"
	default:
		t.Status = "done"
	}
	s.releaseContinuations()
	s.Revision++
	return nil
}

// BeginEffect serializes cancellation against admission to an external effect.
// Admission is not execution: a lost response thereafter remains unknown.
func (s *State) BeginEffect(id, runner, key string, now int64) error {
	t := s.task(id)
	if t == nil || t.Runner != runner || t.ActionKey != key || t.Status != "running" || t.ApprovedVersion != t.Version || len(s.Pending()) > 0 {
		return ErrConflict
	}
	t.EffectStarted = true
	t.UpdatedAt = now
	s.Revision++
	return nil
}
func (s *State) releaseContinuations() {
	for i := range s.Inputs {
		in := &s.Inputs[i]
		if len(in.WaitFor) == 0 {
			continue
		}
		ready := true
		for _, id := range in.WaitFor {
			t := s.task(id)
			if t == nil || t.Status == "cancelled" || t.Status == "cancel_requested" && t.StopAction != "pause" {
				in.WaitFor = nil
				ready = false
				break
			}
			if t.Status != "done" && t.Status != "failed" {
				ready = false
			}
		}
		if ready {
			in.Planned = false
			in.Ready = true
			in.WaitFor = nil
		}
	}
}
