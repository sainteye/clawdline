package orchestrator

import (
	"context"
	"testing"
	"time"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type quietPush struct {
	sentPush
	quiet bool
}

func followupBroker(t *testing.T) (*Broker, *[]quietPush, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Now()
	var sent []quietPush
	b := &Broker{Store: s, Dir: dir, Clock: func() time.Time { return now }, BotPresence: &BotPresence{},
		Push: func(ctx context.Context, title, body, terminal, tag string) (int, int, error) {
			bot, conversation := adapterpush.BotDestinationFromContext(ctx)
			sent = append(sent, quietPush{sentPush{bot, conversation, adapterpush.BotDecisionFromContext(ctx), title, body, terminal, tag}, adapterpush.QuietFromContext(ctx)})
			return 1, 0, nil
		}}
	return b, &sent, &now
}

// beat is the recovery beat after a Bot commit's effect is old enough to be
// this process's unattended work, which is how a Bot push is run.
func beat(b *Broker, now *time.Time) {
	*now = now.Add(unattendedPending)
	b.RecoverEffects(context.Background())
}

func commitDecision(t *testing.T, b *Broker, offer pa.PermissionOffer) {
	t.Helper()
	ctx := context.Background()
	tx, err := b.Store.ProjectAssistantDatabase().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: "xavia"}, Name: "Xavia"}
	if err = b.BotDecisionIntent(ctx, tx, bot, "chat", offer); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func decisionOffer(state pa.PermissionState) pa.PermissionOffer {
	return pa.PermissionOffer{ID: "decision-1", State: state, Kind: pa.DecisionOfferKind,
		Decision: &pa.DecisionRequest{Question: "Ship it?"}}
}

func TestAPushedDecisionThatClosesIsReplacedQuietly(t *testing.T) {
	b, sent, now := followupBroker(t)
	commitDecision(t, b, decisionOffer(pa.PermissionPending))
	beat(b, now)
	if len(*sent) != 1 {
		t.Fatalf("pushes %d", len(*sent))
	}
	commitDecision(t, b, decisionOffer(pa.PermissionResolved))
	beat(b, now)
	if len(*sent) != 2 {
		t.Fatalf("no retraction: %+v", *sent)
	}
	got := (*sent)[1]
	if got.tag != (*sent)[0].tag || !got.quiet || got.title != "Xavia no longer needs your decision" || got.decision != "" {
		t.Fatalf("retraction %+v", got)
	}
	t.Logf("retraction payload: title=%q body=%q tag=%q quiet=%v", got.title, got.body, got.tag, got.quiet)
	beat(b, now)
	if len(*sent) != 2 {
		t.Fatalf("retracted twice")
	}
}

func TestADecisionThatWasNeverPushedIsNotRetracted(t *testing.T) {
	b, sent, now := followupBroker(t)
	ctx := context.Background()
	b.PushEnabled = func(string) bool { return false }
	commitDecision(t, b, decisionOffer(pa.PermissionPending))
	beat(b, now)
	b.PushEnabled = nil
	commitDecision(t, b, decisionOffer(pa.PermissionCancelled))
	beat(b, now)
	if len(*sent) != 0 {
		t.Fatalf("a never-shown notification was replaced: %+v", *sent)
	}
	effects, _ := b.Store.Effects(ctx, EffectBotDecisionRetract, BotDecisionTag("m", "xavia", "decision-1"))
	if len(effects) != 1 || effects[0].Outcome != "never_pushed" {
		t.Fatalf("retraction effect %+v", effects)
	}
}

func TestADecisionIsHeldWhileItsChatIsOnScreenAndPushedAfterwards(t *testing.T) {
	b, sent, now := followupBroker(t)
	b.BotPresence.Report("xavia", "chat", true, *now)
	commitDecision(t, b, decisionOffer(pa.PermissionPending))
	beat(b, now)
	if len(*sent) != 0 {
		t.Fatalf("pushed while the chat was on screen")
	}
	b.BotPresence.Report("xavia", "other-chat", true, *now)
	b.BotPresence.Report("xavia", "chat", false, *now)
	beat(b, now)
	if len(*sent) != 1 || (*sent)[0].quiet {
		t.Fatalf("not pushed after leaving: %+v", *sent)
	}
}

func TestADecisionHeldForAnOpenScreenIsPushedAtTheHoldLimit(t *testing.T) {
	b, sent, now := followupBroker(t)
	commitDecision(t, b, decisionOffer(pa.PermissionPending))
	for i := 0; i < 3; i++ {
		b.BotPresence.Report("xavia", "chat", true, *now)
		beat(b, now)
	}
	if len(*sent) != 0 {
		t.Fatalf("pushed early")
	}
	*now = now.Add(BotViewingHoldSeconds * time.Second)
	b.BotPresence.Report("xavia", "chat", true, *now)
	beat(b, now)
	if len(*sent) != 1 {
		t.Fatalf("held past the limit")
	}
}

func TestAnExpiredDecisionIsNotPushed(t *testing.T) {
	b, sent, now := followupBroker(t)
	offer := decisionOffer(pa.PermissionPending)
	offer.ExpiresAt = now.Add(-time.Minute)
	b.BotPermission = func(context.Context, pa.BotKey, string) (pa.PermissionOffer, error) { return offer, nil }
	commitDecision(t, b, offer)
	beat(b, now)
	if len(*sent) != 0 {
		t.Fatalf("an expired question was pushed")
	}
}

func TestBotPresenceForgetsAReportAfterItsWindow(t *testing.T) {
	p := &BotPresence{}
	at := time.Now()
	p.Report("b", "c", true, at)
	if !p.Viewing("b", "c", at.Add(BotViewingSeconds*time.Second-time.Second)) || p.Viewing("b", "c", at.Add(BotViewingSeconds*time.Second)) {
		t.Fatal("window")
	}
	for i := 0; i < BotViewingChats+5; i++ {
		p.Report("b", string(rune('a'+i%26))+time.Duration(i).String(), true, at)
	}
	if len(p.seen) > BotViewingChats {
		t.Fatalf("remembered %d chats", len(p.seen))
	}
}
