package projectassistant

import (
	"context"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A suggested-reply press is the person's own message, so it releases a queue
// paused by a restart exactly as a typed follow-up does.
func TestDecisionAnswerReleasesARestartPausedQueue(t *testing.T) {
	fence := pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}, ConversationID: "c", Generation: 3}
	unknown := []pa.Turn{{ID: "u", State: pa.TurnUnknown, Origin: pa.UserOrigin, Fence: fence,
		Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: RestartConfirmedField}}}
	check := func(context.Context) error { return nil }
	typed := Submission{Message: pa.SendMessage{Fence: fence}, AuthorityCheck: check}
	if !canContinueRestartQueue(typed, pa.Conversation{}, unknown) {
		t.Fatal("a typed follow-up no longer releases the queue")
	}
	pressed := typed
	pressed.Decision = &pa.PermissionOffer{ID: "offer"}
	if !canContinueRestartQueue(pressed, pa.Conversation{}, unknown) {
		t.Fatal("a suggested-reply press is refused behind a restart-paused queue")
	}
	unchecked := pressed
	unchecked.AuthorityCheck = nil
	if canContinueRestartQueue(unchecked, pa.Conversation{}, unknown) {
		t.Fatal("a press without a fresh authority check released the queue")
	}
}
