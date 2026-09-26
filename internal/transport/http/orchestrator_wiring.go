package http

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Where the broker gets its hands.
//
// Every dependency below is a function rather than a held value, and that is
// the point: the broker's questions — which sessions are live, is that one
// showing a menu, which terminal does this machine open — are all about *now*,
// and a held answer is wrong by the time the next beat asks.
//
// *Now* is not one thing, though, and the two words for it are `freshReading`
// and `reading` (server.go). The beat's own reading of the machine decides
// whether a child's tab is still there, so it is taken for that call and never
// answered from one already on the shelf; everything the pass then asks about
// a row it has already read is answered from that same reading, because asking
// the machine again per task both costs a full scan and puts two decisions in
// one pass at two different moments.

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
		AssistantQuotas: func(now time.Time) ([]orchestrator.AssistantQuotaSnapshot, error) {
			rows := make([]orchestrator.AssistantQuotaSnapshot, 0, len(limits.Assistants))
			for _, id := range limits.Assistants {
				q := quotaReader().Quota(id, now)
				row := orchestrator.AssistantQuotaSnapshot{
					ID: id, Label: assistantLabel(id), Installed: q.Installed,
					Availability: string(q.Availability), ObservedAt: q.ObservedAt,
					Stale: q.Stale, ResetsAt: q.ResetsAt, Detail: q.Detail,
					LastKnown: string(q.LastKnown), UnknownReason: string(q.Reason),
					Windows: []orchestrator.AssistantQuotaWindow{},
				}
				if q.ObservedAt != nil {
					row.AgeSeconds = ageSeconds(*q.ObservedAt, now)
				}
				if q.FreshFor > 0 {
					fresh := q.FreshFor
					row.FreshForSeconds = &fresh
				}
				for _, window := range q.Windows {
					row.Windows = append(row.Windows, orchestrator.AssistantQuotaWindow{
						Name: window.Name, UsedPercent: window.UsedPercent,
						ResetsAt: window.ResetsAt, Hit: window.Hit,
					})
				}
				rows = append(rows, row)
			}
			return rows, nil
		},
		// The beat's own reading, and the only consumer that may not be
		// answered from a held one: it decides from this whether a child's tab
		// is still there, and that decision settles a task and closes a
		// session. A snapshot taken before the tab opened would be evidence of
		// something nobody observed (D05 ③).
		Live: func(ctx context.Context) []session.Session {
			return s.freshReading(ctx).Sessions
		},
		Reading: func(ctx context.Context) session.Inventory {
			return s.freshReading(ctx)
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
		// The completion notice's way past Claude Code's composer
		// (orchestrator stash.go): the same send, with a keystroke and a look
		// first, inside the same lane turn.
		TypePrepared: func(ctx context.Context, terminalID, text string, prepare orchestrator.Prepare) error {
			_, err := s.actions().SendPrepared(ctx, terminalID, text, prepare)
			return err
		},
		// Read at every stash, so a binding the person adds needs no restart.
		// Absent or unreadable is Claude Code's defaults.
		StashRebound: func() bool {
			home, err := os.UserHomeDir()
			if err != nil {
				return false
			}
			data, err := os.ReadFile(filepath.Join(home, ".claude", "keybindings.json"))
			return err == nil && orchestrator.StashReboundIn(data)
		},
		// A session showing a menu is read from the reading this daemon has
		// already taken, rather than by capturing its screen again: a second
		// capture is a second subprocess against a terminal somebody is typing
		// in, and the answer would be from a different moment anyway.
		//
		// Both of these are asked once per task inside a pass that has just
		// taken a reading of its own, so they read the held one. Scanning the
		// machine again per task was the amplification this change is about,
		// and the answer would be from a different moment than the one the
		// pass decided everything else on.
		Choosing: func(ctx context.Context, terminalID string) bool {
			for _, item := range s.reading(ctx).Sessions {
				if item.ID == terminalID {
					return item.Menu != nil
				}
			}
			return false
		},
		// The row is found in the held reading; the screen itself is captured
		// live, because what this answers is whether a briefing may be typed
		// into that tab now.
		Screen: func(ctx context.Context, terminalID string) (string, bool) {
			for _, item := range s.reading(ctx).Sessions {
				if item.ID == terminalID {
					return s.inventory.Screen.Capture(ctx, item)
				}
			}
			return "", false
		},
		Launcher: terminal.NewLauncher(),
		// The person decided that asking for a new Claude Code session in a
		// registered project answers Claude Code's workspace-trust question
		// for that folder (projects.TrustClaudeProject).
		TrustClaudeProject: func(dir string) error {
			config, err := projects.ClaudeConfigPath()
			if err != nil {
				return err
			}
			_, err = projects.TrustClaudeProject(config, dir)
			return err
		},
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
		// The person's switch for agent-authored pushes, read at every
		// notification so turning it off needs no restart. Absent or
		// unreadable is the setting's default, on.
		NotifyEnabled: s.agentNotifyEnabled,
		ProcessStart:  swiftstore.ProcessStart,
		LeaseLine:     int(CapacityLimit(capacity.LeasesQueue)),
		OpenWaits:     int(CapacityLimit(capacity.WaitsOpen)),
		Port:          s.cfg.Port,
		Dir:           s.cfg.Dir,
		Language:      brokerLanguage(s),
		MaxChildren:   brokerMaxChildren(s),
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
// daemon's directory, the retired Swift app's is read in its place, read-only
// — the one file under ~/.config/clawdline this broker still opens, and only
// while its own is missing (U8) and the legacy switch is on (cutover B1). A
// file that cannot be read is an empty one, which was the Swift app's reading
// too: the policy is advice to a child.
//
// **When the fallback is what answered, it is said out loud, once.** The Swift
// app stopped on 2026-09-19 and its directory stayed where it was, so this is
// no longer a read that ends by itself when the transition does — it can go on
// quietly feeding every briefing on this machine out of a directory nothing
// maintains. The line names both paths so the answer is a copy command, not an
// investigation. Once, not per dispatch: this runs on every dispatch and a
// line per dispatch is noise nobody reads.
func dispatchPolicy(dir string) (base, local string) {
	legacy := ""
	if home, err := os.UserHomeDir(); err == nil && !swiftstore.Disabled() {
		legacy = filepath.Join(home, ".config", "clawdline")
	}
	base, local, src := orchestrator.ReadPolicy(dir, legacy)
	if src.Local == "legacy" {
		legacyPolicyWarned.Do(func() {
			log.Printf("orchestrator: the local dispatch policy every briefing carries is still the retired "+
				"Swift app's copy at %s, because %s does not exist. Nothing writes that directory any more; "+
				"copy the file across and this daemon reads its own.",
				filepath.Join(legacy, orchestrator.PolicyLocalFile),
				filepath.Join(dir, orchestrator.PolicyLocalFile))
		})
	}
	return base, local
}

// legacyPolicyWarned keeps the sentence above to one a process.
var legacyPolicyWarned sync.Once

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
	return defaultCatalog
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
