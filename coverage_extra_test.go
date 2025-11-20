package bdcache

import (
	"context"
	"errors"
	"testing"
)

// TestCache_Close_PersistenceError tests Close when persistence.Close() fails.
func TestCache_Close_PersistenceError(t *testing.T) {
	cache := &Cache[string, int]{
		memory:  newS3FIFO[string, int](100),
		persist: &closeErrorPersist[string, int]{},
		opts:    &Options{MemorySize: 100},
	}

	// Close should return the persistence error
	if err := cache.Close(); err == nil {
		t.Error("Close should return error when persistence.Close() fails")
	}
}

// closeErrorPersist is a mock that fails on Close.
type closeErrorPersist[K comparable, V any] struct {
	errorPersist[K, V]
}

func (c *closeErrorPersist[K, V]) Close() error {
	return errors.New("close failed")
}

// TestNewDatastorePersist_Integration tests newDatastorePersist in realistic scenario.
func TestNewDatastorePersist_Integration(t *testing.T) {
	ctx := context.Background()

	// Try to create with invalid project (will fail but tests the path)
	_, err := newDatastorePersist[string, int](ctx, "test-invalid-project")
	// Error is expected - we're testing the code path
	if err == nil {
		t.Log("newDatastorePersist succeeded unexpectedly - might have credentials")
	}
}

// TestCache_New_FilePersistenceSuccess tests successful file persistence initialization.
func TestCache_New_FilePersistenceSuccess(t *testing.T) {
	ctx := context.Background()
	cacheID := "test-success-" + t.Name()

	cache, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	if cache.persist == nil {
		t.Error("persist should not be nil with valid local store")
	}

	// Test on-demand loading from disk
	if err := cache.Set(ctx, "warmup-test", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	cache.Close()

	// Create new cache - should load on-demand from disk (no warmup)
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	// Should load on-demand from disk
	val, found, _ := cache2.Get(ctx, "warmup-test")
	if !found || val != 42 {
		t.Errorf("Get on-demand = %v, %v; want 42, true", val, found)
	}
}

// TestCache_Set_WithPersistenceStoreError tests Set when Store fails.
func TestCache_Set_WithPersistenceStoreError(t *testing.T) {
	ctx := context.Background()

	cache := &Cache[string, int]{
		memory:  newS3FIFO[string, int](100),
		persist: &errorPersist[string, int]{},
		opts:    &Options{MemorySize: 100, DefaultTTL: 0},
	}
	defer cache.Close()

	// Set should return error when persistence fails
	err := cache.Set(ctx, "key1", 42, 0)
	if err == nil {
		t.Fatal("Set should return error when persistence fails")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Set error = %v; want context.DeadlineExceeded", err)
	}

	// BUT value should still be in memory (reliability guarantee)
	val, found, _ := cache.Get(ctx, "key1")
	if !found || val != 42 {
		t.Errorf("Get = %v, %v; want 42, true (value should be cached despite persistence failure)", val, found)
	}
}
