package http

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Where the broker gets its hands.
//
// Every dependency below is a function rather than a held value, and that is
// the point: the broker's questions — which sessions are live, is that one
// showing a menu, which terminal does this machine open — are all about *now*,
// and a held answer is wrong by the time the next beat asks.

// outboxFault is CLAWDLINE_NEXT_OUTBOX_FAULT: the name of an effect point
// (orchestrator effects.go — "committed", "started") at which this daemon
// kills itself, so that "the process died after the commit and before the
// effect" can be made to happen on the real daemon rather than argued about.
// Unset in every ordinary run.
func outboxFault() func(point string, e store.Effect) {
	want := os.Getenv("CLAWDLINE_NEXT_OUTBOX_FAULT")
	if want == "" {
		return nil
	}
	return func(point string, e store.Effect) {
		if point != want {
			return
		}
		log.Printf("orchestrator: CLAWDLINE_NEXT_OUTBOX_FAULT=%s: killing this daemon at effect %d", point, e.ID)
		if self, err := os.FindProcess(os.Getpid()); err == nil {
			_ = self.Kill()
		}
		select {}
	}
}

func newBroker(s *Server) *orchestrator.Broker {
	return &orchestrator.Broker{
		EffectFault: outboxFault(),
		Store:       s.store,
		Tasks:       taskdir.New(s.cfg.Dir),
		Git:         git.New(),
		Live: func(ctx context.Context) []session.Session {
			return s.inventory.Read(ctx).Sessions
		},
		Reading: func(ctx context.Context) session.Inventory {
			return s.inventory.Read(ctx)
		},
		Fault: beatFault(),
		// Every line the broker types — a briefing, a completion notice, a
		// relayed message — goes through the same Actions.Send a person's send
		// does, and so through that terminal's one lane (D22): the broker and a
		// person can no longer type into one terminal at once.
		Type: func(ctx context.Context, terminalID, text string) error {
			_, err := s.actions().Send(ctx, terminalID, text)
			return err
		},
		// A session showing a menu is read from the inventory this daemon has
		// already taken, rather than by capturing its screen again: a second
		// capture is a second subprocess against a terminal somebody is typing
		// in, and the answer would be from a different moment anyway.
		Choosing: func(ctx context.Context, terminalID string) bool {
			for _, item := range s.inventory.Read(ctx).Sessions {
				if item.ID == terminalID {
					return item.Menu != nil
				}
			}
			return false
		},
		Screen: func(ctx context.Context, terminalID string) (string, bool) {
			for _, item := range s.inventory.Read(ctx).Sessions {
				if item.ID == terminalID {
					return s.inventory.Screen.Capture(ctx, item)
				}
			}
			return "", false
		},
		Launcher: terminal.NewLauncher(),
		// What this machine's terminals can do, asked before a dispatch is
		// admitted (orchestrator capability.go, W7).
		TerminalCapabilities: terminal.NewLauncher().Capabilities,
		Lanes:                app.TerminalLanes(),
		Terminal: func() projects.TerminalChoice {
			values, err := nextconfig.Open(s.cfg.Dir).Read()
			if err != nil {
				return projects.TerminalAuto
			}
			choice, _ := values.String("terminal")
			return projects.ParseTerminalChoice(choice)
		},
		Policy: func() (string, string) { return dispatchPolicy(s.cfg.Dir) },
		// Both of the broker's pushes — a child's own /notify and a dead
		// letter (D24) — go the way every push from this daemon goes: the
		// subscription store with its register limit, the sender with its
		// bounded retries (push.go, after C2).
		Push: func(ctx context.Context, title, body, terminal, tag string) (int, int, error) {
			d, err := s.PushSend(ctx, title, body, terminal, tag, "")
			return d.Sent, d.Failed, err
		},
		ProcessStart: swiftstore.ProcessStart,
		LeaseLine:    int(CapacityLimit(capacity.LeasesQueue)),
		OpenWaits:    int(CapacityLimit(capacity.WaitsOpen)),
		Port:         s.cfg.Port,
		Dir:          s.cfg.Dir,
		Language:     brokerLanguage(s),
		MaxChildren:  brokerMaxChildren(s),
		// W6 (handover.go): the finished child's linger, and the sweep.
		ChildLinger:  func() time.Duration { return brokerChildLinger(s) },
		ReclaimAuto:  os.Getenv("CLAWDLINE_NEXT_RECLAIM") != "off",
		ReclaimGrace: reclaimGrace(),
	}
}

// brokerChildLinger is the `orchestrator_child_linger` setting in seconds;
// -1 leaves a finished child open for good, and absent is the Swift app's
// three minutes.
func brokerChildLinger(s *Server) time.Duration {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err == nil {
		if n, ok := values.Number("orchestrator_child_linger"); ok {
			if n < 0 {
				return -1
			}
			return time.Duration(n) * time.Second
		}
	}
	return orchestrator.LingerDefault
}

// reclaimGrace is CLAWDLINE_NEXT_RECLAIM_GRACE, for a disposable daemon's
// walk of the sweep: a Go duration, where "0s" is no grace at all. Unset is
// the broker's default of twenty-four hours.
func reclaimGrace() time.Duration {
	raw := os.Getenv("CLAWDLINE_NEXT_RECLAIM_GRACE")
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		log.Printf("orchestrator: CLAWDLINE_NEXT_RECLAIM_GRACE=%q is not a duration; the default grace stands", raw)
		return 0
	}
	if d == 0 {
		return -1
	}
	return d
}

// dispatchPolicy is this Mac's house rules, read at every dispatch so that a
// person editing them does not have to restart anything: the base and the
// person's local file, apart, because only the broker's composition knows
// which of the two may be cut (orchestrator.ComposePolicy, D23 ①).
//
// The base is this daemon's own, shipped in the repository and projected into
// its directory at start (orchestrator.ProjectPolicy, D23 ③). The local file
// is the person's and is never written here; until they have one in this
// daemon's directory, the Swift app's is read in its place, read-only — the
// one file under ~/.config/clawdline this broker still opens, and only while
// its own is missing (U8). A file that cannot be read is an empty one, which
// is the Swift app's reading too: the policy is advice to a child.
func dispatchPolicy(dir string) (base, local string) {
	legacy := ""
	if home, err := os.UserHomeDir(); err == nil {
		legacy = filepath.Join(home, ".config", "clawdline")
	}
	base, local, _ = orchestrator.ReadPolicy(dir, legacy)
	return base, local
}

func brokerLanguage(s *Server) string {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err == nil {
		if lang, ok := values.String("language"); ok && lang != "" {
			return lang
		}
	}
	// This daemon ships one catalog and /v1/strings defaults to it, so the one
	// line a child says out loud defaults to the same language rather than to
	// English nobody chose.
	return "zh-Hant"
}

func brokerMaxChildren(s *Server) int {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err == nil {
		if n, ok := values.Number("orchestrator_max_children"); ok && n > 0 {
			return int(n)
		}
	}
	return 5
}

// StartBroker runs the beat that collects results and pumps notices, under
// the supervisor that recovers it when it panics (orchestrator/observe.go).
//
// It is started by the daemon rather than by the first request, because a child
// that finished while nobody was looking has still finished, and its root is
// waiting for a line in its own tab.
func (s *Server) StartBroker(ctx context.Context) {
	// The shipped house rules, where a person can read them (D23 ③).
	if wrote, err := orchestrator.ProjectPolicy(s.cfg.Dir); err != nil {
		log.Printf("orchestrator: the dispatch policy could not be projected into %s: %v", s.cfg.Dir, err)
	} else if wrote {
		log.Printf("orchestrator: projected the shipped dispatch policy into %s", s.cfg.Dir)
	}
	tick := 5 * time.Second
	if v := os.Getenv("CLAWDLINE_NEXT_BEAT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			tick = d
		}
	}
	go s.broker.Run(ctx, tick, func(p orchestrator.Pulse) {
		s.beat.Store(&p)
	})
}
