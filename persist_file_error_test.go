package bdcache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFilePersist_CorruptedFile tests handling of corrupted cache files.
func TestFilePersist_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Create a corrupted file
	filename := filepath.Join(dir, fp.keyToFilename("badkey"))
	// Create subdirectory first (for squid-style layout)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filename, []byte("corrupted data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Load should return not found (file gets deleted)
	_, _, found, err := fp.Load(ctx, "badkey")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("corrupted file should not be found")
	}

	// File should be removed
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Error("corrupted file should be deleted")
	}
}

// TestFilePersist_StoreTempFileError tests error handling during store.
func TestFilePersist_StoreTempFileError(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()

	// Make directory read-only to trigger write error
	oldMode := os.FileMode(0755)
	if err := os.Chmod(dir, 0444); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(dir, oldMode)

	fp.dir = dir

	ctx := context.Background()

	// Store should fail
	err = fp.Store(ctx, "key", 42, time.Time{})
	if err == nil {
		t.Error("Store should fail with read-only directory")
	}
}

// TestFilePersist_LoadAllWithCorruptedFiles tests LoadAll with mixed good/bad files.
func TestFilePersist_LoadAllWithCorruptedFiles(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store good entries
	fp.Store(ctx, "good1", 1, time.Time{})
	fp.Store(ctx, "good2", 2, time.Time{})

	// Create corrupted file
	corruptFile := filepath.Join(dir, "corrupt.gob")
	os.WriteFile(corruptFile, []byte("bad data"), 0644)

	// Create non-gob file (should be skipped)
	txtFile := filepath.Join(dir, "readme.txt")
	os.WriteFile(txtFile, []byte("ignore me"), 0644)

	// LoadAll should skip corrupted and non-gob files
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

	// Should only load good entries
	if len(loaded) != 2 {
		t.Errorf("loaded %d entries; want 2", len(loaded))
	}

	if loaded["good1"] != 1 || loaded["good2"] != 2 {
		t.Errorf("loaded = %v; want good1=1, good2=2", loaded)
	}
}

// TestFilePersist_NewWithInvalidPath tests newFilePersist with invalid path.
func TestFilePersist_NewWithInvalidPath(t *testing.T) {
	// Try to create in a path with null bytes (invalid on all OS)
	_, err := newFilePersist[string, int]("invalid\x00path")
	if err == nil {
		t.Error("newFilePersist should fail with invalid path")
	}
}

// TestFilePersist_DeleteNonExistentKey tests deleting a key that doesn't exist.
func TestFilePersist_DeleteNonExistentKey(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, int](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Delete should succeed even if key doesn't exist
	if err := fp.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete nonexistent: %v", err)
	}
}

// TestFilePersist_ExpiredCleanupDuringLoad tests expired file removal during Load.
func TestFilePersist_ExpiredCleanupDuringLoad(t *testing.T) {
	dir := t.TempDir()
	fp, err := newFilePersist[string, string](filepath.Base(dir))
	if err != nil {
		t.Fatalf("newFilePersist: %v", err)
	}
	defer fp.Close()
	fp.dir = dir

	ctx := context.Background()

	// Store with past expiry
	past := time.Now().Add(-1 * time.Hour)
	filename := fp.keyToFilename("expired")
	if err := fp.Store(ctx, "expired", "value", past); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify file exists
	fullPath := filepath.Join(dir, filename)
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatal("expired file should exist before Load")
	}

	// Load should detect expiry and remove file
	_, _, found, err := fp.Load(ctx, "expired")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("expired key should not be found")
	}

	// File should be removed
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Error("expired file should be deleted after Load")
	}
}
