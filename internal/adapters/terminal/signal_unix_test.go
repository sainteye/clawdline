//go:build !windows

package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pipeSpy stands in for tmux. The signal's whole contract is that the FIFO's
// readability is the message, so what is proved here needs no tmux server: the
// far end of the pipe is an ordinary writer.
type pipeSpy struct {
	command string
	piped   map[string]bool
}

func (p *pipeSpy) Pipe(ctx context.Context, paneID, command string) bool {
	p.command = command
	if p.piped == nil {
		p.piped = map[string]bool{}
	}
	p.piped[paneID] = true
	return true
}

func (p *pipeSpy) Unpipe(ctx context.Context, paneID string) bool {
	delete(p.piped, paneID)
	return true
}

func (p *pipeSpy) PipedPanes(ctx context.Context) map[string]bool { return p.piped }

// TestPaneSignalWakesOnWrite is the one property the whole live screen rests
// on: a byte written to the FIFO wakes the watcher.
//
// It is here because the first build of this did not. The read end is opened
// non-blocking so that it does not wait for a writer that does not exist yet,
// and a reader that treats the resulting EAGAIN as end-of-file exits before the
// first byte ever arrives — which looks exactly like a pane that never moved.
func TestPaneSignalWakesOnWrite(t *testing.T) {
	dir := t.TempDir()
	spy := &pipeSpy{}
	signal := NewPaneSignal(dir, spy)
	moved := make(chan string, 8)
	signal.OnMoved(func(id string) { moved <- id })

	if !signal.Attach("%7") {
		t.Fatal("attach refused")
	}
	defer signal.DetachAll()
	if spy.command != "cat > '"+dir+"/7.fifo'" {
		t.Fatalf("far end is %q", spy.command)
	}

	// The writer tmux would have started, and nothing else: one open, one write.
	writer, err := os.OpenFile(dir+"/7.fifo", os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("a writer could not open the fifo: %v", err)
	}
	defer writer.Close()
	if _, err := writer.Write([]byte("the pane drew something")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case id := <-moved:
		if id != "%7" {
			t.Fatalf("woke for %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a byte on the fifo did not wake the watcher")
	}
}

// TestPaneSignalDetachIsOrdered proves the ownership record survives exactly as
// long as the pipe does: the FIFO is the only thing that names a pane this
// daemon piped, so it is removed last and an abandoned one is still findable.
func TestPaneSignalDetachIsOrdered(t *testing.T) {
	dir := t.TempDir()
	spy := &pipeSpy{}
	signal := NewPaneSignal(dir, spy)
	if !signal.Attach("%12") {
		t.Fatal("attach refused")
	}
	if !signal.IsAttached("%12") {
		t.Fatal("attached pane does not say so")
	}
	signal.Detach("%12")
	if signal.IsAttached("%12") {
		t.Fatal("detached pane still says it is attached")
	}
	if spy.piped["%12"] {
		t.Fatal("the pipe was left on the pane")
	}
	if _, err := os.Stat(dir + "/12.fifo"); !os.IsNotExist(err) {
		t.Fatalf("the fifo outlived the pipe: %v", err)
	}
}

// TestPaneSignalNamesAbandoned is how a previous life's pipes are found again.
func TestPaneSignalNamesAbandoned(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/31.fifo", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/notes.txt", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	signal := NewPaneSignal(dir, &pipeSpy{})
	got := signal.Abandoned()
	if len(got) != 1 || got[0] != "%31" {
		t.Fatalf("abandoned is %v", got)
	}
	signal.Forget("%31")
	if got := signal.Abandoned(); len(got) != 0 {
		t.Fatalf("forgotten pane is still named: %v", got)
	}
}

// TestPipeCommandRefusesAQuote is the one place a path this daemon owns becomes
// a shell line inside somebody's terminal multiplexer.
func TestPipeCommandRefusesAQuote(t *testing.T) {
	if _, ok := pipeCommand("/tmp/it's/874.fifo"); ok {
		t.Fatal("a path that can close the quote was accepted")
	}
	if _, ok := pipeCommand(""); ok {
		t.Fatal("an empty path was accepted")
	}
	if got, ok := pipeCommand("/tmp/a b/874.fifo"); !ok || got != "cat > '/tmp/a b/874.fifo'" {
		t.Fatalf("a path with a space came out %q (%v)", got, ok)
	}
}

func TestSessionSignalPreservesAnExistingTmuxPipe(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clawdline-pipe-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := &Tmux{Binary: bin, Socket: filepath.Join(dir, "private.sock")}
	id, err := host.call(ctx, "", "-f", "/dev/null", "new-session", "-d", "-P", "-F", "#{pane_id}", "cat")
	if err != nil {
		t.Fatalf("start private server: %v", err)
	}
	defer host.run(ctx, "kill-server")
	id = strings.TrimSpace(id)
	if err := host.run(ctx, "pipe-pane", "-t", id, "cat >/dev/null"); err != nil {
		t.Fatal(err)
	}
	fifoDir := filepath.Join(dir, "space $and \"quotes\"")
	signal := NewPaneSignal(fifoDir, host)
	defer signal.DetachAll()
	moved := make(chan string, 8)
	signal.OnMoved(func(id string) { moved <- id })
	if signal.Attach(id) {
		t.Fatal("signal took ownership of a pipe it did not install")
	}
	if !host.PipedPanes(ctx)[id] || len(signal.Abandoned()) != 0 {
		t.Fatal("refusal removed the existing pipe or left a false ownership record")
	}
	if !host.Unpipe(ctx, id) || !signal.Attach(id) {
		t.Fatal("the vacant pane did not accept a signal pipe")
	}
	if err := host.run(ctx, "send-keys", "-t", id, "-l", "redraw"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-moved:
		if got != id {
			t.Fatalf("woke for %q, expected %q", got, id)
		}
	case <-ctx.Done():
		t.Fatal("the newly installed pipe did not deliver a signal")
	}
}
