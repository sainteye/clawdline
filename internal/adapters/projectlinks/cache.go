package projectlinks

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// FreshFor is how long one reading is served before it is read again.
//
// The Swift app measured five consecutive `/links` calls at 380, 345, 365, 319
// and 390 ms (`SessionLinksCache.swift`), every one of them re-running the git
// read and the file reads, because nothing held the answer. Thirty seconds is
// short enough that a sheet opened twice in a minute sees a deploy change, and
// long enough that opening it ten times in a row costs one subprocess.
const FreshFor = 30 * time.Second

// ServeStaleFor is how old a held reading may be and still be handed out
// while a fresh one is taken behind the request.
//
// Stale-while-refresh is right for a directory somebody is watching: the
// console asks `/info` once a minute, so what it is served is never more than
// a minute and a half old. It is wrong for a directory somebody is **coming
// back to**. Measured 2026-09-24: a deploy was `running` at 09:40, finished
// `ok` at 09:45, nobody opened that session again until 11:14 — and the first
// answer then was the 09:40 rows, a `running` deploy the page drew as elapsed
// against typical, clamped at 100%. Two minutes covers the watching cadence
// with room to spare; past it the directory is read again before anything is
// answered, which is the first-read cost (a few hundred milliseconds) paid
// once on return.
const ServeStaleFor = 2 * time.Minute

// refreshTimeout bounds one refresh taken behind a request. Nothing waits for
// it, so what it must not do is outlive the reason it was started.
const refreshTimeout = 20 * time.Second

// Cache is a maintained projection of link rows by working directory.
//
// **Stale is served, not withheld.** A link row that is thirty seconds old is
// a correct answer to "where can I go from here"; a spinner is not. So a held
// reading comes back at once however old it is, a refresh is started behind
// the request when it has aged past FreshFor, and `observedAt` travels with
// the rows so a reader can say how old they are — nothing here decides that
// for it.
//
// **Stale is bounded.** Past ServeStaleFor a held reading is no longer an
// answer about this project but about some earlier hour of it, and it is read
// again synchronously, as a first read is.
//
// **The only synchronous reads are the first one for a directory and the one
// after it has gone unread past ServeStaleFor**, because in both there is
// nothing current to serve. Neither is on the session list's path nor on the
// event stream: one subprocess per session per beat is what the route it
// replaces was written to avoid.
type Cache struct {
	mu          sync.Mutex
	limit       int
	order       *list.List // the front is the most recently read
	items       map[string]*list.Element
	evicted     int64
	lastEvicted time.Time
	now         func() time.Time
	// refreshing counts the reads taken behind a request, so a test can wait
	// for them instead of sleeping.
	refreshing sync.WaitGroup
}

type held struct {
	key string
	// at is when these rows were computed, not when they were last handed
	// out: it is what `observedAt` carries.
	at time.Time
	// lastRead is what eviction looks at. A directory nobody looks at should
	// go even if a refresh touched it a moment ago.
	lastRead time.Time
	refresh  bool
	reading  Reading
}

// NewCache is an empty projection at the register row's default limit.
func NewCache() *Cache {
	return &Cache{
		limit: int(capacity.Default(capacity.CacheSessionLinks)),
		order: list.New(),
		items: map[string]*list.Element{},
		now:   time.Now,
	}
}

// SetLimit is the capacity override; zero or less keeps the one it has.
func (c *Cache) SetLimit(n int64) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = int(n)
	c.trim()
}

// Get is the held rows for one directory and when they were computed.
//
// read is called with the caller's own context on the first read for a key and
// when what is held is older than ServeStaleFor. A refresh gets a context of its own, because the request that started
// it has already been answered and its cancellation is not this work's.
func (c *Cache) Get(ctx context.Context, key string, read func(context.Context) Reading) (Reading, time.Time) {
	now := c.now()
	c.mu.Lock()
	if e, ok := c.items[key]; ok && now.Sub(e.Value.(*held).at) <= ServeStaleFor {
		h := e.Value.(*held)
		h.lastRead = now
		c.order.MoveToFront(e)
		stale := now.Sub(h.at) > FreshFor
		start := stale && !h.refresh
		if start {
			h.refresh = true
		}
		reading, at := h.reading, h.at
		c.mu.Unlock()
		if start {
			c.refresh(key, read)
		}
		return reading, at
	}
	c.mu.Unlock()

	// Nothing held, or nothing held that is still about now. Read here: a
	// first answer that is late beats a first answer that is empty, and a late
	// answer beats one from an hour ago.
	reading := read(ctx)
	at := c.now()
	c.store(key, reading, at)
	return reading, at
}

func (c *Cache) refresh(key string, read func(context.Context) Reading) {
	c.refreshing.Add(1)
	go func() {
		defer c.refreshing.Done()
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		reading := read(ctx)
		c.store(key, reading, c.now())
	}()
}

// Settle waits for every refresh taken behind a request. For tests, and for a
// shutdown that would rather not leave a git running.
func (c *Cache) Settle() { c.refreshing.Wait() }

func (c *Cache) store(key string, reading Reading, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		h := e.Value.(*held)
		// A refresh started behind one request can land after a synchronous
		// read taken by a later one. Its rows are older and do not replace
		// the newer ones; it still clears the flag, because it has finished.
		h.refresh = false
		if at.Before(h.at) {
			return
		}
		h.reading, h.at = reading, at
		if h.lastRead.IsZero() {
			h.lastRead = at
		}
		c.order.MoveToFront(e)
		return
	}
	c.items[key] = c.order.PushFront(&held{key: key, at: at, lastRead: at, reading: reading})
	c.trim()
}

// trim lets go of the directory read longest ago until the projection is
// within its limit. The caller holds the lock.
func (c *Cache) trim() {
	for c.order.Len() > c.limit {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*held).key)
		c.evicted++
		c.lastEvicted = c.now()
	}
}

// Invalidate forgets everything. A re-pointed project registry makes every
// held row a statement about the wrong place.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.items = map[string]*list.Element{}
}

// Reading is the `cache.session_links` row: how many directories it holds, and
// how many it has let go since this process started.
func (c *Cache) Reading() capacity.Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	return capacity.Reading{Known: true, Used: int64(c.order.Len()),
		Counters: capacity.Counters{Evicted: c.evicted, LastActionAt: c.lastEvicted}}
}
