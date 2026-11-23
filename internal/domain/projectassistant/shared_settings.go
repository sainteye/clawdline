package projectassistant

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"
)

// Machine-wide Bots settings: dispatch rules that pick the agent for a work
// kind, and memory text every Bot on the machine reads. Both lists start
// empty, and an empty list keeps the behavior from before this feature.
const (
	MaxSharedMemoryEntries    = 20
	MaxSharedMemoryEntryBytes = 1024
	MaxSharedMemoryTotalBytes = 4096
	MaxDispatchRules          = 12
	MaxDispatchRuleAgents     = 4
	// MaxSharedSettingsBodyBytes bounds one whole-document write.
	MaxSharedSettingsBodyBytes = 64 << 10
)

const (
	maxDispatchKindBytes = 64
	maxDispatchHintBytes = 300
	maxDispatchModel     = 64
)

// SharedScope selects Projects. All includes Projects added later and is the
// default. A row whose listed Projects were all removed applies nowhere; it
// is never widened back to every Project.
type SharedScope struct {
	All      bool     `json:"all"`
	Projects []string `json:"projects,omitempty"`
}

func (s SharedScope) Applies(projectID string) bool {
	return s.All || projectID != "" && slices.Contains(s.Projects, projectID)
}

// DispatchRuleAgent is one candidate. Model and Effort are optional; empty
// keeps the agent's own default.
type DispatchRuleAgent struct {
	Agent  Agent  `json:"agent"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// DispatchRule maps a work kind the Bot names to an ordered agent list. The
// first agent available on this machine runs the work (ordered fallback), so
// the choice is the person's and can be predicted from the table.
type DispatchRule struct {
	ID     string              `json:"id"`
	Kind   string              `json:"kind"`
	Hint   string              `json:"hint,omitempty"`
	Agents []DispatchRuleAgent `json:"agents"`
	Scope  SharedScope         `json:"scope"`
}

type SharedMemoryEntry struct {
	ID    string      `json:"id"`
	Text  string      `json:"text"`
	Scope SharedScope `json:"scope"`
}

// ProjectSettingsOverride is reserved for a Project that replaces the
// machine's rules for itself. The console does not edit it yet; resolution
// already honors it so the stored shape does not change later.
type ProjectSettingsOverride struct {
	Rules []DispatchRule `json:"rules,omitempty"`
}

type SharedSettings struct {
	Version          int64                              `json:"version"`
	Rules            []DispatchRule                     `json:"rules"`
	Memory           []SharedMemoryEntry                `json:"memory"`
	ProjectOverrides map[string]ProjectSettingsOverride `json:"project_overrides,omitempty"`
}

// DispatchAgentOption is an agent this machine can dispatch, with the effort
// values its launcher accepts. An agent without effort support lists none.
type DispatchAgentOption struct {
	Agent   Agent    `json:"agent"`
	Efforts []string `json:"efforts"`
}

// DispatchChoice is what a Bot passed to dispatch_work.
type DispatchChoice struct {
	Kind   string `json:"kind,omitempty"`
	Agent  string `json:"agent,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

func (c DispatchChoice) Empty() bool { return c == DispatchChoice{} }

// ResolvedDispatch says which agent runs the work and why.
type ResolvedDispatch struct {
	Assistant string
	Model     string
	Effort    string
	RuleID    string
	// Source is explicit, rule or default.
	Source string
}

type dispatchChoiceKey struct{}

// WithDispatchChoice carries the Bot's dispatch arguments from the tool call
// to the request builder, beside the positional persona arguments.
func WithDispatchChoice(ctx context.Context, c DispatchChoice) context.Context {
	if c.Empty() {
		return ctx
	}
	return context.WithValue(ctx, dispatchChoiceKey{}, c)
}

func DispatchChoiceFrom(ctx context.Context) DispatchChoice {
	c, _ := ctx.Value(dispatchChoiceKey{}).(DispatchChoice)
	return c
}

func validModel(m string) bool {
	if m == "" || len(m) > maxDispatchModel {
		return false
	}
	for _, r := range m {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func agentNames(options []DispatchAgentOption) []string {
	out := make([]string, 0, len(options))
	for _, o := range options {
		out = append(out, string(o.Agent))
	}
	return out
}

func findOption(options []DispatchAgentOption, agent Agent) (DispatchAgentOption, bool) {
	for _, o := range options {
		if o.Agent == agent {
			return o, true
		}
	}
	return DispatchAgentOption{}, false
}

// RulesFor lists the rules that apply in a Project: a Project override first,
// then rules scoped to that Project, then rules for every Project.
func (s SharedSettings) RulesFor(projectID string) []DispatchRule {
	var out []DispatchRule
	if o, ok := s.ProjectOverrides[projectID]; ok && projectID != "" {
		out = append(out, o.Rules...)
	}
	for _, r := range s.Rules {
		if !r.Scope.All && r.Scope.Applies(projectID) {
			out = append(out, r)
		}
	}
	for _, r := range s.Rules {
		if r.Scope.All {
			out = append(out, r)
		}
	}
	return out
}

// MemoryFor is the shared memory text that applies in a Project, in order.
func (s SharedSettings) MemoryFor(projectID string) []SharedMemoryEntry {
	var out []SharedMemoryEntry
	for _, m := range s.Memory {
		if m.Scope.Applies(projectID) {
			out = append(out, m)
		}
	}
	return out
}

func invalidChoice(field string, allowed []string) error {
	return &Failure{Code: Invalid, Field: field, Allowed: strings.Join(allowed, ", ")}
}

// ResolveDispatch applies explicit arguments > a matching rule > fallback.
// With no rules and no arguments the result is exactly fallback, the agent
// every Bot dispatch used before rules existed.
func ResolveDispatch(s SharedSettings, options []DispatchAgentOption, projectID, fallback string, c DispatchChoice) (ResolvedDispatch, error) {
	if c.Model != "" && !validModel(c.Model) {
		return ResolvedDispatch{}, invalidChoice("dispatch_model", nil)
	}
	var out ResolvedDispatch
	switch {
	case c.Agent != "":
		if _, ok := findOption(options, Agent(c.Agent)); !ok {
			return ResolvedDispatch{}, invalidChoice("dispatch_agent", agentNames(options))
		}
		out = ResolvedDispatch{Assistant: c.Agent, Source: "explicit"}
	case c.Kind != "":
		out = ResolvedDispatch{Assistant: fallback, Source: "default"}
		for _, rule := range s.RulesFor(projectID) {
			if !strings.EqualFold(strings.TrimSpace(rule.Kind), strings.TrimSpace(c.Kind)) {
				continue
			}
			found := false
			for _, candidate := range rule.Agents {
				if _, ok := findOption(options, candidate.Agent); ok {
					out = ResolvedDispatch{Assistant: string(candidate.Agent), Model: candidate.Model, Effort: candidate.Effort, RuleID: rule.ID, Source: "rule"}
					found = true
					break
				}
			}
			if !found {
				names := make([]string, 0, len(rule.Agents))
				for _, a := range rule.Agents {
					names = append(names, string(a.Agent))
				}
				return ResolvedDispatch{}, &Failure{Code: Unsupported, Field: "dispatch_rule_agents", Allowed: strings.Join(names, ", ")}
			}
			break
		}
	default:
		out = ResolvedDispatch{Assistant: fallback, Source: "default"}
	}
	if c.Model != "" {
		out.Model = c.Model
	}
	if c.Effort != "" {
		out.Effort = c.Effort
	}
	if out.Effort != "" {
		option, _ := findOption(options, Agent(out.Assistant))
		if !slices.Contains(option.Efforts, out.Effort) {
			return ResolvedDispatch{}, invalidChoice("dispatch_effort", slices.Clone(option.Efforts))
		}
	}
	return out, nil
}

func validScope(s SharedScope) bool {
	if s.All && len(s.Projects) > 0 || len(s.Projects) > MaxProjectsPerScope {
		return false
	}
	for _, p := range s.Projects {
		if p == "" || len(p) > 256 || !utf8.ValidString(p) {
			return false
		}
	}
	return true
}

// MaxProjectsPerScope bounds one row's Project list.
const MaxProjectsPerScope = 64

func validRowID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validRules(rules []DispatchRule) error {
	if len(rules) > MaxDispatchRules {
		return fail(CapacityFull, "dispatch_rules")
	}
	seen := map[string]bool{}
	for _, r := range rules {
		kind := strings.ToLower(strings.TrimSpace(r.Kind))
		if !validRowID(r.ID) || seen["id:"+r.ID] || kind == "" || len(r.Kind) > maxDispatchKindBytes || seen["kind:"+kind] ||
			len(r.Hint) > maxDispatchHintBytes || !utf8.ValidString(r.Kind+r.Hint) || !validScope(r.Scope) {
			return fail(Invalid, "dispatch_rule")
		}
		seen["id:"+r.ID], seen["kind:"+kind] = true, true
		if len(r.Agents) == 0 || len(r.Agents) > MaxDispatchRuleAgents {
			return fail(Invalid, "dispatch_rule_agents")
		}
		for _, a := range r.Agents {
			if !a.Agent.Valid() || a.Model != "" && !validModel(a.Model) || len(a.Effort) > 16 {
				return fail(Invalid, "dispatch_rule_agents")
			}
		}
	}
	return nil
}

// ValidateSharedSettings checks a whole-list write before it is stored.
func ValidateSharedSettings(s SharedSettings) error {
	if err := validRules(s.Rules); err != nil {
		return err
	}
	for _, o := range s.ProjectOverrides {
		if err := validRules(o.Rules); err != nil {
			return err
		}
	}
	if len(s.Memory) > MaxSharedMemoryEntries {
		return fail(CapacityFull, "shared_memory")
	}
	total := 0
	seen := map[string]bool{}
	for _, m := range s.Memory {
		text := strings.TrimSpace(m.Text)
		if !validRowID(m.ID) || seen[m.ID] || text == "" || len(m.Text) > MaxSharedMemoryEntryBytes || !utf8.ValidString(m.Text) || !validScope(m.Scope) {
			return fail(Invalid, "shared_memory")
		}
		seen[m.ID] = true
		total += len(m.Text)
	}
	if total > MaxSharedMemoryTotalBytes {
		return fail(CapacityFull, "shared_memory_total")
	}
	return nil
}

// SharedMemoryOpening is the labelled section a Bot reads before its own
// memory. It is empty when nothing applies or the Bot turned it off.
func SharedMemoryOpening(s SharedSettings, projectID string, use bool) string {
	if !use {
		return ""
	}
	rows := s.MemoryFor(projectID)
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Memory shared by every Bot on this machine. Your standing instructions and your own memory take precedence over it.\n")
	for _, m := range rows {
		b.WriteString("- ")
		b.WriteString(strings.ReplaceAll(strings.TrimSpace(m.Text), "\n", " "))
		b.WriteString("\n")
	}
	return b.String()
}
