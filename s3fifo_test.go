package bdcache

import (
	"context"
	"fmt"
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
	for i := range capacity {
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
	for i := range 10 {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := range 100 {
				cache.set(offset*100+j, j, time.Time{})
			}
		}(i)
	}

	// Concurrent readers
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 100 {
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

	for i := range b.N {
		cache.set(i%10000, i, time.Time{})
	}
}

func BenchmarkS3FIFO_Get(b *testing.B) {
	cache := newS3FIFO[int, int](10000)
	for i := range 10000 {
		cache.set(i, i, time.Time{})
	}
	b.ResetTimer()

	for i := range b.N {
		cache.get(i % 10000)
	}
}

func BenchmarkS3FIFO_Mixed(b *testing.B) {
	cache := newS3FIFO[int, int](10000)
	b.ResetTimer()

	for i := range b.N {
		if i%2 == 0 {
			cache.set(i%10000, i, time.Time{})
		} else {
			cache.get(i % 10000)
		}
	}
}

// Test to understand S3-FIFO behavior with one-hit wonders
func TestS3FIFOBehavior(t *testing.T) {
	ctx := context.Background()
	cache, err := New[int, int](ctx, WithMemorySize(100))
	if err != nil {
		t.Fatal(err)
	}

	// Get internal s3fifo structure
	s3fifo := cache.memory

	fmt.Println("\n=== S3-FIFO Capacity Configuration ===")
	fmt.Printf("Total capacity: %d\n", s3fifo.capacity)
	fmt.Printf("Small capacity: %d (%.0f%%)\n", s3fifo.smallCap, float64(s3fifo.smallCap)/float64(s3fifo.capacity)*100)
	fmt.Printf("Main capacity: %d (%.0f%%)\n", s3fifo.mainCap, float64(s3fifo.mainCap)/float64(s3fifo.capacity)*100)

	// Phase 1: Insert hot items that will be accessed multiple times
	fmt.Println("\n=== Phase 1: Insert 50 hot items (will be accessed again) ===")
	for i := 0; i < 50; i++ {
		_ = cache.Set(ctx, i, i, 0)
	}
	fmt.Printf("After insertion: Small=%d, Main=%d, Total=%d\n",
		s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Phase 2: Access hot items once (should promote some to Main)
	fmt.Println("\n=== Phase 2: Access hot items (should promote to Main) ===")
	for i := 0; i < 50; i++ {
		_, _, _ = cache.Get(ctx, i)
	}
	fmt.Printf("After first access: Small=%d, Main=%d, Total=%d\n",
		s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Phase 3: Insert one-hit wonders (should stay in Small and be evicted quickly)
	fmt.Println("\n=== Phase 3: Insert 60 one-hit wonders ===")
	for i := 1000; i < 1060; i++ {
		_ = cache.Set(ctx, i, i, 0)
	}
	fmt.Printf("After one-hit wonders: Small=%d, Main=%d, Total=%d\n",
		s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Phase 4: Check if hot items are still in cache
	fmt.Println("\n=== Phase 4: Check if hot items survived ===")
	hotItemsFound := 0
	for i := 0; i < 50; i++ {
		if _, found, _ := cache.Get(ctx, i); found {
			hotItemsFound++
		}
	}
	fmt.Printf("Hot items still in cache: %d/50\n", hotItemsFound)
	fmt.Printf("Final state: Small=%d, Main=%d, Total=%d\n",
		s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Expected behavior:
	// - Hot items should mostly be in Main queue after first access
	// - One-hit wonders should fill Small queue and be evicted from Small
	// - Hot items in Main should survive
	if hotItemsFound < 40 {
		t.Errorf("Expected most hot items to survive, got %d/50", hotItemsFound)
	}
}

// Test eviction order
func TestS3FIFOEvictionOrder(t *testing.T) {
	ctx := context.Background()
	cache, err := New[int, int](ctx, WithMemorySize(10))
	if err != nil {
		t.Fatal(err)
	}

	s3fifo := cache.memory
	fmt.Println("\n=== Testing Eviction Order ===")
	fmt.Printf("Cache capacity: %d (Small=%d, Main=%d)\n",
		s3fifo.capacity, s3fifo.smallCap, s3fifo.mainCap)

	// Fill cache with items
	for i := 0; i < 10; i++ {
		_ = cache.Set(ctx, i, i, 0)
		fmt.Printf("Inserted %d: Small=%d, Main=%d\n", i, s3fifo.small.Len(), s3fifo.main.Len())
	}

	// Access first 5 items (should promote them)
	fmt.Println("\nAccessing items 0-4 (should promote to Main):")
	for i := 0; i < 5; i++ {
		_, _, _ = cache.Get(ctx, i)
		fmt.Printf("Accessed %d: Small=%d, Main=%d\n", i, s3fifo.small.Len(), s3fifo.main.Len())
	}

	// Insert new items (should evict from Small, not Main)
	fmt.Println("\nInserting new items 100-104:")
	for i := 100; i < 105; i++ {
		_ = cache.Set(ctx, i, i, 0)
		fmt.Printf("Inserted %d: Small=%d, Main=%d\n", i, s3fifo.small.Len(), s3fifo.main.Len())
	}

	// Check which items survived
	fmt.Println("\nChecking which items are still in cache:")
	for i := 0; i < 10; i++ {
		if _, found, _ := cache.Get(ctx, i); found {
			fmt.Printf("  Item %d: FOUND\n", i)
		} else {
			fmt.Printf("  Item %d: EVICTED\n", i)
		}
	}
}

// Test to verify S3-FIFO is actually different from LRU behavior
func TestS3FIFODetailed(t *testing.T) {
	ctx := context.Background()
	cache, err := New[int, int](ctx, WithMemorySize(10))
	if err != nil {
		t.Fatal(err)
	}

	s3fifo := cache.memory

	fmt.Println("\n=== Detailed S3-FIFO Test ===")
	fmt.Printf("Capacity: %d (Small=%d, Main=%d)\n\n", s3fifo.capacity, s3fifo.smallCap, s3fifo.mainCap)

	// Step 1: Insert items 1-10 into cache
	fmt.Println("Step 1: Insert items 1-10")
	for i := 1; i <= 10; i++ {
		_ = cache.Set(ctx, i, i*100, 0)
	}
	fmt.Printf("  Small=%d, Main=%d, Total=%d\n", s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Step 2: Access items 1-5 (should mark them with freq > 0)
	fmt.Println("\nStep 2: Access items 1-5 (mark as hot)")
	for i := 1; i <= 5; i++ {
		_, _, _ = cache.Get(ctx, i)
	}
	fmt.Printf("  Small=%d, Main=%d, Total=%d\n", s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Step 3: Insert one-hit wonders 100-104 (should evict unaccessed items 6-10, promote accessed 1-5)
	fmt.Println("\nStep 3: Insert one-hit wonders 100-104")
	for i := 100; i < 105; i++ {
		_ = cache.Set(ctx, i, i*100, 0)
		fmt.Printf("  Inserted %d: Small=%d, Main=%d\n", i, s3fifo.small.Len(), s3fifo.main.Len())
	}

	// Step 4: Check which items survived
	fmt.Println("\nStep 4: Check which items survived")
	fmt.Println("  Items 1-5 (accessed, should be in Main):")
	for i := 1; i <= 5; i++ {
		if _, found, _ := cache.Get(ctx, i); found {
			fmt.Printf("    Item %d: ✓ FOUND\n", i)
		} else {
			fmt.Printf("    Item %d: ✗ EVICTED (BAD - was accessed!)\n", i)
		}
	}

	fmt.Println("  Items 6-10 (not accessed, should be evicted):")
	for i := 6; i <= 10; i++ {
		if _, found, _ := cache.Get(ctx, i); found {
			fmt.Printf("    Item %d: ✗ FOUND (BAD - should have been evicted!)\n", i)
		} else {
			fmt.Printf("    Item %d: ✓ EVICTED\n", i)
		}
	}

	fmt.Println("  One-hit wonders 100-104 (should be in Small):")
	for i := 100; i < 105; i++ {
		if _, found, _ := cache.Get(ctx, i); found {
			fmt.Printf("    Item %d: ✓ FOUND\n", i)
		} else {
			fmt.Printf("    Item %d: ✗ EVICTED\n", i)
		}
	}

	fmt.Printf("\nFinal state: Small=%d, Main=%d, Total=%d\n", s3fifo.small.Len(), s3fifo.main.Len(), len(s3fifo.items))

	// Verify expected behavior
	hotSurvived := 0
	for i := 1; i <= 5; i++ {
		if _, found, _ := cache.Get(ctx, i); found {
			hotSurvived++
		}
	}

	if hotSurvived != 5 {
		t.Errorf("Expected all 5 hot items to survive, got %d", hotSurvived)
	}
}
