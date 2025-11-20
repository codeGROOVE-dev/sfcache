package bdcache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCache_MemoryOnly(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, int](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Test Set and Get
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != 42 {
		t.Errorf("Get value = %d; want 42", val)
	}

	// Test miss
	_, found, err = cache.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if found {
		t.Error("missing key should not be found")
	}

	// Test delete
	cache.Delete(ctx, "key1")

	_, found, err = cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if found {
		t.Error("deleted key should not be found")
	}
}

func TestCache_WithTTL(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, string](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Set with short TTL
	if err := cache.Set(ctx, "temp", "value", 50*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Should be available immediately
	val, found, _ := cache.Get(ctx, "temp")
	if !found || val != "value" {
		t.Error("temp should be found immediately")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, found, _ = cache.Get(ctx, "temp")
	if found {
		t.Error("temp should be expired")
	}
}

func TestCache_DefaultTTL(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, int](ctx, WithDefaultTTL(50*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Set without explicit TTL (ttl=0 uses default)
	if err := cache.Set(ctx, "key", 100, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Should be available immediately
	_, found, _ := cache.Get(ctx, "key")
	if !found {
		t.Error("key should be found immediately")
	}

	// Wait for default TTL expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, found, _ = cache.Get(ctx, "key")
	if found {
		t.Error("key should be expired after default TTL")
	}
}

func TestCache_Cleanup(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, int](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Add expired and valid entries
	if err := cache.Set(ctx, "expired1", 1, 1*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "expired2", 2, 1*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "valid", 3, 1*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Run cleanup
	removed := cache.Cleanup()
	if removed != 2 {
		t.Errorf("Cleanup removed %d items; want 2", removed)
	}

	// Valid entry should still exist
	_, found, _ := cache.Get(ctx, "valid")
	if !found {
		t.Error("valid entry should still exist")
	}
}

func TestCache_Concurrent(t *testing.T) {
	ctx := context.Background()
	cache, err := New[int, int](ctx, WithMemorySize(1000))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if err := cache.Set(ctx, offset*100+j, j, 0); err != nil {
					t.Errorf("Set: %v", err)
				}
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Get(ctx, j)
			}
		}()
	}

	wg.Wait()

	// Cache should be at or near capacity
	if cache.Len() > 1000 {
		t.Errorf("cache length = %d; should not exceed capacity", cache.Len())
	}
}

func TestCache_WithFilePersistence(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-file-cache-" + time.Now().Format("20060102150405")

	cache, err := New[string, string](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Store values
	if err := cache.Set(ctx, "key1", "value1", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "key2", "value2", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cache.Close()

	// Create new cache instance - should load from files
	cache2, err := New[string, string](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	// Give warmup time to complete
	time.Sleep(100 * time.Millisecond)

	// Values should be available
	val, found, err := cache2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || val != "value1" {
		t.Errorf("Get key1 = %v, %v; want value1, true", val, found)
	}
}

func TestCache_Len(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, int](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	if cache.Len() != 0 {
		t.Errorf("initial length = %d; want 0", cache.Len())
	}

	if err := cache.Set(ctx, "a", 1, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "b", 2, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Set(ctx, "c", 3, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if cache.Len() != 3 {
		t.Errorf("length = %d; want 3", cache.Len())
	}

	cache.Delete(ctx, "b")

	if cache.Len() != 2 {
		t.Errorf("length after delete = %d; want 2", cache.Len())
	}
}

func BenchmarkCache_Set(b *testing.B) {
	ctx := context.Background()
	cache, _ := New[int, int](ctx)
	defer cache.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cache.Set(ctx, i%10000, i, 0); err != nil {
			b.Fatalf("Set: %v", err)
		}
	}
}

func BenchmarkCache_Get_Hit(b *testing.B) {
	ctx := context.Background()
	cache, _ := New[int, int](ctx)
	defer cache.Close()

	// Populate cache
	for i := 0; i < 10000; i++ {
		if err := cache.Set(ctx, i, i, 0); err != nil {
			b.Fatalf("Set: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(ctx, i%10000)
	}
}

func BenchmarkCache_Get_Miss(b *testing.B) {
	ctx := context.Background()
	cache, _ := New[int, int](ctx)
	defer cache.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(ctx, i)
	}
}

func BenchmarkCache_Mixed(b *testing.B) {
	ctx := context.Background()
	cache, _ := New[int, int](ctx)
	defer cache.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%3 == 0 {
			if err := cache.Set(ctx, i%10000, i, 0); err != nil {
				b.Fatalf("Set: %v", err)
			}
		} else {
			cache.Get(ctx, i%10000)
		}
	}
}
