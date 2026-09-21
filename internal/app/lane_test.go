package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// overlapHost is a terminal that counts how many writes are inside it at once,
// per pane. A write takes long enough that two unserialised ones meet.
type overlapHost struct {
	panes []string
	mu    sync.Mutex
	in    map[string]int
	peak  map[string]int
	total atomic.Int64
}

func newOverlapHost(panes ...string) *overlapHost {
	return &overlapHost{panes: panes, in: map[string]int{}, peak: map[string]int{}}
}

func (h *overlapHost) write(id string) {
	h.mu.Lock()
	h.in[id]++
	if h.in[id] > h.peak[id] {
		h.peak[id] = h.in[id]
	}
	h.mu.Unlock()
	h.total.Add(1)
	time.Sleep(15 * time.Millisecond)
	h.mu.Lock()
	h.in[id]--
	h.mu.Unlock()
}

func (h *overlapHost) Peak(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.peak[id]
}

func (h *overlapHost) Name() string { return "tmux" }
func (h *overlapHost) Inventory(context.Context) (session.Inventory, error) {
	inv := session.Inventory{Complete: true, Provenance: "tmux"}
	for _, p := range h.panes {
		inv.Sessions = append(inv.Sessions, session.Session{ID: p, TTY: "tty" + p, Backend: session.BackendTmux,
			Assistant: session.AssistantClaude})
	}
	return inv, nil
}
func (h *overlapHost) Open(context.Context, ports.OpenRequest) (session.Session, error) {
	return session.Session{}, nil
}
func (h *overlapHost) Send(_ context.Context, s session.Session, _ string) error {
	h.write(s.ID)
	return nil
}
func (h *overlapHost) Interrupt(_ context.Context, s session.Session) error {
	h.write(s.ID)
	return nil
}
func (h *overlapHost) Close(context.Context, session.Session) error                { return nil }
func (h *overlapHost) Reveal(context.Context, session.Session, bool) error         { return nil }
func (h *overlapHost) Screen(context.Context, session.Session, int) (string, bool) { return "", false }

// ② Two writers — here eight: the broker's briefing, its notice pump, a
// relayed message and a person's sends are all Actions.Send — aimed at one
// terminal overlap zero times. Before W1 nothing stood between them (G09).
//
// The control runs first and is the same writes aimed at the same host
// without Actions: it has to see an overlap, or this test could not tell a
// lane from a host too fast to be caught.
func TestTwoWritersToOneTerminalNeverOverlap(t *testing.T) {
	ctx := context.Background()
	hammer := func(n int, write func(i int)) {
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				write(i)
			}(i)
		}
		close(start)
		wg.Wait()
	}

	control := newOverlapHost("%1")
	pane := session.Session{ID: "%1", Backend: session.BackendTmux}
	hammer(8, func(int) { _ = control.Send(ctx, pane, "x") })
	if control.Peak("%1") < 2 {
		t.Fatalf("control: eight unserialised writes never met (peak %d); this test cannot see an overlap", control.Peak("%1"))
	}

	host := newOverlapHost("%1", "%2")
	a := Actions{
		Inventory: Inventory{Terminals: []ports.TerminalHost{host}},
		Terminals: []ports.TerminalHost{host},
	}
	hammer(8, func(i int) {
		var err error
		if i%4 == 3 {
			_, err = a.Interrupt(ctx, "%1")
		} else {
			_, err = a.Send(ctx, "%1", "line")
		}
		if err != nil {
			t.Errorf("write %d: %v", i, err)
		}
	})
	if got := host.Peak("%1"); got != 1 {
		t.Fatalf("writes to one terminal overlapped: peak %d at once, want 1", got)
	}
	if host.total.Load() != 8 {
		t.Fatalf("%d of 8 writes happened", host.total.Load())
	}

	// The lane is per terminal, not one lock for the machine: two panes are
	// written at once.
	hammer(8, func(i int) {
		id := "%1"
		if i%2 == 1 {
			id = "%2"
		}
		if _, err := a.Send(ctx, id, "line"); err != nil {
			t.Errorf("write %d: %v", i, err)
		}
	})
	if host.Peak("%1") != 1 || host.Peak("%2") != 1 {
		t.Fatalf("per-pane peaks %d/%d, want 1/1", host.Peak("%1"), host.Peak("%2"))
	}
}
