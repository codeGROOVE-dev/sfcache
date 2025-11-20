package bdcache

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestOptions_WithCloudDatastore tests the WithCloudDatastore option.
func TestOptions_WithCloudDatastore(t *testing.T) {
	opts := defaultOptions()
	WithCloudDatastore("test-project")(opts)

	if opts.CacheID != "test-project" {
		t.Errorf("CacheID = %s; want test-project", opts.CacheID)
	}
	if !opts.UseDatastore {
		t.Error("UseDatastore should be true")
	}
}

// TestOptions_WithBestStore tests WithBestStore with K_SERVICE.
func TestOptions_WithBestStore_WithKService(t *testing.T) {
	// Set K_SERVICE environment variable
	os.Setenv("K_SERVICE", "test-service")
	defer os.Unsetenv("K_SERVICE")

	opts := defaultOptions()
	WithBestStore("test-cache")(opts)

	if !opts.UseDatastore {
		t.Error("UseDatastore should be true when K_SERVICE is set")
	}
}

// TestOptions_WithBestStore_WithoutKService tests WithBestStore without K_SERVICE.
func TestOptions_WithBestStore_WithoutKService(t *testing.T) {
	// Ensure K_SERVICE is not set
	os.Unsetenv("K_SERVICE")

	opts := defaultOptions()
	WithBestStore("test-cache")(opts)

	if opts.UseDatastore {
		t.Error("UseDatastore should be false when K_SERVICE is not set")
	}
}

// TestCache_New_WithDatastoreOption tests New with datastore option.
func TestCache_New_WithDatastoreOption(t *testing.T) {
	ctx := context.Background()

	// This will fail to connect but should gracefully degrade
	cache, err := New[string, int](ctx, WithCloudDatastore("invalid-project"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Should work in memory-only mode after persistence fails
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
}

// TestCache_Close_WithNilPersist tests Close with nil persistence.
func TestCache_Close_WithNilPersist(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Close with nil persistence should not error
	if err := cache.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestCache_Get_PersistenceError tests Get when persistence returns error.
func TestCache_Get_PersistenceError(t *testing.T) {
	ctx := context.Background()

	// Create cache with mock that returns errors
	cache := &Cache[string, int]{
		memory:  newS3FIFO[string, int](100),
		persist: &errorPersist[string, int]{},
		opts:    &Options{MemorySize: 100},
	}
	defer cache.Close()

	// Get should handle persistence error gracefully
	_, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get should not return error on persistence failure: %v", err)
	}
	if found {
		t.Error("key1 should not be found")
	}
}

// TestCache_Delete_PersistenceError tests Delete when persistence fails.
func TestCache_Delete_PersistenceError(t *testing.T) {
	ctx := context.Background()

	cache := &Cache[string, int]{
		memory:  newS3FIFO[string, int](100),
		persist: &errorPersist[string, int]{},
		opts:    &Options{MemorySize: 100},
	}
	defer cache.Close()

	// Set directly in memory (bypass persistence which would fail)
	cache.memory.set("key1", 42, time.Time{})

	// Delete should handle persistence error gracefully
	cache.Delete(ctx, "key1")

	// Should be deleted from memory
	_, found, _ := cache.Get(ctx, "key1")
	if found {
		t.Error("key1 should be deleted from memory")
	}
}

// errorPersist is a mock persistence layer that always returns errors.
type errorPersist[K comparable, V any] struct{}

func (e *errorPersist[K, V]) ValidateKey(key K) error {
	return nil // Allow all keys for test
}

func (e *errorPersist[K, V]) Load(ctx context.Context, key K) (V, time.Time, bool, error) {
	var zero V
	return zero, time.Time{}, false, context.DeadlineExceeded
}

func (e *errorPersist[K, V]) Store(ctx context.Context, key K, value V, expiry time.Time) error {
	return context.DeadlineExceeded
}

func (e *errorPersist[K, V]) Delete(ctx context.Context, key K) error {
	return context.DeadlineExceeded
}

func (e *errorPersist[K, V]) LoadRecent(ctx context.Context, limit int) (<-chan Entry[K, V], <-chan error) {
	entryCh := make(chan Entry[K, V])
	errCh := make(chan error, 1)
	close(entryCh)
	errCh <- context.DeadlineExceeded
	return entryCh, errCh
}

func (e *errorPersist[K, V]) LoadAll(ctx context.Context) (<-chan Entry[K, V], <-chan error) {
	return e.LoadRecent(ctx, 0)
}

func (e *errorPersist[K, V]) Close() error {
	return nil
}
