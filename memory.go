// Package multicache provides a high-performance cache with optional persistence.
package multicache

import (
	"sync"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
)

// Cache is an in-memory cache. All operations are synchronous and infallible.
type Cache[K comparable, V any] struct {
	flights    *xsync.Map[K, *flightCall[V]]
	memory     *s3fifo[K, V]
	defaultTTL time.Duration
	noExpiry   bool
}

// flightCall holds an in-flight computation for singleflight deduplication.
//
//nolint:govet // fieldalignment: semantic grouping preferred
type flightCall[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// New creates an in-memory cache.
func New[K comparable, V any](opts ...Option) *Cache[K, V] {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return &Cache[K, V]{
		flights:    xsync.NewMap[K, *flightCall[V]](),
		memory:     newS3FIFO[K, V](cfg),
		defaultTTL: cfg.defaultTTL,
		noExpiry:   cfg.defaultTTL == 0,
	}
}

// Get returns the value for key, or zero and false if not found.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	return c.memory.get(key)
}

// Set stores a value. Uses default TTL if none provided.
func (c *Cache[K, V]) Set(key K, value V, ttl ...time.Duration) {
	if c.noExpiry && len(ttl) == 0 {
		c.memory.set(key, value, 0)
		return
	}
	var t time.Duration
	if len(ttl) > 0 {
		t = ttl[0]
	}
	c.memory.set(key, value, timeToNano(c.expiry(t)))
}

// Delete removes a key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.memory.del(key)
}

// SetIfAbsent stores value only if key is missing. No deduplication.
func (c *Cache[K, V]) SetIfAbsent(key K, value V, ttl ...time.Duration) (V, bool) {
	if val, ok := c.memory.get(key); ok {
		return val, true
	}
	var t time.Duration
	if len(ttl) > 0 {
		t = ttl[0]
	}
	c.memory.set(key, value, timeToNano(c.expiry(t)))
	return value, false
}

// GetSet returns cached value or calls loader to compute it.
// Concurrent calls for the same key share one loader invocation.
func (c *Cache[K, V]) GetSet(key K, loader func() (V, error), ttl ...time.Duration) (V, error) {
	if val, ok := c.memory.get(key); ok {
		return val, nil
	}

	call, loaded := c.flights.LoadOrCompute(key, func() (*flightCall[V], bool) {
		fc := &flightCall[V]{}
		fc.wg.Add(1)
		return fc, false
	})

	if loaded {
		// Another goroutine is computing; wait for result.
		call.wg.Wait()
		return call.val, call.err
	}

	// We're the first; check cache again then compute.
	if val, ok := c.memory.get(key); ok {
		c.flights.Delete(key)
		call.wg.Done()
		return val, nil
	}

	val, err := loader()
	if err == nil {
		var t time.Duration
		if len(ttl) > 0 {
			t = ttl[0]
		}
		c.memory.set(key, val, timeToNano(c.expiry(t)))
	}

	call.val, call.err = val, err
	c.flights.Delete(key)
	call.wg.Done()

	return val, err
}

// Len returns the number of entries.
func (c *Cache[K, V]) Len() int {
	return c.memory.len()
}

// Flush removes all entries. Returns count removed.
func (c *Cache[K, V]) Flush() int {
	return c.memory.flush()
}

// Close is a no-op for Cache (provided for API symmetry with TieredCache).
func (*Cache[K, V]) Close() {}

func (c *Cache[K, V]) expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}

type config struct {
	size       int
	defaultTTL time.Duration
}

func defaultConfig() *config {
	return &config{size: 16384}
}

// Option configures a Cache.
type Option func(*config)

// Size sets maximum entries. Default 16384.
func Size(n int) Option {
	return func(c *config) { c.size = n }
}

// TTL sets default expiration. Default 0 (none).
func TTL(d time.Duration) Option {
	return func(c *config) { c.defaultTTL = d }
}
