package skillmenu

import (
	"container/list"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// FreshFor is how long one reading is served before it is read again: the
// Swift app's `SessionLinksCache.skills` window. A skill added a minute ago
// can wait the rest of it; a menu that walks the skills directories on every
// `/` pays that walk on every keystroke that opens it.
const FreshFor = 5 * time.Minute

// Cache holds readings by key — working directory or rollout, and assistant —
// at most the `cache.session_skills` row's limit of them, letting go of the
// one used longest ago to make room. Everything in it is read again on a
// miss, so letting go costs a walk and loses nothing.
type Cache struct {
	mu          sync.Mutex
	limit       int
	order       *list.List // the front is the most recent
	items       map[string]*list.Element
	evicted     int64
	lastEvicted time.Time
	now         func() time.Time
}

type held struct {
	key string
	at  time.Time
	r   Reading
}

// NewCache is an empty cache at the register row's default limit.
func NewCache() *Cache {
	return &Cache{
		limit: int(capacity.Default(capacity.CacheSessionSkills)),
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

// Get answers from the cache while the reading is fresh, and otherwise reads
// again with read. The read happens outside the lock: two askers for one cold
// key may both read, and the later answer is the one kept.
func (c *Cache) Get(key string, read func() Reading) (Reading, time.Time) {
	c.mu.Lock()
	if e, ok := c.items[key]; ok {
		h := e.Value.(*held)
		if c.now().Sub(h.at) < FreshFor {
			c.order.MoveToFront(e)
			c.mu.Unlock()
			return h.r, h.at
		}
	}
	c.mu.Unlock()

	r := read()
	at := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		h := e.Value.(*held)
		h.r, h.at = r, at
		c.order.MoveToFront(e)
		return r, at
	}
	c.items[key] = c.order.PushFront(&held{key: key, at: at, r: r})
	c.trim()
	return r, at
}

// trim lets go of the oldest until the cache is within its limit. The caller
// holds the lock.
func (c *Cache) trim() {
	for c.order.Len() > c.limit {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*held).key)
		c.evicted++
		c.lastEvicted = c.now()
	}
}

// Reading is the `cache.session_skills` row: how many readings it holds, and
// how many it has let go since this process started.
func (c *Cache) Reading() capacity.Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	return capacity.Reading{Known: true, Used: int64(c.order.Len()),
		Counters: capacity.Counters{Evicted: c.evicted, LastActionAt: c.lastEvicted}}
}
