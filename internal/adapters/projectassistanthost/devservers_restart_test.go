package projectassistanthost

import (
	"context"
	"errors"
	"testing"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestHostServerCleanRestartStartsANewAttemptUnderTheStandingRule(t *testing.T) {
	f, count, started := newServerHostFixture(t)
	bot := f.create(t, "server-restart")
	tools, session := beginServerNative(t, f, bot, "one")
	first := startReviewedServer(t, f, bot, tools, "start")
	process := fixtureReceive(t, started)
	session.reply <- app.PromptResult{FinalText: "The source owns this server"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	providers := f.factories.Load()
	if err := f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !process.closed.Load() {
		t.Fatal("restart did not stop the old tree")
	}
	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.start(t)
	f.enable(t)
	fixtureEventually(t, func() bool { return count.Load() == 2 })
	second := fixtureReceive(t, started)
	rows, err := f.h.serverRegistry.Inspect(context.Background(), bot.Key, bot.Project)
	if err != nil || len(rows) != 1 || rows[0].ID != first.ID || rows[0].Attempt.Identity.AttemptID == first.Attempt || second.closed.Load() {
		t.Fatal("restart did not retain the definition with a new owned attempt", rows, err)
	}
	if f.factories.Load() != providers {
		t.Fatal("server restoration initialized an Agent provider")
	}
	if err := f.h.serverRegistry.ResumeClean(context.Background(), f.h.serverBoot); err != nil || count.Load() != 2 {
		t.Fatal("repeated recovery replayed the consumed checkpoint", err)
	}
}

func TestHostServerRestartDoesNotCarryAuthorityOrRestartStoppedWork(t *testing.T) {
	for _, name := range []string{"allow-once", "deny", "revoked", "boot-changed", "boot-unknown", "stopped", "disabled", "exited", "cleanup-unknown", "forgotten", "project-changed"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f, count, started := newServerHostFixture(t)
			bot := f.create(t, "server-restart-fence")
			service, _ := f.h.Service()
			if name == "forgotten" {
				if _, err := service.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: bot.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "topic"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "topic", Key: bot.Key, Project: bot.Project, Kind: pa.TopicMemory, Title: "Synthetic", Summary: "Synthetic", Text: "Synthetic"}}); err != nil {
					t.Fatal(err)
				}
			}
			tools, session := beginServerNative(t, f, bot, "one")
			choice := pa.AllowAlways
			if name == "allow-once" {
				choice = pa.AllowOnce
			}
			_ = startReviewedServer(t, f, bot, tools, "start", choice)
			process := fixtureReceive(t, started)
			session.reply <- app.PromptResult{FinalText: "Owned by source"}
			fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
			c, err := serverstore.NewCatalog(ctx, f.o.Database)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			switch name {
			case "stopped":
				if err := f.h.serverRegistry.StopExpected(ctx, rows[0]); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if err := f.h.Disable(ctx); err != nil {
					t.Fatal(err)
				}
			case "exited":
				if err := process.Close(ctx); err != nil {
					t.Fatal(err)
				}
				fixtureEventually(t, func() bool {
					rows, err := f.h.serverRegistry.Inspect(ctx, bot.Key, bot.Project)
					return err == nil && len(rows) == 1 && rows[0].Attempt.State == servers.Stopped
				})
			case "cleanup-unknown":
				process.closeErr = errors.New("synthetic cleanup uncertainty")
			case "forgotten":
				if _, err := service.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget"}, Project: bot.Project, ExpectedRevision: 2, SourceIDs: []string{"topic"}}); err != nil {
					t.Fatal(err)
				}
			}
			closeErr := f.h.Close(ctx)
			if name == "cleanup-unknown" {
				if pa.Code(closeErr) != pa.ExecutionEffectUnknown {
					t.Fatal(closeErr)
				}
			} else if closeErr != nil {
				t.Fatal(closeErr)
			}
			switch name {
			case "deny":
				rule, err := c.Rule(ctx, rows[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.SaveRule(ctx, rule, servers.Deny); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				f.revoked.Store(true)
			case "boot-changed":
				f.o.ServerBootID = func(context.Context) (string, error) { return "different-synthetic-boot", nil }
			case "boot-unknown":
				f.o.ServerBootID = func(context.Context) (string, error) { return "", errors.New("synthetic boot read denied") }
			case "project-changed":
				f.scope.CanonicalID = "replacement-project"
			}
			f.o.Broker = f.broker(t, f.o.Broker.Dir)
			f.start(t)
			f.enable(t)
			u := f.h.serverRegistry.Usage()
			want := 0
			if name == "cleanup-unknown" {
				want = 1 // Unknown original occupancy must remain reserved.
			}
			if count.Load() != 1 || u.Processes != want || u.Known == (name == "cleanup-unknown") {
				t.Fatal("restart crossed a fence or lost unknown ownership", count.Load(), u)
			}
			var checkpoints int
			if err := f.o.Database.QueryRow(`SELECT count(*) FROM bot_devserver_restarts`).Scan(&checkpoints); err != nil || checkpoints != 0 {
				t.Fatal("rejected restart remained queued for later permission", checkpoints, err)
			}
		})
	}
}
