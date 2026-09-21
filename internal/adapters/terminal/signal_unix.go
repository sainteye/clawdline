//go:build !windows

package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// paneSignal is `pipe-pane` into a FIFO, where the readability of the FIFO is
// the entire message.
//
// The bytes are read and thrown away. What is wanted is the wake-up: 0.014 ms
// median from a byte reaching the pane to the FIFO becoming readable, measured
// over forty samples, against 2.78 ms for the same wait when the write is a
// `send-keys` subprocess — the difference being fork and exec, not the signal.
//
// **Why a FIFO and not a file.** Both are fast enough. The FIFO wins on what
// happens when this process is not there any more: nothing lands on disk, there
// is nothing to poll, and the pipe target dies of its own accord once the
// reading end is gone — measured, the far end dies on the pane's *first* write
// and tmux clears `#{pane_pipe}` on the second. A file target keeps running and
// keeps growing, 51.8 MB per pane per working day at the measured 599 B/s, and
// would need this daemon to be alive to stop it.
//
// **The write end this holds open is not decoration.** A reader on a FIFO with
// no writer sees end-of-file, and end-of-file on a FIFO is readable, so the
// loop below would spin. Holding a descriptor of our own means the FIFO never
// reaches that state while this daemon is running, and it does not weaken the
// safety net: a writer is not a reader, so the far end's SIGPIPE still arrives
// the moment the read descriptor closes.
type paneSignal struct {
	dir  string
	pipe ports.PipeHost

	mu      sync.Mutex
	entries map[string]*signalEntry
	moved   func(string)
}

type signalEntry struct {
	fifo  string
	read  *os.File
	write *os.File
	done  chan struct{}
}

// NewPaneSignal is the tmux change signal on a platform that has FIFOs.
//
// `dir` is this daemon's own directory for them, and it is the ownership
// record: a `%N.fifo` left there names a pane this daemon piped and did not
// take back. There is deliberately no second file to keep in step with it.
func NewPaneSignal(dir string, pipe ports.PipeHost) ports.PaneSignal {
	return &paneSignal{dir: dir, pipe: pipe, entries: map[string]*signalEntry{}}
}

func (p *paneSignal) OnMoved(fn func(string)) {
	p.mu.Lock()
	p.moved = fn
	p.mu.Unlock()
}

// fifoPath is the FIFO for a pane. `%31` becomes `31.fifo`: the `%` is dropped
// because it is punctuation in both a shell and a `display-message` format, and
// the id is reconstructible from the name, which is what makes the directory an
// ownership record.
func (p *paneSignal) fifoPath(paneID string) (string, bool) {
	if !IsPaneID(paneID) {
		return "", false
	}
	return filepath.Join(p.dir, paneID[1:]+".fifo"), true
}

// paneOfFIFO is the pane id a leftover FIFO names, or "" for a file this did
// not make.
func paneOfFIFO(name string) string {
	if !strings.HasSuffix(name, ".fifo") {
		return ""
	}
	digits := strings.TrimSuffix(name, ".fifo")
	if digits == "" {
		return ""
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return "%" + digits
}

// pipeCommand is what tmux runs on the far end of the pipe.
//
// Single-quoted because this daemon's own directories contain spaces, and
// refused outright if the path could close the quote: this string becomes a
// shell line inside somebody's terminal multiplexer, which is not a place to be
// generous.
func pipeCommand(path string) (string, bool) {
	if path == "" || strings.Contains(path, "'") {
		return "", false
	}
	return "cat > '" + path + "'", true
}

func (p *paneSignal) IsAttached(paneID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.entries[paneID] != nil
}

// Attach puts the signal on a pane, or reports why not. Idempotent: a pane
// already attached is already the answer.
func (p *paneSignal) Attach(paneID string) bool {
	p.mu.Lock()
	if p.entries[paneID] != nil {
		p.mu.Unlock()
		return true
	}
	p.mu.Unlock()

	fifo, ok := p.fifoPath(paneID)
	if !ok {
		return false
	}
	command, ok := pipeCommand(fifo)
	if !ok {
		return false
	}
	if os.MkdirAll(p.dir, 0o700) != nil {
		return false
	}
	// A leftover from a previous life is replaced rather than reused: its
	// permissions and its readers are not knowable from here.
	_ = os.Remove(fifo)
	if syscall.Mkfifo(fifo, 0o600) != nil {
		return false
	}
	// The reading end first, and non-blocking, because opening a FIFO for
	// reading blocks until somebody opens it for writing — and the writer is
	// the `cat` that does not exist yet.
	//
	// **`syscall.Open` and `os.NewFile`, not `os.OpenFile`, and that is the
	// difference between this working and not.** Measured on macOS 15:
	// `os.OpenFile` on a FIFO does not hand the descriptor to the runtime
	// poller, so every `Read` on an empty pipe answers `EAGAIN` at once and
	// nothing ever waits for a byte — the watcher below then either spins or
	// exits, and a pane that is writing looks exactly like a pane that never
	// moved. A descriptor opened non-blocking and wrapped with `os.NewFile` is
	// pollable: the read parks, wakes 0.3 ms after a write in the same test,
	// and `Close` from another goroutine unblocks it, which is what Detach
	// needs.
	fd, err := syscall.Open(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		_ = os.Remove(fifo)
		return false
	}
	read := os.NewFile(uintptr(fd), fifo)
	write, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		_ = read.Close()
		_ = os.Remove(fifo)
		return false
	}

	entry := &signalEntry{fifo: fifo, read: read, write: write, done: make(chan struct{})}
	p.mu.Lock()
	p.entries[paneID] = entry
	p.mu.Unlock()
	go p.watch(paneID, entry)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	attached := p.pipe.Pipe(ctx, paneID, command)
	cancel()
	if !attached {
		p.Detach(paneID)
		return false
	}
	return true
}

// watch reads and discards. The bytes are a perfect record of what the pane
// drew and this deliberately does not keep them: reconstructing a screen from
// them needs a terminal emulator, and a reader joining mid-stream is wrong for
// half the rows and at some join points never converges, because an assistant
// repaints only the lines that changed.
func (p *paneSignal) watch(paneID string, entry *signalEntry) {
	defer close(entry.done)
	buf := make([]byte, 1<<14)
	for {
		n, err := entry.read.Read(buf)
		if n > 0 {
			p.mu.Lock()
			moved, current := p.moved, p.entries[paneID]
			p.mu.Unlock()
			if current != entry {
				return
			}
			if moved != nil {
				moved(paneID)
			}
			continue
		}
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
			// With this object's own write end open a FIFO cannot reach
			// end-of-file, so both of these are the shape of a descriptor
			// going away rather than of a quiet pane.
			return
		}
		if err == nil || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			// Neither is reachable on a build where the runtime polls this
			// descriptor: a read of nothing with no error, and the refusal a
			// descriptor the poller declined answers with. Sleeping rather than
			// spinning turns the second into a 25 ms sampler instead of a hot
			// loop — a worse live screen and still a live screen — and stops the
			// first from being a watcher that quietly dies, which is the shape
			// this whole file has already been wrong in once.
			time.Sleep(25 * time.Millisecond)
			continue
		}
		return
	}
}

// Detach takes the pipe off, closes the descriptors and removes the FIFO. In
// that order, because the directory listing is the ownership record and a FIFO
// removed before the pipe is gone would be a pane nothing can find again.
func (p *paneSignal) Detach(paneID string) {
	p.mu.Lock()
	entry := p.entries[paneID]
	delete(p.entries, paneID)
	p.mu.Unlock()
	if entry == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	p.pipe.Unpipe(ctx, paneID)
	cancel()
	_ = entry.read.Close()
	_ = entry.write.Close()
	<-entry.done
	_ = os.Remove(entry.fifo)
}

func (p *paneSignal) DetachAll() {
	p.mu.Lock()
	ids := make([]string, 0, len(p.entries))
	for id := range p.entries {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		p.Detach(id)
	}
}

// Abandoned is the panes this daemon piped in a previous life, as named by the
// FIFOs it left behind.
func (p *paneSignal) Abandoned() []string {
	items, err := os.ReadDir(p.dir)
	if err != nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, item := range items {
		pane := paneOfFIFO(item.Name())
		if pane == "" || p.entries[pane] != nil {
			continue
		}
		out = append(out, pane)
	}
	sort.Strings(out)
	return out
}

func (p *paneSignal) Forget(paneID string) {
	p.mu.Lock()
	held := p.entries[paneID] != nil
	p.mu.Unlock()
	if held {
		return
	}
	if fifo, ok := p.fifoPath(paneID); ok {
		_ = os.Remove(fifo)
	}
}
