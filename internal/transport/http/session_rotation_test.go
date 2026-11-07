package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestRotationReminderDoesNotReopenAnExistingHandoff(t *testing.T) {
	for _, tc := range []struct {
		state   string
		attempt int64
		offer   bool
	}{
		{orchestrator.HandoffOpening, 0, false}, {orchestrator.HandoffDelivered, 1, false},
		{orchestrator.HandoffSpawnFailed, 0, true}, {orchestrator.HandoffSpawnFailed, 1, false},
	} {
		t.Run(tc.state+time.Unix(tc.attempt, 0).String(), func(t *testing.T) {
			p := &pane{s: session.Session{ID: "%19", Assistant: session.AssistantCodex, ConversationID: "10000000-0000-4000-8000-000000000001"}}
			s := paneServer(t, p)
			ctx := context.Background()
			if s.rotationInfo(ctx, p.s) == nil {
				t.Fatal("new source should offer a reminder")
			}
			h := orchestrator.Handoff{ID: orchestrator.NewUUID(), State: tc.state, FromSession: p.s.ConversationID, FromTerm: p.s.ID, Milestone: true, TypeAttemptedAt: tc.attempt}
			body, _ := json.Marshal(h)
			if err := s.store.CreateOpened(ctx, store.TableHandoffs, store.Opened{ID: h.ID, State: h.State, Record: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil); err != nil {
				t.Fatal(err)
			}
			if got := s.rotationInfo(ctx, p.s) != nil; got != tc.offer {
				t.Fatalf("offer=%v want %v", got, tc.offer)
			}
			other := p.s
			other.ConversationID = "10000000-0000-4000-8000-000000000002"
			if s.rotationInfo(ctx, other) == nil {
				t.Fatal("another conversation inherited the handoff")
			}
		})
	}
}
