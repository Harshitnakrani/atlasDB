package cache

import (
	"sync"
)

type node[K comparable, V any] struct {
	key   K
	value V
	prev  *node[K, V]
	next  *node[K, V]
}

// LRUCache is a concurrency-safe in-memory cache for query plans and metadata.
type LRUCache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	items    map[K]*node[K, V]
	head     *node[K, V]
	tail     *node[K, V]
}

func NewLRUCache[K comparable, V any](capacity int) *LRUCache[K, V] {
	if capacity <= 0 {
		capacity = 64
	}
	return &LRUCache[K, V]{
		capacity: capacity,
		items:    make(map[K]*node[K, V]),
	}
}

func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, found := c.items[key]
	if !found {
		var zero V
		return zero, false
	}
	c.moveToHead(n)
	return n.value, true
}

func (c *LRUCache[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if n, found := c.items[key]; found {
		n.value = value
		c.moveToHead(n)
		return
	}

	if len(c.items) >= c.capacity {
		c.removeTail()
	}

	n := &node[K, V]{key: key, value: value}
	c.items[key] = n
	c.addToHead(n)
}

func (c *LRUCache[K, V]) addToHead(n *node[K, V]) {
	n.prev = nil
	n.next = c.head
	if c.head != nil {
		c.head.prev = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *LRUCache[K, V]) removeNode(n *node[K, V]) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		c.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		c.tail = n.prev
	}
}

func (c *LRUCache[K, V]) moveToHead(n *node[K, V]) {
	if c.head == n {
		return
	}
	c.removeNode(n)
	c.addToHead(n)
}

func (c *LRUCache[K, V]) removeTail() {
	if c.tail == nil {
		return
	}
	delete(c.items, c.tail.key)
	c.removeNode(c.tail)
}
