package transcript

import (
	"container/list"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// lru is a map that holds at most limit entries and, to make room, lets go of
// the one read or written longest ago: the capacity register's cache class
// (docs/limits.md N18). Everything in one is rebuilt on a miss, so letting go
// costs a read and loses nothing. The caller holds the lock.
type lru[V any] struct {
	limit       int
	order       *list.List // the front is the most recent
	items       map[string]*list.Element
	evicted     int64
	lastEvicted time.Time
}

type lruEntry[V any] struct {
	key   string
	value V
}

// newLRU is an empty cache at the register row's default limit.
func newLRU[V any](row string) *lru[V] {
	return &lru[V]{limit: int(capacity.Default(row)), order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru[V]) get(key string) (V, bool) {
	e, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*lruEntry[V]).value, true
}

func (c *lru[V]) put(key string, value V) {
	if e, ok := c.items[key]; ok {
		e.Value.(*lruEntry[V]).value = value
		c.order.MoveToFront(e)
		return
	}
	c.items[key] = c.order.PushFront(&lruEntry[V]{key: key, value: value})
	c.trim()
}

// setLimit is the capacity override; zero or less keeps the one it has.
func (c *lru[V]) setLimit(n int64) {
	if n > 0 {
		c.limit = int(n)
		c.trim()
	}
}

func (c *lru[V]) trim() {
	for c.order.Len() > c.limit {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*lruEntry[V]).key)
		c.evicted++
		c.lastEvicted = time.Now()
	}
}

// reading is the cache's row: how many entries it holds and how many it has
// let go since this process started.
func (c *lru[V]) reading() capacity.Reading {
	return capacity.Reading{Known: true, Used: int64(c.order.Len()),
		Counters: capacity.Counters{Evicted: c.evicted, LastActionAt: c.lastEvicted}}
}
