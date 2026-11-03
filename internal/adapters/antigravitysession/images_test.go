package antigravitysession

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestNativeImageAddressBindsExactHistoryAndSurvivesRestart(t *testing.T) {
	h, row, opts := persistentFixture(t)
	c, _ := h.lookup(row)
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "assistant", Text: "Before image"})
	c.mu.Unlock()
	record, after, err := h.ImageAddress(context.Background(), row)
	if err != nil || after != 2 {
		t.Fatal("image boundary", after, err)
	}
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "tool", Text: "Tool result"})
	c.append(contract.TranscriptEntry{Role: "assistant", Text: "After image"})
	c.mu.Unlock()
	anchor, err := h.ImageReplyOrdinal(context.Background(), row, record, after)
	if err != nil || anchor != 3 {
		t.Fatal("wrong first reply", anchor, err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	anchor, err = h.ImageReplyOrdinal(context.Background(), row, record, after)
	if err != nil || anchor != 3 {
		t.Fatal("restart lost image boundary", err)
	}
	wrong := row
	wrong.ConversationID = "another-native"
	if _, err := h.ImageReplyOrdinal(context.Background(), wrong, record, after); err == nil {
		t.Fatal("cross-conversation image accepted")
	}
	c, _ = h.lookup(row)
	c.mu.Lock()
	c.generation = "different-history"
	_ = c.saveState()
	c.mu.Unlock()
	if _, err := h.ImageReplyOrdinal(context.Background(), row, record, after); err == nil {
		t.Fatal("old generation's image ordinal reused")
	}
}
