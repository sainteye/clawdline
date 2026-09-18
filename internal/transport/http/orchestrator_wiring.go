package http

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Where the broker gets its hands.
//
// Every dependency below is a function rather than a held value, and that is
// the point: the broker's questions — which sessions are live, is that one
// showing a menu, which terminal does this machine open — are all about *now*,
// and a held answer is wrong by the time the next beat asks.

func newBroker(s *Server) *orchestrator.Broker {
	return &orchestrator.Broker{
		Store: s.store,
		Tasks: taskdir.New(s.cfg.Dir),
		Git:   git.New(),
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
		Lanes:    app.TerminalLanes(),
		Terminal: func() projects.TerminalChoice {
			values, err := nextconfig.Open(s.cfg.Dir).Read()
			if err != nil {
				return projects.TerminalAuto
			}
			choice, _ := values.String("terminal")
			return projects.ParseTerminalChoice(choice)
		},
		Policy:      dispatchPolicy,
		Port:        s.cfg.Port,
		Dir:         s.cfg.Dir,
		Language:    brokerLanguage(s),
		MaxChildren: brokerMaxChildren(s),
	}
}

// dispatchPolicy is this Mac's house rules, read at every dispatch so that a
// person editing them does not have to restart anything: the base file and the
// person's local one, apart, because only the broker's composition knows which
// of the two may be cut (orchestrator.ComposePolicy, D23 ①).
//
// Both files are the Swift app's own paths, and they are read rather than
// copied: they are the person's words about how work is handed out on this
// machine, and a second copy under this daemon's directory would be a second
// set of house rules nobody meant to write. Nothing else under that directory
// is opened — plan.md §4 lists these two among the read-only paths (D23 ②).
// A file that cannot be read is an empty one here, which is the Swift app's
// reading too: the policy is advice to a child, and its absence is not a fault.
func dispatchPolicy() (base, local string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(home, ".config", "clawdline", name))
		if err != nil {
			return ""
		}
		return string(body)
	}
	return read("dispatch-policy.md"), read("dispatch-policy.local.md")
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
