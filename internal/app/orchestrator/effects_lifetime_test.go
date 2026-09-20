package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// The client goes away mid-flight, which is the ordinary way a request ends:
// a browser tab is closed, a phone sleeps, a curl is interrupted.
//
// The effect that types the message is not the request. Once the intent is
// durable the message is owed, and what the caller's context still decides is
// how long anybody waits for the answer. These tests fail on the shape this
// replaced: the typing ran on the request's context, so a cancelled one either
// stopped the attempt from starting or stopped its outcome from being written
// — and in the second case the bytes were in somebody's composer while the
// account of them said `pending`, for ever, because the row's owner was this
// process, alive, and no recovery takes a live owner's row.

// relayMessage is the one message these tests send.
func relayMessage() Message {
	return Message{From: w2Source, To: w2Target, Text: "hello once"}
}

// cancelAt is a broker whose caller goes away at one point of the effect.
func cancelAt(t *testing.T, dir, point string) (*Broker, context.Context) {
	t.Helper()
	b, _ := w2Broker(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.EffectFault = func(p string, _ store.Effect) {
		if p == point {
			cancel()
		}
	}
	return b, ctx
}

// The caller leaves after the attempt has begun. The bytes go in — that half
// never went through the HTTP connection — and the record of them goes in too,
// so the same key asked again is answered rather than told the request is
// still running.
func TestKeystrokesTypedAfterTheClientLeftAreStillRecorded(t *testing.T) {
	dir := t.TempDir()
	b, ctx := cancelAt(t, dir, "started")
	sent, err := b.Relay(ctx, relayMessage(), "key-1")
	if err != nil {
		t.Fatalf("the send answered %v; the caller leaving is not a reason to lose it", err)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("typed %d line(s), want 1", n)
	}
	if sent.Stage != StageDelivered || sent.At.IsZero() {
		t.Fatalf("the answer is %+v; want delivered with a time", sent)
	}
	if err := ctx.Err(); err == nil {
		t.Fatal("the caller's context was never cancelled; this test proves nothing")
	}
	// The receipt is complete, so the resend replays rather than finding a
	// request that is for ever in progress.
	again, err := b.Relay(context.Background(), relayMessage(), "key-1")
	if err != nil {
		t.Fatalf("the resend answered %v, want the first answer replayed", err)
	}
	if !again.Replayed || !again.At.Equal(sent.At) {
		t.Fatalf("the resend answered %+v, want %+v replayed", again, sent)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("the resend typed it again: %d lines", n)
	}
}

// The caller leaves between the commit and the attempt — the window the old
// shape lost outright, because the store write that marks the row `started`
// took the cancelled context and failed.
func TestAMessageAcceptedBeforeTheClientLeftIsStillTyped(t *testing.T) {
	dir := t.TempDir()
	b, ctx := cancelAt(t, dir, "committed")
	sent, err := b.Relay(ctx, relayMessage(), "key-1")
	if err != nil {
		t.Fatalf("the send answered %v", err)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("typed %d line(s) after the caller left, want 1", n)
	}
	if sent.AcceptedAt.IsZero() || sent.At.IsZero() {
		t.Fatalf("the answer is %+v; accepted and delivered are two facts and both are known here", sent)
	}
	if _, err := b.Relay(context.Background(), relayMessage(), "key-1"); err != nil {
		t.Fatalf("the resend answered %v, want the first answer", err)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("the resend typed it again: %d lines", n)
	}
}

// An intent recorded and then left by whoever recorded it: no goroutine in
// this process is holding it, and its owner is this process, so AdoptEffects
// will not take it. The beat settles it instead, and the key that was stuck
// answers.
func TestAnEffectItsRequestAbandonedIsSettledByTheBeat(t *testing.T) {
	dir := t.TempDir()
	b, _ := w2Broker(t, dir)
	ctx := context.Background()
	now := time.Now()
	b.Clock = func() time.Time { return now }
	msg := relayMessage()

	receipt := store.ReceiptKey{Scope: ScopeMessages, Actor: "machine", Key: "stuck"}
	sum := sha256.Sum256([]byte(msg.From + "\x00" + msg.To + "\x00" + msg.Text))
	claim, err := b.Store.ClaimReceipt(ctx, receipt, hex.EncodeToString(sum[:]), store.ReceiptPolicy{}, now)
	if err != nil || claim.Outcome != store.ReceiptNew {
		t.Fatalf("claiming the receipt: %v %v", claim.Outcome, err)
	}
	payload, _ := json.Marshal(messageEffect{Target: w2Target, Source: w2Source, Accepted: now.Unix(),
		Wire: "<clawdline-message>abandoned</clawdline-message>"})
	if _, err := b.Store.RecordIntent(ctx, nil,
		[]store.Effect{{Kind: EffectMessage, Subject: w2Target, Payload: payload, Receipt: &receipt}}); err != nil {
		t.Fatal(err)
	}

	// Straight away it is nobody's business but the request's: a row this
	// young is one somebody is about to run, and taking it would be the race.
	if n := b.RecoverEffects(ctx); n != 0 {
		t.Fatalf("the beat took %d fresh row(s); that is a race, not a recovery", n)
	}
	if n := typedLines(t, dir); n != 0 {
		t.Fatalf("a fresh row was typed by the beat: %d lines", n)
	}
	// The request never came back for it.
	now = now.Add(unattendedPending + time.Second)
	if n := b.RecoverEffects(ctx); n != 1 {
		t.Fatalf("the beat settled %d row(s), want 1", n)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("the abandoned message was typed %d time(s), want 1", n)
	}
	if n := b.RecoverEffects(ctx); n != 0 {
		t.Fatalf("a second pass settled %d row(s)", n)
	}
	sent, err := b.Relay(ctx, msg, "stuck")
	if err != nil {
		t.Fatalf("the key that was stuck answers %v, want the settled answer", err)
	}
	if !sent.Replayed || sent.Stage != StageDelivered {
		t.Fatalf("the key answers %+v, want the delivered answer replayed", sent)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("asking again typed it: %d lines", n)
	}
}

// A row whose attempt had begun cannot be settled by repeating it: nothing can
// be asked afterwards whether bytes went into a composer. The beat says
// unknown and types nothing, and the key is told so rather than left pending.
func TestAnAbandonedAttemptIsCalledUnknownAndNotRepeated(t *testing.T) {
	dir := t.TempDir()
	b, _ := w2Broker(t, dir)
	ctx := context.Background()
	now := time.Now()
	b.Clock = func() time.Time { return now }
	msg := relayMessage()

	receipt := store.ReceiptKey{Scope: ScopeMessages, Actor: "machine", Key: "half"}
	sum := sha256.Sum256([]byte(msg.From + "\x00" + msg.To + "\x00" + msg.Text))
	if claim, err := b.Store.ClaimReceipt(ctx, receipt, hex.EncodeToString(sum[:]), store.ReceiptPolicy{}, now); err != nil ||
		claim.Outcome != store.ReceiptNew {
		t.Fatalf("claiming the receipt: %v %v", claim.Outcome, err)
	}
	payload, _ := json.Marshal(messageEffect{Target: w2Target, Source: w2Source, Accepted: now.Unix(),
		Wire: "<clawdline-message>half typed</clawdline-message>"})
	ids, err := b.Store.RecordIntent(ctx, nil,
		[]store.Effect{{Kind: EffectMessage, Subject: w2Target, Payload: payload, Receipt: &receipt}})
	if err != nil || len(ids) != 1 {
		t.Fatal(err)
	}
	// The attempt began and its outcome was never written.
	if _, err := b.Store.StartEffect(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	now = now.Add(unattendedStarted + time.Second)
	if n := b.RecoverEffects(ctx); n != 1 {
		t.Fatalf("the beat settled %d row(s), want 1", n)
	}
	if n := typedLines(t, dir); n != 0 {
		t.Fatalf("the beat repeated an attempt it cannot check: %d lines", n)
	}
	if _, err := b.Relay(ctx, msg, "half"); refusalCode(err) != "request_outcome_unknown" {
		t.Fatalf("the key answers %v, want request_outcome_unknown", err)
	}
}

// Accepted and delivered are two facts. The answer carries both and says which
// rung it is reporting; it never claims the two above it.
func TestTheAnswerSaysWhichRungItReached(t *testing.T) {
	dir := t.TempDir()
	b, _ := w2Broker(t, dir)
	ctx := context.Background()
	// Whole seconds: a receipt keeps Unix seconds, and this test is about
	// which fact each field holds, not about the clock's resolution.
	now := time.Now().Truncate(time.Second)
	b.Clock = func() time.Time { return now }
	sent, err := b.Relay(ctx, relayMessage(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if sent.Stage != StageDelivered {
		t.Fatalf("the answer claims %q", sent.Stage)
	}
	if sent.Stage == StageObserved || sent.Stage == StageAcknowledged {
		t.Fatal("this daemon cannot know either of those")
	}
	if !sent.AcceptedAt.Equal(now) || !sent.At.Equal(now) {
		t.Fatalf("accepted %v delivered %v, both should be this frozen clock %v", sent.AcceptedAt, sent.At, now)
	}
	// The receipt keeps both, so a replay is not a lesser answer.
	again, err := b.Relay(ctx, relayMessage(), "k")
	if err != nil || !again.Replayed {
		t.Fatalf("resend %+v %v", again, err)
	}
	if !again.AcceptedAt.Equal(sent.AcceptedAt) || again.Stage != sent.Stage {
		t.Fatalf("the replay lost a fact: %+v vs %+v", again, sent)
	}
}
