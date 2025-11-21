// Package bdcache provides a high-performance cache with S3-FIFO eviction and optional persistence.
package bdcache

import (
	"context"
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
	opts := defaultOptions()
	for _, opt := range options {
		opt(opts)
	}

	cache := &Cache[K, V]{
		memory: newS3FIFO[K, V](opts.MemorySize),
		opts:   opts,
	}

	// Initialize persistence if configured
	if opts.CacheID != "" {
		var err error
		if opts.UseDatastore {
			cache.persist, err = newDatastorePersist[K, V](ctx, opts.CacheID)
			if err != nil {
				slog.Warn("failed to initialize datastore persistence, continuing with memory-only cache",
					"error", err, "cache_id", opts.CacheID)
				cache.persist = nil
			} else {
				slog.Info("initialized cache with datastore persistence", "cache_id", opts.CacheID)
			}
		} else {
			cache.persist, err = newFilePersist[K, V](opts.CacheID)
			if err != nil {
				slog.Warn("failed to initialize file persistence, continuing with memory-only cache",
					"error", err, "cache_id", opts.CacheID)
				cache.persist = nil
			} else {
				slog.Info("initialized cache with file persistence", "cache_id", opts.CacheID)
			}
		}

		// Run background cleanup if configured
		if cache.persist != nil && opts.CleanupEnabled {
			go func() {
				deleted, err := cache.persist.Cleanup(ctx, opts.CleanupMaxAge)
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
			go cache.warmup(ctx)
		}
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

// Set stores a value in the cache with an optional TTL.
// A zero TTL means no expiration (or uses DefaultTTL if configured).
// The value is ALWAYS stored in memory, even if persistence fails.
// Returns an error if the key violates persistence constraints or if persistence fails.
// Even when an error is returned, the value is cached in memory.
func (c *Cache[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) error {
	var expiry time.Time
	if ttl > 0 {
		expiry = time.Now().Add(ttl)
	} else if c.opts.DefaultTTL > 0 {
		expiry = time.Now().Add(c.opts.DefaultTTL)
	}

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

// Delete removes a value from the cache.
//
//nolint:revive // confusing-naming - standard cache operation
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
//
//nolint:revive // confusing-naming - standard cache operation
func (c *Cache[K, V]) Cleanup() int {
	return c.memory.cleanupMemory()
}

// Len returns the number of items in the memory cache.
func (c *Cache[K, V]) Len() int {
	return c.memory.memoryLen()
}

// Close releases resources held by the cache.
//
//nolint:revive // confusing-naming - standard cache operation
func (c *Cache[K, V]) Close() error {
	if c.persist != nil {
		if err := c.persist.Close(); err != nil {
			return fmt.Errorf("close persistence: %w", err)
		}
	}
	return nil
}
