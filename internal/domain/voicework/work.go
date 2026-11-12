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

type Input struct {
	ID      string `json:"id"`
	Call    string `json:"call,omitempty"`
	Text    string `json:"text"`
	At      int64  `json:"at"`
	Planned bool   `json:"planned"`
	Ready   bool   `json:"ready"`
}

type Task struct {
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
	return slices.Contains([]string{"list_sessions", "get_sessions_summary", "get_session_summary", "get_session_recap", "get_session_activity", "explain_session_reply", "get_session_notes", "get_session_screen", "list_machines", "list_projects", "get_devstack_links", "clawdline_help", "list_snippets"}, tool)
}
func KnownTool(tool string) bool {
	return IsRead(tool) || slices.Contains([]string{"send_message_to_session", "answer_session_question", "close_session", "start_session", "run_snippet", "open_session", "open_snippets", "set_speaking_speed", "set_answer_length"}, tool)
}

func (s *State) AddInput(in Input) (bool, error) {
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
	for _, id := range ids {
		found := false
		for _, in := range s.Inputs {
			if in.ID == id {
				found = true
				break
			}
		}
		if !found {
			return ErrConflict
		}
	}
	for i := range s.Inputs {
		if slices.Contains(ids, s.Inputs[i].ID) && !s.Inputs[i].Ready {
			s.Inputs[i].Ready = true
			s.Revision++
		}
	}
	return nil
}
func (s State) ReadyPending() []Input {
	out := []Input{}
	for _, in := range s.Pending() {
		if !in.Ready {
			break
		}
		out = append(out, in)
	}
	return out
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
			if !IsRead(c.Tool) || c.Tool == "get_devstack_links" && c.Args["open"] == true {
				status = "waiting_confirmation"
			}
			next.Tasks = append(next.Tasks, Task{ID: c.ID, Text: c.Text, Tool: c.Tool, Args: c.Args, Sources: slices.Clone(sources), DependsOn: c.DependsOn, Priority: c.Priority, Version: 1, Status: status, At: now, UpdatedAt: now})
		case "amend":
			if t == nil || t.Status == "running" || t.Status == "cancel_requested" || t.Status == "done" || t.Status == "outcome_unknown" {
				return ErrConflict
			}
			if !ValidText(c.Text) || !KnownTool(c.Tool) || c.Args == nil {
				return ErrInvalid
			}
			t.Text, t.Tool, t.Args, t.DependsOn, t.Priority = c.Text, c.Tool, c.Args, c.DependsOn, c.Priority
			t.Sources = append(t.Sources, sources...)
			t.Version++
			t.UpdatedAt = now
			t.Status = "queued"
			if !IsRead(t.Tool) || t.Tool == "get_devstack_links" && t.Args["open"] == true {
				t.Status = "waiting_confirmation"
			}
		case "pause", "cancel":
			if t == nil || t.Status == "done" || t.Status == "outcome_unknown" {
				return ErrConflict
			}
			if t.Status == "running" || t.Status == "cancel_requested" {
				t.Status = "cancel_requested"
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
	for i := range next.Inputs {
		if slices.Contains(sources, next.Inputs[i].ID) {
			next.Inputs[i].Planned = true
		}
	}
	if utf8.RuneCountInString(reply) > MaxTextRunes {
		return ErrInvalid
	}
	next.Reply, next.PlannerError = reply, ""
	next.PlanVersion++
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
		if t.Status != "paused" && t.Status != "failed" && !(t.Status == "outcome_unknown" && IsRead(t.Tool)) {
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
	default:
		return ErrInvalid
	}
	t.UpdatedAt = now
	s.PlanVersion++
	s.Revision++
	return nil
}

// Claim captures the exact task version. A lost write is never made safe by a lease.
func (s *State) Claim(runner string, now int64) *Task {
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
			writing = writing || !IsRead(t.Tool) || t.Tool == "get_devstack_links" && t.Args["open"] == true
			resources[resource(*t)] = true
		}
	}
	if writing || running >= MaxParallelReads {
		return nil
	}
	var picked *Task
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.Status != "queued" || resources[resource(*t)] {
			continue
		}
		if !IsRead(t.Tool) || t.Tool == "get_devstack_links" && t.Args["open"] == true {
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
	picked.Status = "running"
	picked.Runner = runner
	picked.ActionKey = fmt.Sprintf("voice:%s:%d", picked.ID, picked.Version)
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
	t.Result = result
	t.UpdatedAt = now
	s.Revision++
	return nil
}

func (s *State) Finish(id, runner, key, result, problem string, unknown bool, now int64) error {
	t := s.task(id)
	if t == nil || t.Runner != runner || t.ActionKey != key || (t.Status != "running" && t.Status != "cancel_requested") {
		return ErrConflict
	}
	if utf8.RuneCountInString(result) > MaxTextRunes || utf8.RuneCountInString(problem) > MaxTextRunes {
		return ErrInvalid
	}
	t.Result, t.Error, t.UpdatedAt = result, problem, now
	switch {
	case unknown:
		t.Status = "outcome_unknown"
	case problem != "":
		t.Status = "failed"
	case t.Tool == "send_message_to_session":
		t.Status = "waiting_result"
	default:
		t.Status = "done"
	}
	s.Revision++
	return nil
}
