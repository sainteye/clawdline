package projectassistant

import (
	"encoding/json"
	"time"
)

// Values follow Codync 9f67eb09 host/src/agent/fallback/{mod,rest}.rs.
const (
	MaxFallbackAgents               = 3
	MaxFallbackUsageRestSeconds     = 3600
	MaxFallbackCreditsRestSeconds   = 21600
	MaxFallbackSignedOutRestSeconds = 3600
	MaxFallbackBackoffOneSeconds    = 60
	MaxFallbackBackoffTwoSeconds    = 300
	MaxFallbackBackoffThreeSeconds  = 1500
	MaxFallbackBackoffMaxSeconds    = 3600
)

type AttemptStage string

const (
	AttemptOpening  AttemptStage = "opening"
	AttemptOpened   AttemptStage = "opened"
	AttemptRefused  AttemptStage = "refused_before_send"
	AttemptSending  AttemptStage = "sending"
	AttemptSent     AttemptStage = "sent"
	AttemptAnswered AttemptStage = "answered"
	AttemptClosed   AttemptStage = "closed"
	AttemptHeld     AttemptStage = "held_unknown"
)

// AttemptFact is append-only. Opening and Sending deliberately mean unknown
// after a crash; a reason alone never authorizes another attempt.
type AttemptFact struct {
	LoginProfile string `json:"login_profile,omitempty"`
	Ordinal      int    `json:"ordinal"`
	Agent        Agent  `json:"agent"`
	// Fallback records the selected identity relative to the configured
	// primary at execution time, including a resting primary skipped on
	// the first attempt. Old receipts may omit it.
	Fallback          bool         `json:"fallback,omitempty"`
	PrimaryRestReason string       `json:"primary_rest_reason,omitempty"`
	Fence             Fence        `json:"fence"`
	Stage             AttemptStage `json:"stage"`
	SendStarted       bool         `json:"send_started"`
	Effect            string       `json:"effect"`
	CleanupConfirmed  bool         `json:"cleanup_confirmed"`
	Reason            string       `json:"reason,omitempty"`
	Opened            bool         `json:"opened,omitempty"`
	// SendReturned means Send returned a pending handle, not that a remote
	// model acknowledged, executed or completed the prompt.
	SendReturned   bool `json:"send_returned,omitempty"`
	NativeActivity bool `json:"native_activity,omitempty"`
	ToolActivity   bool `json:"tool_activity,omitempty"`
	AnswerObserved bool `json:"answer_observed,omitempty"`
	// ResumeBindingID names historical provenance, never an execution grant.
	ResumeBindingID string `json:"resume_binding_id,omitempty"`
	CarryKind       string `json:"carry_kind,omitempty"`
	CarryAfterSeq   int64  `json:"carry_after_seq,omitempty"`
}

// AgentRest belongs to one machine and one login identity, never Bot memory.
type AgentRest struct {
	LoginProfile string    `json:"login_profile,omitempty"`
	RecordedAt   time.Time `json:"recorded_at"`
	MachineID    string    `json:"machine_id"`
	Agent        Agent     `json:"agent"`
	Until        time.Time `json:"until"`
	Reason       string    `json:"reason"`
	Failures     int       `json:"failures"`
}

func ValidateFallbacks(primary Agent, agents []Agent) error {
	if len(agents) > MaxFallbackAgents {
		return fail(CapacityFull, "fallback_agents")
	}
	seen := map[Agent]bool{primary: true}
	for _, a := range agents {
		if a != ClaudeCode && a != Codex {
			return fail(Unsupported, "fallback_agent")
		}
		if seen[a] {
			return fail(Invalid, "fallback_identity")
		}
		seen[a] = true
	}
	return nil
}

// AgentChain is a display projection; execution selection uses StepChain so
// repeated Agents on different logins keep their separate identity.
func AgentChain(bot Bot) []Agent {
	out := []Agent{}
	for _, step := range StepChain(bot) {
		out = append(out, step.Agent)
	}
	return out
}

// RestAfter does not classify errors and does not decide replay eligibility.
// No reset telemetry is qualified in this slice, so usage uses upstream's 1h.
func RestAfter(machine string, agent Agent, reason string, now time.Time, before *AgentRest) *AgentRest {
	failures := 1
	if before != nil && now.Sub(before.Until) < time.Duration(MaxFallbackBackoffMaxSeconds)*time.Second {
		failures = before.Failures + 1
	}
	seconds := 0
	switch reason {
	case "usage_limit":
		seconds = MaxFallbackUsageRestSeconds
	case "out_of_credits":
		seconds = MaxFallbackCreditsRestSeconds
	case "signed_out":
		seconds = MaxFallbackSignedOutRestSeconds
	case "rate_limited", "overloaded":
		switch failures {
		case 1:
			seconds = MaxFallbackBackoffOneSeconds
		case 2:
			seconds = MaxFallbackBackoffTwoSeconds
		case 3:
			seconds = MaxFallbackBackoffThreeSeconds
		default:
			seconds = MaxFallbackBackoffMaxSeconds
		}
	case "model_missing":
		return nil
	default:
		return nil
	}
	return &AgentRest{RecordedAt: now, MachineID: machine, Agent: agent, Reason: reason, Until: now.Add(time.Duration(seconds) * time.Second), Failures: failures}
}

func SafeAttemptClosed(t Turn) bool {
	if len(t.Attempts) == 0 {
		return false
	}
	f := t.Attempts[len(t.Attempts)-1]
	if f.Stage != AttemptClosed || !f.CleanupConfirmed || f.NativeActivity || f.ToolActivity || f.AnswerObserved || f.Effect != "none" {
		return false
	}
	if f.SendStarted {
		return f.SendReturned && IsQuotaReason(f.Reason)
	}
	return !f.SendReturned && f.Reason != ""
}

func IsQuotaReason(reason string) bool {
	return reason == "usage_limit" || reason == "out_of_credits"
}

// FallbackConfig keeps Bot's existing value semantics. The wire remains an
// optional array; old rows decode to the empty configuration without migration.
type FallbackConfig [MaxFallbackAgents]Agent

func FallbackConfigOf(agents []Agent) FallbackConfig {
	var c FallbackConfig
	copy(c[:], agents)
	return c
}
func (c FallbackConfig) Agents() []Agent {
	out := []Agent{}
	for _, a := range c {
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}
func (c FallbackConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.Agents()) }
func (c *FallbackConfig) UnmarshalJSON(raw []byte) error {
	var agents []Agent
	if err := json.Unmarshal(raw, &agents); err != nil {
		return err
	}
	if len(agents) > MaxFallbackAgents {
		return fail(CapacityFull, "fallback_agents")
	}
	for _, agent := range agents {
		if agent != ClaudeCode && agent != Codex {
			return fail(Unsupported, "fallback_agent")
		}
	}
	*c = FallbackConfigOf(agents)
	return nil
}

// LoginKey is generated only from validated source-owned identities.
func LoginKey(agent Agent, profile string) string {
	if profile == "" {
		return string(agent)
	}
	return string(agent) + "#profile-" + profile
}

// FallbackStep names a real Agent and its source-owned login. Empty profile means
// that Agent's existing default login; it never inherits the primary's profile.
type FallbackStep struct {
	Agent        Agent  `json:"agent"`
	LoginProfile string `json:"login_profile,omitempty"`
}

// Fixed-size value semantics and omitzero preserve genuine pre-feature archives.
type FallbackStepConfig [MaxFallbackAgents]FallbackStep

func FallbackStepConfigOf(steps []FallbackStep) FallbackStepConfig {
	var c FallbackStepConfig
	copy(c[:], steps)
	return c
}
func (c FallbackStepConfig) Steps() []FallbackStep {
	out := []FallbackStep{}
	for _, step := range c {
		if step.Agent != "" {
			out = append(out, step)
		}
	}
	return out
}
func (c FallbackStepConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.Steps()) }
func (c *FallbackStepConfig) UnmarshalJSON(raw []byte) error {
	var steps []FallbackStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		return err
	}
	if len(steps) > MaxFallbackAgents {
		return fail(CapacityFull, "fallback_steps")
	}
	for _, step := range steps {
		if step.Agent != ClaudeCode && step.Agent != Codex {
			return fail(Unsupported, "fallback_agent")
		}
		if !ValidLoginProfileID(step.LoginProfile) {
			return fail(Invalid, "login_profile")
		}
	}
	*c = FallbackStepConfigOf(steps)
	return nil
}
func ValidateFallbackSteps(primary FallbackStep, steps []FallbackStep) error {
	if len(steps) > MaxFallbackAgents {
		return fail(CapacityFull, "fallback_steps")
	}
	seen := map[FallbackStep]bool{primary: true}
	for _, step := range steps {
		if step.Agent != ClaudeCode && step.Agent != Codex {
			return fail(Unsupported, "fallback_agent")
		}
		if !ValidLoginProfileID(step.LoginProfile) {
			return fail(Invalid, "login_profile")
		}
		if seen[step] {
			return fail(Invalid, "fallback_identity")
		}
		seen[step] = true
	}
	return nil
}
func StepChain(bot Bot) []FallbackStep {
	out := []FallbackStep{{Agent: bot.Agent, LoginProfile: bot.LoginProfile}}
	if steps := bot.FallbackSteps.Steps(); len(steps) > 0 {
		return append(out, steps...)
	}
	for _, agent := range bot.FallbackAgents.Agents() {
		out = append(out, FallbackStep{Agent: agent})
	}
	return out
}
func HasFallbacks(bot Bot) bool {
	return len(bot.FallbackSteps.Steps())+len(bot.FallbackAgents.Agents()) > 0
}
func HasLoginSelection(bot Bot) bool {
	for _, step := range StepChain(bot) {
		if step.LoginProfile != "" {
			return true
		}
	}
	return false
}
