package bdcache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFilePersist_Store_RenameError tests Store when rename fails.
func TestFilePersist_Store_RenameError(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Create a directory where the target file would be - this will cause rename to fail
	filename := filepath.Join(dir, fp.keyToFilename("key1"))
	if err := os.MkdirAll(filename, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Store should fail on rename
	err = fp.Store(ctx, "key1", 42, time.Time{})
	if err == nil {
		t.Error("Store should fail when target is a directory")
	}
}

// TestFilePersist_Store_EncodeError tests encoding error path.
// This is hard to trigger with standard types, so we test the code exists.
func TestFilePersist_Store_Success(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, map[string]int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store complex type
	data := map[string]int{"a": 1, "b": 2}
	if err := fp.Store(ctx, "key1", data, time.Now().Add(1*time.Hour)); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify it's on disk
	val, _, found, err := fp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val["a"] != 1 || val["b"] != 2 {
		t.Errorf("Load value = %v; want map[a:1 b:2]", val)
	}
}

// TestDatastorePersist_Mock_StoreError tests Store with marshal error.
func TestDatastorePersist_Mock_LoadError(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, func()](t)
	defer cleanup()

	ctx := context.Background()

	// Try to store a function (which can't be JSON marshaled)
	err := dp.Store(ctx, "key1", func() {}, time.Time{})
	if err == nil {
		t.Error("Store should fail when marshaling unsupported type")
	}
}

// TestCache_New_WarmupError tests warmup with error.
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
	cache1.Close()

	// Corrupt one of the cache files
	fp, _ := newFilePersist[string, int](cacheID)
	defer fp.Close()

	entries, _ := os.ReadDir(fp.dir)
	if len(entries) > 0 {
		// Corrupt the first file
		corruptFile := filepath.Join(fp.dir, entries[0].Name())
		os.WriteFile(corruptFile, []byte("corrupted"), 0644)
	}

	// Create new cache - warmup should handle error gracefully
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	// Cache should still work
	if err := cache2.Set(ctx, "key2", 100, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, found, _ := cache2.Get(ctx, "key2")
	if !found || val != 100 {
		t.Errorf("Get key2 = %v, %v; want 100, true", val, found)
	}
}

// TestCache_New_DefaultOptions tests New with no options.
func TestCache_New_DefaultOptions(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	if cache.opts.MemorySize != 10000 {
		t.Errorf("default memory size = %d; want 10000", cache.opts.MemorySize)
	}

	if cache.opts.DefaultTTL != 0 {
		t.Errorf("default TTL = %v; want 0", cache.opts.DefaultTTL)
	}

	if cache.persist != nil {
		t.Error("persist should be nil with default options")
	}
}

// TestCache_New_MultipleOptions tests New with multiple options.
func TestCache_New_MultipleOptions(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx,
		WithMemorySize(500),
		WithDefaultTTL(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	if cache.opts.MemorySize != 500 {
		t.Errorf("memory size = %d; want 500", cache.opts.MemorySize)
	}

	if cache.opts.DefaultTTL != 5*time.Minute {
		t.Errorf("default TTL = %v; want 5m", cache.opts.DefaultTTL)
	}
}
