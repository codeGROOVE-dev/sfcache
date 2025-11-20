package bdcache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilePersist_StoreLoad(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()

	// Override directory to use temp dir
	fp.dir = dir

	ctx := context.Background()

	// Store a value
	if err := fp.Store(ctx, "key1", 42, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Load the value
	val, expiry, found, err := fp.Load(ctx, "key1")
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

func TestFilePersist_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Load non-existent key
	_, _, found, err := fp.Load(ctx, "missing")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("missing key should not be found")
	}
}

func TestFilePersist_TTL(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, string](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store with past expiry
	past := time.Now().Add(-1 * time.Second)
	if err := fp.Store(ctx, "expired", "value", past); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Should not be loadable
	_, _, found, err := fp.Load(ctx, "expired")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("expired key should not be found")
	}

	// File should be removed (subdirectory may remain, but should be empty or only have empty subdirs)
	filename := filepath.Join(dir, fp.keyToFilename("expired"))
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Error("expired file should be removed")
	}
}

func TestFilePersist_Delete(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store and delete
	if err := fp.Store(ctx, "key1", 42, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if err := fp.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Should not be loadable
	_, _, found, err := fp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("deleted key should not be found")
	}

	// Deleting non-existent key should not error
	if err := fp.Delete(ctx, "missing"); err != nil {
		t.Errorf("Delete missing key: %v", err)
	}
}

func TestFilePersist_LoadAll(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store multiple entries
	entries := map[string]int{
		"key1": 1,
		"key2": 2,
		"key3": 3,
	}
	for k, v := range entries {
		if err := fp.Store(ctx, k, v, time.Time{}); err != nil {
			t.Fatalf("Store %s: %v", k, err)
		}
	}

	// Store expired entry
	if err := fp.Store(ctx, "expired", 99, time.Now().Add(-1*time.Second)); err != nil {
		t.Fatalf("Store expired: %v", err)
	}

	// Load all
	entryCh, errCh := fp.LoadAll(ctx)

	loaded := make(map[string]int)
	for entry := range entryCh {
		loaded[entry.Key] = entry.Value
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("LoadAll error: %v", err)
		}
	default:
	}

	// Verify all non-expired entries loaded
	if len(loaded) != len(entries) {
		t.Errorf("loaded %d entries; want %d", len(loaded), len(entries))
	}

	for k, v := range entries {
		if loaded[k] != v {
			t.Errorf("loaded[%s] = %d; want %d", k, loaded[k], v)
		}
	}

	// Expired entry should not be loaded
	if _, ok := loaded["expired"]; ok {
		t.Error("expired entry should not be loaded")
	}
}

func TestFilePersist_Update(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, string](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store initial value
	if err := fp.Store(ctx, "key", "value1", time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Update value
	if err := fp.Store(ctx, "key", "value2", time.Time{}); err != nil {
		t.Fatalf("Store update: %v", err)
	}

	// Load and verify updated value
	val, _, found, err := fp.Load(ctx, "key")
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

func TestFilePersist_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	// Store many entries with valid alphanumeric keys
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		if err := fp.Store(ctx, key, i, time.Time{}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// Cancel context during LoadAll
	ctx, cancel := context.WithCancel(context.Background())
	entryCh, errCh := fp.LoadAll(ctx)

	// Read a few entries, then cancel
	count := 0
	cancel()

	for range entryCh {
		count++
	}

	// Should get context cancellation error
	err = <-errCh
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error; got %v", err)
	}
}
