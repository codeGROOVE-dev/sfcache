package bdcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/ds9/pkg/datastore"
)

// newMockDatastorePersist creates a datastore persistence layer with mock client.
func newMockDatastorePersist[K comparable, V any](t *testing.T) (*datastorePersist[K, V], func()) {
	client, cleanup := datastore.NewMockClient(t)

	return &datastorePersist[K, V]{
		client: client,
		kind:   "CacheEntry",
	}, cleanup
}

func TestDatastorePersist_Mock_StoreLoad(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	ctx := context.Background()

	// Store a value
	if err := dp.Store(ctx, "key1", 42, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Load the value
	val, expiry, found, err := dp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != 42 {
		t.Errorf("Load value = %d; want 42", val)
	}
	if !expiry.IsZero() {
		t.Error("expiry should be zero")
	}
}

func TestDatastorePersist_Mock_LoadMissing(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	ctx := context.Background()

	// Load non-existent key
	_, _, found, err := dp.Load(ctx, "missing")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("missing key should not be found")
	}
}

func TestDatastorePersist_Mock_TTL(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, string](t)
	defer cleanup()

	ctx := context.Background()

	// Store with past expiry
	past := time.Now().Add(-1 * time.Second)
	if err := dp.Store(ctx, "expired", "value", past); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Should not be loadable
	_, _, found, err := dp.Load(ctx, "expired")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("expired key should not be found")
	}
}

func TestDatastorePersist_Mock_Delete(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	ctx := context.Background()

	// Store and delete
	if err := dp.Store(ctx, "key1", 42, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if err := dp.Delete(ctx, "key1"); err != nil {
	}

	// Should not be loadable
	_, _, found, err := dp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("deleted key should not be found")
	}

	// Deleting non-existent key should not error
	if err := dp.Delete(ctx, "missing"); err != nil {
		t.Errorf("Delete missing key: %v", err)
	}
}

func TestDatastorePersist_Mock_Update(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, string](t)
	defer cleanup()

	ctx := context.Background()

	// Store initial value
	if err := dp.Store(ctx, "key", "value1", time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Update value
	if err := dp.Store(ctx, "key", "value2", time.Time{}); err != nil {
		t.Fatalf("Store update: %v", err)
	}

	// Load and verify updated value
	val, _, found, err := dp.Load(ctx, "key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key not found")
	}
	if val != "value2" {
		t.Errorf("Load value = %s; want value2", val)
	}
}

func TestDatastorePersist_Mock_ComplexValue(t *testing.T) {
	type User struct {
		Name  string
		Email string
		Age   int
	}

	dp, cleanup := newMockDatastorePersist[string, User](t)
	defer cleanup()

	ctx := context.Background()

	user := User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
	}

	// Store complex value
	if err := dp.Store(ctx, "user1", user, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Load and verify
	loaded, _, found, err := dp.Load(ctx, "user1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("user1 not found")
	}
	if loaded.Name != user.Name || loaded.Email != user.Email || loaded.Age != user.Age {
		t.Errorf("Load value = %+v; want %+v", loaded, user)
	}
}

func TestDatastorePersist_Mock_LoadAll(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple entries
	entries := map[string]int{
		"key1": 1,
		"key2": 2,
		"key3": 3,
	}
	for k, v := range entries {
		if err := dp.Store(ctx, k, v, time.Time{}); err != nil {
			t.Fatalf("Store %s: %v", k, err)
		}
	}

	// Store expired entry
	if err := dp.Store(ctx, "expired", 99, time.Now().Add(-1*time.Second)); err != nil {
		t.Fatalf("Store expired: %v", err)
	}

	// LoadAll - note: this doesn't return entries (by design, see comment in LoadAll)
	// but it should clean up expired entries
	entryCh, errCh := dp.LoadAll(ctx)

	// Consume channels
	for range entryCh {
		// Should be empty
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("LoadAll error: %v", err)
		}
	default:
	}

	// Verify non-expired entries still exist
	for k, v := range entries {
		val, _, found, err := dp.Load(ctx, k)
		if err != nil {
			t.Fatalf("Load %s: %v", k, err)
		}
		if !found {
			t.Errorf("%s not found after LoadAll", k)
		}
		if val != v {
			t.Errorf("Load %s = %d; want %d", k, val, v)
		}
	}
}

func TestDatastorePersist_Mock_LoadAllContextCancellation(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	baseCtx := context.Background()

	// Store many entries
	for i := 0; i < 100; i++ {
		if err := dp.Store(baseCtx, string(rune(i)), i, time.Time{}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// Cancel context during LoadAll
	ctx, cancel := context.WithCancel(context.Background())
	entryCh, errCh := dp.LoadAll(ctx)

	// Cancel immediately
	cancel()

	// Consume channels
	for range entryCh {
	}

	// Should get context cancellation error (may be wrapped)
	err := <-errCh
	if err == nil || (err != context.Canceled && !errors.Is(err, context.Canceled)) {
		t.Errorf("expected context.Canceled error; got %v", err)
	}
}

func TestDatastorePersist_Mock_Close(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	if err := dp.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestDatastorePersist_Mock_WithTTL(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, string](t)
	defer cleanup()

	ctx := context.Background()

	// Store with future expiry
	future := time.Now().Add(1 * time.Hour)
	if err := dp.Store(ctx, "key", "value", future); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Should be loadable
	val, expiry, found, err := dp.Load(ctx, "key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key not found")
	}
	if val != "value" {
		t.Errorf("Load value = %s; want value", val)
	}

	// Expiry should be set (within 1 second of expected)
	if expiry.IsZero() {
		t.Error("expiry should not be zero")
	}
	if expiry.Sub(future).Abs() > time.Second {
		t.Errorf("expiry = %v; want ~%v", expiry, future)
	}
}

// TestCache_WithDatastoreMock tests end-to-end cache with mock datastore.
func TestCache_WithDatastoreMock(t *testing.T) {
	ctx := context.Background()

	// Create mock datastore persistence
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	// Create cache with mock persistence
	cache := &Cache[string, int]{
		memory:  newS3FIFO[string, int](100),
		persist: dp,
		opts:    &Options{MemorySize: 100, DefaultTTL: 0},
	}
	defer cache.Close()

	// Test operations
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || val != 42 {
		t.Errorf("Get = %v, %v; want 42, true", val, found)
	}

	// Delete from memory to test persistence fallback
	cache.memory.delete("key1")

	// Should load from persistence
	val, found, err = cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get from persistence: %v", err)
	}
	if !found || val != 42 {
		t.Errorf("Get from persistence = %v, %v; want 42, true", val, found)
	}

	// Should now be in memory again
	if _, memFound := cache.memory.get("key1"); !memFound {
		t.Error("key1 should be promoted to memory after persistence load")
	}
}
