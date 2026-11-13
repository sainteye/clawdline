package http

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paResilienceAwait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("resilience condition did not become true")
}

func TestProjectAssistantServerRevocationDuringExecutionPreparation(t *testing.T) {
	f := newPAServerFixture(t, true)
	var caps, opens atomic.Int32
	prepared, release := make(chan struct{}), make(chan struct{})
	identity := adaptercloud.NewIdentityStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName))
	t.Cleanup(func() {
		if err := identity.Save(f.identity); err != nil {
			t.Error(err)
		}
	})
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{capability: func(b pa.Bot) (pa.Capabilities, error) {
			if caps.Add(1) == 2 {
				close(prepared)
				<-release
				replacement := f.identity
				replacement.AccountID = "synthetic-replacement-account"
				if err := identity.Save(replacement); err != nil {
					return pa.Capabilities{}, err
				}
			}
			return paServerCaps(b), nil
		}, open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			opens.Add(1)
			return nil, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "unexpected_open"}
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.lan, "revoked-during-execution-preparation"))
	select {
	case <-prepared:
	case <-time.After(3 * time.Second):
		t.Fatal("execution preparation did not start")
	}
	close(release)
	paResilienceAwait(t, func() bool { return f.s.projectAssistants.host.Usage().ActiveActors == 0 })
	x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil || len(x.Turns) != 1 || x.Turns[0].State != pa.TurnFailed || len(x.Messages) != 1 || opens.Load() != 0 {
		t.Fatalf("revoked execution persisted effects: turns=%+v messages=%d opens=%d error=%v", x.Turns, len(x.Messages), opens.Load(), err)
	}
	for _, binding := range x.Bindings {
		if binding.NativeSessionID != "" {
			t.Fatal("revoked preparation bound a native session")
		}
	}
}

func TestProjectAssistantServerCapacityAndReconnectDoNotRepeatEffects(t *testing.T) {
	f := newPAServerFixture(t, true)
	var opens atomic.Int32
	session := &paServerSession{sent: make(chan struct{})}
	limits := app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses, PromptTimeout: time.Minute, CleanupTimeout: time.Second}
	f.s.projectAssistants.limits = &limits
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			opens.Add(1)
			return session, nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	initial := f.get(t, f.local)
	first := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "first"), Text: "Synthetic first input",
		Fence: paWireFence(pa.Fence{Key: f.key, Project: paDomainProjectScope(initial.Bot.Project), ConversationID: initial.Conversation.ID, BindingID: initial.Conversation.BindingID, Generation: initial.Conversation.Generation})}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, first))
	select {
	case <-session.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no prompt")
	}
	for i := 1; i < pa.MaxPendingTurns; i++ {
		paServerOK(t, f.send(t, f.local, fmt.Sprintf("queued-%d", i)))
	}
	st := featurestore.New(f.s.store.ProjectAssistantDatabase())
	before, err := st.Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.send(t, f.local, "overflow"), 429, pa.CapacityFull)
	// A lost HTTP response can be retried at capacity, without another prompt.
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, first))
	conflict := first
	conflict.Text = "Different payload"
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, conflict), 409, pa.IdempotencyConflict)
	after, err := st.Export(context.Background(), f.key)
	if err != nil || !reflect.DeepEqual(before, after) || opens.Load() != 1 {
		t.Fatalf("capacity/reconnect mutated facts or opened another process: opens=%d error=%v", opens.Load(), err)
	}
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	after, err = st.Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	unknown, cancelled := 0, 0
	for _, turn := range after.Turns {
		switch turn.State {
		case pa.TurnUnknown:
			unknown++
		case pa.TurnCancelled:
			cancelled++
		}
	}
	if unknown != 1 || cancelled != pa.MaxPendingTurns-1 || opens.Load() != 1 || session.closes.Load() != 1 {
		t.Fatalf("disable: unknown=%d cancelled=%d opens=%d closes=%d", unknown, cancelled, opens.Load(), session.closes.Load())
	}
}

func TestProjectAssistantServerLateOpenRetainsUnknownCleanupHandle(t *testing.T) {
	f := newPAServerFixture(t, true)
	started, release := make(chan struct{}), make(chan struct{})
	session := &paServerSession{sent: make(chan struct{})}
	session.closeFails.Store(true)
	limits := app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses, PromptTimeout: time.Minute, CleanupTimeout: 30 * time.Millisecond}
	f.s.projectAssistants.limits = &limits
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			close(started)
			<-release
			return session, nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "late-open"))
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("Open did not start")
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`), 500, pa.ExecutionEffectUnknown)
	if usage := f.s.projectAssistants.host.Usage(); usage.Known || usage.Processes != 1 || usage.ActiveActors != 1 {
		t.Fatalf("late Open lost owner: %+v", usage)
	}
	close(release)
	paResilienceAwait(t, func() bool {
		x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
		return err == nil && len(x.Turns) == 1 && x.Turns[0].State == pa.TurnUnknown
	})
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`), 500, pa.ExecutionEffectUnknown)
	session.closeFails.Store(false)
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	if usage := f.s.projectAssistants.host.Usage(); !usage.Known || usage.Processes != 0 || usage.ActiveActors != 0 {
		t.Fatalf("explicit cleanup failed: %+v", usage)
	}
	select {
	case <-session.sent:
		t.Fatal("late Open sent a prompt after disable")
	default:
	}
	if session.closes.Load() < 2 {
		t.Fatal("unknown cleanup was not retried explicitly")
	}
}
