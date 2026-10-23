package antigravitysession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Controlled regressions discovered by independent review; synthetic profile only.
func TestRetainedToolIDsShareViewBudget(t *testing.T) {
	h, row, _ := fixture(t)
	c, _ := h.lookup(row)
	for i := 0; i < 100; i++ {
		frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": row.ConversationID, "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": strings.Repeat("x", 20000) + strings.Repeat("y", i), "title": "Read", "kind": "read", "status": "completed"}}})
		if err := c.update(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
	}
	keyBytes := 0
	for id := range c.tools {
		keyBytes += len(id)
	}
	t.Logf("retained view bytes=%d; retained tool-ID bytes=%d; declared view budget=%d", c.bytes, keyBytes, MaxViewBytes)
	if _, measured := h.Stats(); measured != c.bytes || measured > MaxViewBytes || measured < keyBytes {
		t.Fatalf("capacity measurement mismatch: %d", measured)
	}
	if keyBytes > MaxViewBytes {
		t.Fatalf("tool index alone exceeds retained memory budget")
	}
}

func TestLateTextEvictsOversizedEntry(t *testing.T) {
	h, row, dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "hello"); err != nil {
		t.Fatal(err)
	}
	settled(t, h, row)
	c, _ := h.lookup(row)
	for i := 0; i < 2; i++ {
		frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": row.ConversationID, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": strings.Repeat("x", 600000)}}}})
		err := c.update(context.Background(), frame)
		t.Logf("frame bytes=%d update error=%v", len(frame), err)
	}
	p, _ := h.Transcript(row, 100)
	raw, _ := json.Marshal(p.Entries)
	t.Logf("retained bytes=%d budget=%d", len(raw), MaxViewBytes)
	if p.Truncation == nil {
		t.Fatal("evicted content has no truncation receipt")
	}
	if len(raw) > MaxViewBytes {
		t.Fatalf("oversized snapshot remains readable after refusal")
	}
}

func TestRevocationAfterSettingsReadStarts(t *testing.T) {
	h, row, dir := fixture(t)
	settings := filepath.Join(dir, "antigravity-acp", "settings.json")
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(settings, 0600); err != nil {
		t.Fatal(err)
	}
	var revoked atomic.Bool
	var checks atomic.Int32
	completed := make(chan error, 1)
	go func() {
		completed <- h.SendProtocol(context.Background(), row, "must be refused", func() error {
			checks.Add(1)
			if revoked.Load() {
				return refusal("revoked")
			}
			return nil
		})
	}()
	// Opening the FIFO writer proves the client's post-check settings reader is waiting.
	f, err := os.OpenFile(settings, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	revoked.Store(true)
	if _, err = f.Write([]byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	select {
	case err := <-completed:
		t.Logf("send result=%v; authorization checks=%d; revoked=%v", err, checks.Load(), revoked.Load())
		if err == nil {
			t.Fatalf("prompt accepted after revocation while blocked reading settings")
		}
		var unsent interface{ BeforeWrite() bool }
		if !errors.As(err, &unsent) || !unsent.BeforeWrite() {
			t.Fatalf("refusal lost unsent evidence: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
			t.Fatal("revoked prompt reached peer")
		}
		if c, _ := h.lookup(row); c.client.Err() != nil {
			t.Fatal("local refusal terminated a healthy session")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("send did not return")
	}
}
