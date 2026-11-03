package http

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// waitingStartServer is a Server with a store and a broker, as serve builds
// one, whose every reading shows one Claude session standing on a question.
func waitingStartServer(t *testing.T) (*Server, session.Session) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	asking := session.Session{ID: "%7", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "conv-7", Label: "root 7", State: session.StateWaiting}
	s := &Server{cfg: config.Config{Dir: dir}, store: st,
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}}
	s.readings = app.NewInventoryReading(func(context.Context) session.Inventory {
		return session.Inventory{Sessions: []session.Session{asking}, Complete: true,
			Sources: map[string]bool{string(session.BackendTmux): true}}
	}, time.Millisecond)
	return s, asking
}

func TestObserveWaitingPushesModelCapacityWithIndependentPreference(t *testing.T) {
	s, row := waitingStartServer(t)
	row.State = session.StateIdle
	row.Assistant = session.AssistantCodex
	row.Activity.ProviderBlock = session.ProviderBlockModelCapacity
	s.readings = app.NewInventoryReading(func(context.Context) session.Inventory {
		return session.Inventory{Sessions: []session.Session{row}, Complete: true,
			Sources: map[string]bool{string(session.BackendTmux): true}}
	}, time.Millisecond)
	file := nextconfig.Open(s.cfg.Dir)
	if _, err := file.Set(map[string]any{"push_on_waiting": false, "push_on_model_capacity": false}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.broker.PushEnabled = s.notificationEnabled
	s.broker.Push = func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		calls++
		if terminal != row.ID || tag != "model-capacity-"+row.ID || title != "Model at capacity: root 7" {
			t.Fatalf("push %q %q %q", terminal, tag, title)
		}
		return 1, 0, nil
	}
	observe := func() {
		t.Helper()
		if err := s.observeWaiting(context.Background(), time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	observe()
	if calls != 0 {
		t.Fatal("disabled model notification sent")
	}
	if _, err := file.Set(map[string]any{"push_on_model_capacity": true}); err != nil {
		t.Fatal(err)
	}
	observe()
	observe()
	if calls != 1 {
		t.Fatalf("capacity stop sent %d times", calls)
	}
}

// The push for a session waiting on the person went silent for a month
// because the only call that started its watcher lived inside the retired v1
// board sweep. StartWaiting, the call serve makes, must by itself lead to one
// recorded push for a stop that has stood long enough, however many readings
// follow, and to none while push_on_waiting is off.
func TestStartWaitingPushesAStandingQuestionOnce(t *testing.T) {
	saved := waitingTick
	waitingTick = 5 * time.Millisecond
	t.Cleanup(func() { waitingTick = saved })
	t.Setenv("CLAWDLINE_NEXT_WAITING_AFTER", "1ms")

	pushes := func(s *Server, asking session.Session) int {
		t.Helper()
		got, err := s.store.Effects(context.Background(), orchestrator.EffectWaitingPush, app.WaitingKey(asking))
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}

	t.Run("on", func(t *testing.T) {
		s, asking := waitingStartServer(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.StartWaiting(ctx)
		deadline := time.Now().Add(3 * time.Second)
		for pushes(s, asking) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if n := pushes(s, asking); n != 1 {
			t.Fatalf("a question standing past its wait recorded %d pushes after StartWaiting; want 1", n)
		}
		// Many more readings of the same stop: still the one push.
		time.Sleep(100 * time.Millisecond)
		if n := pushes(s, asking); n != 1 {
			t.Fatalf("one stop read for 100ms more recorded %d pushes; want 1", n)
		}
	})

	t.Run("off", func(t *testing.T) {
		s, asking := waitingStartServer(t)
		if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"push_on_waiting": false}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.StartWaiting(ctx)
		time.Sleep(100 * time.Millisecond)
		if n := pushes(s, asking); n != 0 {
			t.Fatalf("with push_on_waiting off, %d pushes were recorded; want 0", n)
		}
		// Zero alone would also be the answer of a watcher that never ran:
		// turned back on while the question still stands, it pushes once.
		if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"push_on_waiting": true}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for pushes(s, asking) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if n := pushes(s, asking); n != 1 {
			t.Fatalf("turned back on, a question still standing recorded %d pushes; want 1", n)
		}
	})
}
