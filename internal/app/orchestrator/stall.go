package orchestrator

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A child that was briefed and then sat still.
//
// Measured on 2026-09-25: the broker typed a child its first line, the model
// answered a lone `<br>` in one second and ended its turn, and the tab sat at
// an empty composer for forty minutes. No receipt came, so the task stayed
// `spawning`; the tab was there and quiet, which the spawn clock deliberately
// decides nothing about (spawnVerdict); and nothing told the root, which would
// have learned at the 240-minute timeout. One line typed into the tab — "you
// stopped; read CHILD.md and start" — got it working at once.
//
// So the beat watches for exactly that shape, and only that shape:
//
//   - the briefing was typed (the tab is recorded and the task is not
//     Unbriefed), the task is still `spawning`, and it has not signed;
//   - every reading of its screen for stallIdleLimit said idle: the
//     assistant's prompt, an empty composer, no menu, no live working line.
//     Any other reading — working, a dialog, a draft, a screen that could not
//     be read — starts the interval again. A child reading files draws a live
//     line, so a slow child is never mistaken for one that stopped.
//
// Then it is typed one short nudge naming its CHILD.md, never the secret —
// the secret is already in its context, and a line typed into a terminal is
// a line anybody reading that terminal has. The nudge is recorded on the task
// before it is typed (Stall.NudgedAt, `task.nudged`), so a daemon that
// restarts, forgetting every screen it read, never types a second one.
//
// If it is still unsigned and idle stallReportLimit after the nudge, the task
// ends: `spawn_failed`, with a verdict that says stalled and a notice of kind
// `task_stalled` on the same ladder a finished child's takes. Ended rather
// than left running, because there is no cancel route: the root's one answer
// is `/respawn`, and a respawn is only of a spawn_failed task. Its tab is
// closed as every spawn_failed tab is (closeChild) — it is idle, and the
// respawn opens a fresh one.

const (
	// stallIdleLimit is how long a briefed, unsigned child must read idle,
	// continuously, before it is nudged. A child that reads its briefing
	// draws a working line within seconds and signs within a minute or two;
	// five minutes of an empty prompt is not a child reading files.
	stallIdleLimit = 5 * time.Minute
	// stallReportLimit is how long it must then stay idle and unsigned after
	// the nudge before its root is told. The nudge is one turn: a child that
	// answers it signs inside that turn.
	stallReportLimit = 5 * time.Minute
)

// Stall is what the broker did about a child that sat idle after its briefing.
type Stall struct {
	// NudgedAt is when the one nudge was recorded, just before it was typed.
	NudgedAt time.Time `json:"nudged_at,omitempty"`
	// NudgeError is why typing it failed, when it did. The nudge is not tried
	// again: at most one per task, and the report follows either way.
	NudgeError string `json:"nudge_error,omitempty"`
	// ReportedAt is when the broker decided to tell the root. It is written
	// before the settlement, so a settlement a crash interrupted is finished
	// by the next pass rather than judged again.
	ReportedAt time.Time `json:"reported_at,omitempty"`
}

// Stalled reports whether the broker ended this task because its child sat
// idle after its briefing.
func (r Record) Stalled() bool {
	return r.Stall != nil && !r.Stall.ReportedAt.IsZero()
}

// stallWatch is the beat's memory of since when each candidate's screen has
// read idle. In memory on purpose: a restart forgets it and the interval
// starts again, which only ever waits longer.
type stallWatch struct {
	mu   sync.Mutex
	idle map[string]time.Time
}

// since records one reading and answers since when the task has read idle
// without a break, or false when this reading was not idle.
func (w *stallWatch) since(id string, idle bool, now time.Time) (time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !idle {
		delete(w.idle, id)
		return time.Time{}, false
	}
	if w.idle == nil {
		w.idle = map[string]time.Time{}
	}
	at, ok := w.idle[id]
	if !ok {
		w.idle[id] = now
		at = now
	}
	return at, true
}

func (w *stallWatch) forget(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.idle, id)
}

// stallCandidate is a task whose briefing was typed and which has signed
// nothing since.
func stallCandidate(r Record) bool {
	return r.State == StateSpawning && r.AcceptedAt.IsZero() && !r.Unbriefed &&
		r.ChildTerminalID != "" && !r.SpawnedAt.IsZero()
}

// tendStall is the beat's look at one live task. It answers true when the
// task became terminal.
func (b *Broker) tendStall(ctx context.Context, rd reading, r Record, p *Pulse) bool {
	if !stallCandidate(r) {
		b.stalls.forget(r.ID)
		return false
	}
	if r.Stalled() {
		return b.reportStall(ctx, rd, r, p)
	}
	now := b.now()
	since, idle := b.stalls.since(r.ID, b.childIdle(ctx, rd, r), now)
	if !idle {
		return false
	}
	if r.Stall == nil || r.Stall.NudgedAt.IsZero() {
		if now.Sub(since) >= stallIdleLimit {
			b.nudge(ctx, r, p)
		}
		return false
	}
	if since.Before(r.Stall.NudgedAt) {
		since = r.Stall.NudgedAt
	}
	if now.Sub(since) < stallReportLimit {
		return false
	}
	marked, err := b.mutate(ctx, r.ID, "task.stalled", func(t *Record) error {
		if !stallCandidate(*t) || t.Stall == nil || t.Stalled() {
			return errUnchanged
		}
		t.Stall.ReportedAt = now
		return nil
	})
	if err != nil || !marked.Stalled() || marked.State != StateSpawning {
		return false
	}
	return b.reportStall(ctx, rd, marked, p)
}

// childIdle is one reading of the child's own screen: idle only on positive
// evidence — the tab is in the reading, its screen answered, the assistant's
// prompt is drawn with nothing in it, and nothing on it is a menu.
func (b *Broker) childIdle(ctx context.Context, rd reading, r Record) bool {
	if _, present := rd.session(r.ChildTerminalID); !present || b.Screen == nil {
		return false
	}
	screen, ok := b.Screen(ctx, r.ChildTerminalID)
	if !ok {
		return false
	}
	assistant := session.Assistant(r.Assistant)
	state, _ := session.ReadState(screen, assistant)
	return state == session.StateIdle && !Choosing(screen, assistant) &&
		ReadComposer(screen, assistant) == ComposerEmpty
}

// nudge records the one nudge and then types it, through the same lane every
// typed line takes (b.Type). A typing that fails is recorded and not retried.
func (b *Broker) nudge(ctx context.Context, r Record, p *Pulse) {
	now := b.now()
	// Recorded by this call and no other: a beat that read the task before
	// another's nudge was written finds it there, and types nothing.
	recorded := false
	_, err := b.mutate(ctx, r.ID, "task.nudged", func(t *Record) error {
		recorded = false
		if !stallCandidate(*t) || (t.Stall != nil && !t.Stall.NudgedAt.IsZero()) {
			return errUnchanged
		}
		t.Stall = &Stall{NudgedAt: now}
		recorded = true
		return nil
	})
	if err != nil || !recorded {
		return
	}
	p.Nudged++
	if err := b.typeLine(ctx, r.ChildTerminalID, b.nudgeLine(r)); err != nil {
		_, _ = b.mutate(ctx, r.ID, "task.nudge.failed", func(t *Record) error {
			if t.Stall == nil {
				return errUnchanged
			}
			t.Stall.NudgeError = truncate(err.Error(), 300)
			return nil
		})
	}
}

// nudgeLine is the one line typed at a child that stopped after its briefing.
func (b *Broker) nudgeLine(r Record) string {
	return fmt.Sprintf("You stopped after your Clawdline briefing without signing for it, and this tab has been idle "+
		"for %d minutes. Read %s and follow it exactly, starting with the receipt; the task secret is in your "+
		"first message.", int(stallIdleLimit/time.Minute), filepath.Join(b.Tasks.Path(r.ID), "CHILD.md"))
}

// reportStall ends a stalled task and opens its root's notice, in one
// settlement, and closes the tab it leaves when this pass's reading shows it.
func (b *Broker) reportStall(ctx context.Context, rd reading, r Record, p *Pulse) bool {
	_, present := rd.session(r.ChildTerminalID)
	_, ids, err := b.settle(ctx, r.ID, StateSpawnFailed, stalledVerdict(r), nil, b.closeChild(r, present)...)
	if err != nil {
		return false
	}
	b.stalls.forget(r.ID)
	p.Stalled++
	p.SpawnFail++
	for _, res := range b.runRecorded(ctx, ids) {
		if res.state == store.EffectDone && res.outcome == "closed" {
			p.Closed++
		}
	}
	return true
}

// stalledVerdict is the sentence the task ends with.
func stalledVerdict(r Record) string {
	nudge := "was nudged once"
	if r.Stall != nil && r.Stall.NudgeError != "" {
		nudge = "could not be nudged (" + r.Stall.NudgeError + ")"
	}
	return fmt.Sprintf("The child stalled: its briefing was typed, it never signed for it, and its tab sat idle at an "+
		"empty prompt for %d minutes, %s, and stayed idle %d minutes more. Respawn it, or dispatch it again.",
		int(stallIdleLimit/time.Minute), nudge, int(stallReportLimit/time.Minute))
}

// NoticeKind is the `kind` a task's notice is typed and listed with.
func NoticeKind(r Record) string {
	if r.Stalled() {
		return "task_stalled"
	}
	return "task_finished"
}
