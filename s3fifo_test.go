package bdcache

import (
	"sync"
	"testing"
	"time"
)

func TestS3FIFO_BasicOperations(t *testing.T) {
	cache := newS3FIFO[string, int](100)

	// Test set and get
	cache.set("key1", 42, time.Time{})
	if val, ok := cache.get("key1"); !ok || val != 42 {
		t.Errorf("get(key1) = %v, %v; want 42, true", val, ok)
	}

	// Test missing key
	if val, ok := cache.get("missing"); ok {
		t.Errorf("get(missing) = %v, %v; want _, false", val, ok)
	}

	// Test update
	cache.set("key1", 100, time.Time{})
	if val, ok := cache.get("key1"); !ok || val != 100 {
		t.Errorf("get(key1) after update = %v, %v; want 100, true", val, ok)
	}

	// Test delete
	cache.delete("key1")
	if val, ok := cache.get("key1"); ok {
		t.Errorf("get(key1) after delete = %v, %v; want _, false", val, ok)
	}
}

func TestS3FIFO_Capacity(t *testing.T) {
	capacity := 100
	cache := newS3FIFO[int, string](capacity)

	// Fill cache to capacity
	for i := 0; i < capacity; i++ {
		cache.set(i, "value", time.Time{})
	}

	if cache.len() != capacity {
		t.Errorf("cache length = %d; want %d", cache.len(), capacity)
	}

	// Add one more - should trigger eviction
	cache.set(capacity, "value", time.Time{})

	if cache.len() != capacity {
		t.Errorf("cache length after eviction = %d; want %d", cache.len(), capacity)
	}
}

func TestS3FIFO_Eviction(t *testing.T) {
	cache := newS3FIFO[string, int](10)

	// Fill small queue (10% of 10 = 1 item)
	cache.set("small1", 1, time.Time{})

	// Fill remaining capacity (9 items, but they start in small queue)
	for i := 2; i <= 10; i++ {
		cache.set("key"+string(rune(i)), i, time.Time{})
	}

	// Access small1 to increase its frequency
	cache.get("small1")

	// Add one more item - should evict least frequently used
	cache.set("new", 99, time.Time{})

	// small1 should still exist (it was accessed)
	if _, ok := cache.get("small1"); !ok {
		t.Error("small1 was evicted but should have been promoted")
	}

	// Some other key should have been evicted
	if cache.len() != 10 {
		t.Errorf("cache length = %d; want 10", cache.len())
	}
}

func TestS3FIFO_GhostQueue(t *testing.T) {
	cache := newS3FIFO[string, int](3)

	// Fill cache
	cache.set("a", 1, time.Time{})
	cache.set("b", 2, time.Time{})
	cache.set("c", 3, time.Time{})

	// Evict "a" by adding "d"
	cache.set("d", 4, time.Time{})

	// "a" should be in ghost queue
	if _, ok := cache.get("a"); ok {
		t.Error("'a' should have been evicted")
	}

	// Re-add "a" - should go directly to main queue (not small)
	cache.set("a", 10, time.Time{})

	cache.mu.RLock()
	ent := cache.items["a"]
	isInSmall := ent.inSmall
	cache.mu.RUnlock()

	if isInSmall {
		t.Error("'a' should be in main queue after ghost promotion")
	}
}

func TestS3FIFO_TTL(t *testing.T) {
	cache := newS3FIFO[string, int](10)

	// Set item with past expiry
	past := time.Now().Add(-1 * time.Second)
	cache.set("expired", 42, past)

	// Should not be retrievable
	if val, ok := cache.get("expired"); ok {
		t.Errorf("get(expired) = %v, %v; want _, false", val, ok)
	}

	// Set item with future expiry
	future := time.Now().Add(1 * time.Hour)
	cache.set("valid", 100, future)

	// Should be retrievable
	if val, ok := cache.get("valid"); !ok || val != 100 {
		t.Errorf("get(valid) = %v, %v; want 100, true", val, ok)
	}
}

func TestS3FIFO_Cleanup(t *testing.T) {
	cache := newS3FIFO[string, int](10)

	// Add some items with different expiries
	now := time.Now()
	cache.set("expired1", 1, now.Add(-1*time.Second))
	cache.set("expired2", 2, now.Add(-1*time.Second))
	cache.set("valid1", 3, now.Add(1*time.Hour))
	cache.set("valid2", 4, time.Time{}) // No expiry

	// Run cleanup
	removed := cache.cleanup()

	if removed != 2 {
		t.Errorf("cleanup removed %d items; want 2", removed)
	}

	if cache.len() != 2 {
		t.Errorf("cache length after cleanup = %d; want 2", cache.len())
	}

	// Verify correct items remain
	if _, ok := cache.get("valid1"); !ok {
		t.Error("valid1 should still exist")
	}
	if _, ok := cache.get("valid2"); !ok {
		t.Error("valid2 should still exist")
	}
}

func TestS3FIFO_Concurrent(t *testing.T) {
	cache := newS3FIFO[int, int](1000)
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.set(offset*100+j, j, time.Time{})
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.get(j)
			}
		}()
	}

	wg.Wait()

	// Cache should be at capacity
	if cache.len() != 1000 {
		t.Errorf("cache length = %d; want 1000", cache.len())
	}
}

func TestS3FIFO_FrequencyPromotion(t *testing.T) {
	cache := newS3FIFO[string, int](10)

	// Add items - they start in small queue
	cache.set("key0", 0, time.Time{})
	cache.set("key1", 1, time.Time{})

	// Access key0 to increase frequency (will promote to main on next eviction)
	cache.get("key0")

	// Fill to capacity
	for i := 2; i < 10; i++ {
		cache.set("key"+string(rune('0'+i)), i, time.Time{})
	}

	// Add one more to trigger eviction
	cache.set("new", 99, time.Time{})

	// key0 should still exist - it was accessed so it gets promoted instead of evicted
	if _, ok := cache.get("key0"); !ok {
		t.Error("key0 should not have been evicted due to frequency access")
	}
}

func TestS3FIFO_SmallCapacity(t *testing.T) {
	// Test with capacity of 3 (small=1, main=2)
	cache := newS3FIFO[string, int](3)

	// Fill to capacity
	cache.set("a", 1, time.Time{})
	cache.set("b", 2, time.Time{})
	cache.set("c", 3, time.Time{})

	if cache.len() != 3 {
		t.Errorf("cache length = %d; want 3", cache.len())
	}

	// Adding fourth item should trigger eviction
	cache.set("d", 4, time.Time{})

	// Should still be at capacity
	if cache.len() != 3 {
		t.Errorf("cache length after eviction = %d; want 3", cache.len())
	}

	// Newest item should exist
	if val, ok := cache.get("d"); !ok || val != 4 {
		t.Errorf("get(d) = %v, %v; want 4, true", val, ok)
	}
}

func TestS3FIFO_ZeroCapacity(t *testing.T) {
	// Zero capacity should default to 10000
	cache := newS3FIFO[string, int](0)

	if cache.capacity != 10000 {
		t.Errorf("capacity = %d; want 10000", cache.capacity)
	}
}

func BenchmarkS3FIFO_Set(b *testing.B) {
	cache := newS3FIFO[int, int](10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.set(i%10000, i, time.Time{})
	}
}

func BenchmarkS3FIFO_Get(b *testing.B) {
	cache := newS3FIFO[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.set(i, i, time.Time{})
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.get(i % 10000)
	}
}

func BenchmarkS3FIFO_Mixed(b *testing.B) {
	cache := newS3FIFO[int, int](10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			cache.set(i%10000, i, time.Time{})
		} else {
			cache.get(i % 10000)
		}
	}
}
