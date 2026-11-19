package projectassistant

import (
	"strings"
	"time"
)

const (
	MaxRoutinePreviewFieldBytes     = 1024
	MaxRoutineDefinitions           = 1000
	MaxRoutineTickMilliseconds      = 1000
	MaxRoutinePendingPerDefinition  = 1
	MaxRoutineNameBytes             = 200
	MaxRoutineInstructionBytes      = 32000
	MaxRoutineIntervalSeconds       = 31536000
	MaxRoutineTimeoutSeconds        = 86400
	MaxRoutineFinishedRuns          = 1000
	MaxRoutineTriggers              = 20
	MaxRoutineProgressTokens        = 2000
	MaxRoutineEventNameBytes        = 200
	MaxRoutineEventFilters          = 20
	MaxRoutineEventFilterKeyBytes   = 200
	MaxRoutineEventFilterValueBytes = 4000
	MaxRoutineWebhookBodyBytes      = 64 << 10
	MaxRoutineDeliveryIDBytes       = 200
	MaxRoutineGitHubEventNameBytes  = 200
	MaxRoutineWebhookSecretBytes    = 32
	MaxRoutineFollowUps             = 10
	MaxRoutineFollowUpSeconds       = 30 * 24 * 60 * 60
	MaxRoutineFollowUpChain         = 24
	MaxRoutineFollowUpTokens        = 500
	MaxRoutineRetryAttempts         = 3
	MaxRoutineRetryDelaySeconds     = 900
)

// Routine belongs to one source Bot, canonical Project and conversation.
// Legacy interval rows keep their cadence when read or edited.
type Routine struct {
	FollowUp         bool             `json:"follow_up,omitempty"`
	FollowUpChain    int              `json:"follow_up_chain,omitempty"`
	MemoryRevision   int64            `json:"memory_revision,omitempty"`
	ID               string           `json:"id"`
	Key              BotKey           `json:"key"`
	Project          ProjectScope     `json:"project"`
	ConversationID   string           `json:"conversation_id"`
	Name             string           `json:"name"`
	Instruction      string           `json:"instruction"`
	IntervalSeconds  int64            `json:"interval_seconds"`
	Triggers         []RoutineTrigger `json:"triggers,omitempty"`
	NextRuns         []time.Time      `json:"next_runs,omitempty"`
	TimeoutSeconds   int64            `json:"timeout_seconds"`
	Enabled          bool             `json:"enabled"`
	Deleted          bool             `json:"deleted"`
	NextDue          time.Time        `json:"next_due"`
	Progress         string           `json:"progress,omitempty"`
	ProgressRevision int64            `json:"progress_revision,omitempty"`
	Version          int64            `json:"version"`
}

type RoutineRun struct {
	MemoryRevision   int64        `json:"memory_revision,omitempty"`
	RetryCount       int          `json:"retry_count,omitempty"`
	NotBefore        time.Time    `json:"not_before,omitempty,omitzero"`
	NotStartedProved bool         `json:"not_started_proved,omitempty"`
	DeliveryID       string       `json:"delivery_id,omitempty"`
	Event            RoutineEvent `json:"event,omitzero"`
	ID               string       `json:"id"`
	RoutineID        string       `json:"routine_id"`
	Source           string       `json:"source"`
	Key              BotKey       `json:"key"`
	Project          ProjectScope `json:"project"`
	ConversationID   string       `json:"conversation_id"`
	TurnID           string       `json:"turn_id,omitempty"`
	BindingID        string       `json:"binding_id,omitempty"`
	State            TurnState    `json:"state"`
	Instruction      string       `json:"instruction,omitempty"`
	TimeoutSeconds   int64        `json:"timeout_seconds"`
	CreatedAt        time.Time    `json:"created_at"`
	FinishedAt       *time.Time   `json:"finished_at,omitempty"`
	Published        bool         `json:"published"`
}

func ValidateRoutine(r Routine) error {
	if r.FollowUp {
		if r.FollowUpChain < 0 || r.FollowUpChain > MaxRoutineFollowUpChain || r.MemoryRevision < 1 || len(r.Triggers) != 1 || r.Triggers[0].Type != "once" || EstimateRoutineProgressTokens(r.Instruction) > MaxRoutineFollowUpTokens {
			return &Failure{Code: Invalid, Field: "follow_up"}
		}
	} else if r.FollowUpChain != 0 {
		return &Failure{Code: Invalid, Field: "follow_up_chain"}
	}
	if r.ID == "" || r.Key.MachineID == "" || r.Key.BotID == "" || r.Project.MachineID != r.Key.MachineID || r.Project.ProjectID == "" || r.Project.CanonicalID == "" || r.ConversationID == "" || r.Version < 1 {
		return &Failure{Code: Invalid, Field: "routine_owner"}
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > MaxRoutineNameBytes || strings.TrimSpace(r.Instruction) == "" || len(r.Instruction) > MaxRoutineInstructionBytes {
		return &Failure{Code: Invalid, Field: "routine_text"}
	}
	if err := ValidateRoutineProgress(r.Progress); err != nil {
		return err
	}
	if r.ProgressRevision < 0 || r.Progress != "" && r.ProgressRevision < 1 {
		return &Failure{Code: Invalid, Field: "routine_progress_revision"}
	}
	if r.TimeoutSeconds < 1 || r.TimeoutSeconds > MaxRoutineTimeoutSeconds {
		return &Failure{Code: Invalid, Field: "routine_interval_timeout"}
	}
	if len(r.Triggers) == 0 {
		if r.IntervalSeconds < 1 || r.IntervalSeconds > MaxRoutineIntervalSeconds || r.NextDue.IsZero() || len(r.NextRuns) != 0 {
			return &Failure{Code: Invalid, Field: "routine_interval_timeout"}
		}
	} else {
		if r.IntervalSeconds != 0 || len(r.Triggers) > MaxRoutineTriggers || len(r.NextRuns) != len(r.Triggers) {
			return &Failure{Code: Invalid, Field: "routine_triggers"}
		}
		for i, trigger := range r.Triggers {
			if err := trigger.Validate(); err != nil {
				return err
			}
			// Hooks are externally driven and have no clock occurrence.
			if r.NextRuns[i].IsZero() && trigger.Type != "once" && trigger.Type != "webhook" && trigger.Type != "event" {
				return &Failure{Code: Invalid, Field: "routine_next_run"}
			}
			if (trigger.Type == "webhook" || trigger.Type == "event") && !r.NextRuns[i].IsZero() {
				return &Failure{Code: Invalid, Field: "routine_next_run"}
			}
			if trigger.Type == "once" && !r.NextRuns[i].IsZero() && !r.NextRuns[i].Equal(time.UnixMilli(trigger.At)) {
				return &Failure{Code: Invalid, Field: "routine_next_run"}
			}
		}
		if !r.NextDue.Equal(earliestRoutineRun(r.NextRuns)) {
			return &Failure{Code: Invalid, Field: "routine_next_due"}
		}
	}
	return nil
}

// AdvanceRoutine advances a legacy elapsed interval, preserving its cadence.
func AdvanceRoutine(r Routine, now time.Time) time.Time {
	if now.Before(r.NextDue) {
		return r.NextDue
	}
	step := time.Duration(r.IntervalSeconds) * time.Second
	return r.NextDue.Add((now.Sub(r.NextDue)/step + 1) * step)
}
