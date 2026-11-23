package projectassistanthost

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type dispatchOutcome struct {
	link pa.WorkLink
	err  error
}

func startAskingBot(t *testing.T) *nativeFixture {
	t.Helper()
	return startNative(t, func(f *hostFixture) {
		f.o.Projects = leadDispatchResolver{fixtureResolver: fixtureResolver{f}}
	})
}

func dispatchToProject(n *nativeFixture, key string) <-chan dispatchOutcome {
	done := make(chan dispatchOutcome, 1)
	go func() {
		link, err := n.tools.DispatchToProject(context.Background(), key, "Keep draft messages after reconnecting", "Change only a.go in the isolated worktree.", []string{"a.go"}, "fixture-repository")
		done <- dispatchOutcome{link, err}
	}()
	return done
}

func dispatchCards(t *testing.T, n *nativeFixture) []pa.PermissionOffer {
	t.Helper()
	x, err := n.f.h.store.Export(context.Background(), n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	var out []pa.PermissionOffer
	for _, p := range x.Permissions {
		if strings.HasPrefix(p.ID, app.DispatchPermissionPrefix) {
			out = append(out, p)
		}
	}
	return out
}

func awaitDispatchCard(t *testing.T, n *nativeFixture, count int) pa.PermissionOffer {
	t.Helper()
	var pending pa.PermissionOffer
	fixtureEventually(t, func() bool {
		cards := dispatchCards(t, n)
		for _, card := range cards {
			if card.State == pa.PermissionPending {
				pending = card
			}
		}
		return len(cards) == count && pending.ID != ""
	})
	return pending
}

func answerDispatchCard(t *testing.T, n *nativeFixture, card pa.PermissionOffer, option string) {
	t.Helper()
	s, err := n.f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	command := pa.Command{Key: n.bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "answer-" + card.ID}
	if _, err = s.Answer(context.Background(), command, pa.PermissionAnswer{Fence: card.Fence, TurnID: card.TurnID, OfferID: card.ID, OptionID: option}); err != nil {
		t.Fatal(err)
	}
}

func receiveDispatch(t *testing.T, done <-chan dispatchOutcome) dispatchOutcome {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(8 * time.Second):
		t.Fatal("dispatch did not finish")
		return dispatchOutcome{}
	}
}

func TestNonLeadProjectDispatchAsksThePersonInsteadOfRefusing(t *testing.T) {
	n := startAskingBot(t)
	done := dispatchToProject(n, "cross-project")
	card := awaitDispatchCard(t, n, 1)
	if card.Action == nil || card.Action.ToolKind != "dispatch_project" || card.Action.Title != n.bot.Name+" → "+filepath.Base(n.f.repo) {
		t.Fatalf("card does not name the Bot and destination: %+v", card.Action)
	}
	if !strings.Contains(card.Action.Detail, "Keep draft messages after reconnecting") || !strings.Contains(card.Action.Detail, "- a.go") {
		t.Fatalf("card omits the title or claims: %q", card.Action.Detail)
	}
	kinds := []pa.PermissionOptionKind{}
	for _, option := range card.Options {
		kinds = append(kinds, option.Kind)
	}
	if len(kinds) != 3 || kinds[0] != pa.AllowOnce || kinds[1] != pa.AllowAlways || kinds[2] != pa.RejectOnce {
		t.Fatalf("options = %v", kinds)
	}
	// Waiting: nothing is dispatched while the card is unanswered.
	select {
	case out := <-done:
		t.Fatalf("dispatched without an answer: %+v", out)
	case <-time.After(150 * time.Millisecond):
	}
	if n.f.launcher.calls.Load() != 0 {
		t.Fatal("work launched before the answer")
	}
	answerDispatchCard(t, n, card, dispatchAllowOnce)
	out := receiveDispatch(t, done)
	if out.err != nil || out.link.TaskID == "" {
		t.Fatalf("allowed once: %+v", out)
	}
	// The same key reads the same card and result; no second card, no new task.
	again, err := n.tools.DispatchToProject(context.Background(), "cross-project", "Keep draft messages after reconnecting", "Change only a.go in the isolated worktree.", []string{"a.go"}, "fixture-repository")
	// As for any dispatch, a resent key is answered by its existing receipt.
	if pa.Code(err) != pa.OutcomeUnknown || len(dispatchCards(t, n)) != 1 || n.f.launcher.calls.Load() != 1 {
		t.Fatalf("resend: %+v %v cards=%d", again, err, len(dispatchCards(t, n)))
	}
	// A once answer saves no rule: a new key asks again.
	second := dispatchToProject(n, "cross-project-2")
	next := awaitDispatchCard(t, n, 2)
	rules, err := n.f.h.dispatchRules(context.Background(), n.bot.Key)
	if err != nil || len(rules) != 0 {
		t.Fatalf("once saved a rule: %v %v", rules, err)
	}
	answerDispatchCard(t, n, next, dispatchDecline)
	if out := receiveDispatch(t, second); pa.Code(out.err) != pa.Declined {
		t.Fatalf("decline: %+v", out)
	}
}

func TestDeclinedProjectDispatchIsTypedAndStaysDeclinedOnResend(t *testing.T) {
	n := startAskingBot(t)
	done := dispatchToProject(n, "declined")
	answerDispatchCard(t, n, awaitDispatchCard(t, n, 1), dispatchDecline)
	out := receiveDispatch(t, done)
	if pa.Code(out.err) != pa.Declined || pa.Code(out.err) == pa.ScopeDenied {
		t.Fatalf("decline: %+v", out)
	}
	if out := receiveDispatch(t, dispatchToProject(n, "declined")); pa.Code(out.err) != pa.Declined || len(dispatchCards(t, n)) != 1 {
		t.Fatalf("resend after decline: %+v cards=%d", out, len(dispatchCards(t, n)))
	}
	if n.f.launcher.calls.Load() != 0 {
		t.Fatal("declined work launched")
	}
}

func TestAlwaysAllowSavesAVisibleRevocableBotToProjectRule(t *testing.T) {
	n := startAskingBot(t)
	ctx := context.Background()
	done := dispatchToProject(n, "always")
	answerDispatchCard(t, n, awaitDispatchCard(t, n, 1), dispatchAllowAlways)
	if out := receiveDispatch(t, done); out.err != nil {
		t.Fatalf("always: %+v", out)
	}
	s, err := n.f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.Rules(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	var saved []pa.PermissionRule
	for _, rule := range rules {
		if isDispatchRule(rule) {
			saved = append(saved, rule)
		}
	}
	if len(saved) != 1 || saved[0].Behavior != "allow" {
		t.Fatalf("rule not visible: %+v", rules)
	}
	// The second dispatch to the same destination does not ask again.
	if out := receiveDispatch(t, dispatchToProject(n, "always-2")); out.err != nil || len(dispatchCards(t, n)) != 1 {
		t.Fatalf("saved rule asked again: %+v cards=%d", out, len(dispatchCards(t, n)))
	}
	if err = s.RemoveRule(ctx, pa.Command{Key: n.bot.Key, Operation: pa.RuleRemoveOperation, IdempotencyKey: "remove-dispatch-rule"}, saved[0]); err != nil {
		t.Fatal(err)
	}
	if left, err := n.f.h.dispatchRules(ctx, n.bot.Key); err != nil || len(left) != 0 {
		t.Fatalf("revoked rule remains: %v %v", left, err)
	}
	dispatchToProject(n, "after-revoke")
	awaitDispatchCard(t, n, 2)
}

func TestUnansweredProjectDispatchCardNeverDispatches(t *testing.T) {
	n := startAskingBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := n.tools.DispatchToProject(ctx, "silent", "Keep draft messages after reconnecting", "Change only a.go in the isolated worktree.", []string{"a.go"}, "fixture-repository")
	if err == nil {
		t.Fatal("an unanswered card dispatched")
	}
	fixtureEventually(t, func() bool {
		cards := dispatchCards(t, n)
		return len(cards) == 1 && cards[0].State != pa.PermissionPending
	})
	// The same key keeps the same unanswered outcome and never asks again.
	if _, err := n.tools.DispatchToProject(context.Background(), "silent", "Keep draft messages after reconnecting", "Change only a.go in the isolated worktree.", []string{"a.go"}, "fixture-repository"); pa.Code(err) != pa.Expired {
		t.Fatalf("resend after no answer: %v", err)
	}
	if n.f.launcher.calls.Load() != 0 || len(dispatchCards(t, n)) != 1 {
		t.Fatal("unanswered card launched work or asked again")
	}
}

func TestOnlyADirectUserTurnMayAskForProjectDispatch(t *testing.T) {
	room := pa.Turn{ID: "t", Origin: pa.UserOrigin, Room: &pa.RoomTicket{}}
	network := pa.Turn{ID: "t", Origin: pa.UserOrigin, Network: &pa.NetworkWork{}}
	routine := pa.Turn{ID: "t", Origin: pa.UserOrigin, RoutineRunID: "run"}
	for name, turn := range map[string]pa.Turn{"room": room, "network": network, "routine": routine, "none": {}} {
		if directUserTurn(turn) {
			t.Fatalf("%s turn may ask the person", name)
		}
	}
	if !directUserTurn(pa.Turn{ID: "t", Origin: pa.UserOrigin}) {
		t.Fatal("a direct turn cannot ask")
	}
}
