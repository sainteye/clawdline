package projectlinks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a hand-wound clock, so the freshness window is tested without
// waiting thirty seconds for it.
type clock struct{ at atomic.Int64 }

func (c *clock) now() time.Time          { return time.Unix(0, c.at.Load()) }
func (c *clock) advance(d time.Duration) { c.at.Add(int64(d)) }

func testCache(t *testing.T, limit int) (*Cache, *clock) {
	t.Helper()
	c := NewCache()
	if limit > 0 {
		c.SetLimit(int64(limit))
	}
	k := &clock{}
	k.at.Store(int64(time.Hour))
	c.now = k.now
	return c, k
}

func counted(n *atomic.Int64, label string) func(context.Context) Reading {
	return func(context.Context) Reading {
		n.Add(1)
		return Reading{Links: []Link{{Label: label, Kind: "site", State: "ok"}}}
	}
}

// TestOnlyTheFirstReadIsSynchronous, and everything after it is served from
// what is held.
func TestOnlyTheFirstReadIsSynchronous(t *testing.T) {
	c, _ := testCache(t, 0)
	var reads atomic.Int64
	read := counted(&reads, "one")

	got, at := c.Get(context.Background(), "/projects/widgets", read)
	if len(got.Links) != 1 || at.IsZero() {
		t.Fatalf("the first read answers and is stamped: %+v %v", got, at)
	}
	for i := 0; i < 10; i++ {
		if _, again := c.Get(context.Background(), "/projects/widgets", read); !again.Equal(at) {
			t.Fatal("a held reading keeps the moment it was taken")
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("ten sheets opened in a row cost one walk, not %d", reads.Load())
	}
}

// TestStaleIsServedNotWithheld: the whole reason this is a projection. A
// thirty-second-old link is a correct answer to "where can I go from here";
// a spinner is not.
func TestStaleIsServedNotWithheld(t *testing.T) {
	c, k := testCache(t, 0)
	var reads atomic.Int64

	first, firstAt := c.Get(context.Background(), "/projects/widgets", counted(&reads, "one"))
	if first.Links[0].Label != "one" {
		t.Fatalf("unexpected first answer: %+v", first)
	}
	k.advance(FreshFor + time.Second)

	// The stale rows come straight back — the caller waits for nothing — and
	// the refresh happens behind the request.
	stale, staleAt := c.Get(context.Background(), "/projects/widgets", counted(&reads, "two"))
	if stale.Links[0].Label != "one" {
		t.Fatalf("what is held is served, however old: %+v", stale)
	}
	if !staleAt.Equal(firstAt) {
		t.Fatal("observedAt says when these rows were taken, not when they were handed out")
	}
	c.Settle()
	if reads.Load() != 2 {
		t.Fatalf("the refresh was taken behind the request: %d reads", reads.Load())
	}
	fresh, freshAt := c.Get(context.Background(), "/projects/widgets", counted(&reads, "three"))
	if fresh.Links[0].Label != "two" {
		t.Fatalf("the refresh replaced what is held: %+v", fresh)
	}
	if !freshAt.After(firstAt) {
		t.Fatal("and stamped it with its own moment")
	}
	if reads.Load() != 2 {
		t.Fatalf("a fresh reading costs nothing: %d reads", reads.Load())
	}
}

// TestOneRefreshAtATimePerDirectory: ten readers of one stale directory start
// one walk between them, not ten.
func TestOneRefreshAtATimePerDirectory(t *testing.T) {
	c, k := testCache(t, 0)
	var reads atomic.Int64
	started := make(chan struct{}, 16)
	release := make(chan struct{})
	slow := func(context.Context) Reading {
		reads.Add(1)
		started <- struct{}{}
		<-release
		return Reading{}
	}
	c.Get(context.Background(), "/projects/widgets", func(context.Context) Reading { return Reading{} })
	k.advance(FreshFor + time.Second)
	for i := 0; i < 10; i++ {
		c.Get(context.Background(), "/projects/widgets", slow)
	}
	// Wait for the one refresh to have begun rather than for a clock: what is
	// being tested is that the other nine started none.
	<-started
	if reads.Load() != 1 {
		t.Fatalf("one refresh in flight per directory, not %d", reads.Load())
	}
	close(release)
	c.Settle()
	if reads.Load() != 1 {
		t.Fatalf("and none of the nine was waiting to start: %d", reads.Load())
	}
}

// TestTheDirectoryReadLongestAgoGoesFirst — read, not computed: a directory
// nobody looks at should go even if a refresh touched it a moment ago.
func TestTheDirectoryReadLongestAgoGoesFirst(t *testing.T) {
	c, k := testCache(t, 2)
	empty := func(context.Context) Reading { return Reading{} }
	c.Get(context.Background(), "/a", empty)
	k.advance(time.Second)
	c.Get(context.Background(), "/b", empty)
	k.advance(time.Second)
	// Reading /a again makes /b the one nobody has looked at.
	c.Get(context.Background(), "/a", empty)
	k.advance(time.Second)
	c.Get(context.Background(), "/c", empty)

	if r := c.Reading(); r.Used != 2 || r.Counters.Evicted != 1 {
		t.Fatalf("the register row counts what is held and what was let go: %+v", r)
	}
	var reads int
	c.Get(context.Background(), "/a", func(context.Context) Reading { reads++; return Reading{} })
	c.Get(context.Background(), "/b", func(context.Context) Reading { reads++; return Reading{} })
	if reads != 1 {
		t.Fatalf("/b was the one let go, so only it is walked again: %d reads", reads)
	}
}

// TestNothingHeldIsNothingLost: a miss costs a walk and loses no fact, which
// is what makes this a cache rather than evidence.
func TestNothingHeldIsNothingLost(t *testing.T) {
	c, _ := testCache(t, 0)
	var reads atomic.Int64
	read := counted(&reads, "one")
	c.Get(context.Background(), "/projects/widgets", read)
	c.Invalidate()
	if r := c.Reading(); r.Used != 0 {
		t.Fatalf("invalidate forgets everything: %+v", r)
	}
	c.Get(context.Background(), "/projects/widgets", read)
	if reads.Load() != 2 {
		t.Fatalf("and the next reader walks it again: %d reads", reads.Load())
	}
}

// TestAReadingNobodyLookedAtIsNotServed is the Board report "the deploy
// finished and the bar stayed at 100%". A deploy running at 09:40 was the last
// thing held for that directory; nobody opened the session again until 11:14,
// and the first answer then was the 09:40 rows — `running`, which the page
// draws as elapsed against typical and clamps at 100%. Stale-while-refresh is
// right for a directory somebody is watching and wrong for one they are only
// coming back to: past ServeStaleFor the held rows are read again before
// anything is answered.
func TestAReadingNobodyLookedAtIsNotServed(t *testing.T) {
	c, k := testCache(t, 0)
	var reads atomic.Int64

	c.Get(context.Background(), "/projects/widgets", counted(&reads, "running"))
	k.advance(94 * time.Minute)

	got, at := c.Get(context.Background(), "/projects/widgets", counted(&reads, "ok"))
	if got.Links[0].Label != "ok" {
		t.Fatalf("an hour and a half later the answer is today's, not the held one: %+v", got)
	}
	if !at.Equal(k.now()) {
		t.Fatalf("observedAt is when the new rows were taken: %v, want %v", at, k.now())
	}
	c.Settle()
	if reads.Load() != 2 {
		t.Fatalf("one read then, one read now, and nothing behind the request: %d", reads.Load())
	}

	// Inside the window the projection behaves as before: served at once,
	// refreshed behind.
	k.advance(ServeStaleFor - time.Second)
	if again, _ := c.Get(context.Background(), "/projects/widgets", counted(&reads, "later")); again.Links[0].Label != "ok" {
		t.Fatalf("a reading still inside ServeStaleFor is served: %+v", again)
	}
	c.Settle()
}

// TestALateRefreshDoesNotOverwriteANewerRead: a refresh started behind one
// request can finish after a synchronous read taken by the next. What it
// carries is older, and the older rows must not win.
func TestALateRefreshDoesNotOverwriteANewerRead(t *testing.T) {
	c, k := testCache(t, 0)
	c.Get(context.Background(), "/projects/widgets", func(context.Context) Reading {
		return Reading{Links: []Link{{Label: "old"}}}
	})
	newer := k.now().Add(time.Hour)
	c.store("/projects/widgets", Reading{Links: []Link{{Label: "new"}}}, newer)
	c.store("/projects/widgets", Reading{Links: []Link{{Label: "late"}}}, newer.Add(-time.Minute))
	if got, at := c.Get(context.Background(), "/projects/widgets", nil); got.Links[0].Label != "new" || !at.Equal(newer) {
		t.Fatalf("the newer reading stays: %+v at %v", got, at)
	}
}
