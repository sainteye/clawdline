package task

import (
	"fmt"
	"strings"
	"time"
)

// Assistant is which runtime a task is for. Claude and Codex are peers: either
// may be dispatched by the other, and neither is the default.
type Assistant string

const (
	AssistantClaude Assistant = "claude"
	AssistantCodex  Assistant = "codex"
)

// Task is one dispatched piece of work.
type Task struct {
	ID         string    `json:"task_id"`
	Assistant  Assistant `json:"assistant"`
	ProjectDir string    `json:"project_dir"`
	Brief      string    `json:"instructions"`
	// Claims are the paths this task intends to write, declared before it
	// starts. They are a dispatch-time reservation compared between roots, not
	// filesystem enforcement: nothing stops a child writing outside them, and
	// pretending otherwise would be the more dangerous lie.
	Claims    []string  `json:"claims"`
	CreatedAt time.Time `json:"created_at"`
	State     State     `json:"state"`
}

// Refusal is a typed reason a dispatch was not accepted.
//
// Every refusal names itself. A caller that reads "bad request" learns nothing
// it can act on, and the difference between "you declared no claims" and "those
// paths are already reserved" is the whole question.
type Refusal struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (r Refusal) Error() string { return r.Code + ": " + r.Detail }

// Validate checks a draft before anything is created.
//
// Claims are required rather than optional. The Swift app made them optional
// first and measured the consequence: two roots dispatched a correction of the
// same delivery six seconds apart, both isolated, and nothing refused either of
// them.
func (t Task) Validate() error {
	if t.ID == "" {
		return Refusal{"bad_task", "a task needs an id"}
	}
	switch t.Assistant {
	case AssistantClaude, AssistantCodex:
	default:
		return Refusal{"bad_task", fmt.Sprintf("unknown assistant %q", t.Assistant)}
	}
	if strings.TrimSpace(t.ProjectDir) == "" {
		return Refusal{"bad_task", "a task needs a project directory"}
	}
	if strings.TrimSpace(t.Brief) == "" {
		return Refusal{"bad_task", "a task needs instructions"}
	}
	if t.Claims == nil {
		return Refusal{"claims_required",
			"declare the paths this task will write, or an empty list to declare none"}
	}
	return nil
}

// Overlaps reports the claims two tasks share, by ancestry rather than by
// string equality: a task claiming a directory conflicts with one claiming a
// file inside it.
//
// An empty claim list reserves nothing and never conflicts. That is a real
// answer — "this task writes nothing" — and is different from declaring
// nothing at all, which Validate refuses.
func Overlaps(a, b []string) []string {
	shared := []string{}
	for _, x := range a {
		for _, y := range b {
			if covers(x, y) || covers(y, x) {
				shared = append(shared, x)
				break
			}
		}
	}
	return shared
}

func covers(parent, child string) bool {
	p := strings.TrimSuffix(parent, "/")
	c := strings.TrimSuffix(child, "/")
	return p == c || strings.HasPrefix(c, p+"/")
}

// Result is what a finished task wrote down for itself.
type Result struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// Settle maps a child's own word to the state the broker records. An
// unrecognised word is a failure rather than a success: a result nobody can
// read is not a delivery.
func Settle(r Result) State {
	switch strings.ToLower(strings.TrimSpace(r.Status)) {
	case "success", "ok", "done":
		return StateSuccess
	default:
		return StateFailure
	}
}
