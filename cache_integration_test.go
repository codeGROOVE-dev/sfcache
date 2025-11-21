package bdcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	if err := cache1.Close(); err != nil {
		t.Logf("Close error: %v", err)
	}

	// Create second cache WITHOUT warmup - memory should be empty
	cache2, err := NewWithOptions[string, string](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Verify memory is empty (no warmup)
	if cache2.memory.memoryLen() != 0 {
		t.Errorf("cache2 memory length without warmup = %d; want 0", cache2.memory.memoryLen())
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
	if cache2.memory.memoryLen() != 1 {
		t.Errorf("cache2 memory length after Get = %d; want 1", cache2.memory.memoryLen())
	}
	if _, memFound := cache2.memory.getFromMemory("key1"); !memFound {
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
	if cache2.memory.memoryLen() != 2 {
		t.Errorf("cache2 memory length = %d; want 2", cache2.memory.memoryLen())
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

	if err := cache1.Close(); err != nil {
		t.Logf("Close error: %v", err)
	}

	// Create second cache instance WITH warmup
	cache2, err := NewWithOptions[string, string](ctx, WithLocalStore(cacheID), WithWarmup(10))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Give warmup goroutine time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify items loaded into memory from disk via warmup
	if cache2.memory.memoryLen() != 2 {
		t.Errorf("cache2 memory length after warmup = %d; want 2", cache2.memory.memoryLen())
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
	cache := newCacheWithPersistence[string, int](ctx, &Options{
		MemorySize:   100,
		CacheID:      "/invalid/path/that/cannot/be/created/\x00null",
		UseDatastore: false,
	})
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

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
func newCacheWithPersistence[K comparable, V any](ctx context.Context, opts *Options) *Cache[K, V] {
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

	return cache
}

// TestCache_GetMissWithPersistence tests Get miss path with persistence.
func TestCache_GetMissWithPersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-miss-" + time.Now().Format("20060102150405")

	cache, err := NewWithOptions[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

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
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Set and delete
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cache.Delete(ctx, "key1")

	// Verify deleted from memory
	_, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if found {
		t.Error("key1 should be deleted from memory")
	}

	// Create new cache - should not find deleted key
	cache2, err := NewWithOptions[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	_, found, err = cache2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get from cache2: %v", err)
	}
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
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	if cache.opts.MemorySize != 10000 {
		t.Errorf("default memory size = %d; want 10000", cache.opts.MemorySize)
	}
}

// NewWithOptions is a helper for testing - allows direct options struct.
func NewWithOptions[K comparable, V any](ctx context.Context, options ...Option) (*Cache[K, V], error) {
	return New[K, V](ctx, options...)
}

func TestCache_CleanupWithPersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "cleanup-test-" + time.Now().Format("20060102150405")

	cache, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Add expired and valid entries
	if err := cache.Set(ctx, "expired1", 1, 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "expired2", 2, 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "valid", 3, 1*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Run cleanup
	removed := cache.Cleanup()
	if removed != 2 {
		t.Errorf("Cleanup removed %d; want 2", removed)
	}

	// Valid entry should remain
	_, found, err := cache.Get(ctx, "valid")
	if err != nil {
		t.Fatalf("Get valid: %v", err)
	}
	if !found {
		t.Error("valid entry should still exist")
	}
}

func TestCache_ComprehensiveDiskToMemoryPath(t *testing.T) {
	ctx := context.Background()
	cacheID := "comprehensive-test-" + time.Now().Format("20060102150405")

	// Step 1: Create cache, store values, close
	cache1, err := New[string, string](ctx, WithLocalStore(cacheID), WithMemorySize(5))
	if err != nil {
		t.Fatalf("New cache1: %v", err)
	}

	values := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range values {
		if err := cache1.Set(ctx, k, v, 1*time.Hour); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// Verify in memory
	if cache1.Len() != 3 {
		t.Errorf("cache1 length = %d; want 3", cache1.Len())
	}

	if err := cache1.Close(); err != nil {
		t.Logf("Close error: %v", err)
	}

	// Step 2: Create new cache with warmup - should load from disk
	cache2, err := New[string, string](ctx, WithLocalStore(cacheID), WithMemorySize(5), WithWarmup(10))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Wait for warmup
	time.Sleep(150 * time.Millisecond)

	// Verify all values loaded into memory via warmup
	if cache2.Len() != 3 {
		t.Errorf("cache2 length after warmup = %d; want 3", cache2.Len())
	}

	// Test Get - should hit memory
	for k, expectedV := range values {
		v, found, err := cache2.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get %s: %v", k, err)
		}
		if !found {
			t.Errorf("%s not found after warmup", k)
		}
		if v != expectedV {
			t.Errorf("Get %s = %s; want %s", k, v, expectedV)
		}
	}

	// Step 3: Create third cache but don't wait for warmup
	cache3, err := New[string, string](ctx, WithLocalStore(cacheID), WithMemorySize(5))
	if err != nil {
		t.Fatalf("New cache3: %v", err)
	}
	defer func() {
		if err := cache3.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Immediately Get (before warmup completes) - should load from disk
	v, found, err := cache3.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get key1 before warmup: %v", err)
	}
	if !found {
		t.Fatal("key1 should be found from disk even before warmup")
	}
	if v != "value1" {
		t.Errorf("Get key1 = %s; want value1", v)
	}

	// Verify it's now in memory (promoted)
	if _, memFound := cache3.memory.getFromMemory("key1"); !memFound {
		t.Error("key1 should be in memory after Get from disk")
	}

	// Second Get should hit memory
	v2, found2, err := cache3.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Second Get key1: %v", err)
	}
	if !found2 || v2 != "value1" {
		t.Errorf("Second Get key1 = %s, %v; want value1, true", v2, found2)
	}
}

func TestCache_MemoryCapacityWithDisk(t *testing.T) {
	ctx := context.Background()
	cacheID := "capacity-test-" + time.Now().Format("20060102150405")

	cache, err := New[int, int](ctx, WithLocalStore(cacheID), WithMemorySize(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Fill beyond memory capacity
	for i := 1; i <= 10; i++ {
		if err := cache.Set(ctx, i, i*10, 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// Memory should be at capacity
	if cache.Len() > 3 {
		t.Errorf("cache length = %d; should not exceed 3", cache.Len())
	}

	// All items should be retrievable (from disk if not in memory)
	for i := 1; i <= 10; i++ {
		val, found, err := cache.Get(ctx, i)
		if err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
		if !found {
			t.Errorf("%d not found - should be in memory or disk", i)
		}
		if val != i*10 {
			t.Errorf("Get %d = %d; want %d", i, val, i*10)
		}
	}
}

func TestCache_New_CompletePersistenceFlow(t *testing.T) {
	ctx := context.Background()
	cacheID := "complete-flow-" + time.Now().Format("20060102150405")

	// Test file persistence initialization
	cache1, err := New[string, int](ctx,
		WithLocalStore(cacheID),
		WithMemorySize(100),
		WithDefaultTTL(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("New with file: %v", err)
	}

	if cache1.persist == nil {
		t.Error("persist should not be nil with WithLocalStore")
	}

	if err := cache1.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache1.Close(); err != nil {
		t.Logf("Close error: %v", err)
	}

	// Test loading from persistence on-demand (no warmup)
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Should load from disk on-demand
	val, found, err := cache2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if !found || val != 42 {
		t.Errorf("Get after reload = %v, %v; want 42, true (should load on-demand from disk)", val, found)
	}
}

func TestCache_New_WarmupError(t *testing.T) {
	ctx := context.Background()
	cacheID := "warmup-error-test-" + time.Now().Format("20060102150405")

	// Create cache with some data
	cache1, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache1: %v", err)
	}

	if err := cache1.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache1.Close(); err != nil {
		t.Logf("Close error: %v", err)
	}

	// Corrupt one of the cache files
	fp, err := newFilePersist[string, int](cacheID)
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer func() {
		if err := fp.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Find and corrupt a .gob file (not a directory)
	var corruptFile string
	if err := filepath.Walk(fp.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".gob" && corruptFile == "" {
			corruptFile = path
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if corruptFile != "" {
		if err := os.WriteFile(corruptFile, []byte("corrupted"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	// Create new cache - warmup should handle error gracefully
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Cache should still work
	if err := cache2.Set(ctx, "key2", 100, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, found, err := cache2.Get(ctx, "key2")
	if err != nil {
		t.Fatalf("Get key2: %v", err)
	}
	if !found || val != 100 {
		t.Errorf("Get key2 = %v, %v; want 100, true", val, found)
	}
}

func TestCache_SetWithDefaultTTL(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx, WithDefaultTTL(100*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Set with ttl=0 should use default TTL
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Should exist immediately
	_, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get immediately: %v", err)
	}
	if !found {
		t.Error("key1 should exist immediately")
	}

	// Wait for default TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, found, err = cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get after expire: %v", err)
	}
	if found {
		t.Error("key1 should be expired after default TTL")
	}
}

func TestCache_Warmup_WithErrors(t *testing.T) {
	ctx := context.Background()
	cacheID := "warmup-errors-" + time.Now().Format("20060102150405")

	// Create cache and add data
	cache1, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Use valid alphanumeric keys
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("key%d", i)
		if err := cache1.Set(ctx, key, i, 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if err := cache1.Close(); err != nil {
		t.Logf("Close error: %v", err)
	}

	// Corrupt some cache files to trigger warmup errors
	fp, err := newFilePersist[string, int](cacheID)
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer func() {
		if err := fp.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Walk directory tree to find .gob files (accounting for squid-style subdirs)
	var gobFiles []string
	if err := filepath.Walk(fp.dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".gob" {
			gobFiles = append(gobFiles, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(gobFiles) > 0 {
		// Corrupt first file
		if err := os.WriteFile(gobFiles[0], []byte("bad data"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	// Create new cache with warmup - should handle errors gracefully
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID), WithWarmup(10))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer func() {
		if err := cache2.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	time.Sleep(150 * time.Millisecond) // Wait for warmup

	// Some entries should still load despite corruption
	// (exact count depends on which file was corrupted)
	if cache2.Len() == 0 {
		t.Error("at least some entries should have loaded from warmup")
	}
}
