package bdcache

import (
	"context"
	"testing"
	"time"
)

// TestCache_ComprehensiveDiskToMemoryPath tests the complete disk-to-memory flow.
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

	cache1.Close()

	// Step 2: Create new cache with warmup - should load from disk
	cache2, err := New[string, string](ctx, WithLocalStore(cacheID), WithMemorySize(5), WithWarmup(10))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

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
	defer cache3.Close()

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
	if _, memFound := cache3.memory.get("key1"); !memFound {
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

// TestCache_MemoryCapacityWithDisk tests eviction with disk persistence.
func TestCache_MemoryCapacityWithDisk(t *testing.T) {
	ctx := context.Background()
	cacheID := "capacity-test-" + time.Now().Format("20060102150405")

	cache, err := New[int, int](ctx, WithLocalStore(cacheID), WithMemorySize(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

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

// TestCache_SetWithDefaultTTL tests Set with default TTL option.
func TestCache_SetWithDefaultTTL(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx, WithDefaultTTL(100*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Set with ttl=0 should use default TTL
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Should exist immediately
	_, found, _ := cache.Get(ctx, "key1")
	if !found {
		t.Error("key1 should exist immediately")
	}

	// Wait for default TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, found, _ = cache.Get(ctx, "key1")
	if found {
		t.Error("key1 should be expired after default TTL")
	}
}

// TestCache_CleanupWithPersistence tests Cleanup with persisted data.
func TestCache_CleanupWithPersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "cleanup-test-" + time.Now().Format("20060102150405")

	cache, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

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
	_, found, _ := cache.Get(ctx, "valid")
	if !found {
		t.Error("valid entry should still exist")
	}
}
