package bdcache

import (
	"context"
	"testing"
	"time"
)

// TestCache_OnDemandLoadFromDisk tests that cache loads from disk on-demand WITHOUT warmup.
func TestCache_OnDemandLoadFromDisk(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-ondemand-" + time.Now().Format("20060102150405")

	// Create first cache and store items
	cache1, err := NewWithOptions[string, string](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache1: %v", err)
	}

	// Store values to disk
	if err := cache1.Set(ctx, "key1", "value1", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache1.Set(ctx, "key2", "value2", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache1.Set(ctx, "key3", "value3", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cache1.Close()

	// Create second cache WITHOUT warmup - memory should be empty
	cache2, err := NewWithOptions[string, string](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	// Verify memory is empty (no warmup)
	if cache2.memory.len() != 0 {
		t.Errorf("cache2 memory length without warmup = %d; want 0", cache2.memory.len())
	}

	// First Get - should load from disk on-demand
	val, found, err := cache2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get key1: %v", err)
	}
	if !found {
		t.Fatal("key1 not found - should have loaded from disk on-demand")
	}
	if val != "value1" {
		t.Errorf("Get key1 = %v; want value1", val)
	}

	// Verify it's now in memory (promoted)
	if cache2.memory.len() != 1 {
		t.Errorf("cache2 memory length after Get = %d; want 1", cache2.memory.len())
	}
	if _, memFound := cache2.memory.get("key1"); !memFound {
		t.Error("key1 should be in memory after on-demand load")
	}

	// Second Get of same key - should hit memory this time
	val2, found2, err := cache2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Second Get key1: %v", err)
	}
	if !found2 {
		t.Fatal("key1 not found on second get")
	}
	if val2 != "value1" {
		t.Errorf("Second Get key1 = %v; want value1", val2)
	}

	// Get another key - should also load from disk on-demand
	val3, found3, err := cache2.Get(ctx, "key2")
	if err != nil {
		t.Fatalf("Get key2: %v", err)
	}
	if !found3 {
		t.Fatal("key2 not found - should have loaded from disk on-demand")
	}
	if val3 != "value2" {
		t.Errorf("Get key2 = %v; want value2", val3)
	}

	// Now memory should have 2 items
	if cache2.memory.len() != 2 {
		t.Errorf("cache2 memory length = %d; want 2", cache2.memory.len())
	}
}

// TestCache_DiskToMemoryPromotion tests that items on disk are promoted to memory cache with warmup.
func TestCache_DiskToMemoryPromotion(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-promotion-" + time.Now().Format("20060102150405")

	// Create first cache and store items
	cache1, err := NewWithOptions[string, string](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache1: %v", err)
	}

	// Store values to disk
	if err := cache1.Set(ctx, "key1", "value1", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache1.Set(ctx, "key2", "value2", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cache1.Close()

	// Create second cache instance WITH warmup
	cache2, err := NewWithOptions[string, string](ctx, WithLocalStore(cacheID), WithWarmup(10))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	// Give warmup goroutine time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify items loaded into memory from disk via warmup
	if cache2.memory.len() != 2 {
		t.Errorf("cache2 memory length after warmup = %d; want 2", cache2.memory.len())
	}

	// Get should hit memory (already warmed up)
	val, found, err := cache2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get key1: %v", err)
	}
	if !found {
		t.Fatal("key1 not found after warmup")
	}
	if val != "value1" {
		t.Errorf("Get key1 = %v; want value1", val)
	}
}

// TestCache_PersistenceFailureGracefulDegradation tests graceful degradation.
func TestCache_PersistenceFailureGracefulDegradation(t *testing.T) {
	ctx := context.Background()

	// Create cache with invalid directory to trigger persistence failure
	cache, err := newCacheWithPersistence[string, int](ctx, &Options{
		MemorySize:   100,
		CacheID:      "/invalid/path/that/cannot/be/created/\x00null",
		UseDatastore: false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Persistence should have failed, but cache should still work
	if cache.persist != nil {
		t.Error("persist should be nil due to initialization failure")
	}

	// Cache should still work in memory-only mode
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get with failed persistence: %v", err)
	}
	if !found || val != 42 {
		t.Errorf("Get = %v, %v; want 42, true", val, found)
	}
}

// newCacheWithPersistence is a helper that allows testing persistence initialization failures.
func newCacheWithPersistence[K comparable, V any](ctx context.Context, opts *Options) (*Cache[K, V], error) {
	cache := &Cache[K, V]{
		memory: newS3FIFO[K, V](opts.MemorySize),
		opts:   opts,
	}

	if opts.CacheID != "" {
		var err error
		if opts.UseDatastore {
			cache.persist, err = newDatastorePersist[K, V](ctx, opts.CacheID)
			if err != nil {
				cache.persist = nil
			}
		} else {
			cache.persist, err = newFilePersist[K, V](opts.CacheID)
			if err != nil {
				cache.persist = nil
			}
		}

		if cache.persist != nil {
			go cache.warmup(ctx)
		}
	}

	return cache, nil
}

// TestCache_GetMissWithPersistence tests Get miss path with persistence.
func TestCache_GetMissWithPersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-miss-" + time.Now().Format("20060102150405")

	cache, err := NewWithOptions[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Get non-existent key - should check persistence
	_, found, err := cache.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if found {
		t.Error("nonexistent key should not be found")
	}
}

// TestCache_DeleteWithPersistence tests Delete with persistence.
func TestCache_DeleteWithPersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-delete-" + time.Now().Format("20060102150405")

	cache, err := NewWithOptions[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Set and delete
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cache.Delete(ctx, "key1")

	// Verify deleted from memory
	_, found, _ := cache.Get(ctx, "key1")
	if found {
		t.Error("key1 should be deleted from memory")
	}

	// Create new cache - should not find deleted key
	cache2, err := NewWithOptions[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	time.Sleep(100 * time.Millisecond)

	_, found, _ = cache2.Get(ctx, "key1")
	if found {
		t.Error("key1 should not be loaded from persistence after delete")
	}
}

// TestCache_CloseWithPersistence tests Close with persistence.
func TestCache_CloseWithPersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-close-" + time.Now().Format("20060102150405")

	cache, err := NewWithOptions[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := cache.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Close should be idempotent
	if err := cache.Close(); err != nil {
		t.Errorf("Second Close: %v", err)
	}
}

// TestNew_HelperFunction tests the New helper that uses NewWithOptions.
func TestNew_HelperFunction(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	if cache.opts.MemorySize != 10000 {
		t.Errorf("default memory size = %d; want 10000", cache.opts.MemorySize)
	}
}

// NewWithOptions is a helper for testing - allows direct options struct.
func NewWithOptions[K comparable, V any](ctx context.Context, options ...Option) (*Cache[K, V], error) {
	return New[K, V](ctx, options...)
}
