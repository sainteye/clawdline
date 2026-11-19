package projectassistanthost

import (
	"context"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestHumanKnownServerControlsNeedNoNativeTurnAndKeepTheirAttemptFence(t *testing.T) {
	f, count, started := newServerHostFixture(t)
	bot := f.create(t, "human-server")
	tools, session := beginServerNative(t, f, bot, "create")
	first := startReviewedServer(t, f, bot, tools, "create-server")
	_ = fixtureReceive(t, started)
	fixtureEventually(t, func() bool {
		rows := f.h.serverRegistry.Snapshot(bot.Key, bot.Project)
		return len(rows) == 1 && rows[0].Attempt.State == "running"
	})
	session.reply <- app.PromptResult{FinalText: "Server ready"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	service, _ := f.h.Service()
	ctx := context.Background()
	read, err := service.DevServers(ctx, bot.Key, "")
	if err != nil || len(read.Servers) != 1 {
		t.Fatal(read, err)
	}
	info := read.Servers[0]
	q := app.DevServerControl{Key: bot.Key, ID: first.ID, Fingerprint: info.Fingerprint, Attempt: info.Attempt, Revision: info.Revision, IdempotencyKey: "restart", Action: "restart"}
	restarted, err := service.ControlDevServer(ctx, q)
	if err != nil || restarted.Server == nil || restarted.Server.Attempt == first.Attempt {
		t.Fatal(restarted, err)
	}
	second := fixtureReceive(t, started)
	fixtureEventually(t, func() bool {
		rows := f.h.serverRegistry.Snapshot(bot.Key, bot.Project)
		return rows[0].Attempt.State == "running"
	})
	if again, err := service.ControlDevServer(ctx, q); err != nil || again.Server == nil || again.Server.Attempt != restarted.Server.Attempt || count.Load() != 2 {
		t.Fatal("human retry started another process", again, err, count.Load())
	}
	stale := q
	stale.Action = "stop"
	stale.IdempotencyKey = "old-stop"
	if _, err := service.ControlDevServer(ctx, stale); pa.Code(err) != pa.StaleVersion || second.closed.Load() {
		t.Fatal("old user control stopped the restart", err)
	}
	foreign := q
	foreign.ID = "foreign"
	if _, err := service.ControlDevServer(ctx, foreign); pa.Code(err) != pa.NotFound {
		t.Fatal(err)
	}
	read, err = service.DevServers(ctx, bot.Key, "")
	if err != nil {
		t.Fatal(err)
	}
	info = read.Servers[0]
	stop := app.DevServerControl{Key: bot.Key, ID: info.ID, Fingerprint: info.Fingerprint, Attempt: info.Attempt, Revision: info.Revision, IdempotencyKey: "stop", Action: "stop"}
	if _, err := service.ControlDevServer(ctx, stop); err != nil || !second.closed.Load() {
		t.Fatal(err)
	}
	if err := f.h.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if logs, err := service.DevServers(ctx, bot.Key, info.ID); err != nil || logs.Log == nil {
		t.Fatal("disabled human history was hidden", logs, err)
	}
	if _, err := service.ControlDevServer(ctx, stop); err == nil {
		t.Fatal("disabled history granted a mutation")
	}
}

func TestHumanRestartRefusalAfterStoppingRequiresInspection(t *testing.T) {
	f, count, started := newServerHostFixture(t)
	bot := f.create(t, "human-partial-restart")
	tools, _ := beginServerNative(t, f, bot, "create")
	info := startReviewedServer(t, f, bot, tools, "source")
	process := fixtureReceive(t, started)
	fixtureEventually(t, func() bool {
		rows := f.h.serverRegistry.Snapshot(bot.Key, bot.Project)
		return rows[0].Attempt.State == "running"
	})
	current := serverInfo(f.h.serverRegistry.Snapshot(bot.Key, bot.Project)[0])
	authority := app.HumanServerAuthority{Principal: "synthetic-person", Check: func(context.Context) error {
		if process.closed.Load() {
			return failure(pa.Revoked, "human_grant")
		}
		return nil
	}}
	_, err := (hostDevServers{f.h}).Control(context.Background(), bot.Key, bot.Project, authority, app.DevServerControl{Key: bot.Key, ID: info.ID, Fingerprint: current.Fingerprint, Attempt: current.Attempt, Revision: current.Revision, IdempotencyKey: "restart", Action: "restart"})
	if pa.Code(err) != pa.OutcomeUnknown || !process.closed.Load() || count.Load() != 1 {
		t.Fatal("partially applied restart looked unapplied", err, process.closed.Load(), count.Load())
	}
}

func TestSavedServerRuleCannotBypassARevokedHumanControl(t *testing.T) {
	f, count, started := newServerHostFixture(t)
	bot := f.create(t, "human-revoked")
	tools, session := beginServerNative(t, f, bot, "create")
	info := startReviewedServer(t, f, bot, tools, "source")
	_ = fixtureReceive(t, started)
	fixtureEventually(t, func() bool {
		rows := f.h.serverRegistry.Snapshot(bot.Key, bot.Project)
		return rows[0].Attempt.State == "running"
	})
	rows := f.h.serverRegistry.Snapshot(bot.Key, bot.Project)
	if err := f.h.serverRegistry.StopExpected(context.Background(), rows[0]); err != nil {
		t.Fatal(err)
	}
	session.reply <- app.PromptResult{FinalText: "Dormant server"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	rows = f.h.serverRegistry.Snapshot(bot.Key, bot.Project)
	current := serverInfo(rows[0])
	var checks atomic.Int32
	authority := app.HumanServerAuthority{Principal: "synthetic-person", Check: func(context.Context) error {
		if checks.Add(1) > 1 {
			return failure(pa.Revoked, "human_grant")
		}
		return nil
	}}
	_, err := (hostDevServers{f.h}).Control(context.Background(), bot.Key, bot.Project, authority, app.DevServerControl{Key: bot.Key, ID: info.ID, Fingerprint: current.Fingerprint, Attempt: current.Attempt, Revision: current.Revision, IdempotencyKey: "start", Action: "start"})
	if pa.Code(err) != pa.Revoked || count.Load() != 1 {
		t.Fatal("standing source grant substituted for human authority", err, count.Load())
	}
}
