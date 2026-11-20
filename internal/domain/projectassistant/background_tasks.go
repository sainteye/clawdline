package projectassistant

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxBackgroundPending     = 20
	DefaultBackgroundThreads = 3
	MaxBackgroundThreads     = 6
)

// BackgroundConcurrency uses Codync's default for older saved Bots.
func (b Bot) BackgroundConcurrency() int {
	if b.BackgroundThreads == 0 {
		return DefaultBackgroundThreads
	}
	return b.BackgroundThreads
}

// BackgroundTask belongs to the Bot that received the user's message. Its
// brief and source message remain available when the worker's turn changes.
type BackgroundTask struct {
	ID              string    `json:"id"`
	Fence           Fence     `json:"fence"`
	OriginTurnID    string    `json:"origin_turn_id"`
	ExchangeID      string    `json:"exchange_id,omitempty"`
	OriginMessageID string    `json:"origin_message_id"`
	Title           string    `json:"title"`
	Brief           string    `json:"brief"`
	CreatedAt       time.Time `json:"created_at"`
}

func ValidateBackgroundBrief(title, brief string) error {
	if title != strings.TrimSpace(title) || title == "" || strings.ContainsAny(title, "\r\n") || !utf8.ValidString(title) || utf8.RuneCountInString(title) > MaxNameRunes {
		return &Failure{Code: Invalid, Field: "background_title"}
	}
	if err := ValidateMessage(brief); err != nil {
		return &Failure{Code: Code(err), Field: "background_brief"}
	}
	return nil
}

// CanDelegateBackground limits the tool to an active, ordinary chat turn.
// Instructions and model text never select that authority.
func CanDelegateBackground(turn Turn) bool {
	return turn.Room == nil && turn.RoutineRunID == "" && turn.Network == nil &&
		(turn.Origin == UserOrigin || turn.Origin == "") &&
		(turn.State == TurnRunning || turn.State == TurnWaitingPermission)
}

// BackgroundTaskSummary projects one item into its owner's chat. The worker
// gets its own ordinary, independently fenced transcript.
type BackgroundTaskSummary struct {
	ID              string
	Title           string
	State           string
	Activity        string
	Worker          *BotKey
	OriginMessageID string
	StartedAt       *time.Time
	EndedAt         *time.Time
}
