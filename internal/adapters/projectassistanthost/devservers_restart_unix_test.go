//go:build darwin || linux

package projectassistanthost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestHostServerCleanRestartStopsAndRestartsTheRealSourceProcess(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "real-server-restart")
	tools, session := beginServerNative(t, f, bot, "one")
	type outcome struct {
		result app.DevServerResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := tools.DevServers(ctx, app.DevServerRequest{Action: "start", Key: "start", Name: "Synthetic source command", Command: "printf '%s\\n' started >> restart-receipts.txt; exec /bin/sleep 60"})
		done <- outcome{r, err}
	}()
	offer := hostSourceOffer(t, f, bot.Key)
	service, _ := f.h.Service()
	if _, err := service.Answer(ctx, pa.Command{Key: bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "allow"}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: string(pa.AllowAlways)}); err != nil {
		t.Fatal(err)
	}
	first := fixtureReceive(t, done)
	if first.err != nil || first.result.Server == nil {
		t.Fatal(first)
	}
	starts := func() int {
		b, _ := os.ReadFile(filepath.Join(f.repo, "restart-receipts.txt"))
		return strings.Count(string(b), "started\n")
	}
	fixtureEventually(t, func() bool { return starts() == 1 })
	session.reply <- app.PromptResult{FinalText: "The server belongs to the Host"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if err := f.h.Close(ctx); err != nil {
		t.Fatal("old source process cleanup was not proved", err)
	}
	if u := f.h.serverRegistry.Usage(); !u.Known || u.Processes != 0 {
		t.Fatal("old tree retained occupancy", u)
	}
	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.start(t)
	f.enable(t)
	fixtureEventually(t, func() bool { return starts() == 2 })
	rows, err := f.h.serverRegistry.Inspect(ctx, bot.Key, bot.Project)
	if err != nil || len(rows) != 1 || rows[0].ID != first.result.Server.ID || rows[0].Attempt.Identity.AttemptID == first.result.Server.Attempt {
		t.Fatal("actual source did not create a new attempt of the same server", rows, err)
	}
	if err := f.h.Close(ctx); err != nil {
		t.Fatal("new source process cleanup was not proved", err)
	}
}
