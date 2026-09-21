package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A capacity push is an outbox row like any other, so a daemon that died
// between the commit and the push leaves it to the next one — and the next
// one settles it exactly once: the push never begun is sent, the one begun and
// unfinished is recorded unknown and not sent again, because a push cannot be
// asked afterwards whether it went. Without the handler registered, the
// never-begun push would be finished `failed` with no handler, and nothing
// would be sent; that is the red this test is about.
func TestACapacityPushLeftByADeadDaemonIsSettledExactlyOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	earlier, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	owe := func(name, state string) int64 {
		payload, _ := json.Marshal(CapacityPush{Name: name, State: state, Used: 10, Limit: 10,
			Title: "容量已滿：" + name, Body: name + " 用了 10／10", Tag: "capacity-" + name})
		ids, err := earlier.RecordIntent(ctx, []store.Event{{Kind: "capacity.notify", Subject: name}},
			[]store.Effect{{Kind: EffectCapacityPush, Subject: name, Payload: payload}})
		if err != nil || len(ids) != 1 {
			t.Fatalf("record %s: %v %v", name, ids, err)
		}
		return ids[0]
	}
	owe("store.db", "full")
	begun := owe("audit.security", "critical")
	if _, err := earlier.StartEffect(ctx, begun); err != nil {
		t.Fatal(err)
	}
	// The earlier daemon is gone: this process's pid under another nonce.
	_ = earlier.Close()

	later, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	var mu sync.Mutex
	var tags []string
	b := &Broker{Store: later, Push: func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		mu.Lock()
		defer mu.Unlock()
		tags = append(tags, tag)
		return 1, 0, nil
	}}
	if n := b.RecoverEffects(ctx); n != 2 {
		t.Fatalf("recovery settled %d, want 2", n)
	}
	if n := b.RecoverEffects(ctx); n != 0 {
		t.Fatalf("a second recovery settled %d", n)
	}
	if len(tags) != 1 || tags[0] != "capacity-store.db" {
		t.Fatalf("pushed %v; want the never-begun one, once", tags)
	}
	sent, _ := later.Effects(ctx, EffectCapacityPush, "store.db")
	unknown, _ := later.Effects(ctx, EffectCapacityPush, "audit.security")
	if len(sent) != 1 || sent[0].State != store.EffectDone || sent[0].Outcome != "pushed" {
		t.Fatalf("the never-begun push: %+v", sent)
	}
	if len(unknown) != 1 || unknown[0].State != store.EffectUnknown {
		t.Fatalf("the begun push: %+v", unknown)
	}
	if n, _ := later.EventCount(ctx, "capacity.notify.pushed"); n != 1 {
		t.Fatalf("%d pushed events", n)
	}
	latest, err := later.LatestEffects(ctx, EffectCapacityPush)
	if err != nil || len(latest) != 2 || latest[0].Subject != "audit.security" || latest[1].Subject != "store.db" {
		t.Fatalf("latest: %+v %v", latest, err)
	}
}
