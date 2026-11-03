package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativePromptChangePublishesFreshContentWithoutWaitingForTheSnapshotTick(t *testing.T) {
	changes := make(chan struct{}, 1)
	out := &collector{}
	p := newPublisher(&fixedRouter{body: completeScan}, out)
	p.Every = time.Hour
	p.PromptChanges = changes
	var fresh atomic.Int32
	p.Sessions = func(context.Context) ([]byte, bool) { return []byte(completeScan), true }
	p.Fresh = func(context.Context) ([]byte, bool) {
		fresh.Add(1)
		var body map[string]any
		if err := json.Unmarshal([]byte(completeScan), &body); err != nil {
			return nil, false
		}
		for _, v := range body["sessions"].([]any) {
			v.(map[string]any)["prompt_suggestion"] = map[string]any{"state": "available", "id": "fixture-prompt", "text": "private fixture next prompt", "conversation_id": "fixture-conversation", "source": "claude-terminal", "observed_at": 1}
		}
		raw, _ := json.Marshal(body)
		return raw, true
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	defer func() { cancel(); <-done }()
	// No wall-clock tick can occur in this test. Only the native invalidation
	// can cause the fresh pass, and its content must stay out of ss/.
	changes <- struct{}{}
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("native invalidation waited for the ordinary publisher tick")
		case <-tick.C:
			out.mu.Lock()
			found := false
			for _, v := range out.out {
				if len(v.Channel) >= 3 && v.Channel[:3] == "ss/" && bytes.Contains(v.Payload, []byte("private fixture next prompt")) {
					out.mu.Unlock()
					t.Fatal("prompt content leaked to the status feed")
				}
				if len(v.Channel) >= 2 && v.Channel[:2] == "s/" && bytes.Contains(v.Payload, []byte("private fixture next prompt")) {
					found = true
				}
			}
			out.mu.Unlock()
			if found {
				if fresh.Load() != 1 {
					t.Fatalf("fresh reads=%d", fresh.Load())
				}
				return
			}
		}
	}
}
