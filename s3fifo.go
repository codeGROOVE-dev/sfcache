package bdcache

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// s3fifo implements the S3-FIFO eviction algorithm from SOSP'23 paper
// "FIFO queues are all you need for cache eviction"
//
// Algorithm:
// - Small queue (S): 10% of cache, for new objects
// - Main queue (M): 90% of cache, for promoted objects
// - Ghost queue (G): Tracks evicted keys (no data)
//
// On cache miss:
//   - If object not in ghost → insert into Small
//   - If object in ghost → insert into Main (was accessed before)
//
// On eviction from Small:
//   - If freq == 0 → evict and add to ghost
//   - If freq > 0 → promote to Main and reset freq to 0
//
// On eviction from Main:
//   - If freq == 0 → evict (don't add to ghost, already there)
//   - If freq > 0 → reinsert to back of Main and decrement freq (lazy promotion)
type s3fifo[K comparable, V any] struct {
	items     map[K]*entry[K, V]
	small     *list.List
	main      *list.List
	ghost     *list.List
	ghostKeys map[K]*list.Element
	capacity  int
	smallCap  int
	mainCap   int
	ghostCap  int
	mu        sync.RWMutex
}

// entry represents a cached item with metadata.
type entry[K comparable, V any] struct {
	expiry  time.Time
	key     K
	value   V
	element *list.Element
	freq    atomic.Int32 // Access frequency counter (atomic for lock-free reads)
	inSmall bool         // True if in Small queue, false if in Main
}

// newS3FIFO creates a new S3-FIFO cache with the given capacity.
func newS3FIFO[K comparable, V any](capacity int) *s3fifo[K, V] {
	if capacity <= 0 {
		capacity = 10000
	}

	// Small queue is 10% of capacity
	smallCap := capacity / 10
	if smallCap < 1 {
		smallCap = 1
	}
	mainCap := capacity - smallCap

	return &s3fifo[K, V]{
		capacity:  capacity,
		smallCap:  smallCap,
		mainCap:   mainCap,
		ghostCap:  capacity, // Ghost queue same size as total capacity
		items:     make(map[K]*entry[K, V]),
		small:     list.New(),
		main:      list.New(),
		ghost:     list.New(),
		ghostKeys: make(map[K]*list.Element),
	}
}

// getFromMemory retrieves a value from the in-memory cache.
// On hit, increments frequency counter (used during eviction).
func (c *s3fifo[K, V]) getFromMemory(key K) (V, bool) {
	c.mu.RLock()
	ent, ok := c.items[key]
	if !ok {
		c.mu.RUnlock()
		var zero V
		return zero, false
	}

	// Fast path: check expiration while holding read lock
	// This is safe because we're only reading ent.expiry
	if !ent.expiry.IsZero() && time.Now().After(ent.expiry) {
		c.mu.RUnlock()
		var zero V
		return zero, false
	}

	val := ent.value
	c.mu.RUnlock()

	// S3-FIFO: Increment frequency on access (lazy promotion)
	// Items are promoted during eviction, not on access
	// Use atomic increment to avoid lock contention on hot path
	ent.freq.Add(1)

	return val, true
}

// setToMemory adds or updates a value in the in-memory cache.
func (c *s3fifo[K, V]) setToMemory(key K, value V, expiry time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry
	if ent, ok := c.items[key]; ok {
		ent.value = value
		ent.expiry = expiry
		ent.freq.Add(1)
		return
	}

	// Check if key is in ghost queue (previously evicted, being re-accessed)
	inGhost := false
	if ghostElem, ok := c.ghostKeys[key]; ok {
		inGhost = true
		c.ghost.Remove(ghostElem)
		delete(c.ghostKeys, key)
	}

	// Create new entry
	// S3-FIFO rule: If in ghost → insert into Main, else → insert into Small
	ent := &entry[K, V]{
		key:     key,
		value:   value,
		expiry:  expiry,
		inSmall: !inGhost,
	}
	// freq starts at 0 (atomic.Int32 zero value)

	// S3-FIFO: Make room if at total capacity
	for len(c.items) >= c.capacity {
		c.evict()
	}

	// Add to appropriate queue
	if ent.inSmall {
		ent.element = c.small.PushBack(ent)
	} else {
		ent.element = c.main.PushBack(ent)
	}

	c.items[key] = ent
}

// deleteFromMemory removes a value from the in-memory cache.
func (c *s3fifo[K, V]) deleteFromMemory(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ent, ok := c.items[key]
	if !ok {
		return
	}

	if ent.inSmall {
		c.small.Remove(ent.element)
	} else {
		c.main.Remove(ent.element)
	}

	delete(c.items, key)
}

// evict removes one item according to S3-FIFO algorithm.
// S3-FIFO prioritizes evicting from Small queue when it has items.
func (c *s3fifo[K, V]) evict() {
	// Evict from Small queue if it has items
	if c.small.Len() > 0 {
		c.evictFromSmall()
		return
	}

	// Otherwise evict from Main queue
	c.evictFromMain()
}

// evictFromSmall evicts an item from the small queue.
// S3-FIFO rule:
//   - If freq == 0: evict and add key to ghost
//   - If freq > 0: promote to Main and reset freq to 0
func (c *s3fifo[K, V]) evictFromSmall() {
	for c.small.Len() > 0 {
		elem := c.small.Front()
		ent, ok := elem.Value.(*entry[K, V])
		if !ok {
			c.small.Remove(elem)
			continue
		}

		c.small.Remove(elem)

		// One-hit wonder: never accessed since insertion
		if ent.freq.Load() == 0 {
			// Evict and track in ghost queue
			delete(c.items, ent.key)
			c.addToGhost(ent.key)
			return
		}

		// Hot item: promote to Main queue
		ent.freq.Store(0) // Reset frequency
		ent.inSmall = false
		ent.element = c.main.PushBack(ent)
	}
}

// evictFromMain evicts an item from the main queue.
// S3-FIFO rule:
//   - If freq == 0: evict (already in ghost from Small eviction)
//   - If freq > 0: reinsert to back of Main (lazy promotion) and decrement freq
func (c *s3fifo[K, V]) evictFromMain() {
	for c.main.Len() > 0 {
		elem := c.main.Front()
		ent, ok := elem.Value.(*entry[K, V])
		if !ok {
			c.main.Remove(elem)
			continue
		}

		c.main.Remove(elem)

		// Not accessed recently: evict
		if ent.freq.Load() == 0 {
			delete(c.items, ent.key)
			// Note: Don't add to ghost - item was already added when evicted from Small
			// Only Small queue evictions go to ghost
			return
		}

		// Accessed recently: lazy promotion (FIFO-Reinsertion / Second Chance)
		// Move to back of Main queue and decrement frequency
		ent.freq.Add(-1)
		ent.element = c.main.PushBack(ent)
	}
}

// addToGhost adds a key to the ghost queue for tracking.
// Ghost queue holds only keys (no values) of evicted items from Small queue.
func (c *s3fifo[K, V]) addToGhost(key K) {
	// Evict oldest ghost entry if at capacity
	if c.ghost.Len() >= c.ghostCap {
		elem := c.ghost.Front()
		if ghostKey, ok := elem.Value.(K); ok {
			c.ghost.Remove(elem)
			delete(c.ghostKeys, ghostKey)
		} else {
			c.ghost.Remove(elem)
		}
	}

	// Add key to ghost queue
	elem := c.ghost.PushBack(key)
	c.ghostKeys[key] = elem
}

// memoryLen returns the number of items in the in-memory cache.
func (c *s3fifo[K, V]) memoryLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// cleanupMemory removes expired entries from the in-memory cache.
func (c *s3fifo[K, V]) cleanupMemory() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	// Collect expired keys
	var expired []K
	for key, ent := range c.items {
		if !ent.expiry.IsZero() && now.After(ent.expiry) {
			expired = append(expired, key)
		}
	}

	// Remove expired entries
	for _, key := range expired {
		ent := c.items[key]
		if ent.inSmall {
			c.small.Remove(ent.element)
		} else {
			c.main.Remove(ent.element)
		}
		delete(c.items, key)
		removed++
	}

	return removed
}
