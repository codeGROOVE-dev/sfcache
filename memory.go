// Package sfcache provides a high-performance cache with S3-FIFO eviction and optional persistence.
package sfcache

import (
	"time"
)

// MemoryCache is a fast in-memory cache without persistence.
// All operations are context-free and never return errors.
type MemoryCache[K comparable, V any] struct {
	memory     *s3fifo[K, V]
	defaultTTL time.Duration
}

// Memory creates a new memory-only cache.
//
// Example:
//
//	cache := sfcache.Memory[string, User](
//	    sfcache.WithSize(10000),
//	    sfcache.WithTTL(time.Hour),
//	)
//	defer cache.Close()
//
//	cache.Set("user:123", user)              // uses default TTL
//	cache.Set("user:123", user, time.Hour)   // explicit TTL
//	user, ok := cache.Get("user:123")
func Memory[K comparable, V any](opts ...Option) *MemoryCache[K, V] {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return &MemoryCache[K, V]{
		memory:     newS3FIFO[K, V](cfg),
		defaultTTL: cfg.defaultTTL,
	}
}

// Get retrieves a value from the cache.
// Returns the value and true if found, or the zero value and false if not found.
func (c *MemoryCache[K, V]) Get(key K) (V, bool) {
	return c.memory.get(key)
}

// GetOrSet retrieves a value from the cache, or computes and stores it if not found.
// The loader function is only called if the key is not in the cache.
// If no TTL is provided, the default TTL is used.
// This is optimized to perform a single shard lookup and lock acquisition.
func (c *MemoryCache[K, V]) GetOrSet(key K, loader func() V, ttl ...time.Duration) V {
	// We can't use the optimized path with a loader since we'd hold the lock during loader()
	if val, ok := c.memory.get(key); ok {
		return val
	}
	val := loader()
	c.Set(key, val, ttl...)
	return val
}

// SetIfAbsent stores a value only if the key is not already in the cache.
// Returns the existing value and true if found, or the new value and false if inserted.
// This is optimized to perform a single shard lookup and lock acquisition.
func (c *MemoryCache[K, V]) SetIfAbsent(key K, value V, ttl ...time.Duration) (V, bool) {
	var t time.Duration
	if len(ttl) > 0 {
		t = ttl[0]
	}
	return c.memory.getOrSet(key, value, timeToNano(c.expiry(t)))
}

// Set stores a value in the cache.
// If no TTL is provided, the default TTL is used.
// If no default TTL is configured, the entry never expires.
func (c *MemoryCache[K, V]) Set(key K, value V, ttl ...time.Duration) {
	var t time.Duration
	if len(ttl) > 0 {
		t = ttl[0]
	}
	c.memory.set(key, value, timeToNano(c.expiry(t)))
}

// Delete removes a value from the cache.
func (c *MemoryCache[K, V]) Delete(key K) {
	c.memory.del(key)
}

// Len returns the number of entries in the cache.
func (c *MemoryCache[K, V]) Len() int {
	return c.memory.len()
}

// Flush removes all entries from the cache.
// Returns the number of entries removed.
func (c *MemoryCache[K, V]) Flush() int {
	return c.memory.flush()
}

// Close releases resources held by the cache.
// For MemoryCache this is a no-op, but provided for API consistency.
func (*MemoryCache[K, V]) Close() {
	// No-op for memory-only cache
}

// expiry returns the expiry time based on TTL and default TTL.
func (c *MemoryCache[K, V]) expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}

// config holds configuration for both MemoryCache and PersistentCache.
type config struct {
	size       int
	defaultTTL time.Duration
	warmup     int
	smallRatio float64
	ghostRatio float64
}

func defaultConfig() *config {
	return &config{
		size:       16384, // 2^14, divides evenly by numShards
		smallRatio: 0.0,   // 0.0 means auto-tune based on capacity
		ghostRatio: 0.0,   // 0.0 means auto-tune based on capacity
	}
}

// Option configures a MemoryCache or PersistentCache.
type Option func(*config)

// WithSize sets the maximum number of entries in the memory cache.
func WithSize(n int) Option {
	return func(c *config) {
		c.size = n
	}
}

// WithSmallRatio sets the ratio of the small queue to the total cache size.
// Default is 0.1 (10%).
func WithSmallRatio(r float64) Option {
	return func(c *config) {
		c.smallRatio = r
	}
}

// WithGhostRatio sets the ratio of the ghost queue to the total cache size.
// Default is 1.0 (100%).
func WithGhostRatio(r float64) Option {
	return func(c *config) {
		c.ghostRatio = r
	}
}

// WithTTL sets the default TTL for cache entries.
// Entries without an explicit TTL will use this value.
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		c.defaultTTL = d
	}
}

// WithWarmup enables cache warmup by loading the N most recently updated entries
// from persistence on startup. Only applies to PersistentCache.
// By default, warmup is disabled (0). Set to a positive number to load that many entries.
func WithWarmup(n int) Option {
	return func(c *config) {
		c.warmup = n
	}
}
