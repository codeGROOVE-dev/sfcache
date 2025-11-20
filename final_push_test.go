package bdcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFilePersist_Store_CloseError tests when temp file close fails.
// We create a file in a location that will cause close to behave differently.
func TestFilePersist_Store_CompleteFlow(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, string](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Test complete store flow with expiry
	expiry := time.Now().Add(1 * time.Hour)
	if err := fp.Store(ctx, "key1", "value1", expiry); err != nil {
		t.Fatalf("Store with expiry: %v", err)
	}

	// Load and verify expiry is set
	val, loadedExpiry, found, err := fp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != "value1" {
		t.Errorf("value = %s; want value1", val)
	}

	// Verify expiry was stored correctly (within 1 second)
	if loadedExpiry.Sub(expiry).Abs() > time.Second {
		t.Errorf("expiry = %v; want ~%v", loadedExpiry, expiry)
	}
}

// TestCache_New_BothPersistencePaths tests both file and datastore initialization paths.
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
	cache1.Close()

	// Test loading from persistence on-demand (no warmup)
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	// Should load from disk on-demand
	val, found, _ := cache2.Get(ctx, "key1")
	if !found || val != 42 {
		t.Errorf("Get after reload = %v, %v; want 42, true (should load on-demand from disk)", val, found)
	}
}

// TestCache_New_DatastorePath tests datastore initialization (will fail gracefully without credentials).
func TestCache_New_DatastorePath(t *testing.T) {
	ctx := context.Background()

	// This will fail to connect but exercises the datastore path in New()
	cache, err := New[string, int](ctx,
		WithCloudDatastore("test-datastore-project"),
		WithMemorySize(50),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Should work in memory even if datastore failed
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, found, _ := cache.Get(ctx, "key1")
	if !found || val != 42 {
		t.Errorf("Get = %v, %v; want 42, true", val, found)
	}
}

// TestCache_Warmup_ErrorHandling tests warmup error handling.
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
	cache1.Close()

	// Corrupt some cache files to trigger warmup errors
	fp, _ := newFilePersist[string, int](cacheID)
	defer fp.Close()

	// Walk directory tree to find .gob files (accounting for squid-style subdirs)
	var gobFiles []string
	filepath.Walk(fp.dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".gob" {
			gobFiles = append(gobFiles, path)
		}
		return nil
	})

	if len(gobFiles) > 0 {
		// Corrupt first file
		os.WriteFile(gobFiles[0], []byte("bad data"), 0644)
	}

	// Create new cache with warmup - should handle errors gracefully
	cache2, err := New[string, int](ctx, WithLocalStore(cacheID), WithWarmup(10))
	if err != nil {
		t.Fatalf("New cache2: %v", err)
	}
	defer cache2.Close()

	time.Sleep(150 * time.Millisecond) // Wait for warmup

	// Some entries should still load despite corruption
	// (exact count depends on which file was corrupted)
	if cache2.Len() == 0 {
		t.Error("at least some entries should have loaded from warmup")
	}
}

// TestDatastorePersist_Mock_StoreWithExpiry tests Store path with expiry set.
func TestDatastorePersist_Mock_StoreWithExpiry(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, string](t)
	defer cleanup()

	ctx := context.Background()

	expiry := time.Now().Add(2 * time.Hour)
	if err := dp.Store(ctx, "key1", "value1", expiry); err != nil {
		t.Fatalf("Store with expiry: %v", err)
	}

	val, loadedExpiry, found, err := dp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != "value1" {
		t.Errorf("value = %s; want value1", val)
	}

	// Verify expiry was stored
	if loadedExpiry.Sub(expiry).Abs() > time.Second {
		t.Errorf("expiry = %v; want ~%v", loadedExpiry, expiry)
	}
}
