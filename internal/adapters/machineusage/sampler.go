package machineusage

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// Sampler keeps the previous sample, so a dashboard polling every few seconds
// is answered from one new reading and the one before it.
//
// Two open dashboards (a laptop and a phone) share the one sampler; a request
// that arrives within minInterval of the last answer gets that answer rather
// than a share measured over a few milliseconds, which is noise. A first
// request, or one after a long quiet, waits a short interval of its own: a share
// over the last ten minutes would describe a machine that is not there any more.
type Sampler struct {
	read   func() (Sample, error)
	swapOf func(int) int64
	sleep  func(context.Context, time.Duration) error

	mu   sync.Mutex
	prev *Sample
	last *Usage
	// lastRoots is what the last answer was computed for: a cached answer is
	// reused only for the same rows.
	lastRoots string
}

const (
	minInterval = 1500 * time.Millisecond
	maxInterval = 30 * time.Second
	firstWait   = 500 * time.Millisecond
	// topOthers is how many process names outside every session are listed.
	topOthers = 6
)

// NewSampler reads this machine.
func NewSampler() *Sampler { return newSampler(Read, Swap, sleepCtx) }

func newSampler(read func() (Sample, error), swapOf func(int) int64,
	sleep func(context.Context, time.Duration) error) *Sampler {
	return &Sampler{read: read, swapOf: swapOf, sleep: sleep}
}

// Usage is this machine's usage now, with each root's tree as a group.
func (s *Sampler) Usage(ctx context.Context, roots []Root) (Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := rootsKey(roots)
	if s.last != nil && key == s.lastRoots && time.Since(s.last.At) < minInterval {
		return *s.last, nil
	}
	cur, err := s.read()
	if err != nil {
		return Usage{}, err
	}
	prev := s.prev
	if prev == nil || cur.At.Sub(prev.At) > maxInterval || cur.At.Sub(prev.At) <= 0 {
		first := cur
		if err := s.sleep(ctx, firstWait); err != nil {
			return Usage{}, err
		}
		if cur, err = s.read(); err != nil {
			return Usage{}, err
		}
		prev = &first
	}
	u := Compute(*prev, cur, roots, s.swapOf, topOthers)
	s.prev, s.last, s.lastRoots = &cur, &u, key
	return u, nil
}

func rootsKey(roots []Root) string {
	b := make([]byte, 0, len(roots)*16)
	for _, r := range roots {
		b = append(b, r.Key...)
		b = append(b, 0)
		b = strconv.AppendInt(b, int64(r.PID), 10)
		b = append(b, 1)
	}
	return string(b)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
