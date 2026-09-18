// Package lane is the per-key bounded lane: one holder per key at a time, and
// one ceiling on how many holders and waiters the whole machine admits
// (docs/design-decisions.md D06 ①).
//
// Its first key is a terminal, and the invariant it keeps is the one the Swift
// app paid for in `23ae603a`: **one writer to one terminal at a time**. A
// briefing, a completion notice, a relayed message and a person's send are
// each a sequence of keystrokes — the words, then a Return; a picture send is
// words, pastes and a Return — and two of them in flight to the same pane
// interleave into a line nobody wrote. The first version of the Go broker had
// no lane at all: its notice pump, its briefing and the message route could
// all type into one terminal at once (G09).
//
// Two answers are kept apart on purpose. A key that is busy is a wait, bounded
// by the caller's context. A machine that is full is a refusal — Busy, before
// anything is typed — because a queue with no ceiling is a pile, and a caller
// told nothing waits for ever.
package lane

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// DefaultLimit is the machine's admission ceiling: holders plus waiters,
// across every key. Sixteen is four times the busiest thing this daemon does
// to terminals at once today — a root's children being briefed (the
// per-root ceiling is five) while its notices are pumped and a person types —
// and small enough that a runaway loop meets the ceiling within a second.
const DefaultLimit = 16

// Lanes is one set of lanes and its ceiling. The zero value is not usable;
// use New.
type Lanes struct {
	limit int

	mu       sync.Mutex
	admitted int
	keys     map[string]*slot

	refused atomic.Int64
}

type slot struct {
	turn chan struct{}
	refs int
}

// New makes a set of lanes admitting at most limit holders and waiters.
func New(limit int) *Lanes {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Lanes{limit: limit, keys: map[string]*slot{}}
}

// Busy is the refusal: the machine was full, or the caller gave up waiting for
// its key. Nothing was typed in either case.
type Busy struct {
	Key   string
	Limit int
	// Waited is true when the caller's context ended while it queued for a
	// busy key, false when admission refused it outright.
	Waited bool
}

func (b Busy) Error() string {
	if b.Waited {
		return fmt.Sprintf("the terminal %s was still being written to when this request gave up waiting; nothing was typed", b.Key)
	}
	return fmt.Sprintf("this machine already has %d terminal writes in hand; nothing was typed, try again shortly", b.Limit)
}

// Acquire takes key's lane, waiting behind its current holder, and answers the
// function that gives it back. The release is safe to call more than once.
func (l *Lanes) Acquire(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	if l.admitted >= l.limit {
		l.mu.Unlock()
		l.refused.Add(1)
		return nil, Busy{Key: key, Limit: l.limit}
	}
	l.admitted++
	s := l.keys[key]
	if s == nil {
		s = &slot{turn: make(chan struct{}, 1)}
		l.keys[key] = s
	}
	s.refs++
	l.mu.Unlock()

	select {
	case s.turn <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-s.turn
				l.leave(key, s)
			})
		}, nil
	case <-ctx.Done():
		l.leave(key, s)
		l.refused.Add(1)
		return nil, Busy{Key: key, Limit: l.limit, Waited: true}
	}
}

func (l *Lanes) leave(key string, s *slot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.admitted--
	s.refs--
	if s.refs == 0 && l.keys[key] == s {
		delete(l.keys, key)
	}
}

// Stats is the lanes' account of themselves, for /v1/diagnostics: how many
// writes are held or waiting, against what ceiling, on how many terminals, and
// how many were refused since this process started.
type Stats struct {
	Admitted int
	Limit    int
	Keys     int
	Refused  int64
}

// Stats reads the account.
func (l *Lanes) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Stats{Admitted: l.admitted, Limit: l.limit, Keys: len(l.keys), Refused: l.refused.Load()}
}

// TerminalKey is the lane a session's terminal is written through. The
// backend is part of it: an iTerm2 session id and a tmux pane id are different
// namespaces, and two terminals must never share a lane by coincidence of
// spelling.
func TerminalKey(backend, id string) string { return "terminal:" + backend + ":" + id }
