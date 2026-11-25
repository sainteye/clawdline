package projectassistant

import (
	"context"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (f *runtimeFixture) editQueue(id string, fence pa.Fence, turnID, editID, action, text string) (pa.Receipt, error) {
	return f.r.EditQueue(context.Background(), QueueEditRequest{Command: pa.Command{Key: f.key, Operation: pa.QueueEditOperation, IdempotencyKey: id, ContentHash: strings.Repeat("c", 64)},
		Fence: fence, TurnID: turnID, EditID: editID, Action: action, Text: text}, func(context.Context) error { return nil })
}

// A phone that read the queue before the running turn started still edits it:
// the queued turn's own state is the guard, not the latest generation.
func TestQueueEditAcceptsAFenceFromBeforeTheRunningTurnStarted(t *testing.T) {
	f, _, queued, _, before := steerFixture(t, nil, SteerJoined)
	begun, err := f.editQueue("begin", before, queued, "", "begin", "")
	if err != nil {
		t.Fatalf("begin with an earlier generation: %v", err)
	}
	if _, err = f.editQueue("save", before, queued, begun.ResourceID, "save", "two, edited"); err != nil {
		t.Fatalf("save with an earlier generation: %v", err)
	}
	f.store.mu.Lock()
	last := f.store.mutations[len(f.store.mutations)-1]
	f.store.mu.Unlock()
	if last.QueuedMessageEdit == nil || last.QueuedMessageEdit.Text != "two, edited" || last.Turn == nil || last.Turn.QueueEditID != "" {
		t.Fatalf("the edit was not saved: %+v", last)
	}
}

// A message the agent already took is refused for good, and says so apart
// from an edit that merely lost a race.
func TestQueueEditOfAMessageAlreadySentSaysSo(t *testing.T) {
	f, running, _, _, _ := steerFixture(t, nil, SteerJoined)
	_, err := f.editQueue("begin", f.fence(), running, "", "begin", "")
	if code, field := failureField(err); code != pa.StaleVersion || field != QueueEditSentField {
		t.Fatalf("editing a running message answered %v", err)
	}
}

// A group room's message has no queue editor; it is unsupported, not stale.
func TestQueueEditOfAGroupRoomMessageIsUnsupported(t *testing.T) {
	f, _, queued, _, _ := steerFixture(t, nil, SteerJoined)
	f.store.mu.Lock()
	turn := f.store.turns[queued]
	turn.Room = &pa.RoomTicket{Group: pa.BotKey{MachineID: "machine", BotID: "group"}, Member: f.key, Epoch: 1}
	f.store.turns[queued] = turn
	f.store.mu.Unlock()
	_, err := f.editQueue("begin", f.fence(), queued, "", "begin", "")
	if code, field := failureField(err); code != pa.Unsupported || field != QueueEditRoomField {
		t.Fatalf("editing a group room message answered %v", err)
	}
}
