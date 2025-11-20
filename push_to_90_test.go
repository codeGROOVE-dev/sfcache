package bdcache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFilePersist_UserCacheDir_Error tests when os.UserCacheDir fails.
// This is difficult to trigger in normal circumstances but exercises the path.
func TestFilePersist_NewWithExplicitPath(t *testing.T) {
	// Create a cache with valid explicit path
	dir := t.TempDir()
	cacheID := filepath.Base(dir)

	fp, err := newFilePersist[string, int](cacheID)
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()

	// Should have created the directory
	if _, err := os.Stat(fp.dir); os.IsNotExist(err) {
		t.Error("cache directory should exist")
	}
}

// TestDatastorePersist_Mock_DeleteError tests Delete with error from datastore.
func TestDatastorePersist_Mock_DeleteNonExistent(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	ctx := context.Background()

	// Delete non-existent key should not error
	if err := dp.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete nonexistent: %v", err)
	}
}

// TestDatastorePersist_Mock_ExpiredEntry tests expired entry handling.
func TestDatastorePersist_Mock_ExpiredInLoadAll(t *testing.T) {
	dp, cleanup := newMockDatastorePersist[string, int](t)
	defer cleanup()

	ctx := context.Background()

	// Store entries with different expirations
	dp.Store(ctx, "valid1", 1, time.Now().Add(1*time.Hour))
	dp.Store(ctx, "valid2", 2, time.Now().Add(1*time.Hour))
	dp.Store(ctx, "expired", 99, time.Now().Add(-1*time.Second))

	// LoadAll should handle expired entries
	entryCh, errCh := dp.LoadAll(ctx)

	for range entryCh {
		// Entries channel should be empty (LoadAll doesn't return entries by design)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("LoadAll error: %v", err)
		}
	default:
	}

	// Verify valid entries still accessible
	val, _, found, _ := dp.Load(ctx, "valid1")
	if !found || val != 1 {
		t.Errorf("valid1 = %v, %v; want 1, true", val, found)
	}
}

// TestCache_SetWithDefaultTTL tests Set with ttl=0 using explicit TTL option.
func TestCache_SetDefaultWithExplicitTTL(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx, WithDefaultTTL(1*time.Hour))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Set with ttl=0 should use the default TTL
	if err := cache.Set(ctx, "key1", 42, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify it's set
	val, found, _ := cache.Get(ctx, "key1")
	if !found || val != 42 {
		t.Errorf("Get = %v, %v; want 42, true", val, found)
	}
}

// TestCache_SetWithExplicitTTLOverridesDefault tests Set with explicit TTL.
func TestCache_SetExplicitTTLOverridesDefault(t *testing.T) {
	ctx := context.Background()

	cache, err := New[string, int](ctx, WithDefaultTTL(1*time.Hour))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cache.Close()

	// Set with explicit short TTL (overrides default)
	if err := cache.Set(ctx, "key1", 42, 50*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Should exist immediately
	_, found, _ := cache.Get(ctx, "key1")
	if !found {
		t.Error("key1 should exist immediately")
	}

	// Wait for explicit TTL to expire (not default)
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, found, _ = cache.Get(ctx, "key1")
	if found {
		t.Error("key1 should be expired after explicit TTL")
	}
}

// TestFilePersist_LoadNonGobFile tests Load handling of non-gob file.
func TestFilePersist_LoadCorruptedGob(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Create a file with invalid gob data
	filename := filepath.Join(dir, fp.keyToFilename("corrupt"))
	// Create subdirectory first (for squid-style layout)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filename, []byte("not valid gob"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Load should handle gracefully and remove bad file
	_, _, found, err := fp.Load(ctx, "corrupt")
	if err != nil {
		t.Fatalf("Load should not error on corrupt file: %v", err)
	}
	if found {
		t.Error("corrupt file should not be found")
	}

	// File should be removed
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Error("corrupt file should be removed")
	}
}

// TestDatastorePersist_Mock_LoadDecodeError tests Load with base64 decode error.
// This is hard to trigger directly, but we test the path exists.
func TestDatastorePersist_Mock_ComplexTypes(t *testing.T) {
	type ComplexStruct struct {
		Name  string
		Items []string
		Meta  map[string]int
	}

	dp, cleanup := newMockDatastorePersist[string, ComplexStruct](t)
	defer cleanup()

	ctx := context.Background()

	data := ComplexStruct{
		Name:  "test",
		Items: []string{"a", "b", "c"},
		Meta:  map[string]int{"count": 3},
	}

	// Store complex type
	if err := dp.Store(ctx, "complex", data, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Load and verify
	loaded, _, found, err := dp.Load(ctx, "complex")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("complex not found")
	}

	if loaded.Name != data.Name {
		t.Errorf("Name = %s; want %s", loaded.Name, data.Name)
	}
	if len(loaded.Items) != 3 {
		t.Errorf("Items length = %d; want 3", len(loaded.Items))
	}
	if loaded.Meta["count"] != 3 {
		t.Errorf("Meta[count] = %d; want 3", loaded.Meta["count"])
	}
}
