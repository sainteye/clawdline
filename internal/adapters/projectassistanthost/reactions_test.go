package projectassistanthost

import (
	"context"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"testing"
)

func TestNativeReactionReplacesOwnAndRefusesLateTargets(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	if _, e := n.tools.React(ctx, "👍", ""); e != nil {
		t.Fatal(e)
	}
	if _, e := n.tools.React(ctx, "🎉", ""); e != nil {
		t.Fatal(e)
	}
	x, e := n.f.h.store.Export(ctx, n.bot.Key)
	if e != nil || len(x.Reactions) != 1 || len(x.Reactions[0].Actor.Emojis) != 1 || x.Reactions[0].Actor.Emojis[0] != "🎉" {
		t.Fatal("native replacement", x.Reactions, e)
	}
	if _, e = n.tools.React(ctx, "👍", "foreign"); e == nil {
		t.Fatal("foreign recipient accepted")
	}
	if e = n.f.h.Disable(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = n.tools.React(ctx, "✅", ""); e == nil {
		t.Fatal("late revoked effect accepted")
	}
}

func TestNativeReactionRefusesChangedSourceAndStoppedTurn(t *testing.T) {
	for _, mode := range []string{"source", "project", "same-binding-turn", "generation", "stop"} {
		t.Run(mode, func(t *testing.T) {
			n := startNative(t, nil)
			ctx := context.Background()
			switch mode {
			case "source":
				n.f.revoked.Store(true)
			case "project":
				n.f.projectChanged.Store(true)
			case "same-binding-turn":
				replaceNativeTurn(t, n, false)
			case "generation":
				replaceNativeTurn(t, n, true)
			case "stop":
				service, e := n.f.h.Service()
				if e != nil {
					t.Fatal(e)
				}
				if _, e = service.Stop(ctx, app.StopRequest{Command: pa.Command{Key: n.bot.Key, Operation: pa.StopOperation, IdempotencyKey: "stop-reaction"}, Fence: n.prompt.Turn.Fence, TurnID: n.prompt.Turn.ID}); e != nil {
					t.Fatal(e)
				}
			}
			before, e := n.f.h.store.Export(ctx, n.bot.Key)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = n.tools.React(ctx, "👍", ""); e == nil {
				t.Fatal("stale effect accepted")
			}
			after, e := n.f.h.store.Export(ctx, n.bot.Key)
			if e != nil || len(after.Reactions) != len(before.Reactions) {
				t.Fatal("refused effect changed durable reactions", after.Reactions, e)
			}
		})
	}
}
