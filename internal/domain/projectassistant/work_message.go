package projectassistant

import (
	"strings"
	"time"
	"unicode/utf8"
)

// MaxWorkMessageBytes bounds one message_work text. A correction to a running
// work Session is a few paragraphs; a new brief belongs in dispatch_work.
const MaxWorkMessageBytes = 8 << 10

// WorkMessageStage names the rung a message_work answer reports. Accepted is
// durable intent; delivered is the line typed into the worker's composer.
// Neither means the worker read the message or acted on it.
type WorkMessageStage string

const (
	WorkMessageAccepted  WorkMessageStage = "accepted"
	WorkMessageDelivered WorkMessageStage = "delivered"
)

// WorkMessageReceipt is the receipt of one Bot message to its own running work Session.
type WorkMessageReceipt struct {
	TaskID         string           `json:"task_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Stage          WorkMessageStage `json:"stage"`
	AcceptedAt     time.Time        `json:"accepted_at,omitzero"`
	DeliveredAt    time.Time        `json:"delivered_at,omitzero"`
	Replayed       bool             `json:"replayed,omitempty"`
}

// ValidateWorkMessage admits non-empty valid UTF-8 within MaxWorkMessageBytes.
func ValidateWorkMessage(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" {
		return fail(Invalid, "text")
	}
	if len(text) > MaxWorkMessageBytes {
		return fail(CapacityFull, "text")
	}
	return nil
}
