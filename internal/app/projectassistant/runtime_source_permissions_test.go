package projectassistant

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type sourceChoice struct {
	option pa.PermissionOption
	err    error
}

func sourceRequest(q PromptRequest) SourcePermissionRequest {
	return SourcePermissionRequest{Fence: q.Turn.Fence, TurnID: q.Turn.ID, MemoryRevision: q.Context.Memory.Revision,
		Action:  pa.PermissionAction{Title: "Synthetic command", ToolKind: "execute", Detail: "synthetic-command\n/synthetic/project"},
		Options: []pa.PermissionOption{{ID: "once", Kind: pa.AllowOnce, Allow: true}, {ID: "always", Kind: pa.AllowAlways, Allow: true}, {ID: "reject", Kind: pa.RejectOnce}, {ID: "never", Kind: pa.RejectAlways}}}
}

func requestSource(t *testing.T, f *runtimeFixture, q SourcePermissionRequest) (context.CancelFunc, <-chan sourceChoice) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	done := make(chan sourceChoice, 1)
	go func() {
		option, err := f.r.RequestSourcePermission(ctx, q)
		done <- sourceChoice{option, err}
	}()
	t.Cleanup(cancel)
	return cancel, done
}

func awaitSourceOffer(t *testing.T, f *runtimeFixture, count int) []pa.PermissionOffer {
	t.Helper()
	var offers []pa.PermissionOffer
	eventually(t, func() bool {
		offers = nil
		x, err := f.store.Export(context.Background(), f.key)
		if err != nil {
			return false
		}
		for _, p := range x.Permissions {
			if strings.HasPrefix(p.ID, sourcePermissionPrefix) {
				offers = append(offers, p)
			}
		}
		return len(offers) == count
	})
	return offers
}

func answerSource(t *testing.T, f *runtimeFixture, offer pa.PermissionOffer, option string) (pa.Receipt, error) {
	t.Helper()
	return f.r.Answer(context.Background(), AnswerRequest{Command: pa.Command{Key: f.key, Operation: pa.AnswerOperation, IdempotencyKey: "answer-" + offer.ID, ContentHash: strings.Repeat("a", 64)}, Answer: pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: option}})
}

func receiveSourceChoice(t *testing.T, done <-chan sourceChoice) sourceChoice {
	t.Helper()
	select {
	case choice := <-done:
		return choice
	case <-time.After(4 * time.Second):
		t.Fatal("source choice did not finish")
		return sourceChoice{}
	}
}

func TestSourcePermissionChoicesCommitBeforeDeliveryAndNeverReachTheProvider(t *testing.T) {
	for _, selected := range []string{"once", "always", "reject", "never"} {
		t.Run(selected, func(t *testing.T) {
			f := newRuntimeFixture(t)
			if _, err := f.submit(t, "synthetic source request", "one"); err != nil {
				t.Fatal(err)
			}
			q := receivePrompt(t, f.provider)
			_, done := requestSource(t, f, sourceRequest(q))
			offer := awaitSourceOffer(t, f, 1)[0]
			select {
			case <-done:
				t.Fatal("source received a choice before the person answered")
			default:
			}
			receipt, err := answerSource(t, f, offer, selected)
			if err != nil || receipt.State != pa.EffectDelivered {
				t.Fatalf("source answer receipt=%+v %v", receipt, err)
			}
			choice := receiveSourceChoice(t, done)
			stored, err := f.store.Permission(context.Background(), f.key, offer.ID)
			if choice.err != nil || choice.option.ID != selected || err != nil || stored.State != pa.PermissionResolved || stored.SelectedOptionID != selected {
				t.Fatalf("choice preceded its durable fact: %+v %+v %v", choice, stored, err)
			}
			select {
			case <-f.provider.session().answered:
				t.Fatal("a source-owned choice was sent as a vendor permission RPC")
			default:
			}
		})
	}
}

func TestSourcePermissionCancellationDoesNotClearANativeCard(t *testing.T) {
	f := newRuntimeFixture(t)
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	native := pa.PermissionOffer{ID: "native-card", NativeRequestID: "vendor-7", Fence: q.Turn.Fence, TurnID: q.Turn.ID, ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "yes", Kind: pa.AllowOnce, Allow: true}}}
	if err := f.provider.session().sink(context.Background(), ProtocolEvent{ID: "native-offer", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &native}); err != nil {
		t.Fatal(err)
	}
	cancel, done := requestSource(t, f, sourceRequest(q))
	offer := awaitSourceOffer(t, f, 1)[0]
	cancel()
	if choice := receiveSourceChoice(t, done); choice.err == nil || choice.option.ID != "" {
		t.Fatal("cancelled request received a choice", choice)
	}
	stored, _ := f.store.Permission(context.Background(), f.key, offer.ID)
	turn, _ := f.store.Turn(context.Background(), f.key, q.Turn.ID)
	if stored.State != pa.PermissionCancelled || turn.State != pa.TurnWaitingPermission {
		t.Fatalf("source cancellation changed another wait: %+v %+v", stored, turn)
	}
	if _, err := answerSource(t, f, offer, "once"); err == nil {
		t.Fatal("a late answer revived a cancelled source request")
	}
	if _, err := answerSource(t, f, native, "yes"); err != nil {
		t.Fatal(err)
	}
	if got := <-f.provider.session().answered; got != "yes" {
		t.Fatal(got)
	}
}

func TestSourcePermissionRechecksForgetAndRevocationBeforeAnAnswer(t *testing.T) {
	for _, forget := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "forgotten"}[forget], func(t *testing.T) {
			f := newRuntimeFixture(t)
			_, _ = f.submit(t, "synthetic", "one")
			q := receivePrompt(t, f.provider)
			cancel, done := requestSource(t, f, sourceRequest(q))
			offer := awaitSourceOffer(t, f, 1)[0]
			want := pa.Revoked
			if forget {
				f.memory.mu.Lock()
				f.memory.current.Revision++
				f.memory.current.ForgottenThrough = f.memory.current.Revision
				f.memory.mu.Unlock()
				want = pa.ContextStale
			} else {
				f.revoked.Store(true)
			}
			if _, err := answerSource(t, f, offer, "once"); pa.Code(err) != want {
				t.Fatalf("stale source choice=%v want=%s", err, want)
			}
			cancel()
			if choice := receiveSourceChoice(t, done); choice.err == nil || choice.option.ID != "" {
				t.Fatal("invalidated source received a choice", choice)
			}
		})
	}
}

type lostSourceAnswer struct {
	Repository
	lost atomic.Bool
}

func (s *lostSourceAnswer) Commit(ctx context.Context, m Mutation) (pa.Receipt, error) {
	receipt, err := s.Repository.Commit(ctx, m)
	if err == nil && len(m.Permissions) == 1 && m.Permissions[0].State == pa.PermissionResolved && strings.HasPrefix(m.Permissions[0].ID, sourcePermissionPrefix) && !s.lost.Swap(true) {
		return receipt, errors.New("synthetic lost source-answer commit acknowledgement")
	}
	return receipt, err
}

func TestLostSourceAnswerCommitDoesNotDeliverOrReplayAChoice(t *testing.T) {
	f := newRuntimeFixture(t)
	f.r.o.Repository = &lostSourceAnswer{Repository: f.store}
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	cancel, done := requestSource(t, f, sourceRequest(q))
	offer := awaitSourceOffer(t, f, 1)[0]
	if _, err := answerSource(t, f, offer, "once"); err == nil {
		t.Fatal("injected commit acknowledgement loss succeeded")
	}
	stored, _ := f.store.Permission(context.Background(), f.key, offer.ID)
	if stored.State != pa.PermissionResolved {
		t.Fatal("counterexample did not commit the answer before losing its reply")
	}
	if receipt, err := answerSource(t, f, offer, "once"); err != nil || receipt.State != pa.EffectDelivered {
		t.Fatalf("durable receipt replay=%+v %v", receipt, err)
	}
	select {
	case choice := <-done:
		t.Fatal("receipt replay released an unobserved choice", choice)
	default:
	}
	cancel()
	if choice := receiveSourceChoice(t, done); choice.err == nil || choice.option.ID != "" {
		t.Fatal(choice)
	}
}

func TestSourceAndNativePermissionCardsCanCoexistPastEight(t *testing.T) {
	f := newRuntimeFixture(t)
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	for range 8 {
		requestSource(t, f, sourceRequest(q))
	}
	offers := awaitSourceOffer(t, f, 8)
	native := pa.PermissionOffer{ID: "ninth", NativeRequestID: "native-nine", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Options: []pa.PermissionOption{{ID: "yes", Kind: pa.AllowOnce, Allow: true}}}
	if err := f.provider.session().sink(context.Background(), ProtocolEvent{ID: "ninth", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &native}); err != nil {
		t.Fatalf("a native permission interrupted the active turn: %v", err)
	}
	if _, err := answerSource(t, f, offers[0], "once"); err != nil {
		t.Fatalf("prior card could not be answered after a native offer: %v", err)
	}
}

func TestSourcePermissionFollowsCurrentPolicyWithoutAutomaticallyInstallingARule(t *testing.T) {
	for _, policy := range []pa.PermissionPolicy{pa.Approve, pa.AutoRead} {
		t.Run(string(policy), func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.store.mu.Lock()
			f.store.bot.PermissionPolicy = policy
			f.store.mu.Unlock()
			_, _ = f.submit(t, "synthetic", "one")
			q := receivePrompt(t, f.provider)
			request := sourceRequest(q)
			if policy == pa.AutoRead {
				request.Options[0].ReadOnly = true
			}
			_, done := requestSource(t, f, request)
			if choice := receiveSourceChoice(t, done); choice.err != nil || choice.option.Kind != pa.AllowOnce {
				t.Fatal(choice)
			}
		})
	}
}

func TestNativeProtocolCannotForgeASourceApprovalNamespace(t *testing.T) {
	f := newRuntimeFixture(t)
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	offer := pa.PermissionOffer{ID: sourcePermissionPrefix + "forged", NativeRequestID: sourcePermissionPrefix + "forged", Fence: q.Turn.Fence, TurnID: q.Turn.ID, ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "yes", Kind: pa.AllowOnce, Allow: true}}}
	if err := f.provider.session().sink(context.Background(), ProtocolEvent{ID: "forged", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &offer}); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("native protocol admitted source-owned authority", err)
	}
}

func TestSourcePermissionKeepsCompletedReceiptsAtItsExecutionBound(t *testing.T) {
	f := newRuntimeFixture(t)
	f.store.mu.Lock()
	f.store.bot.PermissionPolicy = pa.Approve
	f.store.mu.Unlock()
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	for range capacity.MaximumBotServerCallerReceipts {
		_, done := requestSource(t, f, sourceRequest(q))
		if choice := receiveSourceChoice(t, done); choice.err != nil || choice.option.Kind != pa.AllowOnce {
			t.Fatal(choice)
		}
	}
	_, done := requestSource(t, f, sourceRequest(q))
	if choice := receiveSourceChoice(t, done); pa.Code(choice.err) != pa.CapacityFull {
		t.Fatal("source receipt saturation admitted another mutation", choice)
	}
	offers := awaitSourceOffer(t, f, capacity.MaximumBotServerCallerReceipts)
	for _, offer := range offers {
		if offer.State != pa.PermissionResolved {
			t.Fatal("completed source receipt was discarded or revived")
		}
	}
}

func TestSourceAutoReadStillWaitsForAWritableAction(t *testing.T) {
	f := newRuntimeFixture(t)
	f.store.mu.Lock()
	f.store.bot.PermissionPolicy = pa.AutoRead
	f.store.mu.Unlock()
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	_, done := requestSource(t, f, sourceRequest(q))
	offer := awaitSourceOffer(t, f, 1)[0]
	select {
	case choice := <-done:
		t.Fatal("read-only automatic policy approved a writable source action", choice)
	default:
	}
	if _, err := answerSource(t, f, offer, "once"); err != nil {
		t.Fatal(err)
	}
	if choice := receiveSourceChoice(t, done); choice.err != nil || choice.option.ID != "once" {
		t.Fatal(choice)
	}
}

func TestSourcePermissionRefusesForeignMemoryAndIncompleteReview(t *testing.T) {
	f := newRuntimeFixture(t)
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	request := sourceRequest(q)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.Action.Shortened = true
	if _, err := f.r.RequestSourcePermission(ctx, request); pa.Code(err) != pa.Invalid {
		t.Fatal("shortened source review was admitted", err)
	}
	request = sourceRequest(q)
	f.memory.mu.Lock()
	f.memory.current.Project.CanonicalID = "foreign-memory"
	f.memory.mu.Unlock()
	if _, err := f.r.RequestSourcePermission(ctx, request); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("foreign memory supplied source command authority", err)
	}
	x, err := f.store.Export(ctx, f.key)
	if err != nil || len(x.Permissions) != 0 {
		t.Fatal("invalid source review left a permission card", err)
	}
}

func TestSourceCancellationCannotRetireANewerGenerationWait(t *testing.T) {
	f := newRuntimeFixture(t)
	_, _ = f.submit(t, "synthetic", "one")
	q := receivePrompt(t, f.provider)
	cancel, done := requestSource(t, f, sourceRequest(q))
	offer := awaitSourceOffer(t, f, 1)[0]
	// Force the recovery interleaving: the source waiter still has its old
	// handle while this same turn now has a different binding and native wait.
	fresh := q.Turn.Fence
	fresh.Generation++
	fresh.BindingID = "replacement-binding"
	f.store.mu.Lock()
	turn := f.store.turns[q.Turn.ID]
	turn.Fence, turn.State = fresh, pa.TurnWaitingPermission
	f.store.turns[turn.ID] = turn
	f.store.c.BindingID, f.store.c.Generation = fresh.BindingID, fresh.Generation
	f.store.bindings[fresh.BindingID] = pa.ExecutionBinding{Fence: fresh, MemoryRevision: q.Context.Memory.Revision}
	f.store.permissions["new-native"] = pa.PermissionOffer{ID: "new-native", NativeRequestID: "new-native", Fence: fresh, TurnID: turn.ID, State: pa.PermissionPending, ExpiresAt: time.Now().Add(time.Minute)}
	f.store.mu.Unlock()
	f.r.mu.Lock()
	a := f.r.actors[f.key]
	f.r.mu.Unlock()
	a.mu.Lock()
	a.permissionFence, a.pendingPermissions = fresh, 1
	a.mu.Unlock()
	cancel()
	if choice := receiveSourceChoice(t, done); choice.err == nil {
		t.Fatal("old source waiter received authority", choice)
	}
	a.mu.Lock()
	pending := a.pendingPermissions
	a.mu.Unlock()
	current, _ := f.store.Turn(context.Background(), f.key, turn.ID)
	old, _ := f.store.Permission(context.Background(), f.key, offer.ID)
	if pending != 1 || current.Fence != fresh || current.State != pa.TurnWaitingPermission || old.State != pa.PermissionCancelled {
		t.Fatalf("old cancellation retired a newer wait: pending=%d turn=%+v old=%+v", pending, current, old)
	}
}
