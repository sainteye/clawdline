package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// A line that answers a status and nothing else, which is what a build with
// Cloud compiled in but nothing to publish is.
type statusOnlyLine struct{}

func (statusOnlyLine) Status() cloudtransport.Status { return cloudtransport.Status{} }

// A line that is also told the task list moved, which is what the real one is.
type taskListLine struct {
	statusOnlyLine
	told int
}

func (l *taskListLine) TaskListChanged() { l.told++ }

// **The broker's signal reaches the registered Cloud link, and a daemon with
// no link is not a crash.** This is the hop between the two halves of the
// change: the broker tells `TaskListChanged` after every commit, and the
// publisher reads its own task list on the next pass instead of on every pass.
func TestTheTaskListSignalReachesTheCloudLine(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}

	// No link registered: the broker's call is a no-op, not a panic.
	s.noteTaskListChanged()

	// A line that does not carry a task list is simply not told.
	SetCloudLine(dir, statusOnlyLine{})
	t.Cleanup(func() { SetCloudLine(dir, nil) })
	s.noteTaskListChanged()

	line := &taskListLine{}
	SetCloudLine(dir, line)
	s.noteTaskListChanged()
	s.noteTaskListChanged()
	if line.told != 2 {
		t.Errorf("the link was told %d times, want 2", line.told)
	}

	// Another machine's state directory is not this one's link.
	other := &Server{cfg: config.Config{Dir: t.TempDir()}}
	other.noteTaskListChanged()
	if line.told != 2 {
		t.Errorf("a second state directory's write was told to this link: %d", line.told)
	}
}
