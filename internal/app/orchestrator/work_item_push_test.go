package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestACompletedWorkItemPushIsRecoveredExactlyOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	earlier, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(WorkItemCompletedPush{WorkID: "item-a", Terminal: "terminal-a",
		Title: "看板項目已完成", Body: "有完成時間", Tag: "work-item-item-a"})
	ids, err := earlier.RecordIntent(ctx, nil,
		[]store.Effect{{Kind: EffectWorkItemCompletedPush, Subject: "item-a", Payload: payload}})
	if err != nil || len(ids) != 1 {
		t.Fatalf("record: %v %v", ids, err)
	}
	_ = earlier.Close()

	later, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	var got WorkItemCompletedPush
	b := &Broker{Store: later, Push: func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		got = WorkItemCompletedPush{WorkID: "item-a", Terminal: terminal, Title: title, Body: body, Tag: tag}
		return 1, 0, nil
	}}
	if n := b.RecoverEffects(ctx); n != 1 {
		t.Fatalf("recovery settled %d, want 1", n)
	}
	if n := b.RecoverEffects(ctx); n != 0 {
		t.Fatalf("second recovery settled %d", n)
	}
	if got.WorkID != "item-a" || got.Terminal != "terminal-a" || got.Title != "看板項目已完成" || got.Body != "有完成時間" {
		t.Fatalf("push: %+v", got)
	}
	effects, _ := later.Effects(ctx, EffectWorkItemCompletedPush, "item-a")
	if len(effects) != 1 || effects[0].State != store.EffectDone || effects[0].Outcome != "pushed" {
		t.Fatalf("effect: %+v", effects)
	}
}

func TestACompletedWorkItemPushRecordsThePushServiceAnswer(t *testing.T) {
	ctx := context.Background()
	payload, _ := json.Marshal(WorkItemCompletedPush{WorkID: "item-a", Title: "done", Body: "title", Tag: "work-item-item-a"})
	e := store.Effect{ID: 9, Kind: EffectWorkItemCompletedPush, Subject: "item-a", Payload: payload}
	for _, c := range []struct {
		sent, failed int
		state        string
		outcome      string
	}{
		{0, 0, store.EffectDone, "not_subscribed"},
		{0, 1, store.EffectFailed, "no push service accepted it"},
		{1, 1, store.EffectDone, "pushed"},
	} {
		b := &Broker{Push: func(context.Context, string, string, string, string) (int, int, error) {
			return c.sent, c.failed, nil
		}}
		got := runWorkItemCompletedPush(ctx, b, e)
		if got.state != c.state || got.outcome != c.outcome || len(got.events) != 1 ||
			got.events[0].Kind != "work.item.completed.pushed" {
			t.Fatalf("sent %d failed %d: %+v", c.sent, c.failed, got)
		}
	}
	if got := runWorkItemCompletedPush(ctx, &Broker{}, e); got.state != store.EffectFailed {
		t.Fatalf("no push seam: %+v", got)
	}
	if got := runWorkItemCompletedPush(ctx, &Broker{}, store.Effect{Payload: []byte(`{}`)}); got.state != store.EffectFailed {
		t.Fatalf("empty effect: %+v", got)
	}
}
