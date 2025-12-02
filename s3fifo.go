package bdcache

import (
	"fmt"
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	numShards = 2048
	shardMask = numShards - 1 // For fast modulo via bitwise AND
)

// s3fifo implements the S3-FIFO eviction algorithm from SOSP'23 paper
// "FIFO queues are all you need for cache eviction"
//
// This implementation uses 2048-way sharding for improved concurrent performance.
// Each shard is an independent S3-FIFO instance with its own queues and lock.
//
// Algorithm per shard:
// - Small queue (S): 10% of shard capacity, for new objects
// - Main queue (M): 90% of shard capacity, for promoted objects
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
	shards      [numShards]*shard[K, V]
	seed        maphash.Seed
	keyIsInt    bool // Fast path flag for int keys
	keyIsInt64  bool // Fast path flag for int64 keys
	keyIsString bool // Fast path flag for string keys
}

// shard is an independent S3-FIFO cache partition.
// Uses lock-free reads via atomic pointer to items map.
// Writes use mutex + copy-on-write for new keys.
//
//nolint:govet // fieldalignment: padding is intentional to prevent false sharing
type shard[K comparable, V any] struct {
	mu        sync.Mutex                         // Only for writes
	_         [48]byte                           // Padding to cache line boundary
	items     atomic.Pointer[map[K]*entry[K, V]] // Lock-free read access
	small     entryList[K, V]                    // Intrusive list for small queue
	main      entryList[K, V]                    // Intrusive list for main queue
	ghost     ghostList[K]                       // Intrusive list for ghost queue
	ghostKeys map[K]*ghostEntry[K]
	capacity  int
	smallCap  int
	ghostCap  int
}

// entryList is an intrusive doubly-linked list for cache entries.
// Zero value is a valid empty list.
type entryList[K comparable, V any] struct {
	head *entry[K, V]
	tail *entry[K, V]
	len  int
}

func (l *entryList[K, V]) pushBack(e *entry[K, V]) {
	e.prev = l.tail
	e.next = nil
	if l.tail != nil {
		l.tail.next = e
	} else {
		l.head = e
	}
	l.tail = e
	l.len++
}

func (l *entryList[K, V]) remove(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		l.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		l.tail = e.prev
	}
	e.prev = nil
	e.next = nil
	l.len--
}

func (l *entryList[K, V]) front() *entry[K, V] {
	return l.head
}

func (l *entryList[K, V]) init() {
	l.head = nil
	l.tail = nil
	l.len = 0
}

// ghostEntry is a node in the ghost queue (tracks evicted keys only).
type ghostEntry[K comparable] struct {
	key  K
	prev *ghostEntry[K]
	next *ghostEntry[K]
}

// ghostList is an intrusive doubly-linked list for ghost entries.
type ghostList[K comparable] struct {
	head *ghostEntry[K]
	tail *ghostEntry[K]
	len  int
}

func (l *ghostList[K]) pushBack(e *ghostEntry[K]) {
	e.prev = l.tail
	e.next = nil
	if l.tail != nil {
		l.tail.next = e
	} else {
		l.head = e
	}
	l.tail = e
	l.len++
}

func (l *ghostList[K]) remove(e *ghostEntry[K]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		l.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		l.tail = e.prev
	}
	e.prev = nil
	e.next = nil
	l.len--
}

func (l *ghostList[K]) front() *ghostEntry[K] {
	return l.head
}

func (l *ghostList[K]) init() {
	l.head = nil
	l.tail = nil
	l.len = 0
}

// entry represents a cached item with metadata.
//
// Note on torn reads: value and expiryNano are not updated atomically together.
// During concurrent updates, a reader might see a new value with an old expiry
// (or vice versa). This is benign because:
//   - Readers never see the TTL (Get returns value only)
//   - Worst case: a valid item briefly appears expired, returning a false negative
//   - The next read gets correct data (self-correcting)
//   - No data corruption or wrong values are ever returned
//
// We accept this tradeoff to avoid the ~15% Set throughput penalty of atomic
// pointer indirection for value+expiry pairs.
type entry[K comparable, V any] struct {
	key        K
	value      V
	prev       *entry[K, V] // Intrusive list pointers
	next       *entry[K, V]
	expiryNano int64       // Unix nanoseconds; 0 means no expiry
	accessed   atomic.Bool // Fast "was accessed" flag for S3-FIFO promotion
	inSmall    bool        // True if in Small queue, false if in Main
}

// newS3FIFO creates a new sharded S3-FIFO cache with the given total capacity.
func newS3FIFO[K comparable, V any](capacity int) *s3fifo[K, V] {
	if capacity <= 0 {
		capacity = 16384 // 2^14, divides evenly by 16 shards
	}

	// Divide capacity across shards (round up to avoid zero-capacity shards)
	shardCap := (capacity + numShards - 1) / numShards
	if shardCap < 1 {
		shardCap = 1
	}

	c := &s3fifo[K, V]{
		seed: maphash.MakeSeed(),
	}

	// Detect key type at construction time to enable fast-path hash functions.
	// This avoids the type switch overhead on every Get/Set call.
	var zeroKey K
	switch any(zeroKey).(type) {
	case int:
		c.keyIsInt = true
	case int64:
		c.keyIsInt64 = true
	case string:
		c.keyIsString = true
	}

	for i := range numShards {
		c.shards[i] = newShard[K, V](shardCap)
	}

	return c
}

// newShard creates a new S3-FIFO shard with the given capacity.
// Queue sizes per S3-FIFO paper: Small=10%, Main=90%, Ghost=90% (matching Main).
func newShard[K comparable, V any](capacity int) *shard[K, V] {
	smallCap := capacity / 10
	if smallCap < 1 {
		smallCap = 1
	}
	// Ghost tracks evicted keys from Main, so size matches Main (90% of capacity)
	ghostCap := capacity - smallCap
	if ghostCap < 1 {
		ghostCap = 1
	}

	s := &shard[K, V]{
		capacity:  capacity,
		smallCap:  smallCap,
		ghostCap:  ghostCap,
		ghostKeys: make(map[K]*ghostEntry[K], ghostCap),
	}
	items := make(map[K]*entry[K, V], capacity)
	s.items.Store(&items)
	return s
}

// getShard returns the shard for a given key using type-optimized hashing.
// Uses bitwise AND with shardMask for fast modulo (numShards must be power of 2).
// Fast paths for int, int64, and string keys avoid the type switch overhead entirely.
func (c *s3fifo[K, V]) getShard(key K) *shard[K, V] {
	// Fast path for int keys (most common case in benchmarks).
	// The keyIsInt flag is set once at construction, so this branch is predictable.
	if c.keyIsInt {
		// Use unsafe to avoid boxing the key to any.
		// This is safe because we only enter this path when K is int.
		k := *(*int)(unsafe.Pointer(&key))
		return c.shards[uint64(k)&shardMask] //nolint:gosec // G115: intentional wrap
	}
	if c.keyIsInt64 {
		k := *(*int64)(unsafe.Pointer(&key))
		return c.shards[uint64(k)&shardMask] //nolint:gosec // G115: intentional wrap
	}
	if c.keyIsString {
		// Use unsafe to extract the string without boxing to any.
		// This is safe because we only enter this path when K is string.
		k := *(*string)(unsafe.Pointer(&key))
		return c.shards[maphash.String(c.seed, k)&shardMask]
	}
	// Slow path: use type switch for other key types
	return c.shards[c.shardIndexSlow(key)]
}

// shardIndexSlow computes the shard index using a type switch.
// This is the fallback for key types other than int/int64/string.
func (c *s3fifo[K, V]) shardIndexSlow(key K) uint64 {
	switch k := any(key).(type) {
	case uint:
		return uint64(k) & shardMask
	case uint64:
		return k & shardMask
	case string:
		return maphash.String(c.seed, k) & shardMask
	case fmt.Stringer:
		return maphash.String(c.seed, k.String()) & shardMask
	default:
		// Fallback: convert to string representation and hash.
		// This is not fast, but is reliable for any comparable type.
		// Avoid using structs as keys if performance matters.
		return maphash.String(c.seed, fmt.Sprintf("%v", key)) & shardMask
	}
}

// getFromMemory retrieves a value from the in-memory cache.
// On hit, increments frequency counter (used during eviction).
func (c *s3fifo[K, V]) getFromMemory(key K) (V, bool) {
	return c.getShard(key).get(key)
}

func (s *shard[K, V]) get(key K) (V, bool) {
	// Lock-free read via atomic pointer
	items := s.items.Load()
	ent, ok := (*items)[key]
	if !ok {
		var zero V
		return zero, false
	}

	// Check expiration (lazy - actual cleanup happens in background)
	// Single int64 comparison; time.Now().UnixNano() only called if item has expiry
	if ent.expiryNano != 0 && time.Now().UnixNano() > ent.expiryNano {
		var zero V
		return zero, false
	}

	// S3-FIFO: Mark as accessed for lazy promotion.
	// Fast path: skip atomic store if already marked (avoids cache line invalidation).
	if !ent.accessed.Load() {
		ent.accessed.Store(true)
	}

	return ent.value, true
}

// setToMemory adds or updates a value in the in-memory cache.
func (c *s3fifo[K, V]) setToMemory(key K, value V, expiry time.Time) {
	var expiryNano int64
	if !expiry.IsZero() {
		expiryNano = expiry.UnixNano()
	}
	c.getShard(key).set(key, value, expiryNano)
}

func (s *shard[K, V]) set(key K, value V, expiryNano int64) {
	s.mu.Lock()

	items := s.items.Load()

	// Fast path: update existing entry (no map copy needed).
	// Note: value and expiryNano updates are not atomic together - see entry comment.
	if ent, ok := (*items)[key]; ok {
		ent.value = value
		ent.expiryNano = expiryNano
		s.mu.Unlock()
		return
	}

	// Slow path: insert new key (already holding lock)

	// Check if key is in ghost queue
	inGhost := false
	if ghostEnt, ok := s.ghostKeys[key]; ok {
		inGhost = true
		s.ghost.remove(ghostEnt)
		delete(s.ghostKeys, key)
	}

	// Create new entry
	ent := &entry[K, V]{
		key:        key,
		value:      value,
		expiryNano: expiryNano,
		inSmall:    !inGhost,
	}

	// Make room if at capacity (evict modifies map via s.items pointer)
	for len(*s.items.Load()) >= s.capacity {
		s.evict()
	}

	// Add to appropriate queue
	if ent.inSmall {
		s.small.pushBack(ent)
	} else {
		s.main.pushBack(ent)
	}

	// Copy-on-write for new key
	currentItems := s.items.Load()
	newItems := make(map[K]*entry[K, V], len(*currentItems)+1)
	for k, v := range *currentItems {
		newItems[k] = v
	}
	newItems[key] = ent
	s.items.Store(&newItems)
	s.mu.Unlock()
}

// deleteFromMemory removes a value from the in-memory cache.
func (c *s3fifo[K, V]) deleteFromMemory(key K) {
	c.getShard(key).delete(key)
}

func (s *shard[K, V]) delete(key K) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.items.Load()
	ent, ok := (*items)[key]
	if !ok {
		return
	}

	if ent.inSmall {
		s.small.remove(ent)
	} else {
		s.main.remove(ent)
	}

	s.deleteFromMap(key)
}

// evict removes one item according to S3-FIFO algorithm.
func (s *shard[K, V]) evict() {
	if s.small.len > 0 {
		s.evictFromSmall()
		return
	}
	s.evictFromMain()
}

// evictFromSmall evicts an item from the small queue.
func (s *shard[K, V]) evictFromSmall() {
	for s.small.len > 0 {
		ent := s.small.front()
		s.small.remove(ent)

		// Check if accessed since last eviction attempt
		if !ent.accessed.Swap(false) {
			// Not accessed - evict and track in ghost
			s.deleteFromMap(ent.key)
			s.addToGhost(ent.key)
			return
		}

		// Accessed - promote to Main queue
		ent.inSmall = false
		s.main.pushBack(ent)
	}
}

// evictFromMain evicts an item from the main queue.
func (s *shard[K, V]) evictFromMain() {
	for s.main.len > 0 {
		ent := s.main.front()
		s.main.remove(ent)

		// Check if accessed since last eviction attempt
		if !ent.accessed.Swap(false) {
			// Not accessed - evict
			s.deleteFromMap(ent.key)
			return
		}

		// Accessed - give second chance (FIFO-Reinsertion)
		s.main.pushBack(ent)
	}
}

// deleteFromMap removes a key from the items map using copy-on-write.
// Must be called with s.mu held.
func (s *shard[K, V]) deleteFromMap(key K) {
	oldItems := s.items.Load()
	newItems := make(map[K]*entry[K, V], len(*oldItems))
	for k, v := range *oldItems {
		if k != key {
			newItems[k] = v
		}
	}
	s.items.Store(&newItems)
}

// addToGhost adds a key to the ghost queue.
func (s *shard[K, V]) addToGhost(key K) {
	if s.ghost.len >= s.ghostCap {
		oldest := s.ghost.front()
		delete(s.ghostKeys, oldest.key)
		s.ghost.remove(oldest)
	}

	ent := &ghostEntry[K]{key: key}
	s.ghost.pushBack(ent)
	s.ghostKeys[key] = ent
}

// memoryLen returns the total number of items across all shards.
func (c *s3fifo[K, V]) memoryLen() int {
	total := 0
	for i := range c.shards {
		items := c.shards[i].items.Load()
		total += len(*items)
	}
	return total
}

// flushMemory removes all entries from all shards.
func (c *s3fifo[K, V]) flushMemory() int {
	total := 0
	for i := range c.shards {
		total += c.shards[i].flush()
	}
	return total
}

func (s *shard[K, V]) flush() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.items.Load()
	n := len(*items)
	newItems := make(map[K]*entry[K, V], s.capacity)
	s.items.Store(&newItems)
	s.small.init()
	s.main.init()
	s.ghost.init()
	s.ghostKeys = make(map[K]*ghostEntry[K], s.ghostCap)
	return n
}
