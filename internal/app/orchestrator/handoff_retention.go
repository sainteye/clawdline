package orchestrator

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A handoff package under <state dir>/handoffs/<id> is kept while its
// handoff is under way and for HandoffPackageRetentionSecondsLimit after it
// ended, then removed (person's decision, 2026-11-27). It has ended when it
// failed to open (spawn_failed), or when it was delivered and nothing still
// waits on it: no Board or Session-board transfer pending, and a milestone
// handoff's receiver named. The clock is the record's last write, which is
// never earlier than the end. A package with no record, an unreadable record,
// a record naming another directory, a name that is not a handoff id, or a
// symbolic link is kept and counted as unknown: nothing proves it ended.
const (
	HandoffPackageRetentionSecondsLimit = 24 * 60 * 60
	// HandoffPackageSweepLimit is the most packages one sweep removes; the
	// rest wait for the next sweep, an hour later.
	HandoffPackageSweepLimit = 64
	handoffPackageSweepEvery = time.Hour
	handoffPackageSweepFirst = 5 * time.Minute
)

type handoffSweepState struct {
	mu      sync.Mutex
	running bool
	next    time.Time
}

// HandoffSweepReport is what one sweep decided.
type HandoffSweepReport struct {
	Removed      int   `json:"removed"`
	RemovedBytes int64 `json:"removed_bytes"`
	InProgress   int   `json:"kept_in_progress"`
	Recent       int   `json:"kept_recent"`
	Unknown      int   `json:"kept_unknown"`
	Failed       int   `json:"failed"`
	Deferred     int   `json:"deferred"`
}

// handoffEnded says whether nothing still waits on this handoff's package.
func handoffEnded(h Handoff) bool {
	switch h.State {
	case HandoffSpawnFailed:
		return true
	case HandoffDelivered:
		boards := h.SessionBoards != nil && len(h.SessionBoards.Boards) > 0
		if (len(h.BoardItems) > 0 || boards) && h.BoardTransferredAt == 0 {
			return false
		}
		if h.Milestone && h.Receiver == "" {
			return false
		}
		return true
	}
	return false
}

// sweepHandoffPackagesDue runs a sweep off the beat once an hour.
func (b *Broker) sweepHandoffPackagesDue(ctx context.Context) {
	now := b.now()
	b.handoffSweep.mu.Lock()
	if b.handoffSweep.next.IsZero() {
		b.handoffSweep.next = now.Add(handoffPackageSweepFirst)
	}
	due := !b.handoffSweep.running && !now.Before(b.handoffSweep.next)
	if due {
		b.handoffSweep.running, b.handoffSweep.next = true, now.Add(handoffPackageSweepEvery)
	}
	b.handoffSweep.mu.Unlock()
	if !due {
		return
	}
	go func() {
		defer func() {
			b.handoffSweep.mu.Lock()
			b.handoffSweep.running = false
			b.handoffSweep.mu.Unlock()
		}()
		sweep, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		_, _ = b.SweepHandoffPackages(sweep)
	}()
}

// SweepHandoffPackages removes the packages of handoffs that ended at least
// the retention ago and records each removal on its handoff.
func (b *Broker) SweepHandoffPackages(ctx context.Context) (HandoffSweepReport, error) {
	var rep HandoffSweepReport
	rootPath := b.HandoffRoot()
	entries, err := os.ReadDir(rootPath)
	if errors.Is(err, fs.ErrNotExist) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return rep, err
	}
	defer root.Close()
	now := b.now()
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		name := e.Name()
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() || !uuidShape.MatchString(name) {
			rep.Unknown++
			continue
		}
		row, err := b.Store.ReadOpened(ctx, store.TableHandoffs, name)
		if err != nil {
			rep.Unknown++
			continue
		}
		h, err := decodeHandoff(row)
		if err != nil || h.ID != name || filepath.Clean(h.Dir) != filepath.Join(rootPath, name) {
			rep.Unknown++
			continue
		}
		if !handoffEnded(h) {
			rep.InProgress++
			continue
		}
		if now.Sub(row.UpdatedAt) < HandoffPackageRetentionSecondsLimit*time.Second {
			rep.Recent++
			continue
		}
		if rep.Removed >= HandoffPackageSweepLimit {
			rep.Deferred++
			continue
		}
		size := packageBytes(root, name)
		if err := root.RemoveAll(name); err != nil {
			rep.Failed++
			log.Printf("handoff packages: %s could not be removed: %v", name, err)
			continue
		}
		rep.Removed++
		rep.RemovedBytes += size
		at := now.Unix()
		if _, err := b.updateHandoff(ctx, name, "handoff.package_removed", func(x *Handoff) { x.PackageRemovedAt = at }); err != nil {
			log.Printf("handoff packages: %s was removed but its record was not marked: %v", name, err)
		}
	}
	if rep.Removed+rep.Failed+rep.Unknown > 0 {
		log.Printf("handoff packages: removed %d (%d bytes), kept %d in progress, %d recent, %d unknown; %d failed, %d deferred",
			rep.Removed, rep.RemovedBytes, rep.InProgress, rep.Recent, rep.Unknown, rep.Failed, rep.Deferred)
	}
	return rep, nil
}

// packageBytes is the size of the regular files under one package, read
// through root so no link is followed out of it.
func packageBytes(root *os.Root, name string) int64 {
	var total int64
	_ = fs.WalkDir(root.FS(), name, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
