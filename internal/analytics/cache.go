package analytics

import (
	"container/list"
	"sync"
)

// Cache is a small mutex-guarded LRU for computed views. The caller puts
// everything the result depends on into the key (range, timezone,
// thresholds, the store's data version), so a stale entry is simply never
// asked for again and ages out.
type Cache[K comparable, V any] struct {
	mu    sync.Mutex
	max   int
	ll    *list.List // front = most recently used
	items map[K]*list.Element
}

type cacheEntry[K comparable, V any] struct {
	key K
	val V
}

// NewCache returns a cache holding at most max entries (minimum 1).
func NewCache[K comparable, V any](max int) *Cache[K, V] {
	if max < 1 {
		max = 1
	}
	return &Cache[K, V]{max: max, ll: list.New(), items: map[K]*list.Element{}}
}

// Get returns the cached value and marks it recently used.
func (c *Cache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[k]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*cacheEntry[K, V]).val, true
	}
	var zero V
	return zero, false
}

// Put stores v, evicting the least recently used entry when full.
func (c *Cache[K, V]) Put(k K, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[k]; ok {
		e.Value.(*cacheEntry[K, V]).val = v
		c.ll.MoveToFront(e)
		return
	}
	c.items[k] = c.ll.PushFront(&cacheEntry[K, V]{key: k, val: v})
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.items, last.Value.(*cacheEntry[K, V]).key)
	}
}

// Do returns the cached value for k, or computes, stores and returns it. fn
// runs without the lock held, so two concurrent misses may both compute; the
// results are equal by construction and the last one wins.
func (c *Cache[K, V]) Do(k K, fn func() V) V {
	if v, ok := c.Get(k); ok {
		return v
	}
	v := fn()
	c.Put(k, v)
	return v
}

// Len is the number of cached entries.
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
