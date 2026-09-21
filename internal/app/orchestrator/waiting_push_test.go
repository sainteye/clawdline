package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// A stopped session's push is an outbox row like the capacity push, so the
// same crash has the same answer: the one never begun is sent, with the tap
// landing on the terminal that is waiting; the one begun and unfinished is
// recorded unknown and not sent again. Without the handler registered the
// never-begun push would be finished `failed` and nothing would be sent.
func TestAWaitingPushLeftByADeadDaemonIsSettledExactlyOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	earlier, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	owe := func(terminal string) int64 {
		key := terminal + "|conv"
		payload, _ := json.Marshal(WaitingPush{Key: key, Terminal: terminal, Title: "等你回答：" + terminal,
			Body: "停在一個問題上 10 分鐘了。", Tag: "waiting-" + terminal})
		ids, err := earlier.RecordIntent(ctx, []store.Event{{Kind: "session.waiting", Subject: key}},
			[]store.Effect{{Kind: EffectWaitingPush, Subject: key, Payload: payload}})
		if err != nil || len(ids) != 1 {
			t.Fatalf("record %s: %v %v", terminal, ids, err)
		}
		return ids[0]
	}
	owe("never-begun")
	begun := owe("begun")
	if _, err := earlier.StartEffect(ctx, begun); err != nil {
		t.Fatal(err)
	}
	_ = earlier.Close()

	later, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	type sent struct{ terminal, tag string }
	var pushes []sent
	b := &Broker{Store: later, Push: func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		pushes = append(pushes, sent{terminal, tag})
		return 1, 0, nil
	}}
	if n := b.RecoverEffects(ctx); n != 2 {
		t.Fatalf("recovery settled %d, want 2", n)
	}
	if n := b.RecoverEffects(ctx); n != 0 {
		t.Fatalf("a second recovery settled %d", n)
	}
	if len(pushes) != 1 || pushes[0] != (sent{"never-begun", "waiting-never-begun"}) {
		t.Fatalf("pushed %v; want the never-begun one, once, opening its terminal", pushes)
	}
	done, _ := later.Effects(ctx, EffectWaitingPush, "never-begun|conv")
	unknown, _ := later.Effects(ctx, EffectWaitingPush, "begun|conv")
	if len(done) != 1 || done[0].State != store.EffectDone || done[0].Outcome != "pushed" {
		t.Fatalf("the never-begun push: %+v", done)
	}
	if len(unknown) != 1 || unknown[0].State != store.EffectUnknown {
		t.Fatalf("the begun push: %+v", unknown)
	}
}

// Nobody subscribed is an answer, not a failure: it is finished and not
// retried, and a push service that refused is a failure with its reason.
func TestAWaitingPushSaysWhatThePushServicesAnswered(t *testing.T) {
	ctx := context.Background()
	payload, _ := json.Marshal(WaitingPush{Key: "A|c", Terminal: "A", Title: "等你回答：A", Body: "…", Tag: "waiting-A"})
	e := store.Effect{ID: 7, Kind: EffectWaitingPush, Subject: "A|c", Payload: payload}
	for _, c := range []struct {
		sent, failed int
		state        string
		outcome      string
	}{
		{0, 0, store.EffectDone, "not_subscribed"},
		{0, 2, store.EffectFailed, "no push service accepted it"},
		{1, 1, store.EffectDone, "pushed"},
	} {
		b := &Broker{Push: func(context.Context, string, string, string, string) (int, int, error) {
			return c.sent, c.failed, nil
		}}
		got := runWaitingPush(ctx, b, e)
		if got.state != c.state || got.outcome != c.outcome || len(got.events) != 1 ||
			got.events[0].Kind != "session.waiting.pushed" {
			t.Fatalf("sent %d failed %d: %+v", c.sent, c.failed, got)
		}
	}
	if got := runWaitingPush(ctx, &Broker{}, e); got.state != store.EffectFailed {
		t.Fatalf("a daemon that cannot push: %+v", got)
	}
	if got := runWaitingPush(ctx, &Broker{}, store.Effect{Payload: []byte(`{}`)}); got.state != store.EffectFailed {
		t.Fatalf("an effect with no stop: %+v", got)
	}
}
