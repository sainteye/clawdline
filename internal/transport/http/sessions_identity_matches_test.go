package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAnUnboundPeerDoesNotMakeAnExactlyBoundSessionAmbiguous(t *testing.T) {
	bound := session.Session{ID: "bound", Assistant: session.AssistantCodex,
		ConversationID: "conversation-a", Binding: session.BindingOpenFile}
	unbound := session.Session{ID: "unbound", Assistant: session.AssistantCodex,
		Binding: session.BindingUnreadable}

	got := identityMatchCounts([]session.Session{bound, unbound})
	if got[bound.ID] != 1 {
		t.Fatalf("an exact open-file binding matched %d rows; want 1", got[bound.ID])
	}
	if got[unbound.ID] != 1 {
		t.Fatalf("the unbound row matched %d rows; its separate unbound evidence should decide it", got[unbound.ID])
	}
}

func TestTwoRowsNamingOneConversationRemainAmbiguous(t *testing.T) {
	items := []session.Session{
		{ID: "first", Assistant: session.AssistantCodex, ConversationID: "same", Binding: session.BindingOpenFile},
		{ID: "second", Assistant: session.AssistantCodex, ConversationID: "same", Binding: session.BindingCommandLine},
	}

	got := identityMatchCounts(items)
	if got["first"] != 2 || got["second"] != 2 {
		t.Fatalf("duplicate exact bindings matched %+v; want both to match 2", got)
	}
}
