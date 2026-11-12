package main

import (
	"context"
	"io"
	"time"
)

// Keep one native host for a mode/profile in charge of receipt recovery and
// polling. The kernel releases ownership on process death; never unlink this
// lock file, since a waiter may already have the same inode open.
func browserProfileLock(ctx context.Context, path string) (io.Closer, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, ok, err := tryBrowserProfileLock(path)
		if err != nil {
			return nil, err
		}
		if ok {
			return lock, nil
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
