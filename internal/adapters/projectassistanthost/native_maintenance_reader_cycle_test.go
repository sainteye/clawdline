package projectassistanthost

import (
	"context"
	"testing"

	journalstore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestHistoryGateRemainsAfterDisableAndEnable(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	state := t.TempDir()
	f.o.NativeHistory = journalstore.NewNativeMaintenanceHistoryReader(state)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "disable-cycle")
	if _, err := f.send(t, bot, "seed"); err != nil {
		t.Fatal(err)
	}
	seed := fixtureReceive(t, f.p.sent)
	fixtureReceive(t, f.p.sessions).reply <- app.PromptResult{FinalText: "seed"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	binding, err := f.h.store.Binding(ctx, bot.Key, seed.Turn.Fence.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	j := establishHostHistoryFixture(t, state)
	intent := app.NativeMaintenanceIntent{
		ID: "protected", Binding: binding, PrincipalID: "server-reader", ReaderID: "server-reader",
		AuthorizationRevision: "current", CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only",
	}
	if _, reserved, err := j.Reserve(ctx, intent); !reserved || err != nil {
		t.Fatal("could not stage the protected history", reserved, err)
	}
	if _, err := j.Resolve(ctx, app.NativeMaintenanceRecord{Intent: intent, State: app.NativeCompactionUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	f.enable(t)
	receipt, err := f.send(t, bot, "guard-after-enable")
	if err != nil {
		t.Fatal(err)
	}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, bot.Key, receipt.ResourceID)
		return err == nil && turn.State == pa.TurnFailed && turn.Failure != nil && turn.Failure.Code == pa.ExecutionEffectUnknown
	})
	if f.p.opens.Load() != 1 {
		t.Fatal("disable and enable permitted native reload", f.p.opens.Load())
	}
}
