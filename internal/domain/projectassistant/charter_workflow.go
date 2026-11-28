package projectassistant

import (
	"reflect"
	"strconv"
	"strings"
)

// A charter workflow is how one piece of work flows through a Project's
// team, as a small state machine: states owned by a Bot (or by any Bot), and
// labelled transitions between them. It is part of the charter document and
// replaces the free-text "how one piece of work flows" section. Only the lead
// Bot proposes it and the person applies it, or a terminal Session writes it
// on the person's words; nobody edits the graph by hand. Every Bot of the
// Project hears it as text rendered from the graph (docs/bots.md, "Workflow").
const (
	// MaxCharterWorkflowStates bounds the states of one workflow.
	MaxCharterWorkflowStates = 40
	// MaxCharterWorkflowTransitions bounds the transitions of one workflow.
	MaxCharterWorkflowTransitions = 80
	// MaxCharterWorkflowTitleBytes bounds a state's title and a
	// transition's label.
	MaxCharterWorkflowTitleBytes = 80
	// MaxCharterWorkflowNoteBytes bounds a state's note.
	MaxCharterWorkflowNoteBytes = 300
	// MaxCharterWorkflowIDBytes bounds a state's identifier.
	MaxCharterWorkflowIDBytes = 40
)

// WorkflowStateKind says where a state stands in the flow.
type WorkflowStateKind string

const (
	WorkflowStart WorkflowStateKind = "start"
	WorkflowStep  WorkflowStateKind = "step"
	WorkflowDone  WorkflowStateKind = "done"
)

// WorkflowTransitionKind says what a transition means for drawing: an
// ordinary next step, the way on after a check passes, or a way back.
type WorkflowTransitionKind string

const (
	WorkflowNormal WorkflowTransitionKind = "normal"
	WorkflowPass   WorkflowTransitionKind = "pass"
	WorkflowReturn WorkflowTransitionKind = "return"
)

// CharterWorkflow is one version's workflow. A version without one has nil.
type CharterWorkflow struct {
	States      []WorkflowState      `json:"states"`
	Transitions []WorkflowTransition `json:"transitions"`
}

// WorkflowState is one step. BotID empty means any Bot of the Project.
type WorkflowState struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	BotID string            `json:"bot_id"`
	Kind  WorkflowStateKind `json:"kind"`
	Note  string            `json:"note"`
}

// WorkflowTransition is one way from a state to another. Label may be empty.
type WorkflowTransition struct {
	From  string                 `json:"from"`
	To    string                 `json:"to"`
	Label string                 `json:"label"`
	Kind  WorkflowTransitionKind `json:"kind"`
}

// SameWorkflow says whether two versions carry the same workflow; nil and
// an empty workflow both mean none.
func SameWorkflow(a, b *CharterWorkflow) bool {
	empty := func(w *CharterWorkflow) bool { return w == nil || len(w.States) == 0 && len(w.Transitions) == 0 }
	if empty(a) || empty(b) {
		return empty(a) == empty(b)
	}
	return reflect.DeepEqual(a.States, b.States) && reflect.DeepEqual(a.Transitions, b.Transitions)
}

func validWorkflowID(id string) bool {
	if id == "" || len(id) > MaxCharterWorkflowIDBytes {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// workflowTransitionName names a transition in a refusal: from->to, or its
// position when an endpoint is not a valid id.
func workflowTransitionName(i int, t WorkflowTransition) string {
	if validWorkflowID(t.From) && validWorkflowID(t.To) {
		return t.From + "->" + t.To
	}
	return "#" + strconv.Itoa(i+1)
}

// ValidateCharterWorkflow checks a whole workflow. visible, when set, says
// whether a Bot id is a visible Bot of the Project; nil skips that check (a
// restore brings back what was valid then). A refusal's field names the
// rule and, after a colon, the state id or from->to transition that broke
// it, so the lead's tool call and the CLI can say what to fix.
func ValidateCharterWorkflow(w CharterWorkflow, visible func(string) bool) error {
	if len(w.States) > MaxCharterWorkflowStates {
		return fail(CapacityFull, "charter_workflow_states")
	}
	if len(w.Transitions) > MaxCharterWorkflowTransitions {
		return fail(CapacityFull, "charter_workflow_transitions")
	}
	if len(w.States) == 0 {
		return fail(Invalid, "workflow_start")
	}
	states := map[string]WorkflowState{}
	start := ""
	done := 0
	for i, s := range w.States {
		if !validWorkflowID(s.ID) {
			if len(s.ID) > MaxCharterWorkflowIDBytes {
				return fail(CapacityFull, "charter_workflow_id:#"+strconv.Itoa(i+1))
			}
			return fail(Invalid, "workflow_state_id:#"+strconv.Itoa(i+1))
		}
		if _, dup := states[s.ID]; dup {
			return fail(Invalid, "workflow_state_id:"+s.ID)
		}
		states[s.ID] = s
		if strings.TrimSpace(s.Title) == "" || !validOneLine(s.Title) {
			return fail(Invalid, "workflow_state_title:"+s.ID)
		}
		if len(s.Title) > MaxCharterWorkflowTitleBytes {
			return fail(CapacityFull, "charter_workflow_title:"+s.ID)
		}
		if !validConflictText(s.Note, false) {
			return fail(Invalid, "workflow_state_note:"+s.ID)
		}
		if len(s.Note) > MaxCharterWorkflowNoteBytes {
			return fail(CapacityFull, "charter_workflow_note:"+s.ID)
		}
		if s.BotID != "" && (len(s.BotID) > MaxTowerFloorProjectBytes || !validOneLine(s.BotID) || visible != nil && !visible(s.BotID)) {
			return fail(Invalid, "workflow_state_bot:"+s.ID)
		}
		switch s.Kind {
		case WorkflowStart:
			if start != "" {
				return fail(Invalid, "workflow_start:"+s.ID)
			}
			start = s.ID
		case WorkflowDone:
			done++
		case WorkflowStep:
		default:
			return fail(Invalid, "workflow_state_kind:"+s.ID)
		}
	}
	if start == "" {
		return fail(Invalid, "workflow_start")
	}
	if done == 0 {
		return fail(Invalid, "workflow_done")
	}
	next := map[string][]string{}
	for i, t := range w.Transitions {
		name := workflowTransitionName(i, t)
		if _, ok := states[t.From]; !ok {
			return fail(Invalid, "workflow_transition_from:"+name)
		}
		if _, ok := states[t.To]; !ok {
			return fail(Invalid, "workflow_transition_to:"+name)
		}
		if states[t.From].Kind == WorkflowDone {
			return fail(Invalid, "workflow_done_exit:"+name)
		}
		if !validOneLine(t.Label) {
			return fail(Invalid, "workflow_transition_label:"+name)
		}
		if len(t.Label) > MaxCharterWorkflowTitleBytes {
			return fail(CapacityFull, "charter_workflow_title:"+name)
		}
		switch t.Kind {
		case WorkflowNormal, WorkflowPass, WorkflowReturn:
		default:
			return fail(Invalid, "workflow_transition_kind:"+name)
		}
		next[t.From] = append(next[t.From], t.To)
	}
	reached := map[string]bool{start: true}
	for queue := []string{start}; len(queue) > 0; queue = queue[1:] {
		for _, to := range next[queue[0]] {
			if !reached[to] {
				reached[to] = true
				queue = append(queue, to)
			}
		}
	}
	for _, s := range w.States {
		if !reached[s.ID] {
			return fail(Invalid, "workflow_unreachable:"+s.ID)
		}
	}
	return nil
}

// workflowOrder lists the states breadth-first from start, each once, in
// the order their transitions are listed. A state no transition reaches
// (which validation refuses) follows in the listed order, so rendering
// never drops one.
func workflowOrder(w CharterWorkflow) []WorkflowState {
	byID := map[string]WorkflowState{}
	start := ""
	for _, s := range w.States {
		byID[s.ID] = s
		if s.Kind == WorkflowStart && start == "" {
			start = s.ID
		}
	}
	seen := map[string]bool{}
	out := []WorkflowState{}
	visit := func(id string) {
		for queue := []string{id}; len(queue) > 0; queue = queue[1:] {
			s, ok := byID[queue[0]]
			if !ok || seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			out = append(out, s)
			for _, t := range w.Transitions {
				if t.From == s.ID && !seen[t.To] {
					queue = append(queue, t.To)
				}
			}
		}
	}
	if start != "" {
		visit(start)
	}
	for _, s := range w.States {
		visit(s.ID)
	}
	return out
}

const workflowRule = "How one piece of work flows in this team. Follow these steps; each line is a step, who does it, and where each outcome goes next."

// RenderCharterWorkflow is the workflow as every Bot hears it: one line per
// state in breadth-first order from start. name gives a Bot's display name
// at render time; nil, or an empty answer, falls back to the id.
func RenderCharterWorkflow(w *CharterWorkflow, name func(string) string) string {
	if w == nil || len(w.States) == 0 {
		return ""
	}
	titles := map[string]string{}
	for _, s := range w.States {
		titles[s.ID] = s.Title
	}
	var b strings.Builder
	b.WriteString(workflowRule)
	for _, s := range workflowOrder(*w) {
		who := "any Bot"
		if s.BotID != "" {
			who = s.BotID
			if name != nil {
				if n := strings.TrimSpace(name(s.BotID)); n != "" {
					who = n
				}
			}
		}
		if s.Kind == WorkflowDone {
			who = "done"
		}
		b.WriteString("\n- " + s.Title + "（" + who + "）")
		ways := []string{}
		for _, t := range w.Transitions {
			if t.From != s.ID {
				continue
			}
			way := "→ " + titles[t.To]
			if t.Label != "" {
				way = t.Label + " " + way
			}
			ways = append(ways, way)
		}
		if len(ways) > 0 {
			b.WriteString("：" + strings.Join(ways, "；"))
		}
		if s.Note != "" {
			b.WriteString("\n  " + strings.ReplaceAll(s.Note, "\n", " "))
		}
	}
	return b.String()
}

// CharterWorkflowEditContext is the structured context a change request
// aimed at the workflow carries to the lead Bot: the current workflow as
// JSON and the person's words as quoted data, never authority.
func CharterWorkflowEditContext(projectID string, version int64, workflowJSON, words string) string {
	if strings.TrimSpace(workflowJSON) == "" {
		workflowJSON = "null"
	}
	v := strconv.FormatInt(version, 10)
	return "<charter_workflow_edit_request project=\"" + projectID + "\" version=\"" + v + "\">\n" +
		"The person asked, from this Project's charter page, to change how one piece of work flows: the team workflow, a state machine of states and transitions. Read the current workflow below (null when there is none), then call propose_charter_workflow once with this project, base_version " + v + ", the whole new workflow and a one-line summary of what you changed. Exactly one start state, at least one done state, every state reachable from start, no transition out of a done state; bot_id is a Bot of this Project or empty for any Bot. Nothing changes until the person presses Apply. Reply briefly in their language with what you changed.\n" +
		"<current_workflow>\n" + xmlText(workflowJSON) + "\n</current_workflow>\n" +
		"<person_request>\n" + xmlText(words) + "\n</person_request>\n</charter_workflow_edit_request>\n\n"
}

// CharterEditTarget says which part of the charter a change request is for.
type CharterEditTarget string

const (
	CharterEditText     CharterEditTarget = ""
	CharterEditWorkflow CharterEditTarget = "workflow"
)

// CharterProposalKind says what a lead proposal replaces.
type CharterProposalKind string

const (
	CharterProposalText     CharterProposalKind = "text"
	CharterProposalPlaybook CharterProposalKind = "playbook"
	CharterProposalWorkflow CharterProposalKind = "workflow"
)
