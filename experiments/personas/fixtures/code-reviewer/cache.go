package main

import (
	"sync"
	"time"
)

type cacheEntry struct {
	order   Order
	expires time.Time
}

// OrderCache keeps recently read orders in memory to take load off the database.
type OrderCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[int64]cacheEntry
}

func NewOrderCache(ttl time.Duration) *OrderCache {
	return &OrderCache{ttl: ttl, entries: make(map[int64]cacheEntry)}
}

func (c *OrderCache) Get(id int64) (Order, bool) {
	e, ok := c.entries[id]
	if !ok || time.Now().After(e.expires) {
		return Order{}, false
	}
	return e.order, true
}

func (c *OrderCache) Set(o Order) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[o.ID] = cacheEntry{order: o, expires: time.Now().Add(c.ttl)}
}

func (c *OrderCache) Delete(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, id)
}
