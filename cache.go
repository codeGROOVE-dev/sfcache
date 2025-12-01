// Package bdcache provides a high-performance cache with S3-FIFO eviction and optional persistence.
package bdcache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Cache is a generic cache with memory and optional persistence layers.
type Cache[K comparable, V any] struct {
	memory  *s3fifo[K, V]
	persist PersistenceLayer[K, V]
	opts    *Options
}

// New creates a new cache with the given options.
func New[K comparable, V any](ctx context.Context, options ...Option) (*Cache[K, V], error) {
	opts := &Options{
		MemorySize: 16384, // 2^14, divides evenly by 16 shards
	}
	for _, opt := range options {
		opt(opts)
	}

	cache := &Cache[K, V]{
		memory: newS3FIFO[K, V](opts.MemorySize),
		opts:   opts,
	}

	// Set persistence from options
	if opts.Persister != nil {
		p, ok := opts.Persister.(PersistenceLayer[K, V])
		if !ok {
			return nil, errors.New("invalid persister type")
		}
		cache.persist = p
		slog.Info("initialized cache with persistence")
	}

	// Run background cleanup if configured
	if cache.persist != nil && opts.CleanupEnabled {
		//nolint:contextcheck // Background cleanup uses detached context to complete independently
		go func() {
			// Create detached context with timeout - cleanup should complete independently
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			deleted, err := cache.persist.Cleanup(bgCtx, opts.CleanupMaxAge)
			if err != nil {
				slog.Warn("error during cache cleanup", "error", err)
				return
			}
			if deleted > 0 {
				slog.Info("cache cleanup complete", "deleted", deleted)
			}
		}()
	}

	// Warm up cache from persistence if configured
	if cache.persist != nil && opts.WarmupLimit > 0 {
		//nolint:contextcheck // Background warmup uses detached context to complete independently
		go func() {
			// Create detached context with timeout - warmup should complete independently
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			cache.warmup(bgCtx)
		}()
	}

	return cache, nil
}

// warmup loads entries from persistence into memory cache.
func (c *Cache[K, V]) warmup(ctx context.Context) {
	entryCh, errCh := c.persist.LoadRecent(ctx, c.opts.WarmupLimit)

	loaded := 0
	for entry := range entryCh {
		c.memory.setToMemory(entry.Key, entry.Value, entry.Expiry)
		loaded++
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			slog.Warn("error during cache warmup", "error", err, "loaded", loaded)
		}
	default:
	}

	if loaded > 0 {
		slog.Info("cache warmup complete", "loaded", loaded)
	}
}

// Get retrieves a value from the cache.
// It first checks the memory cache, then falls back to persistence if available.
//
//nolint:gocritic // unnamedResult - public API signature is intentionally clear without named returns
func (c *Cache[K, V]) Get(ctx context.Context, key K) (V, bool, error) {
	// Check memory first
	if val, ok := c.memory.getFromMemory(key); ok {
		return val, true, nil
	}

	var zero V

	// If no persistence, return miss
	if c.persist == nil {
		return zero, false, nil
	}

	// Validate key before accessing persistence (security: prevent path traversal)
	if err := c.persist.ValidateKey(key); err != nil {
		slog.Warn("invalid key for persistence", "error", err, "key", key)
		return zero, false, nil
	}

	// Check persistence
	val, expiry, found, err := c.persist.Load(ctx, key)
	if err != nil {
		// Log error but don't fail - graceful degradation
		slog.Warn("persistence load failed", "error", err, "key", key)
		return zero, false, nil
	}

	if !found {
		return zero, false, nil
	}

	// Add to memory cache for future hits
	c.memory.setToMemory(key, val, expiry)

	return val, true, nil
}

// expiry returns the expiry time based on TTL and default TTL.
func (c *Cache[K, V]) expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		ttl = c.opts.DefaultTTL
	}
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}

// Set stores a value in the cache with an optional TTL.
// A zero TTL means no expiration (or uses DefaultTTL if configured).
// The value is ALWAYS stored in memory, even if persistence fails.
// Returns an error if the key violates persistence constraints or if persistence fails.
// Even when an error is returned, the value is cached in memory.
func (c *Cache[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) error {
	// Fast path: no TTL, no persistence (common case)
	if ttl == 0 && c.opts.DefaultTTL == 0 && c.persist == nil {
		c.memory.setToMemory(key, value, time.Time{})
		return nil
	}

	expiry := c.expiry(ttl)

	// Validate key early if persistence is enabled
	if c.persist != nil {
		if err := c.persist.ValidateKey(key); err != nil {
			return err
		}
	}

	// ALWAYS update memory first - reliability guarantee
	c.memory.setToMemory(key, value, expiry)

	// Update persistence if available
	if c.persist != nil {
		if err := c.persist.Store(ctx, key, value, expiry); err != nil {
			return fmt.Errorf("persistence store failed: %w", err)
		}
	}

	return nil
}

// SetAsync adds or updates a value in the cache with optional TTL, handling persistence asynchronously.
// Key validation and in-memory caching happen synchronously. Persistence errors are logged but not returned.
// Returns an error only for validation failures (e.g., invalid key format).
func (c *Cache[K, V]) SetAsync(ctx context.Context, key K, value V, ttl time.Duration) error {
	expiry := c.expiry(ttl)

	// Validate key early if persistence is enabled (synchronous)
	if c.persist != nil {
		if err := c.persist.ValidateKey(key); err != nil {
			return err
		}
	}

	// ALWAYS update memory first - reliability guarantee (synchronous)
	c.memory.setToMemory(key, value, expiry)

	// Update persistence asynchronously if available
	if c.persist != nil {
		go func() {
			// Derive context with timeout to prevent hanging
			storeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if err := c.persist.Store(storeCtx, key, value, expiry); err != nil {
				slog.Warn("async persistence store failed", "error", err, "key", key)
			}
		}()
	}

	return nil
}

// Delete removes a value from the cache.
func (c *Cache[K, V]) Delete(ctx context.Context, key K) {
	// Remove from memory
	c.memory.deleteFromMemory(key)

	// Remove from persistence if available
	if c.persist != nil {
		// Validate key before accessing persistence (security: prevent path traversal)
		if err := c.persist.ValidateKey(key); err != nil {
			slog.Warn("invalid key for persistence delete", "error", err, "key", key)
			return
		}
		if err := c.persist.Delete(ctx, key); err != nil {
			// Log error but don't fail - graceful degradation
			slog.Warn("persistence delete failed", "error", err, "key", key)
		}
	}
}

// Cleanup removes expired entries from the cache.
// Returns the number of entries removed.
func (c *Cache[K, V]) Cleanup() int {
	return c.memory.cleanupMemory()
}

// Flush removes all entries from the cache, including persistent storage.
// Returns the total number of entries removed from memory and persistence.
func (c *Cache[K, V]) Flush(ctx context.Context) (int, error) {
	memoryRemoved := c.memory.flushMemory()

	if c.persist == nil {
		slog.Info("cache flushed", "memory", memoryRemoved, "persist", 0)
		return memoryRemoved, nil
	}

	persistRemoved, err := c.persist.Flush(ctx)
	if err != nil {
		return memoryRemoved, fmt.Errorf("persistence flush failed: %w", err)
	}

	slog.Info("cache flushed", "memory", memoryRemoved, "persist", persistRemoved)
	return memoryRemoved + persistRemoved, nil
}

// Len returns the number of items in the memory cache.
func (c *Cache[K, V]) Len() int {
	return c.memory.memoryLen()
}

// Close releases resources held by the cache.
func (c *Cache[K, V]) Close() error {
	if c.persist != nil {
		if err := c.persist.Close(); err != nil {
			return fmt.Errorf("close persistence: %w", err)
		}
	}
	return nil
}
