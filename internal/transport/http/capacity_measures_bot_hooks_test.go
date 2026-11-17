package http

import (
	"context"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
	"testing"
)

func TestBotHookCapacityReadsDurableNamespaceOccupancy(t *testing.T) {
	f := newPAServerFixture(t, true)
	measures := f.s.botRoutineHookCapacityMeasures()
	for _, name := range []string{"bots.hook_pending", "bots.hook_revision"} {
		r := measures[name]()
		if !r.Known || r.Used != 0 || r.Err != "" {
			t.Fatal("fresh optional storage", name, r)
		}
	}
	f.enable(t)
	repo := store.New(f.s.store.ProjectAssistantDatabase())
	ctx := context.Background()
	id := bh.Identity{AccountID: f.identity.AccountID, MachineID: f.identity.MachineID}
	hooks := []bh.Hook{{ID: "synthetic-hook", Key: f.local, Enabled: true}}
	if _, _, err := repo.PrepareCloudHooks(ctx, id, hooks); err != nil {
		t.Fatal(err)
	}
	hooks[0].Enabled = false
	if _, _, err := repo.PrepareCloudHooks(ctx, id, hooks); err != nil {
		t.Fatal(err)
	}
	if r := measures["bots.hook_revision"](); !r.Known || r.Used != 2 {
		t.Fatal("durable revision", r)
	}
	if err := repo.BeginCloudHook(ctx, id, "synthetic-delivery"); err != nil {
		t.Fatal(err)
	}
	other := id
	other.AccountID += "-other"
	if err := repo.BeginCloudHook(ctx, other, "other-delivery"); err != nil {
		t.Fatal(err)
	}
	if r := measures["bots.hook_pending"](); !r.Known || r.Used != 1 {
		t.Fatal("per-namespace limit must not sum distinct accounts", r)
	}
	for _, v := range []struct {
		id       bh.Identity
		delivery string
	}{{id, "synthetic-delivery"}, {other, "other-delivery"}} {
		if err := repo.ClearCloudHook(ctx, v.id, v.delivery); err != nil {
			t.Fatal(err)
		}
	}
	if r := measures["bots.hook_pending"](); !r.Known || r.Used != 0 {
		t.Fatal("cleared barriers", r)
	}
	if err := f.s.store.ProjectAssistantDatabase().Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bots.hook_pending", "bots.hook_revision"} {
		r := measures[name]()
		if r.Known || r.Err == "" {
			t.Fatal("unreadable storage invented a zero", name, r)
		}
	}
}
