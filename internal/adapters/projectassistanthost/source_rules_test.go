package projectassistanthost

import (
	"context"
	"testing"
	"time"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestSourceRulesReachExistingCodexSettingsAndRevocationWithoutNativeRPC(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "source-rule-bot")
	foreign := f.create(t, "foreign-rule-bot")
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c, err := serverstore.NewCatalog(ctx, f.o.Database)
	if err != nil {
		t.Fatal(err)
	}
	saved := servers.SavedServer{ID: "synthetic-source-rule", Spec: servers.Spec{Key: bot.Key, Project: bot.Project, Name: "Synthetic server", Command: "synthetic-command --foreground", Directory: f.repo}}
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	empty, err := c.Rule(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := c.SaveRule(ctx, empty, servers.Allow)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := grant.PermissionRule()
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.Rules(ctx, bot.Key)
	if err != nil || len(rules) != 1 || rules[0] != wire {
		t.Fatal(rules, err)
	}
	if rules, err := s.Rules(ctx, foreign.Key); err != nil || len(rules) != 0 {
		t.Fatal("foreign rules leaked", rules, err)
	}
	if err := s.RemoveRule(ctx, pa.Command{Key: foreign.Key, Operation: pa.RuleRemoveOperation, IdempotencyKey: "foreign-revoke"}, wire); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
	command := pa.Command{Key: bot.Key, Operation: pa.RuleRemoveOperation, IdempotencyKey: "revoke-source"}
	if err := s.RemoveRule(ctx, command, wire); err != nil {
		t.Fatal(err)
	}
	if rules, err := s.Rules(ctx, bot.Key); err != nil || len(rules) != 0 {
		t.Fatal(rules, err)
	}
	if err := s.RemoveRule(ctx, command, wire); pa.Code(err) != pa.StaleVersion {
		t.Fatal("an old settings card changed a newer rule", err)
	}
	if _, err := c.SaveRule(ctx, grant, servers.Allow); pa.Code(err) != pa.StaleVersion {
		t.Fatal(err)
	}
	select {
	case <-f.p.sessions:
		t.Fatal("settings opened a native session")
	default:
	}
}

func TestActualSourceCardInstallsARevocableRuleThroughTheMemoryFence(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "actual-source-rule")
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := service.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: bot.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "topic"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "topic", Key: bot.Key, Project: bot.Project, Kind: pa.TopicMemory, Title: "Synthetic", Summary: "Synthetic", Text: "Synthetic"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.send(t, bot, "source-rule"); err != nil {
		t.Fatal(err)
	}
	_ = fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	tools := session.q.Tools.(NativeWork)
	c, err := serverstore.NewCatalog(ctx, f.o.Database)
	if err != nil {
		t.Fatal(err)
	}
	saved := servers.SavedServer{ID: "reviewed-rule", Spec: servers.Spec{Key: bot.Key, Project: bot.Project, Name: "Synthetic source command", Command: "synthetic-server --foreground", Directory: f.repo}}
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	approval := servers.Approval{Rules: c, Check: func(ctx context.Context, spec servers.Spec) error {
		return f.h.serverMemory(ctx, spec, func() error { return nil })
	}, Guard: f.h.serverMemory, Review: tools.approveSource}
	type result struct {
		check func(context.Context) error
		err   error
	}
	done := make(chan result, 1)
	reviewCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go func() { check, err := approval.Authorize(reviewCtx, saved); done <- result{check, err} }()
	offer := hostSourceOffer(t, f, bot.Key)
	if _, err := service.Answer(ctx, pa.Command{Key: bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "always"}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: string(pa.AllowAlways)}); err != nil {
		t.Fatal(err)
	}
	got := fixtureReceive(t, done)
	if got.err != nil || got.check == nil || got.check(ctx) != nil {
		t.Fatal(got.err)
	}
	if rules, err := service.Rules(ctx, bot.Key); err != nil || len(rules) != 1 {
		t.Fatal(rules, err)
	}
	// The native turn can end; the Host-owned exact check does not borrow it.
	session.reply <- app.PromptResult{FinalText: "Synthetic rule saved"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if err := got.check(ctx); err != nil {
		t.Fatal("native turn end revoked the Host resource", err)
	}
	if _, err := service.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-rule-context"}, Project: bot.Project, ExpectedRevision: 2, SourceIDs: []string{"topic"}}); err != nil {
		t.Fatal(err)
	}
	if err := got.check(ctx); pa.Code(err) != pa.ContextStale {
		t.Fatal("old command authority crossed Forget", err)
	}
	// The person can still inspect and revoke a historical exact rule.
	rules, err := service.Rules(ctx, bot.Key)
	if err != nil || len(rules) != 1 {
		t.Fatal(rules, err)
	}
	if err := service.RemoveRule(ctx, pa.Command{Key: bot.Key, Operation: pa.RuleRemoveOperation, IdempotencyKey: "revoke-forgotten-rule"}, rules[0]); err != nil {
		t.Fatal(err)
	}
}
